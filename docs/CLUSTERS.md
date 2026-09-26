# Adding clusters

Heliostat shows Ray workloads from **exactly the Kubernetes clusters listed in its configuration**, and nothing else.

- **Each entry is one Kubernetes cluster.** Within it, Heliostat sees every namespace: all RayJobs, RayClusters, and RayServices, plus jobs submitted to those Ray clusters.
- **Without a `clusters` list**, Heliostat indexes only the cluster it runs in.
- **Clusters not listed are never contacted.** To add one, add an entry and upgrade the release.

Entries are independent: one unreachable cluster never affects the others or the UI's availability. Each appears in the UI with its display name and region.

## Where configuration lives

| Where Heliostat runs | Configure in | Reference |
|---|---|---|
| Kubernetes (Helm) | the chart's `clusters` value | [charts/heliostat/values.yaml](../charts/heliostat/values.yaml) |
| Your laptop (`make run`) | `config/heliostat.yaml` | [config/heliostat.yaml](../config/heliostat.yaml), annotated with every field |

A minimal entry:

```yaml
clusters:
  - name: platform-us-west-2   # stable DNS label; stored with every job; do not rename later
    displayName: Platform      # optional; shown in the UI
    region: us-west-2          # optional; shown under the name (EKS entries default to their region)
    connection: { type: default }
```

## What goes in the configuration, and what never does

The configuration holds **no secrets**: cluster names, regions, Kubernetes Service names, and IAM role ARNs. Credentials come from the environment:

| Connection | Where the credential comes from |
|---|---|
| `default` | The pod's Kubernetes service-account token, mounted by Kubernetes and rotated automatically. Locally, your kubeconfig. |
| `eks` | EKS Pod Identity (or IRSA) on Heliostat's service account, then a short-lived role session and a 15-minute EKS token. No keys are stored anywhere. |
| `kubeconfig` | A kubeconfig in a Kubernetes **Secret** you create and mount. The configuration only holds the file path. |

It still describes your environment: account IDs inside role ARNs, cluster names, and regions. So:

- **Keep real values out of public repositories.** Put them in a private values file or your GitOps repository, and install with `helm upgrade ... -f my-values.yaml`. The files in this repository are examples.
- **Never put a token or kubeconfig contents in values.** Use the Secret-plus-`extraVolumes` pattern below.
- Role ARNs are identifiers, not credentials. The trust policy on the role decides who can use it (see step 2 below).

## The cluster Heliostat runs in

```yaml
clusters:
  - name: platform-us-west-2
    region: us-west-2
    connection: { type: default }
    historyServer:                                   # optional: links for deleted clusters
      api: ray-history-server.ray-history-server:8080
      dashboard: ray-history-dashboard.ray-history-server:8265
```

The chart grants the RBAC this needs. The History Server archives only Ray clusters whose spec sets `historyServerOptions`. We recommend every Ray cluster archive to S3, so every finished job keeps a dashboard.

## Another Amazon EKS cluster, in any region or AWS account

Heliostat (the *hub*) connects with IAM, not static credentials. For each remote cluster, the hub:

1. Gets base credentials from **EKS Pod Identity** on its own service account.
2. Assumes a **reader role** in the remote account (`sts:AssumeRole`).
3. Calls `eks:DescribeCluster` for the endpoint and CA.
4. Mints a short-lived EKS token (what `aws eks get-token` produces), refreshed every 10 minutes.
5. Watches KubeRay resources and reaches Ray dashboards through the remote **API server's service proxy**. It needs no access to the remote pod network.

**Prerequisites:**

- The hub can reach the remote API endpoint: a public endpoint restricted to the hub's NAT IPs, or a private endpoint over Transit Gateway or VPC peering with DNS resolution.
- The remote cluster uses the `API` or `API_AND_CONFIG_MAP` authentication mode (for access entries).

### 1. Hub account: Pod Identity role

Create a role with the Pod Identity trust policy (`pods.eks.amazonaws.com`, actions `sts:AssumeRole` and `sts:TagSession`) and this permission policy:

```json
{
  "Version": "2012-10-17",
  "Statement": [
    {
      "Effect": "Allow",
      "Action": ["sts:AssumeRole", "sts:TagSession"],
      "Resource": "arn:aws:iam::*:role/heliostat-reader"
    }
  ]
}
```

```bash
aws eks create-pod-identity-association --cluster-name <hub-cluster> \
  --namespace heliostat --service-account heliostat-heliostat \
  --role-arn arn:aws:iam::<hub-account>:role/heliostat-hub
```

### 2. Remote account: reader role

The trust policy allows only the hub role:

```json
{
  "Version": "2012-10-17",
  "Statement": [
    {
      "Effect": "Allow",
      "Principal": { "AWS": "arn:aws:iam::<hub-account>:role/heliostat-hub" },
      "Action": ["sts:AssumeRole", "sts:TagSession"]
    }
  ]
}
```

The permission policy:

```json
{
  "Version": "2012-10-17",
  "Statement": [
    {
      "Effect": "Allow",
      "Action": "eks:DescribeCluster",
      "Resource": "arn:aws:eks:<region>:<remote-account>:cluster/<remote-cluster>"
    }
  ]
}
```

For an external ID, add an `sts:ExternalId` condition to the trust policy and set `externalId` in the entry.

### 3. Remote cluster: read-only Kubernetes access

```bash
aws eks create-access-entry --cluster-name <remote-cluster> \
  --principal-arn arn:aws:iam::<remote-account>:role/heliostat-reader \
  --kubernetes-groups heliostat:readers
kubectl --context <remote> apply -f deploy/remote-cluster/rbac.yaml
```

[deploy/remote-cluster/rbac.yaml](../deploy/remote-cluster/rbac.yaml) grants `get`, `list`, and `watch` on KubeRay resources and `get` on `services/proxy`, nothing else.

### 4. Add the entry

```yaml
  - name: analytics-eu-west-1
    displayName: Analytics (eu-west-1)
    connection:
      type: eks
      clusterName: analytics
      region: eu-west-1
      roleArn: arn:aws:iam::222222222222:role/heliostat-reader
    historyServer:
      api: ray-history-server.ray-history-server:8080
      dashboard: ray-history-dashboard.ray-history-server:8265
```

## On-premises or any other Kubernetes

Create a read-only identity in that cluster: apply [deploy/remote-cluster/rbac.yaml](../deploy/remote-cluster/rbac.yaml) and bind the `heliostat:readers` group to a service account. Put a kubeconfig for it (bearer token or client certificate) into a Secret next to Heliostat:

```bash
kubectl -n heliostat create secret generic heliostat-kubeconfigs --from-file=onprem-dc1.yaml=./onprem-dc1.kubeconfig
```

Mount it and reference the file:

```yaml
extraVolumes:
  - name: remote-kubeconfigs
    secret: { secretName: heliostat-kubeconfigs }
extraVolumeMounts:
  - name: remote-kubeconfigs
    mountPath: /var/run/heliostat/kubeconfigs
    readOnly: true
clusters:
  - name: onprem-dc1
    region: dc1
    connection:
      type: kubeconfig
      path: /var/run/heliostat/kubeconfigs/onprem-dc1.yaml
```

`exec` credential plugins (`gke-gcloud-auth-plugin`, `kubelogin`) aren't in the image, so use a token or certificate. Clusters whose API server the hub cannot reach will need agent mode (see the [roadmap](ROADMAP.md)).

## Checking a new cluster

After `helm upgrade`, the UI shows a yellow banner if any source of any cluster is failing. `GET /api/health` lists every cluster with the state and last error of each source: RayJobs, RayClusters, RayServices, the Ray Jobs API, and the History Server.

## Limits

- Dashboard traffic for remote clusters flows through their API servers. That is fine for interactive use, but large log downloads add API-server load.
- Cluster names are part of stored records and URLs; renaming one orphans its history.
