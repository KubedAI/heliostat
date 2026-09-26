# Developing Heliostat

This guide explains how Heliostat is built and how to work on it. Read [AGENTS.md](../AGENTS.md) first for the architecture and the rules every change must keep.

## How it is built

Heliostat is **one Go binary with the web UI embedded in it**:

```
┌──────────────────────── heliostat (Go) ────────────────────────┐
│ collectors (client-go informers, Ray Jobs API poller,          │
│ History Server index) ──► SQLite job index ──► JSON API        │
│ read-only Ray Dashboard proxy          embedded UI (ui/dist)   │
└────────────────────────────────────────────────────────────────┘
          ▲ built from                           ▲ built from
   cmd/ + internal/ (Go)                  ui/ (Next.js, static export)
```

- **Backend: Go.** It uses client-go shared informers (the same machinery as Kubernetes controllers), the AWS SDK v2 for EKS authentication, and pure-Go SQLite (`modernc.org/sqlite`, so no cgo and a static binary). Go was chosen to match the Kubernetes and KubeRay ecosystem, to reuse client-go's battle-tested watch and relist logic, and to ship a small distroless image (about 13 MB compressed).
- **UI: Next.js + React + TypeScript**, exported as plain HTML, CSS, and JS (`output: 'export'`). There is no Node.js at runtime. The browser calls the Go JSON API. KubeRay's own dashboard uses the same stack.
- **Embedding:** `make ui` copies the export into `ui/dist/`, and `ui/embed.go` compiles it into the binary with `//go:embed`. Without a UI build, the binary still runs; `/` explains how to build the UI.
- **Contract:** `internal/domain/types.go` (Go) and `ui/lib/domain/types.ts` (TypeScript) describe the same JSON. Change them together.

## Prerequisites

| Tool | Version | Used for |
|---|---|---|
| Go | 1.26+ | backend |
| Node.js | 22+ | UI build and dev server |
| kubectl + a kubeconfig | any recent | a cluster with KubeRay to develop against |
| Helm | 3.x | chart checks and installs |
| Docker with buildx | any recent | images |

`make help` lists every task.

## Everyday workflows

### Run against a real cluster

Edit `config/heliostat.yaml` so it points at your cluster; the default uses your current kubeconfig context. Then run the backend and the UI in two terminals:

```bash
make run      # backend on http://127.0.0.1:8080
make ui-dev   # UI with hot reload on http://127.0.0.1:3000, proxying /api, /go, /ray to :8080
```

Open http://127.0.0.1:3000. Go changes need a restart of `make run`; UI changes reload instantly.

Locally, Heliostat reaches Ray dashboards through the API server proxy, so your kubeconfig identity needs `get` on `services/proxy`.

### Build the real thing

```bash
make build                     # bin/heliostat with the UI embedded
./bin/heliostat                # serves everything on :3000
make image                     # container image for your machine
make image-push IMAGE=<registry>/heliostat:<tag>   # multi-arch (amd64 + arm64)
```

The Dockerfile builds the UI once, then cross-compiles Go for each architecture natively, with no emulation, so multi-arch builds take a couple of minutes.

### Verify before a pull request

```bash
make verify   # gofmt, go vet, go test, UI format/lint/typecheck/tests, Helm lint
```

CI runs the same target.

### Deploy your build

```bash
make image-push IMAGE=<registry>/heliostat:dev
make install IMAGE=<registry>/heliostat:dev
make port-forward   # http://127.0.0.1:8080
```

## Testing

- **Unit tests live next to the code** (`internal/**/_test.go`, `ui/test/`). Most logic is pure (`domain`, `links`, `proxy` policy, `config`, `store` with in-memory SQLite) and tested directly.
- **Fixtures are real.** `testdata/` holds RayJob, RayCluster, Ray Jobs API, and History Server responses captured from a KubeRay 1.7.1 / Ray 2.56 cluster. When KubeRay changes a field, capture a new fixture rather than hand-writing one:
  ```bash
  kubectl get rayjob <name> -n <ns> -o json | jq 'del(.metadata.managedFields)' > testdata/rayjob-<case>.json
  ```
- **Security behavior is tested.** `server.TestReadOnly` asserts that no route accepts POST, PUT, PATCH, or DELETE; `proxy.TestCheckPath` covers the denylist and path tricks.
- **End to end:** `experiments/` has CPU-only workloads. `history-switch` shows a job's link moving from live to history, and `standalone-submissions` covers `ray job submit`.

## Common changes

**Show a new field from a KubeRay resource**

1. Read it in `internal/domain/normalize.go`, and add the field to the record in `types.go` with a JSON tag.
2. Add the same field to `ui/lib/domain/types.ts` and render it.
3. Add a fixture-based assertion in `internal/domain/domain_test.go`.

**Add a new data source** (for example Pod events)

1. Add an informer in `internal/collector/collector.go`. Keep handlers cheap, and write to the store only when something changed.
2. Extend RBAC in `charts/heliostat/templates/rbac.yaml` with read verbs only, and document it in [SECURITY-MODEL.md](SECURITY-MODEL.md).
3. Report its health in `Cluster.Health()` so the UI's degradation banner covers it.

**Allow a new Ray Dashboard route**

Everything GET is proxied unless denied in `internal/proxy/proxy.go`. When Ray adds a GET route with side effects, add it to `denied` with a reason and a test.

**Change the architecture diagram**

The home page diagram (`ui/components/home/architecture-diagram.tsx`) is also the README's diagram. After changing it, run `make diagram` to regenerate `docs/assets/architecture-{light,dark}.svg`, and commit them. They are animated SVGs with the theme colors and CSS inlined.

**Change configuration**

Update `internal/config`, `config/heliostat.yaml` (annotated), `charts/heliostat/values.yaml`, and the tests together.

## Conventions

- **Go:** `gofmt`, `go vet`, and errors wrapped with context (`fmt.Errorf("describe EKS cluster %s: %w", ...)`). Log with `log/slog`, key-value fields only, never secrets. Keep packages small and their dependencies pointing inward (`domain` imports nothing internal).
- **TypeScript:** Prettier (100 columns, single quotes) and ESLint (`next/core-web-vitals`). Keep components client-only and data-free until they fetch.
- **Comments** explain why, not what.
- **Commits and PRs:** one change per PR, linked to an approved issue (see [CONTRIBUTING.md](../CONTRIBUTING.md)).

## Working with AI coding agents

Heliostat was built with AI coding tools, and agent contributions are welcome.

- `AGENTS.md` (root) and `ui/AGENTS.md` are the agent entry points. `CLAUDE.md` imports them, and Codex and other agents read `AGENTS.md` directly.
- Ask the agent to run `make verify` and to update AGENTS.md when it changes architecture.
- Review agent PRs like any other, with extra care for the invariants in AGENTS.md, especially the read-only rules.

## Debugging

- `HELIOSTAT_LOG_LEVEL=debug make run` for verbose logs.
- `/api/health` shows every source (RayJobs, RayClusters, RayServices, Ray Jobs API, History Server) per cluster, with the last error.
- `HELIOSTAT_DB_PATH=/tmp/h.db` gives you a throwaway database. The database is plain SQLite: `sqlite3 .data/heliostat.db 'select id, status from jobs'`.

## Releasing

The version lives in one place: `appVersion` (and `version`) in `charts/heliostat/Chart.yaml`. The Makefile reads it, the chart uses it as the default image tag, and the binary reports it at startup.

1. Bump `version` and `appVersion` in `charts/heliostat/Chart.yaml`.
2. `make image-push IMAGE=<registry>/heliostat:<version>`.
3. Tag the commit `v<version>`.

Everyday image builds for testing don't need a version bump. Push them under any tag (`make image-push IMAGE=<registry>/heliostat:dev`) and install with `--set image.tag=dev`.
