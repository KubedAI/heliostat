# Heliostat engineering guide

Read this first, whether you are a person or a coding agent. It explains what Heliostat is, how the code is organized, and the rules every change must keep. [docs/DEVELOPMENT.md](docs/DEVELOPMENT.md) has day-to-day workflows, [docs/CLUSTERS.md](docs/CLUSTERS.md) configuration, and [docs/SECURITY-MODEL.md](docs/SECURITY-MODEL.md) the threat model. Keep this file current in the same change as any architectural change.

## Product boundary

Heliostat is an **experimental**, read-only console for every Ray workload across Kubernetes clusters. It indexes:

- KubeRay `RayJob` resources, plus jobs submitted directly to Ray clusters (`ray job submit`, the Jobs SDK, interactive drivers).
- `RayCluster` and `RayService` resources.

It links every job to its live Ray Dashboard, or to its Ray History Server archive once the cluster is deleted. It never submits, stops, scales, or modifies anything. It does not replace the Ray Dashboard, the History Server, Prometheus, or the KubeRay operator.

## Repository layout

```
cmd/heliostat/        main: config, logging, HTTP server, graceful shutdown
internal/
  config/             YAML configuration schema and validation
  domain/             records + pure normalization of KubeRay CRs and Ray API responses (no I/O)
  kube/               API server connections (default, kubeconfig, EKS IAM tokens) and service transports
  collector/          per-cluster informers, reconciliation, and the Ray Jobs API poller
  history/            Ray History Server index, session loading, cookies
  store/              SQLite job index (schema migrations, queries, retention)
  links/              where a job's deep link goes (pure)
  proxy/              read-only Ray Dashboard proxy and its path policy
  app/                wires store + collectors; builds API views
  server/             HTTP routes, ETags, static UI serving
  models/             approved-model catalog loader
  logging/            structured JSON logs
ui/                   Next.js web UI, exported as static files and embedded by ui/embed.go
charts/heliostat/     Helm chart (the supported install path)
config/               heliostat.yaml (annotated local config) and models.yaml (catalog)
deploy/remote-cluster/ RBAC to apply in clusters indexed from another cluster
testdata/             real KubeRay 1.7.1 / Ray 2.56 objects used by tests
experiments/          CPU-only workloads for exercising Heliostat against a real cluster
docs/                 architecture, clusters, security model, development, roadmap
```

## Architecture

One Go binary serves the UI (static files embedded at build time), the JSON API, the Ray Dashboard proxy, and runs the collectors. There is no separate service and no database server.

For each configured Kubernetes cluster, `collector.Cluster`:

1. Connects with `kube.RESTConfig`. For `eks` connections it resolves the endpoint with `eks:DescribeCluster` and injects a presigned-STS bearer token, refreshed every 10 minutes. Connection failures retry with backoff and surface in `/api/health`.
2. Runs client-go dynamic shared informers for `rayjobs`, `rayclusters`, and `rayservices` (`ray.io/v1`). They LIST, then WATCH, and relist automatically on expiry or errors, emitting deletes for objects that vanished. A transform drops `managedFields` from the cache.
3. After the first sync, reconciles the store against the live objects. Deletions that happened while Heliostat was down are closed.
4. Runs a `SubmissionPoller` that calls `GET /api/jobs/` on every ready Ray head not owned by a RayService. This is the only way to see jobs without a RayJob CR. For RayJob-owned clusters it merges the submission into the RayJob record and learns Ray's hex job ID.
5. Optionally runs a `history.Index` that mirrors the Ray History Server's `/clusters` list.

Every job, whatever its source, is normalized into one `domain.JobRecord` and written to SQLite (`store.Store`), but only when it changed. RayClusters and RayServices are kept in memory.

Lifecycle rules:

- A RayJob deleted before finishing becomes `STOPPED` / `Deleted`; one deleted after finishing keeps its status. Either way `present` becomes false.
- When a Ray cluster disappears, its unfinished submissions become `UNKNOWN` / `ClusterDeleted`.
- Retention deletes records whose backing object has been gone longer than `retention.days`.

API handlers read only the store and collector memory. A browser request never reaches Kubernetes, except for the dashboard proxy routes and `/go/job/<id>`, which may make one History Server call to learn a Ray job ID.

### Deep links

`links.For` decides, without I/O, where a job links:

1. **Live** if the Ray cluster exists with a head service: `/ray/live/<cluster>/<ns>/<raycluster>/#/jobs/<submission id>`.
2. **History** if the History Server has a session: `/ray/history/<cluster>/<ns>/<raycluster>/<session>/#/jobs/<ray job id>`.
3. **None** otherwise, with a reason.

`/go/job/<id>` resolves this at click time, so links never go stale.

### Ray Dashboard proxy and read-only enforcement

`proxy.Serve` serves the Ray Dashboard SPA under a Heliostat path; its assets and API calls are relative.

- It only reaches Ray clusters Heliostat observes and History Server sessions in its index.
- Routes are registered for GET (and HEAD) only; `proxy.CheckPath` also denies GET routes with side effects: profilers, tracebacks, `/api/packages`, and path tricks.
- Browser cookies and credentials are never forwarded; upstream `Set-Cookie` is never relayed.
- The History Server selects a session by cookies, which the proxy synthesizes from the path. `history.Index.EnsureLoaded` calls `/enter_cluster/...` first, because a session must be loaded into the History Server's cache. On a 503 the proxy reloads once and retries.

Transports (`kube.ServiceTransport`):

- **`apiserver`** (the default) goes through the API server's service proxy with a `get`-only identity, so Kubernetes itself rejects any write to Ray.
- The chart adds an egress NetworkPolicy that allows only TCP 443 and DNS, to any destination (ports, not hostnames), so a compromised pod cannot reach Ray's own ports directly. Do not describe it as destination-restricted.
- **`direct`** (cluster DNS) is an opt-in that removes both layers.

The threat model is in [docs/SECURITY-MODEL.md](docs/SECURITY-MODEL.md).

### Multi-cluster

Each entry in `clusters` is independent: one unreachable cluster never affects the others or readiness. The cluster `name` is stored on every record and appears in URLs and filters; never rename it once jobs are recorded.

### Single replica

SQLite on a ReadWriteOnce volume means one replica. The chart enforces it with a `Recreate` strategy. Moving to PostgreSQL is on the roadmap.

## Contracts that span Go and TypeScript

The JSON field names in `internal/domain/types.go` are the API consumed by `ui/lib/domain/types.ts` and the payload stored in SQLite. **Change them together, in one PR**, and keep old stored payloads readable.

## Metadata contract

Workloads may add these labels (preferred) or annotations:

```yaml
metadata:
  labels:
    heliostat.io/model-id: gemma-4-12b-it
    heliostat.io/owner: platform-ai
    heliostat.io/workload-type: native-batch
```

- For `ray job submit`, pass the same keys with `--metadata-json`.
- Without a model label, Heliostat infers the model from `model_id`, `MODEL_ID`, `MODEL_SOURCE`, or `--model` in the entrypoint.
- Token and cost annotations (`heliostat.io/input-tokens`, `output-tokens`, `total-tokens`, `estimated-cost-usd`) are shown only on the job page, labelled as self-reported.
- The GPU column is scheduling intent from the cluster spec, not observed placement. GPU counts include `nvidia.com/gpu`, `amd.com/gpu`, and `aws.amazon.com/neuron`. The model comes from the `karpenter.k8s.aws/instance-gpu-name` or `nvidia.com/gpu.product` selectors.

## Invariants

- Kubernetes access is read-only: `get`, `list`, `watch`, plus `get` on `services/proxy`. A compromised Heliostat must never be able to submit, stop, or scale Ray work. Read docs/SECURITY-MODEL.md before changing RBAC, transports, the NetworkPolicy, or HTTP routes.
- HTTP routes are GET/HEAD only. `server.TestReadOnly` must keep passing.
- Never make a Kubernetes API call per browser request.
- Record every job before relying on KubeRay TTL deletion.
- Store no logs or inference output; `message` and `entrypoint` are truncated.
- Readiness must not depend on any cluster being reachable.
- One Helm release, one container, one replica.
- Update this file in the same change as any architectural change.
