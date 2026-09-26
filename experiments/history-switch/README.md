# Live → History switch

Watches one job's Heliostat link move from the live Ray Dashboard to the Ray History Server archive.
CPU only: one head pod (1 CPU, 2 GiB) for about 4 minutes.

```bash
S3_BUCKET=<history bucket> envsubst < experiments/history-switch/rayjob.yaml | kubectl apply -f -
```

Expected on the job's row in Heliostat:

| Time | Status | Link |
|---|---|---|
| Submitted | pending → running | **Dashboard ↗**: the live Ray Dashboard |
| About 1 minute | succeeded | **Dashboard ↗**: still live, because the cluster exists |
| + 120 s (`ttlSecondsAfterFinished`) | succeeded, "no longer in the cluster" | **No dashboard** for up to one History index refresh (60 s) |
| Then | succeeded | **History ↗**: the archived job page, opened by Ray job ID |

Clean up with `kubectl -n raydata delete rayjob heliostat-history-switch`. The RayJob itself is not
TTL-deleted, only its cluster.
