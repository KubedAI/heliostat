# Heliostat lifecycle validation

These CPU-only RayJobs validate that Heliostat observes real KubeRay CRD
transitions. They intentionally have different runtimes; one job exits with an
error, while `heliostat-running-observer` stays active for ten minutes so the
portal's `RUNNING` state and elapsed-runtime updates can be inspected manually.

```bash
kubectl apply -f experiments/lifecycle-tests/rayjobs.yaml
kubectl get rayjobs -n raydata -l heliostat.io/test=lifecycle -w
```

The resources retain their CRs for 24 hours. Remove only this test set with:

```bash
kubectl delete rayjobs -n raydata -l heliostat.io/test=lifecycle
```

The jobs request CPU only. Model names and token/cost annotations are portal
metadata and do not cause model downloads or GPU provisioning.
