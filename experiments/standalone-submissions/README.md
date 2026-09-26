# Standalone submissions

Validates that Heliostat shows jobs that never had a RayJob CR: work submitted to a shared cluster
with `ray job submit` and interactive drivers. CPU only; the head requests 250m CPU and 1 GiB.

```bash
kubectl apply -f experiments/standalone-submissions/raycluster.yaml
kubectl -n raydata wait raycluster/heliostat-shared --for=jsonpath='{.status.state}'=ready --timeout=5m
kubectl -n raydata port-forward svc/heliostat-shared-head-svc 8265:8265 &

# A succeeding submission with Heliostat metadata
ray job submit --address http://127.0.0.1:8265 \
  --metadata-json '{"heliostat.io/owner": "team-a", "heliostat.io/model-id": "gemma-4-12b-it"}' \
  -- python -c "import time; time.sleep(60); print('done')"

# A failing submission
ray job submit --address http://127.0.0.1:8265 --no-wait -- python -c "import sys; sys.exit(3)"
```

Expected in Heliostat, within one poll interval (30 seconds):

- Both jobs appear with source **Jobs API submission** and Ray cluster `heliostat-shared`. The first has owner `team-a`; the second is **failed** with the exit message on its job page.
- **Dashboard ↗** opens each job in the live dashboard.
- The cluster appears on the **Clusters** page as **Standalone**.

Delete the cluster with `kubectl -n raydata delete raycluster heliostat-shared`. Jobs that had finished keep their status. Any unfinished job becomes **unknown** with reason `ClusterDeleted`.
