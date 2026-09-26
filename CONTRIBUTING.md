# Contributing to Heliostat

Thanks for helping. Heliostat is early and not production ready, so there is plenty to do. Contributions from people and from AI coding agents are equally welcome.

## Before you start

1. **Read [AGENTS.md](AGENTS.md).** It is the engineering guide: architecture, data flow, the metadata contract, and the invariants every change must keep. If you work with a coding agent (Claude Code, Codex, Copilot, Cursor, or others), point it at AGENTS.md first. `CLAUDE.md` already imports it.
2. **Open an issue first.** Describe the bug (with steps to reproduce, KubeRay and Ray versions) or the proposal (the problem, the approach, and alternatives you considered).
3. **Wait for approval.** A maintainer will confirm the approach, or suggest another, before you invest in a pull request. Small fixes such as typos and obvious bugs can skip straight to a PR.

## Pull requests

- Link the approved issue (`Fixes #123`).
- Keep each PR to one change. Update AGENTS.md in the same PR if you change architecture, data flow, configuration, or the metadata contract.
- `make verify` must pass (Go formatting, vet, and tests; UI format, lint, typecheck, and tests; Helm lint). Add tests for new behavior; `testdata/` holds real KubeRay objects.
- If you change a JSON field, update `internal/domain/types.go` and `ui/lib/domain/types.ts` together.
- UI changes: include a screenshot in both light and dark themes.
- If an AI agent wrote most of the change, say so in the PR description. We review it the same way.

## Non-negotiable invariants

These protect every user of Heliostat. A PR that weakens any of them will not be merged:

- **Read-only.** No Kubernetes verbs beyond `get`, `list`, and `watch` (and `get` on `services/proxy`). No write paths to Ray. HTTP routes export only `GET` and `HEAD`.
- **No per-request Kubernetes calls.** The UI and API read from the local index; only the collectors talk to clusters.
- **Secure by default.** New features that need more access must be opt-in and documented in the security model.

## Help wanted: security extensions

The biggest gap is authentication and multi-tenancy. Proposals and contributions are very welcome for:

- OIDC sign-in, native or behind oauth2-proxy.
- Mapping IdP groups to namespaces or teams, and scoping jobs, clusters, and the dashboard proxy accordingly.
- Serving the Ray dashboard proxy from a separate origin, isolated from the console's session.
- Audit logging of who opened which job or dashboard.

Open an issue with your design first; security changes get a careful review.

## Development setup

```bash
make run      # backend on :8080
make ui-dev   # UI with hot reload on :3000
make verify
```

[docs/DEVELOPMENT.md](docs/DEVELOPMENT.md) covers prerequisites, workflows, testing, and common changes.

`experiments/` has CPU-only workloads for trying changes against a real cluster.

## License

By contributing, you agree that your contributions are licensed under the [Apache License 2.0](LICENSE).
