package domain

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"
)

// Metadata contract keys (see AGENTS.md). Labels are preferred; annotations are accepted for
// values that are not label-safe.
const (
	KeyModel            = "heliostat.io/model-id"
	KeyOwner            = "heliostat.io/owner"
	KeyWorkloadType     = "heliostat.io/workload-type"
	KeyInputTokens      = "heliostat.io/input-tokens"
	KeyOutputTokens     = "heliostat.io/output-tokens"
	KeyTotalTokens      = "heliostat.io/total-tokens"
	KeyEstimatedCostUSD = "heliostat.io/estimated-cost-usd"

	maxMessage           = 4000
	maxEntrypoint        = 1000
	defaultDashboardPort = 8265
	epochTimestamp       = "1970-01-01T00:00:00.000Z"
)

var modelHint = regexp.MustCompile(`(?:model_id|MODEL_ID|MODEL_SOURCE|--model)[=:\s]+["']?([^\s"',]+)`)

// read returns a metadata contract key.
func read(values Obj, key string) any { return values[key] }

type metadata struct{ labels, annotations Obj }

func metadataOf(item Obj) metadata {
	m := Dict(item["metadata"])
	return metadata{labels: Dict(m["labels"]), annotations: Dict(m["annotations"])}
}

func (m metadata) lookup(key string) string {
	if v := Str(read(m.labels, key)); v != "" {
		return v
	}
	return Str(read(m.annotations, key))
}

func inferModel(sources ...any) string {
	for _, source := range sources {
		if match := modelHint.FindStringSubmatch(Str(source)); match != nil {
			return match[1]
		}
	}
	return ""
}

func optFloat(values Obj, key string) *float64 {
	if f, ok := OptNumber(read(values, key)); ok {
		return &f
	}
	return nil
}

func reportedUsage(values Obj) *ReportedUsage {
	usage := ReportedUsage{
		InputTokens:      optFloat(values, KeyInputTokens),
		OutputTokens:     optFloat(values, KeyOutputTokens),
		TotalTokens:      optFloat(values, KeyTotalTokens),
		EstimatedCostUSD: optFloat(values, KeyEstimatedCostUSD),
	}
	if usage.TotalTokens == nil && (usage.InputTokens != nil || usage.OutputTokens != nil) {
		total := 0.0
		if usage.InputTokens != nil {
			total += *usage.InputTokens
		}
		if usage.OutputTokens != nil {
			total += *usage.OutputTokens
		}
		usage.TotalTokens = &total
	}
	if usage == (ReportedUsage{}) {
		return nil
	}
	return &usage
}

type identity struct{ uid, name, namespace, createdAt string }

func identityOf(item Obj) (identity, bool) {
	m := Dict(item["metadata"])
	id := identity{
		uid:       Str(m["uid"]),
		name:      Str(m["name"]),
		namespace: Str(m["namespace"]),
		createdAt: Str(m["creationTimestamp"]),
	}
	if id.uid == "" || id.name == "" {
		return id, false
	}
	if id.namespace == "" {
		id.namespace = "default"
	}
	if id.createdAt == "" {
		id.createdAt = epochTimestamp
	}
	return id, true
}

// NormalizeRayJob converts a RayJob custom resource. It returns false for objects without a UID.
func NormalizeRayJob(item Obj, cluster string) (JobRecord, bool) {
	id, ok := identityOf(item)
	if !ok {
		return JobRecord{}, false
	}
	meta := metadataOf(item)
	spec, status := Dict(item["spec"]), Dict(item["status"])
	deployment, jobStatus := Str(status["jobDeploymentStatus"]), Str(status["jobStatus"])

	model := meta.lookup(KeyModel)
	if model == "" {
		model = inferModel(spec["entrypoint"], spec["runtimeEnvYAML"])
	}
	merged := Obj{}
	for k, v := range meta.labels {
		merged[k] = v
	}
	for k, v := range meta.annotations {
		merged[k] = v
	}

	return JobRecord{
		ID:             id.uid,
		Kind:           KindRayJob,
		Cluster:        cluster,
		Namespace:      id.namespace,
		Name:           id.name,
		RayClusterName: Str(status["rayClusterName"]),
		SubmissionID:   Str(status["jobId"]),
		Status:         RayJobResourceStatus(deployment, jobStatus),
		Phase:          RayJobPhase(deployment, jobStatus),
		Reason:         Str(status["reason"]),
		Message:        Truncate(Str(status["message"]), maxMessage),
		Entrypoint:     Truncate(Str(spec["entrypoint"]), maxEntrypoint),
		Model:          model,
		Owner:          meta.lookup(KeyOwner),
		WorkloadType:   meta.lookup(KeyWorkloadType),
		Compute:        ComputeFor(Dict(spec["rayClusterSpec"])),
		CreatedAt:      id.createdAt,
		StartedAt:      Str(status["startTime"]),
		FinishedAt:     Str(status["endTime"]),
		Reported:       reportedUsage(merged),
		Present:        true,
	}, true
}

// RayClusterContext identifies the Ray cluster a Ray Jobs API entry came from.
type RayClusterContext struct {
	Cluster        string
	Namespace      string
	RayClusterName string
	RayClusterUID  string
	Compute        ComputeIntent
}

// NormalizeRayAPIJob converts one entry of a Ray head's GET /api/jobs/. Submissions made through
// the Jobs API carry a submission_id; interactive drivers only have Ray's hex job_id.
func NormalizeRayAPIJob(entry Obj, ctx RayClusterContext, now time.Time) (JobRecord, bool) {
	submissionID, rayJobID := Str(entry["submission_id"]), Str(entry["job_id"])
	key := submissionID
	if key == "" {
		key = rayJobID
	}
	if key == "" {
		return JobRecord{}, false
	}
	meta := Dict(entry["metadata"])
	startedAt := EpochMillisToISO(entry["start_time"])
	createdAt := startedAt
	if createdAt == "" {
		createdAt = now.UTC().Format("2006-01-02T15:04:05.000Z")
	}
	kind, name := KindSubmission, submissionID
	if submissionID == "" {
		kind, name = KindDriver, "driver-"+rayJobID
	}
	phase := Str(entry["status"])
	if phase == "" {
		phase = string(StatusUnknown)
	}
	model := Str(read(meta, KeyModel))
	if model == "" {
		model = inferModel(entry["entrypoint"])
	}
	compute := ctx.Compute
	if compute.InstanceTypes == nil {
		compute.InstanceTypes = []string{}
	}

	return JobRecord{
		ID:             ctx.RayClusterUID + "." + key,
		Kind:           kind,
		Cluster:        ctx.Cluster,
		Namespace:      ctx.Namespace,
		Name:           name,
		RayClusterName: ctx.RayClusterName,
		RayClusterUID:  ctx.RayClusterUID,
		SubmissionID:   submissionID,
		RayJobID:       rayJobID,
		Status:         RayJobStatus(Str(entry["status"])),
		Phase:          phase,
		Reason:         Str(entry["error_type"]),
		Message:        Truncate(Str(entry["message"]), maxMessage),
		Entrypoint:     Truncate(Str(entry["entrypoint"]), maxEntrypoint),
		Model:          model,
		Owner:          Str(read(meta, KeyOwner)),
		WorkloadType:   Str(read(meta, KeyWorkloadType)),
		Compute:        compute,
		CreatedAt:      createdAt,
		StartedAt:      startedAt,
		FinishedAt:     EpochMillisToISO(entry["end_time"]),
		Reported:       reportedUsage(meta),
		Present:        true,
	}, true
}

// NormalizeRayCluster converts a RayCluster custom resource.
func NormalizeRayCluster(item Obj, cluster string) (RayClusterRecord, bool) {
	id, ok := identityOf(item)
	if !ok {
		return RayClusterRecord{}, false
	}
	spec, status := Dict(item["spec"]), Dict(item["status"])
	var owner *Owner
	if refs := List(Dict(item["metadata"])["ownerReferences"]); len(refs) > 0 && Str(refs[0]["kind"]) != "" {
		owner = &Owner{Kind: Str(refs[0]["kind"]), Name: Str(refs[0]["name"])}
	}
	state := strings.ToLower(Str(status["state"]))
	if state == "" {
		state = "provisioning"
	}
	port := int(Number(Dig(status, "endpoints")["dashboard"]))
	if port == 0 {
		port = defaultDashboardPort
	}
	_, historyEnabled := spec["historyServerOptions"]

	return RayClusterRecord{
		ID:              id.uid,
		Cluster:         cluster,
		Namespace:       id.namespace,
		Name:            id.name,
		State:           state,
		Owner:           owner,
		RayVersion:      Str(spec["rayVersion"]),
		HeadServiceName: Str(Dig(status, "head")["serviceName"]),
		DashboardPort:   port,
		Desired: Resources{
			CPU:    Str(status["desiredCPU"]),
			Memory: Str(status["desiredMemory"]),
			GPU:    Str(status["desiredGPU"]),
		},
		Workers: Workers{
			Ready:   int(Number(status["readyWorkerReplicas"])),
			Desired: int(Number(status["desiredWorkerReplicas"])),
		},
		Compute:        ComputeFor(spec),
		HistoryEnabled: historyEnabled,
		CreatedAt:      id.createdAt,
	}, true
}

// NormalizeRayService converts a RayService custom resource.
func NormalizeRayService(item Obj, cluster string) (EndpointRecord, bool) {
	id, ok := identityOf(item)
	if !ok {
		return EndpointRecord{}, false
	}
	meta := metadataOf(item)
	spec, status := Dict(item["spec"]), Dict(item["status"])
	active := Dict(status["activeServiceStatus"])

	appStatuses := Dict(active["applicationStatuses"])
	names := make([]string, 0, len(appStatuses))
	for name := range appStatuses {
		names = append(names, name)
	}
	sort.Strings(names)
	apps := make([]Application, 0, len(names))
	statuses := make([]string, 0, len(names))
	for _, name := range names {
		s := Str(Dict(appStatuses[name])["status"])
		if s == "" {
			s = "UNKNOWN"
		}
		apps = append(apps, Application{Name: name, Status: s})
		statuses = append(statuses, s)
	}

	var conditions []Condition
	for _, c := range List(status["conditions"]) {
		conditions = append(conditions, Condition{
			Type: Str(c["type"]), Status: Str(c["status"]), Reason: Str(c["reason"]), Message: Str(c["message"]),
		})
	}
	health, detail := EndpointHealth(conditions, statuses)
	model := meta.lookup(KeyModel)
	if model == "" {
		model = inferModel(spec["serveConfigV2"])
	}

	return EndpointRecord{
		ID:                    id.uid,
		Cluster:               cluster,
		Namespace:             id.namespace,
		Name:                  id.name,
		Status:                health,
		StatusDetail:          detail,
		Applications:          apps,
		ActiveRayClusterName:  Str(active["rayClusterName"]),
		PendingRayClusterName: Str(Dig(status, "pendingServiceStatus")["rayClusterName"]),
		ServeURL:              fmt.Sprintf("http://%s-serve-svc.%s.svc.cluster.local:8000", id.name, id.namespace),
		Model:                 model,
		Owner:                 meta.lookup(KeyOwner),
		Compute:               ComputeFor(Dict(spec["rayClusterConfig"])),
		CreatedAt:             id.createdAt,
	}, true
}
