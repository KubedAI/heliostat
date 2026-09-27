// Package domain holds Heliostat's normalized records and the pure functions that build them from
// KubeRay custom resources and Ray Jobs API responses. It performs no I/O.
//
// JSON field names are the HTTP API contract consumed by the UI (ui/lib/domain/types.ts) and the
// payload format stored in SQLite; change them only together with both.
package domain

// JobStatus is Heliostat's normalized job state.
type JobStatus string

const (
	StatusPending   JobStatus = "PENDING"
	StatusRunning   JobStatus = "RUNNING"
	StatusSuspended JobStatus = "SUSPENDED"
	StatusSucceeded JobStatus = "SUCCEEDED"
	StatusFailed    JobStatus = "FAILED"
	StatusStopped   JobStatus = "STOPPED"
	StatusUnknown   JobStatus = "UNKNOWN"
)

// JobStatuses lists every status in display order.
var JobStatuses = []JobStatus{
	StatusPending, StatusRunning, StatusSuspended, StatusSucceeded, StatusFailed, StatusStopped, StatusUnknown,
}

// Terminal reports whether a job can no longer change state.
func (s JobStatus) Terminal() bool {
	return s == StatusSucceeded || s == StatusFailed || s == StatusStopped
}

// JobKind is where a job came from.
type JobKind string

const (
	// KindRayJob is a KubeRay RayJob custom resource.
	KindRayJob JobKind = "RayJob"
	// KindSubmission is a job submitted through the Ray Jobs API to an existing cluster.
	KindSubmission JobKind = "Submission"
	// KindDriver is an interactive driver (ray.init() from a notebook or pod).
	KindDriver JobKind = "Driver"
)

// ComputeIntent is the scheduling intent declared in a Ray cluster spec, not observed placement.
type ComputeIntent struct {
	InstanceTypes []string `json:"instanceTypes"`
	// GPUs counts GPUs or AWS Neuron devices across the head and all desired worker replicas.
	GPUs int `json:"gpus"`
	// GPUModel is the accelerator model from node selectors, for example "L40S".
	GPUModel string `json:"gpuModel,omitempty"`
}

// ReportedUsage holds values a workload reported about itself. Heliostat does not measure them.
type ReportedUsage struct {
	InputTokens      *float64 `json:"inputTokens,omitempty"`
	OutputTokens     *float64 `json:"outputTokens,omitempty"`
	TotalTokens      *float64 `json:"totalTokens,omitempty"`
	EstimatedCostUSD *float64 `json:"estimatedCostUsd,omitempty"`
}

// JobRecord is one job, whatever its source.
type JobRecord struct {
	// ID is the RayJob UID, or "<ray cluster uid>.<submission or job id>" for jobs without a CR.
	ID        string  `json:"id"`
	Kind      JobKind `json:"kind"`
	Cluster   string  `json:"cluster"`
	Namespace string  `json:"namespace"`
	Name      string  `json:"name"`

	RayClusterName string `json:"rayClusterName,omitempty"`
	RayClusterUID  string `json:"rayClusterUid,omitempty"`
	// SubmissionID is the Ray Jobs API submission ID (status.jobId on a RayJob).
	SubmissionID string `json:"submissionId,omitempty"`
	// RayJobID is Ray's hex job ID, for example "03000000", when known.
	RayJobID string `json:"rayJobId,omitempty"`

	Status       JobStatus      `json:"status"`
	Phase        string         `json:"phase"`
	Reason       string         `json:"reason,omitempty"`
	Message      string         `json:"message,omitempty"`
	Entrypoint   string         `json:"entrypoint,omitempty"`
	Model        string         `json:"model,omitempty"`
	Owner        string         `json:"owner,omitempty"`
	WorkloadType string         `json:"workloadType,omitempty"`
	Compute      ComputeIntent  `json:"compute"`
	CreatedAt    string         `json:"createdAt"`
	StartedAt    string         `json:"startedAt,omitempty"`
	FinishedAt   string         `json:"finishedAt,omitempty"`
	Reported     *ReportedUsage `json:"reportedUsage,omitempty"`
	// Present is false once the backing RayJob CR or Ray cluster no longer exists.
	Present bool `json:"present"`
}

// DiagnosticsKind says where a job's deep link goes.
type DiagnosticsKind string

const (
	DiagnosticsLive    DiagnosticsKind = "live"
	DiagnosticsHistory DiagnosticsKind = "history"
	DiagnosticsNone    DiagnosticsKind = "none"
)

// JobView is a JobRecord plus link availability, as served by the API.
type JobView struct {
	JobRecord
	Diagnostics       DiagnosticsKind `json:"diagnostics"`
	DiagnosticsReason string          `json:"diagnosticsReason,omitempty"`
}

// Owner is the controller that created a Ray cluster.
type Owner struct {
	Kind string `json:"kind"`
	Name string `json:"name"`
}

// Resources are totals KubeRay reports for a Ray cluster.
type Resources struct {
	CPU    string `json:"cpu,omitempty"`
	Memory string `json:"memory,omitempty"`
	GPU    string `json:"gpu,omitempty"`
}

// Workers counts worker replicas.
type Workers struct {
	Ready   int `json:"ready"`
	Desired int `json:"desired"`
}

// RayClusterRecord is one live Ray cluster.
type RayClusterRecord struct {
	ID              string        `json:"id"`
	Cluster         string        `json:"cluster"`
	Namespace       string        `json:"namespace"`
	Name            string        `json:"name"`
	State           string        `json:"state"`
	Owner           *Owner        `json:"owner,omitempty"`
	RayVersion      string        `json:"rayVersion,omitempty"`
	HeadServiceName string        `json:"headServiceName,omitempty"`
	DashboardPort   int           `json:"dashboardPort"`
	Desired         Resources     `json:"desired"`
	Workers         Workers       `json:"workers"`
	Compute         ComputeIntent `json:"compute"`
	HistoryEnabled  bool          `json:"historyEnabled"`
	CreatedAt       string        `json:"createdAt"`
}

// RayClusterView adds live job counts and the dashboard link.
type RayClusterView struct {
	RayClusterRecord
	ActiveJobs    int    `json:"activeJobs"`
	DashboardHref string `json:"dashboardHref,omitempty"`
}

// EndpointStatus is a RayService's normalized health.
type EndpointStatus string

const (
	EndpointReady     EndpointStatus = "READY"
	EndpointUpgrading EndpointStatus = "UPGRADING"
	EndpointDeploying EndpointStatus = "DEPLOYING"
	EndpointUnhealthy EndpointStatus = "UNHEALTHY"
	EndpointUnknown   EndpointStatus = "UNKNOWN"
)

// Application is one Ray Serve application.
type Application struct {
	Name   string `json:"name"`
	Status string `json:"status"`
}

// EndpointRecord is one RayService.
type EndpointRecord struct {
	ID                    string         `json:"id"`
	Cluster               string         `json:"cluster"`
	Namespace             string         `json:"namespace"`
	Name                  string         `json:"name"`
	Status                EndpointStatus `json:"status"`
	StatusDetail          string         `json:"statusDetail,omitempty"`
	Applications          []Application  `json:"applications"`
	ActiveRayClusterName  string         `json:"activeRayClusterName,omitempty"`
	PendingRayClusterName string         `json:"pendingRayClusterName,omitempty"`
	ServeURL              string         `json:"serveUrl"`
	Model                 string         `json:"model,omitempty"`
	Owner                 string         `json:"owner,omitempty"`
	Compute               ComputeIntent  `json:"compute"`
	CreatedAt             string         `json:"createdAt"`
}

// EndpointView adds the dashboard link.
type EndpointView struct {
	EndpointRecord
	DashboardHref string `json:"dashboardHref,omitempty"`
}

// Model is one entry of the approved-model catalog.
type Model struct {
	ID            string   `json:"id"`
	Name          string   `json:"name"`
	Description   string   `json:"description"`
	Provider      string   `json:"provider"`
	Revision      string   `json:"revision"`
	Task          string   `json:"task"`
	Status        string   `json:"status"`
	GPUProfiles   []string `json:"gpuProfiles"`
	ContextLength int      `json:"contextLength"`
	Tags          []string `json:"tags"`
}

// ClusterInfo describes a configured Kubernetes cluster.
type ClusterInfo struct {
	Name        string `json:"name"`
	DisplayName string `json:"displayName"`
	Region      string `json:"region,omitempty"`
}

// SourceHealth is the state of one data source.
type SourceHealth struct {
	Synced        bool   `json:"synced"`
	LastSuccessAt string `json:"lastSuccessAt,omitempty"`
	Error         string `json:"error,omitempty"`
}

// ClusterHealth is the state of every source for one Kubernetes cluster.
type ClusterHealth struct {
	ClusterInfo
	RayJobs       SourceHealth  `json:"rayJobs"`
	RayClusters   SourceHealth  `json:"rayClusters"`
	RayServices   SourceHealth  `json:"rayServices"`
	Submissions   *SourceHealth `json:"submissions,omitempty"`
	HistoryServer *SourceHealth `json:"historyServer,omitempty"`
}

// Counts are totals across all clusters.
type Counts struct {
	Jobs        int `json:"jobs"`
	ActiveJobs  int `json:"activeJobs"`
	RayClusters int `json:"rayClusters"`
	Endpoints   int `json:"endpoints"`
}

// HealthView is served by /api/health.
type HealthView struct {
	StartedAt string          `json:"startedAt"`
	Clusters  []ClusterHealth `json:"clusters"`
	Counts    Counts          `json:"counts"`
}

// TimeWindow limits the job list to recently created jobs.
type TimeWindow string

var TimeWindows = []TimeWindow{"24h", "7d", "30d", "all"}

// JobQuery filters and pages the job list.
type JobQuery struct {
	Q         string
	Status    JobStatus
	Cluster   string
	Namespace string
	// RayCluster matches jobs of one Ray cluster by exact name. Combine with Cluster and
	// Namespace to identify it uniquely; the same name can exist in several places.
	RayCluster string
	Kind       JobKind
	Window     TimeWindow
	Cursor     string
	Limit      int
}

// Facets summarize the filtered job list.
type Facets struct {
	Status     map[JobStatus]int `json:"status"`
	Namespaces []string          `json:"namespaces"`
}

// JobPage is served by /api/jobs.
type JobPage struct {
	Items      []JobView     `json:"items"`
	NextCursor string        `json:"nextCursor,omitempty"`
	Facets     Facets        `json:"facets"`
	Clusters   []ClusterInfo `json:"clusters"`
}

// ListResponse is served by /api/clusters and /api/endpoints.
type ListResponse[T any] struct {
	Items    []T           `json:"items"`
	Clusters []ClusterInfo `json:"clusters"`
}
