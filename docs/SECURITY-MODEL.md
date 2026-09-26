# Security model

Heliostat is **experimental** and has **no authentication yet**. Anyone who can reach the UI can see every job, its failure messages, and its Ray logs. Run it behind `kubectl port-forward` or an internal load balancer on a trusted network. To report a vulnerability, see [SECURITY.md](../SECURITY.md).

## Goal

**Even someone who fully compromises Heliostat (its pod, image, or service-account token) must not be able to control Ray clusters or GPUs:** no submitting, stopping, or deleting jobs, no deploying Serve apps, no scaling clusters, and no running code.

## Layers

Each layer holds on its own; together they cover each other's gaps.

| Layer | Enforced by | What it guarantees |
|---|---|---|
| **Kubernetes RBAC** | the API server | The service account has only `get`, `list`, and `watch` on `rayjobs`, `rayclusters`, and `rayservices`, plus `get` on `services/proxy`. No create, update, patch, delete, scale, or exec, and no Secrets. |
| **API server proxy** (`serviceTransport: apiserver`, the default) | the API server | Every call to a Ray head or the History Server goes through the API server's service proxy. A `get`-only identity cannot send `POST`, `PUT`, or `DELETE`, so job submission, stopping, and Serve deployment are refused by Kubernetes itself, whatever Heliostat's code does. |
| **Egress NetworkPolicy** (`networkPolicy.enabled`, the default) | your CNI | The pod may only open TCP 443 (API servers, AWS APIs) and DNS. Ray's unauthenticated ports (8265 dashboard and Jobs API, 10001 client, 6379 GCS, 8000 Serve) are unreachable, so a compromised pod cannot go around the API server. |
| **Application** | Heliostat | Routes are registered for `GET`/`HEAD` only (tested). The dashboard proxy reaches only Ray clusters Heliostat observes, strips browser cookies and credentials, never relays `Set-Cookie`, and denies Ray `GET` routes with side effects: profilers, tracebacks, package downloads, and path tricks. |
| **Container** | Kubernetes | Distroless image with no shell, non-root (UID 65532), read-only root filesystem, all capabilities dropped, `RuntimeDefault` seccomp. |

## Verified behavior

These results were tested on a live EKS cluster using Heliostat's own service-account token:

| Attempt | Result |
|---|---|
| Create, patch, delete, or scale a RayJob, RayCluster, or RayService; exec into pods; read Secrets | Denied by RBAC |
| Submit, stop, or delete a Ray job, or deploy a Serve app, through the API server | `403 Forbidden` (`cannot create resource "services/proxy"`) |
| Submit a job through Heliostat's proxy | `405 Method Not Allowed` |
| Run a profiler or traceback through Heliostat's proxy | `403 Forbidden` |
| Any `POST`/`PUT`/`PATCH`/`DELETE` on any Heliostat route | `405` (asserted in `server.TestReadOnly` on every build) |

## Operator checklist

- [ ] **Enforce NetworkPolicy** in every cluster Heliostat runs in: the Amazon VPC CNI with `enableNetworkPolicy: "true"`, Cilium, or Calico. Without enforcement, the egress layer is inert and a compromised pod could reach a Ray head directly. Enabling it also enforces every other NetworkPolicy in the cluster, so review those first.
- [ ] **Keep the default transport** (`apiserver`). `direct` removes the API-server layer and cannot be combined with the egress policy.
- [ ] **Keep the service `ClusterIP`**, or use an internal load balancer only. The chart refuses an internet-facing LoadBalancer unless you override it.
- [ ] **Keep configuration private.** It holds no credentials, but it names clusters, regions, and account IDs. See [CLUSTERS.md](CLUSTERS.md#what-goes-in-the-configuration-and-what-never-does).
- [ ] **Scope remote access.** Apply [deploy/remote-cluster/rbac.yaml](../deploy/remote-cluster/rbac.yaml) unchanged, and trust only the hub role in remote accounts.

## Known limitations

- **`services/proxy` is cluster-wide.** Kubernetes cannot scope it to Ray services, so Heliostat's token can issue `GET` requests to any Service through the API server, including Ray `GET` routes that Heliostat's own proxy denies. It cannot write.
- **No authentication or tenancy.** Every viewer sees every namespace. Planned; see the [roadmap](ROADMAP.md).
- **Shared origin.** The Ray dashboard runs on Heliostat's origin, so an XSS in Ray's dashboard would share the console's origin. This matters once authentication exists; the roadmap moves the proxy to its own origin.
- **Self-reported metadata.** Owner, model, and token annotations are whatever workloads set. They are for display, not access control.

## Help wanted

Authentication (OIDC), per-team scoping, a separate proxy origin, and audit logging are the most valuable security contributions. Open an issue with your design first; see [CONTRIBUTING.md](../CONTRIBUTING.md).
