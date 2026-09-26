'use client';

import Link from 'next/link';
import { PageHeader } from '@/components/page-header';
import { GpuCell } from '@/components/gpu-cell';
import { StatusBadge } from '@/components/status-badge';
import { clusterStateTone, formatDateTime, formatRelative } from '@/lib/client/format';
import { useApi } from '@/lib/client/use-api';
import type { ListResponse, RayClusterView } from '@/lib/domain/types';
import { ClusterCell } from '@/components/cluster-cell';

function ownerLabel(cluster: RayClusterView) {
  if (!cluster.owner) return 'Standalone';
  return `${cluster.owner.kind} ${cluster.owner.name}`;
}

export function ClustersView() {
  const { data, error, loading } = useApi<ListResponse<RayClusterView>>('/api/clusters');
  const items = [...(data?.items ?? [])].sort((a, b) => b.createdAt.localeCompare(a.createdAt));

  return (
    <>
      <PageHeader
        eyebrow="Infrastructure"
        title="Ray clusters"
        lede="Every running Ray cluster, owned by a RayJob, a RayService, or shared as standalone."
      />
      {error && <div className="notice error">Could not refresh clusters: {error}</div>}
      <section className="card">
        <div className="table-scroll">
          <table>
            <caption className="visually-hidden">Live Ray clusters</caption>
            <thead>
              <tr>
                <th scope="col">Ray cluster</th>
                <th scope="col">K8s cluster</th>
                <th scope="col">State</th>
                <th scope="col">Owner</th>
                <th scope="col">Workers</th>
                <th scope="col">GPU</th>
                <th scope="col">Active jobs</th>
                <th scope="col">Created</th>
                <th scope="col" className="action">
                  <span className="visually-hidden">Ray Dashboard</span>
                </th>
              </tr>
            </thead>
            <tbody>
              {items.map((cluster) => (
                <tr key={cluster.id}>
                  <td>
                    <span className="primary-text">{cluster.name}</span>
                    <span className="secondary-text">
                      {[cluster.namespace, cluster.rayVersion && `Ray ${cluster.rayVersion}`]
                        .filter(Boolean)
                        .join(' · ')}
                    </span>
                  </td>
                  <td>
                    <ClusterCell name={cluster.cluster} clusters={data?.clusters} />
                  </td>
                  <td>
                    <StatusBadge label={cluster.state} tone={clusterStateTone(cluster.state)} />
                    <span className="secondary-text">
                      {cluster.historyEnabled ? 'History archived' : 'Not archived'}
                    </span>
                  </td>
                  <td>{ownerLabel(cluster)}</td>
                  <td className="numeric">
                    <span className="primary-text">
                      {cluster.workers.ready} / {cluster.workers.desired}
                    </span>
                    <span className="secondary-text">ready / desired</span>
                  </td>
                  <td>
                    <GpuCell compute={cluster.compute} />
                    <span className="secondary-text">
                      {[cluster.desired.cpu && `${cluster.desired.cpu} CPU`, cluster.desired.memory]
                        .filter(Boolean)
                        .join(' · ') || '—'}
                    </span>
                  </td>
                  <td className="numeric">
                    {cluster.activeJobs ? (
                      <Link
                        href={`/jobs?q=${encodeURIComponent(cluster.name)}&namespace=${encodeURIComponent(cluster.namespace)}`}
                      >
                        {cluster.activeJobs}
                      </Link>
                    ) : (
                      '0'
                    )}
                  </td>
                  <td>
                    <span className="primary-text">{formatRelative(cluster.createdAt)}</span>
                    <span className="secondary-text">{formatDateTime(cluster.createdAt)}</span>
                  </td>
                  <td className="action">
                    {cluster.dashboardHref ? (
                      <a
                        className="button small"
                        href={cluster.dashboardHref}
                        target="_blank"
                        rel="noopener noreferrer"
                      >
                        Dashboard ↗
                      </a>
                    ) : (
                      <span
                        className="button small"
                        aria-disabled="true"
                        title="The head service is not ready yet."
                      >
                        No dashboard
                      </span>
                    )}
                  </td>
                </tr>
              ))}
              {items.length === 0 && (
                <tr>
                  <td colSpan={9} className="empty">
                    {loading ? 'Loading clusters…' : <strong>No Ray clusters are running.</strong>}
                  </td>
                </tr>
              )}
            </tbody>
          </table>
        </div>
      </section>
    </>
  );
}
