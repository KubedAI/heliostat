package domain

import "strings"

const (
	labelInstanceType      = "node.kubernetes.io/instance-type"
	labelKarpenterFamily   = "karpenter.k8s.aws/instance-family"
	labelKarpenterCategory = "karpenter.k8s.aws/instance-category"
)

var (
	gpuResources = []string{"nvidia.com/gpu", "amd.com/gpu", "aws.amazon.com/neuron"}
	// gpuModelLabels name the accelerator model: Karpenter on EKS, then NVIDIA GPU Feature Discovery.
	gpuModelLabels = []string{"karpenter.k8s.aws/instance-gpu-name", "nvidia.com/gpu.product"}
)

func requiredAffinityValues(podSpec Obj, key string) []string {
	var values []string
	terms := List(Dig(podSpec, "affinity", "nodeAffinity", "requiredDuringSchedulingIgnoredDuringExecution")["nodeSelectorTerms"])
	for _, term := range terms {
		for _, expr := range List(term["matchExpressions"]) {
			if Str(expr["key"]) == key && Str(expr["operator"]) == "In" {
				values = append(values, Strings(expr["values"])...)
			}
		}
	}
	return values
}

// DeclaredInstanceTypes returns the instance types a pod spec asks for, most specific first: an
// exact nodeSelector, then required node-affinity In values, then Karpenter family or category.
func DeclaredInstanceTypes(podSpec Obj) []string {
	selector := Dict(podSpec["nodeSelector"])
	if exact := Str(selector[labelInstanceType]); exact != "" {
		return []string{exact}
	}
	if types := requiredAffinityValues(podSpec, labelInstanceType); len(types) > 0 {
		return types
	}
	if family := Str(selector[labelKarpenterFamily]); family != "" {
		return []string{family + ".*"}
	}
	if families := requiredAffinityValues(podSpec, labelKarpenterFamily); len(families) > 0 {
		out := make([]string, len(families))
		for i, f := range families {
			out[i] = f + ".*"
		}
		return out
	}
	if category := Str(selector[labelKarpenterCategory]); category != "" {
		return []string{category + "*"}
	}
	return []string{}
}

// DeclaredGPUModel returns the accelerator model a pod spec pins, if any, for example "L40S".
func DeclaredGPUModel(podSpec Obj) string {
	selector := Dict(podSpec["nodeSelector"])
	for _, key := range gpuModelLabels {
		value := Str(selector[key])
		if value == "" {
			value = strings.Join(requiredAffinityValues(podSpec, key), " / ")
		}
		if value != "" {
			return strings.ToUpper(value)
		}
	}
	return ""
}

func podGPUs(podSpec Obj) int {
	total := 0.0
	for _, container := range List(podSpec["containers"]) {
		resources := Dict(container["resources"])
		limits, requests := Dict(resources["limits"]), Dict(resources["requests"])
		for _, key := range gpuResources {
			if n := Number(limits[key]); n != 0 {
				total += n
			} else {
				total += Number(requests[key])
			}
		}
	}
	return int(total)
}

// ComputeFor summarizes a RayClusterSpec: instance types requested by the workers (or the head for
// head-only clusters), GPUs for the desired replica count across every group, and the GPU model.
func ComputeFor(clusterSpec Obj) ComputeIntent {
	headPod := Dig(clusterSpec, "headGroupSpec", "template", "spec")
	gpus := podGPUs(headPod)
	gpuModel := ""
	if gpus > 0 {
		gpuModel = DeclaredGPUModel(headPod)
	}

	seen := map[string]bool{}
	workerTypes := []string{}
	for _, group := range List(clusterSpec["workerGroupSpecs"]) {
		podSpec := Dig(group, "template", "spec")
		for _, t := range DeclaredInstanceTypes(podSpec) {
			if !seen[t] {
				seen[t] = true
				workerTypes = append(workerTypes, t)
			}
		}
		replicas := group["replicas"]
		if replicas == nil {
			replicas = group["minReplicas"]
		}
		hosts := max(1, int(Number(group["numOfHosts"])))
		perPod := podGPUs(podSpec)
		gpus += perPod * int(Number(replicas)) * hosts
		if perPod > 0 && gpuModel == "" {
			gpuModel = DeclaredGPUModel(podSpec)
		}
	}

	instanceTypes := workerTypes
	if len(instanceTypes) == 0 {
		instanceTypes = DeclaredInstanceTypes(headPod)
	}
	return ComputeIntent{InstanceTypes: instanceTypes, GPUs: gpus, GPUModel: gpuModel}
}
