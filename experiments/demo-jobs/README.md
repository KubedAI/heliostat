# CPU-only portal demonstration

These eight lightweight RayJobs exercise Heliostat's live KubeRay watch and job-history UX without requesting GPUs or loading models. Names and model labels are representative; the commands only sleep or intentionally fail.

```bash
kubectl apply -f experiments/demo-jobs/rayjobs.yaml
kubectl get rayjob -n raydata -w
```

They remain as RayJob CRs for 24 hours so the table can demonstrate status and history filters. KubeRay removes each generated RayCluster after completion.

Remove only this demonstration set:

```bash
kubectl delete rayjob -n raydata -l heliostat.io/demo=true
```
