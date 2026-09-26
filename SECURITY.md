# Security policy

Heliostat is **experimental** and has **no authentication**. Run it behind `kubectl port-forward` or an internal load balancer on a trusted network.

## Reporting a vulnerability

Please do **not** open a public issue. Report privately through GitHub's [private vulnerability reporting](../../security/advisories/new) for this repository, with steps to reproduce and the affected version. We aim to acknowledge reports within a week.

## How Heliostat is secured

Heliostat is read-only by design: even a fully compromised Heliostat must not be able to control Ray clusters or GPUs. [docs/SECURITY-MODEL.md](docs/SECURITY-MODEL.md) covers the layers that enforce this, the verified attack results, an operator checklist, and known limitations.
