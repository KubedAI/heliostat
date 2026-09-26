/**
 * Normalized records shared by the collector, the HTTP API, and the UI.
 *
 * This module must stay free of server-only imports: client components import it for types.
 */

export const JOB_STATUSES = [
  'PENDING',
  'RUNNING',
  'SUSPENDED',
  'SUCCEEDED',
  'FAILED',
  'STOPPED',
  'UNKNOWN',
] as const;
export type JobStatus = (typeof JOB_STATUSES)[number];

const TERMINAL_STATUSES: ReadonlySet<JobStatus> = new Set(['SUCCEEDED', 'FAILED', 'STOPPED']);
export const isTerminal = (status: JobStatus): boolean => TERMINAL_STATUSES.has(status);

/**
 * - `RayJob`: a KubeRay RayJob custom resource.
 * - `Submission`: submitted through the Ray Jobs API (`ray job submit`, SDK) to an existing cluster.
 * - `Driver`: an interactive driver (`ray.init()` from a notebook or pod) on an existing cluster.
 */
export type JobKind = 'RayJob' | 'Submission' | 'Driver';

/** Scheduling intent declared in the Ray cluster spec. It is not observed placement. */
export type ComputeIntent = {
  instanceTypes: string[];
  /** GPUs or AWS Neuron devices requested across the head and all worker replicas. */
  gpus: number;
  /** Accelerator model from node selectors, for example `L40S`. */
  gpuModel?: string;
};

/** Values the workload reported about itself through annotations. Not measured by Heliostat. */
export type ReportedUsage = {
  inputTokens?: number;
  outputTokens?: number;
  totalTokens?: number;
  estimatedCostUsd?: number;
};

export type JobRecord = {
  /** RayJob UID, or `<ray cluster uid>.<submission or job id>` for jobs without a CR. */
  id: string;
  kind: JobKind;
  /** Heliostat cluster name (a Kubernetes cluster), from configuration. */
  cluster: string;
  namespace: string;
  name: string;
  rayClusterName?: string;
  rayClusterUid?: string;
  /** Ray Jobs API submission ID (`status.jobId` on a RayJob). */
  submissionId?: string;
  /** Ray's hex job ID (for example `03000000`), when known. */
  rayJobId?: string;
  status: JobStatus;
  /** Source-specific phase, for example `Complete · SUCCEEDED` or `Retrying`. */
  phase: string;
  reason?: string;
  message?: string;
  entrypoint?: string;
  model?: string;
  owner?: string;
  workloadType?: string;
  compute: ComputeIntent;
  createdAt: string;
  startedAt?: string;
  finishedAt?: string;
  reportedUsage?: ReportedUsage;
  /** False once the backing RayJob CR or Ray cluster no longer exists. */
  present: boolean;
};

export type Diagnostics =
  | { kind: 'live'; href: string }
  | { kind: 'history'; href: string }
  | { kind: 'none'; reason: string };

export type JobView = JobRecord & {
  diagnostics: Diagnostics['kind'];
  diagnosticsReason?: string;
};

export type RayClusterRecord = {
  id: string;
  cluster: string;
  namespace: string;
  name: string;
  /** Lower-case KubeRay state such as `ready`, `suspended`, or `provisioning`. */
  state: string;
  owner?: { kind: string; name: string };
  rayVersion?: string;
  headServiceName?: string;
  dashboardPort: number;
  desired: { cpu?: string; memory?: string; gpu?: string };
  workers: { ready: number; desired: number };
  compute: ComputeIntent;
  /** True when the cluster runs the Ray History Server collector sidecar. */
  historyEnabled: boolean;
  createdAt: string;
};

export type RayClusterView = RayClusterRecord & {
  activeJobs: number;
  dashboardHref?: string;
};

export type EndpointStatus = 'READY' | 'UPGRADING' | 'DEPLOYING' | 'UNHEALTHY' | 'UNKNOWN';

export type EndpointRecord = {
  id: string;
  cluster: string;
  namespace: string;
  name: string;
  status: EndpointStatus;
  statusDetail?: string;
  applications: { name: string; status: string }[];
  activeRayClusterName?: string;
  pendingRayClusterName?: string;
  /** In-cluster Ray Serve URL for batch clients. */
  serveUrl: string;
  model?: string;
  owner?: string;
  compute: ComputeIntent;
  createdAt: string;
};

export type EndpointView = EndpointRecord & { dashboardHref?: string };

export type Model = {
  id: string;
  name: string;
  description: string;
  provider: string;
  revision: string;
  task: string;
  status: string;
  gpuProfiles: string[];
  contextLength: number;
  tags: string[];
};

/** A configured Kubernetes cluster, as shown next to every record. */
export type ClusterInfo = {
  name: string;
  displayName: string;
  region?: string;
};

export type SourceHealth = {
  synced: boolean;
  lastSuccessAt?: string;
  error?: string;
};

export type ClusterHealth = ClusterInfo & {
  rayJobs: SourceHealth;
  rayClusters: SourceHealth;
  rayServices: SourceHealth;
  submissions?: SourceHealth;
  historyServer?: SourceHealth;
};

export type HealthView = {
  startedAt: string;
  clusters: ClusterHealth[];
  counts: { jobs: number; activeJobs: number; rayClusters: number; endpoints: number };
};

export const TIME_WINDOWS = ['24h', '7d', '30d', 'all'] as const;
export type TimeWindow = (typeof TIME_WINDOWS)[number];

export type JobQuery = {
  q?: string;
  status?: JobStatus;
  cluster?: string;
  namespace?: string;
  kind?: JobKind;
  window: TimeWindow;
  cursor?: string;
  limit: number;
};

export type JobPage = {
  items: JobView[];
  nextCursor?: string;
  facets: {
    status: Partial<Record<JobStatus, number>>;
    namespaces: string[];
  };
  clusters: ClusterInfo[];
};

export type ListResponse<T> = { items: T[]; clusters: ClusterInfo[] };
