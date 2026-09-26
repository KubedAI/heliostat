# Roadmap

## Delivered

### 0.1.0 (first release, experimental)

- **Every Ray job, however it was submitted:** RayJob CRs, `ray job submit` and Jobs SDK submissions to existing clusters, and interactive drivers, in one job model with status, failure reason, runtime, owner, model, and GPUs.
- **Automatic deep links:** the live Ray Dashboard while a cluster runs, then its Ray History Server archive, with an explanation when neither exists.
- **Many clusters:** Amazon EKS in any region or AWS account (EKS Pod Identity and role assumption, no static credentials), and on-premises Kubernetes through a mounted kubeconfig.
- **Read-only by construction:** get-only RBAC, all Ray traffic through the API server's service proxy, and an egress NetworkPolicy. Attack-tested; see [SECURITY-MODEL.md](SECURITY-MODEL.md).
- **Durable job index:** SQLite with schema migrations, reconciliation after downtime, and retention.
- **Console:** animated architecture overview, jobs with URL-shareable filters and pagination, job detail with the driver's last logs, Ray clusters, endpoints, and the model catalog. Light and dark themes.
- **Small and simple to run:** one Go binary with the UI embedded, a distroless image of about 13 MB, and one Helm release.

## Next

### Near term

- **Model catalog as its own ConfigMap**, owned by the platform team and restricted by RBAC, editable without redeploying Heliostat.
- **Guarantee History Server coverage:** a ValidatingAdmissionPolicy that requires `historyServerOptions` (archiving to S3) on every RayJob, RayCluster, and RayService.
- **An "archiving…" state** for the minute between a Ray cluster's deletion and its archive appearing.

### 1. Authentication and multi-tenant views

Everyone currently sees everything, including logs through the proxy.

- Authenticate users with OIDC (for example, oauth2-proxy in front, or native Auth.js). Map IdP groups to namespaces or teams in configuration.
- Scope the job list, clusters, and Ray proxy to a user's namespaces. Platform admins keep the global view.
- Serve the Ray proxy from a separate origin (for example `ray.<heliostat-host>`). Ray Dashboard JavaScript then never runs alongside the console's session cookie.

### 2. Why is my job stuck?

The most common question for Pending jobs.

- Watch Ray head and worker Pods and their Events (`get/list/watch` on `pods`, `events`). Surface `Insufficient nvidia.com/gpu`, `ImagePullBackOff`, `FailedMount`, and OOMKilled on the job page.
- Surface Karpenter NodeClaim launch failures (for example, insufficient capacity for an instance type).

### 3. Observed placement

Record what each Ray pod actually ran on before the pods disappear:

- instance type
- capacity type (spot or on-demand)
- availability zone
- GPU model and count

This replaces the GPU column's declared intent with facts and is a prerequisite for cost.

### 4. Measured tokens and cost

Replace self-reported annotations with measurements.

| Signal | Source | Attribution |
|---|---|---|
| Prompt and generated tokens | vLLM token counters exported through Ray metrics, scraped into Prometheus or Amazon Managed Prometheus | By Ray cluster label. Exact for RayJobs, which get one cluster per job. |
| Tokens on shared clusters and RayServices | Per-request `usage` from vLLM's OpenAI-compatible responses, or Ray Data LLM per-row token columns | Totalled by the client and published as job metadata at completion |
| Actual hardware and duration | Observed placement (item 3) | Per pod |
| Cost estimate | OpenCost allocation API, filtered on the `ray.io/cluster` label over the job's lifetime | Per job, available immediately |
| Billed cost | AWS CUR 2.0 with split cost allocation data for EKS, via Athena | Reconciled after about 24 hours. The job shows "estimated" until then, then "billed". |
| Efficiency | DCGM GPU utilization in Prometheus | Cost per useful GPU-hour |

Heliostat queries these at job completion and stores the results. Cost per million tokens is measured cost divided by measured tokens.

### 5. Kueue integration

Useful once GPU quota is managed with Kueue:

- Show queue, admission state, position, and pending reason (from the `Workload` owned by each RayJob) on Pending and Suspended jobs.
- A quota view per ClusterQueue and LocalQueue: nominal quota, borrowing, and usage per team. This pairs with multi-tenant views.

Heliostat already maps KubeRay's `Suspended` state, which Kueue uses while a job waits for admission.

### 6. Agent mode for unreachable clusters

For clusters whose API endpoint the hub cannot reach, run Heliostat in `agent` mode inside that cluster. The agent pushes normalized records to the hub and serves its own Ray proxy.

### 7. Scale and operations

- PostgreSQL storage for more than one replica.
- A Prometheus `/metrics` endpoint: watch lag, events, poll latency, store size.
- WebSocket support in the proxy for live log tailing.

## Upstreaming checklist

Before proposing Heliostat to `ray-project` (next to KubeRay) or a Kubernetes SIG:

- [x] Apache 2.0 `LICENSE`, `NOTICE`, `CONTRIBUTING.md`, `SECURITY.md`
- [ ] Code of conduct and `OWNERS`
- [ ] End-to-end tests on kind with KubeRay: RayJob, standalone submission, History Server
- [x] Multi-arch image build (`make image-push`)
- [ ] Published image and Helm chart (OCI), signed with provenance
- [ ] Versioned public HTTP API (`/api/v1`) with an OpenAPI description
- [ ] A compatibility matrix for KubeRay and Ray versions
