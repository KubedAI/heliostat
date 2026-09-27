'use client';

import { PageHeader } from '@/components/page-header';
import { StatusBadge } from '@/components/status-badge';
import { ENDPOINT_TONE, formatDateTime, formatRelative } from '@/lib/client/format';
import { GpuCell } from '@/components/gpu-cell';
import { useSourceStatus } from '@/lib/client/source-status';
import { useApi } from '@/lib/client/use-api';
import { EmptyState } from '@/components/empty-state';
import type { EndpointView, ListResponse } from '@/lib/domain/types';
import { ClusterCell } from '@/components/cluster-cell';

export function EndpointsView() {
  const sourceStatus = useSourceStatus('rayServices');
  const { data, error } = useApi<ListResponse<EndpointView>>('/api/endpoints');
  const items = [...(data?.items ?? [])].sort((a, b) => a.name.localeCompare(b.name));

  return (
    <>
      <PageHeader
        eyebrow="Serving"
        title="Endpoints"
        lede="RayService deployments serving online inference, with the in-cluster URL clients call."
      />
      {error && <div className="notice error">Could not refresh endpoints: {error}</div>}
      <section className="card">
        <div className="table-scroll">
          <table>
            <caption className="visually-hidden">RayService endpoints</caption>
            <thead>
              <tr>
                <th scope="col">Endpoint</th>
                <th scope="col">K8s cluster</th>
                <th scope="col">Status</th>
                <th scope="col">Model</th>
                <th scope="col">Applications</th>
                <th scope="col">GPU</th>
                <th scope="col">Serve URL (in-cluster)</th>
                <th scope="col">Created</th>
                <th scope="col" className="action">
                  <span className="visually-hidden">Ray Dashboard</span>
                </th>
              </tr>
            </thead>
            <tbody>
              {items.map((endpoint) => (
                <tr key={endpoint.id}>
                  <td>
                    <span className="primary-text">{endpoint.name}</span>
                    <span className="secondary-text">
                      {[endpoint.namespace, endpoint.owner].filter(Boolean).join(' · ')}
                    </span>
                  </td>
                  <td>
                    <ClusterCell name={endpoint.cluster} clusters={data?.clusters} />
                  </td>
                  <td>
                    <StatusBadge
                      label={endpoint.status.toLowerCase()}
                      tone={ENDPOINT_TONE[endpoint.status]}
                    />
                    <span className="secondary-text">{endpoint.statusDetail ?? ''}</span>
                  </td>
                  <td>
                    <span className="primary-text">{endpoint.model ?? '—'}</span>
                    <span className="secondary-text">
                      {endpoint.activeRayClusterName ?? 'No active cluster'}
                      {endpoint.pendingRayClusterName && ` → ${endpoint.pendingRayClusterName}`}
                    </span>
                  </td>
                  <td>
                    {endpoint.applications.length
                      ? endpoint.applications.map((app) => (
                          <span key={app.name} className="secondary-text">
                            {app.name}: {app.status.toLowerCase()}
                          </span>
                        ))
                      : '—'}
                  </td>
                  <td>
                    <GpuCell compute={endpoint.compute} />
                  </td>
                  <td>
                    <span className="mono">{endpoint.serveUrl}</span>
                  </td>
                  <td>
                    <span className="primary-text">{formatRelative(endpoint.createdAt)}</span>
                    <span className="secondary-text">{formatDateTime(endpoint.createdAt)}</span>
                  </td>
                  <td className="action">
                    {endpoint.dashboardHref ? (
                      <a
                        className="button small"
                        href={endpoint.dashboardHref}
                        target="_blank"
                        rel="noopener noreferrer"
                      >
                        Dashboard ↗
                      </a>
                    ) : (
                      <span
                        className="button small"
                        aria-disabled="true"
                        title="No active Ray cluster yet."
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
                    {data ? (
                      <EmptyState
                        status={sourceStatus}
                        source="RayServices"
                        empty="No RayService endpoints."
                        hint="RayServices appear here as soon as they are created."
                      />
                    ) : (
                      'Loading endpoints…'
                    )}
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
