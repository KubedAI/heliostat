package domain

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"
)

func fixture(t *testing.T, name string) any {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", "testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	var v any
	if err := json.Unmarshal(data, &v); err != nil {
		t.Fatal(err)
	}
	return v
}

func TestRayJobResourceStatus(t *testing.T) {
	cases := []struct {
		deployment, job string
		want            JobStatus
	}{
		{"Complete", "SUCCEEDED", StatusSucceeded},
		{"Complete", "FAILED", StatusFailed},
		{"Complete", "STOPPED", StatusStopped},
		{"Complete", "", StatusSucceeded},
		// activeDeadlineSeconds exceeded or submitter failure: Ray still says RUNNING.
		{"Failed", "RUNNING", StatusFailed},
		{"Failed", "", StatusFailed},
		{"ValidationFailed", "", StatusFailed},
		{"Failed", "STOPPED", StatusStopped},
		{"Suspended", "RUNNING", StatusSuspended},
		{"Suspending", "", StatusSuspended},
		{"New", "", StatusPending},
		{"Initializing", "", StatusPending},
		{"Waiting", "", StatusPending},
		{"Retrying", "FAILED", StatusPending},
		{"", "", StatusPending},
		{"Running", "RUNNING", StatusRunning},
		{"Running", "PENDING", StatusPending},
		{"Running", "", StatusPending},
		{"Running", "SUCCEEDED", StatusSucceeded},
		{"SomethingNew", "RUNNING", StatusRunning},
		{"SomethingNew", "", StatusUnknown},
	}
	for _, c := range cases {
		if got := RayJobResourceStatus(c.deployment, c.job); got != c.want {
			t.Errorf("%q + %q = %s, want %s", c.deployment, c.job, got, c.want)
		}
	}
	if RayJobStatus("succeeded") != StatusSucceeded || RayJobStatus("bogus") != StatusUnknown {
		t.Error("RayJobStatus mapping")
	}
}

func TestEndpointHealth(t *testing.T) {
	cond := func(typ, status, reason string) Condition {
		return Condition{Type: typ, Status: status, Reason: reason}
	}
	check := func(name string, conditions []Condition, apps []string, want EndpointStatus) {
		t.Helper()
		if got, _ := EndpointHealth(conditions, apps); got != want {
			t.Errorf("%s: got %s, want %s", name, got, want)
		}
	}
	check("upgrade wins", []Condition{cond("Ready", "True", ""), cond("UpgradeInProgress", "True", "Pending")}, nil, EndpointUpgrading)
	check("ready", []Condition{cond("Ready", "True", "NonZeroServeEndpoints")}, []string{"RUNNING"}, EndpointReady)
	check("unhealthy", []Condition{cond("Ready", "False", "")}, []string{"DEPLOY_FAILED"}, EndpointUnhealthy)
	check("deploying", []Condition{cond("Ready", "False", "Initializing")}, []string{"DEPLOYING"}, EndpointDeploying)
	check("legacy ready", nil, []string{"RUNNING", "RUNNING"}, EndpointReady)
	check("legacy deploying", nil, []string{"RUNNING", "DEPLOYING"}, EndpointDeploying)
	check("unknown", nil, nil, EndpointUnknown)
}

func pod(spec Obj, gpus int) Obj {
	resources := Obj{}
	if gpus > 0 {
		resources = Obj{"limits": Obj{"nvidia.com/gpu": json.Number(strconv.Itoa(gpus))}}
	}
	spec["containers"] = []any{Obj{"name": "ray", "resources": resources}}
	return Obj{"template": Obj{"spec": spec}}
}

func TestComputeFor(t *testing.T) {
	group := func(replicas any, hosts int, selector Obj, gpus int) Obj {
		g := pod(Obj{"nodeSelector": selector}, gpus)
		g["replicas"] = replicas
		if hosts > 0 {
			g["numOfHosts"] = float64(hosts)
		}
		return g
	}
	got := ComputeFor(Obj{
		"headGroupSpec": pod(Obj{}, 0),
		"workerGroupSpecs": []any{
			group(float64(2), 0, Obj{"node.kubernetes.io/instance-type": "g6e.12xlarge", "karpenter.k8s.aws/instance-gpu-name": "l40s"}, 4),
			group(float64(1), 2, Obj{"node.kubernetes.io/instance-type": "p5.48xlarge"}, 8),
		},
	})
	want := ComputeIntent{InstanceTypes: []string{"g6e.12xlarge", "p5.48xlarge"}, GPUs: 2*4 + 1*2*8, GPUModel: "L40S"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %+v, want %+v", got, want)
	}

	headOnly := ComputeFor(Obj{"headGroupSpec": pod(Obj{"nodeSelector": Obj{"node.kubernetes.io/instance-type": "m7i.large"}}, 0)})
	if !reflect.DeepEqual(headOnly, ComputeIntent{InstanceTypes: []string{"m7i.large"}}) {
		t.Errorf("head only: %+v", headOnly)
	}

	neuron := ComputeFor(Obj{"workerGroupSpecs": []any{Obj{"replicas": float64(1), "template": Obj{"spec": Obj{
		"containers": []any{Obj{"resources": Obj{"limits": Obj{"aws.amazon.com/neuron": "2"}}}},
	}}}}})
	if neuron.GPUs != 2 {
		t.Errorf("neuron devices: %d", neuron.GPUs)
	}
}

func TestDeclaredInstanceTypesAndModel(t *testing.T) {
	affinity := func(key string, values ...string) Obj {
		vs := make([]any, len(values))
		for i, v := range values {
			vs[i] = v
		}
		return Obj{"affinity": Obj{"nodeAffinity": Obj{"requiredDuringSchedulingIgnoredDuringExecution": Obj{
			"nodeSelectorTerms": []any{Obj{"matchExpressions": []any{Obj{"key": key, "operator": "In", "values": vs}}}},
		}}}}
	}
	if got := DeclaredInstanceTypes(affinity("node.kubernetes.io/instance-type", "g6e.xlarge", "g6e.2xlarge")); !reflect.DeepEqual(got, []string{"g6e.xlarge", "g6e.2xlarge"}) {
		t.Errorf("affinity types: %v", got)
	}
	if got := DeclaredInstanceTypes(Obj{"nodeSelector": Obj{"karpenter.k8s.aws/instance-family": "g6e"}}); !reflect.DeepEqual(got, []string{"g6e.*"}) {
		t.Errorf("family: %v", got)
	}
	if got := DeclaredInstanceTypes(Obj{"nodeSelector": Obj{"kubernetes.io/arch": "amd64"}}); len(got) != 0 {
		t.Errorf("unconstrained: %v", got)
	}
	if got := DeclaredGPUModel(affinity("nvidia.com/gpu.product", "NVIDIA-A10G")); got != "NVIDIA-A10G" {
		t.Errorf("gpu model: %q", got)
	}
}

func TestNormalizeRayJobFixtures(t *testing.T) {
	// These fixtures are real KubeRay 1.7.1 objects that predate the rename and carry legacy
	// heliostat.io/* labels, which must still be read.
	job, ok := NormalizeRayJob(Dict(fixture(t, "rayjob-succeeded.json")), "ray-on-eks")
	if !ok {
		t.Fatal("not normalized")
	}
	want := map[string]string{
		"name": "heliostat-lifecycle-short", "rayClusterName": "heliostat-lifecycle-short-c6vcz",
		"submissionId": "heliostat-lifecycle-short-flpsb", "phase": "Complete · SUCCEEDED",
		"model": "gemma-4-12b-it", "owner": "platform-validation", "workloadType": "native-batch",
		"startedAt": "2026-09-25T20:19:15Z", "finishedAt": "2026-09-25T20:21:48Z",
	}
	got := map[string]string{
		"name": job.Name, "rayClusterName": job.RayClusterName, "submissionId": job.SubmissionID,
		"phase": job.Phase, "model": job.Model, "owner": job.Owner, "workloadType": job.WorkloadType,
		"startedAt": job.StartedAt, "finishedAt": job.FinishedAt,
	}
	if !reflect.DeepEqual(got, want) || job.Status != StatusSucceeded || job.Kind != KindRayJob || !job.Present {
		t.Errorf("got %+v", job)
	}
	if job.Reported == nil || *job.Reported.TotalTokens != 14800 || *job.Reported.EstimatedCostUSD != 0.019 {
		t.Errorf("reported usage: %+v", job.Reported)
	}

	failed, _ := NormalizeRayJob(Dict(fixture(t, "rayjob-failed.json")), "c")
	if failed.Status != StatusFailed || failed.Reason != "AppFailed" || !strings.Contains(failed.Message, "controlled validation failure") {
		t.Errorf("failed job: %+v", failed)
	}

	if _, ok := NormalizeRayJob(Obj{"metadata": Obj{"name": "x"}}, "c"); ok {
		t.Error("object without uid must be rejected")
	}
	inferred, _ := NormalizeRayJob(Obj{
		"metadata": Obj{"name": "j", "uid": "u", "namespace": "n", "creationTimestamp": "2026-01-01T00:00:00Z"},
		"spec":     Obj{"entrypoint": "python run.py --model Qwen/Qwen2.5-7B-Instruct"},
	}, "c")
	if inferred.Model != "Qwen/Qwen2.5-7B-Instruct" || inferred.Status != StatusPending || inferred.Reported != nil {
		t.Errorf("inferred: %+v", inferred)
	}
}

func TestNormalizeRayCluster(t *testing.T) {
	rc, _ := NormalizeRayCluster(Dict(fixture(t, "raycluster-rayjob-owned.json")), "ray-on-eks")
	if rc.Name != "heliostat-lifecycle-short-c6vcz" || rc.State != "ready" || rc.Owner == nil ||
		rc.Owner.Kind != "RayJob" || rc.HeadServiceName != "heliostat-lifecycle-short-c6vcz-head-svc" ||
		rc.DashboardPort != 8265 || rc.RayVersion != "2.56.0" || rc.Desired.CPU != "250m" || rc.HistoryEnabled {
		t.Errorf("got %+v", rc)
	}
}

func TestNormalizeRayAPIJob(t *testing.T) {
	ctx := RayClusterContext{Cluster: "c", Namespace: "ns", RayClusterName: "rc", RayClusterUID: "rc-uid",
		Compute: ComputeIntent{InstanceTypes: []string{"g6e.xlarge"}, GPUs: 1, GPUModel: "L40S"}}
	entries := fixture(t, "ray-api-jobs.json").([]any)
	job, _ := NormalizeRayAPIJob(Dict(entries[0]), ctx, time.Now())
	if job.ID != "rc-uid.heliostat-lifecycle-short-flpsb" || job.Kind != KindSubmission || job.Status != StatusSucceeded ||
		job.StartedAt != "2026-09-25T20:19:38.624Z" || job.Compute.GPUModel != "L40S" {
		t.Errorf("submission: %+v", job)
	}
	driver, _ := NormalizeRayAPIJob(Obj{"type": "DRIVER", "job_id": "02000000", "status": "RUNNING", "start_time": float64(1), "end_time": float64(0)}, ctx, time.Now())
	if driver.Kind != KindDriver || driver.Name != "driver-02000000" || driver.FinishedAt != "" {
		t.Errorf("driver: %+v", driver)
	}
	meta, _ := NormalizeRayAPIJob(Obj{"submission_id": "raysubmit_1", "status": "PENDING",
		"metadata": Obj{"heliostat.io/owner": "team-a", "heliostat.io/model-id": "gemma"}}, ctx, time.Now())
	if meta.Owner != "team-a" || meta.Model != "gemma" {
		t.Errorf("metadata: %+v", meta)
	}
}

func TestNormalizeRayService(t *testing.T) {
	svc := func(status Obj) Obj {
		return Obj{
			"metadata": Obj{"name": "gemma", "namespace": "serving", "uid": "svc-uid", "creationTimestamp": "2026-01-01T00:00:00Z"},
			"spec":     Obj{"serveConfigV2": "applications:\n- args:\n    model_id: google/gemma-3-12b-it\n", "rayClusterConfig": Obj{}},
			"status":   status,
		}
	}
	ep, _ := NormalizeRayService(svc(Obj{
		"conditions":          []any{Obj{"type": "Ready", "status": "True", "reason": "NonZeroServeEndpoints"}},
		"activeServiceStatus": Obj{"rayClusterName": "gemma-raycluster-abcde", "applicationStatuses": Obj{"llm": Obj{"status": "RUNNING"}}},
	}), "c")
	if ep.Status != EndpointReady || ep.ActiveRayClusterName != "gemma-raycluster-abcde" || ep.Model != "google/gemma-3-12b-it" ||
		ep.ServeURL != "http://gemma-serve-svc.serving.svc.cluster.local:8000" || len(ep.Applications) != 1 {
		t.Errorf("got %+v", ep)
	}
	upgrading, _ := NormalizeRayService(svc(Obj{
		"conditions":           []any{Obj{"type": "Ready", "status": "True"}, Obj{"type": "UpgradeInProgress", "status": "True"}},
		"activeServiceStatus":  Obj{"rayClusterName": "old"},
		"pendingServiceStatus": Obj{"rayClusterName": "new"},
	}), "c")
	if upgrading.Status != EndpointUpgrading || upgrading.PendingRayClusterName != "new" {
		t.Errorf("upgrading: %+v", upgrading)
	}
}
