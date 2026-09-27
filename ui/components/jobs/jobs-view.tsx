'use client';

import Link from 'next/link';
import { usePathname, useRouter, useSearchParams } from 'next/navigation';
import { useEffect, useState } from 'react';
import { ClusterCell } from '@/components/cluster-cell';
import { Elapsed } from '@/components/elapsed';
import { EmptyState } from '@/components/empty-state';
import { GpuCell } from '@/components/gpu-cell';
import { PageHeader } from '@/components/page-header';
import { StatusBadge } from '@/components/status-badge';
import { formatDateTime, formatRelative, JOB_TONE } from '@/lib/client/format';
import { useSourceStatus } from '@/lib/client/source-status';
import { useApi } from '@/lib/client/use-api';
import {
  TIME_WINDOWS,
  type ClusterInfo,
  type JobPage,
  type JobStatus,
  type JobView,
} from '@/lib/domain/types';
import { jobLinkPath, jobPagePath } from '@/lib/links';
import { DiagnosticsLink } from './diagnostics-link';

const STATUS_CARDS: { status?: JobStatus; label: string; color: string }[] = [
  { label: 'All jobs', color: 'var(--ink)' },
  { status: 'RUNNING', label: 'Running', color: 'var(--blue)' },
  { status: 'PENDING', label: 'Pending', color: 'var(--accent)' },
  { status: 'FAILED', label: 'Failed', color: 'var(--coral)' },
  { status: 'SUCCEEDED', label: 'Succeeded', color: 'var(--mint)' },
];

const WINDOW_LABELS: Record<(typeof TIME_WINDOWS)[number], string> = {
  '24h': '24h',
  '7d': '7 days',
  '30d': '30 days',
  all: 'All',
};

/** Reads and writes the list's filters in the URL so every view is shareable. */
function useFilters() {
  const params = useSearchParams();
  const router = useRouter();
  const pathname = usePathname();
  const set = (changes: Record<string, string | undefined>, keepCursor = false) => {
    const next = new URLSearchParams(params);
    for (const [key, value] of Object.entries(changes)) {
      if (value) next.set(key, value);
      else next.delete(key);
    }
    if (!keepCursor) next.delete('cursor');
    const query = next.toString();
    router.replace(query ? `${pathname}?${query}` : pathname, { scroll: false });
  };
  return { params, set };
}

export function JobsView() {
  const { params, set } = useFilters();
  const status = params.get('status') ?? '';
  const window = params.get('window') ?? '7d';
  const [search, setSearch] = useState(params.get('q') ?? '');
  const [cursorStack, setCursorStack] = useState<string[]>([]);
  const rayCluster = params.get('rayCluster');
  const sourceStatus = useSourceStatus('rayJobs');

  // Page history belongs to one set of filters: start over whenever anything but the cursor changes.
  const withoutCursor = new URLSearchParams(params);
  withoutCursor.delete('cursor');
  const filterKey = withoutCursor.toString();
  const [pagedFilters, setPagedFilters] = useState(filterKey);
  if (pagedFilters !== filterKey) {
    setPagedFilters(filterKey);
    setCursorStack([]);
  }

  // Debounce typing into the URL.
  const urlQuery = params.get('q') ?? '';
  useEffect(() => {
    if (search === urlQuery) return;
    const timer = setTimeout(() => set({ q: search || undefined }), 250);
    return () => clearTimeout(timer);
    // eslint-disable-next-line react-hooks/exhaustive-deps -- `set` changes every render
  }, [search, urlQuery]);

  const apiParams = new URLSearchParams(params);
  if (!apiParams.has('window')) apiParams.set('window', window);
  const { data, error, loading, updatedAt } = useApi<JobPage>(`/api/jobs?${apiParams}`);

  const facets = data?.facets;
  const total = facets
    ? Object.values(facets.status).reduce((sum, n) => sum + (n ?? 0), 0)
    : undefined;
  const clusters = data?.clusters;
  const multiCluster = (clusters?.length ?? 0) > 1;

  const goOlder = () => {
    if (!data?.nextCursor) return;
    setCursorStack((stack) => [...stack, params.get('cursor') ?? '']);
    set({ cursor: data.nextCursor }, true);
  };
  const goNewer = () => {
    const previous = cursorStack.at(-1);
    setCursorStack((stack) => stack.slice(0, -1));
    set({ cursor: previous || undefined }, true);
  };

  return (
    <>
      <PageHeader
        eyebrow="Workloads"
        title="Jobs"
        lede="Every Ray job across all clusters: RayJobs, ray job submit, and notebooks. Open one for its failure reason or Ray Dashboard."
      />

      <div className="metrics" role="group" aria-label="Filter by status">
        {STATUS_CARDS.map((card) => {
          const count = card.status ? (facets?.status[card.status] ?? 0) : total;
          const pressed = (card.status ?? '') === status;
          return (
            <button
              key={card.label}
              type="button"
              className="metric"
              style={{ '--metric-color': card.color } as React.CSSProperties}
              aria-pressed={pressed}
              onClick={() => set({ status: pressed ? undefined : card.status })}
            >
              <span>{card.label}</span>
              <strong>{count ?? '—'}</strong>
            </button>
          );
        })}
      </div>

      {error && (
        <div className="notice error" role="alert">
          <strong>Could not refresh jobs.</strong> {error}
        </div>
      )}

      <section className="card" aria-labelledby="jobs-table-title">
        {rayCluster && (
          <div className="filter-chips">
            <span className="filter-chip">
              Ray cluster: {params.get('namespace') ? `${params.get('namespace')}/` : ''}
              {rayCluster}
              {params.get('cluster') ? ` in ${params.get('cluster')}` : ''}
              <button
                type="button"
                aria-label="Remove Ray cluster filter"
                onClick={() => set({ rayCluster: undefined })}
              >
                ×
              </button>
            </span>
          </div>
        )}
        <div className="toolbar">
          <label className="field grow">
            <span className="field-label">Search</span>
            <input
              type="search"
              value={search}
              onChange={(event) => setSearch(event.target.value)}
              placeholder="Name, model, owner, namespace, submission ID"
            />
          </label>
          <label className="field">
            <span className="field-label">Namespace</span>
            <select
              value={params.get('namespace') ?? ''}
              onChange={(e) => set({ namespace: e.target.value })}
            >
              <option value="">All</option>
              {facets?.namespaces.map((ns) => (
                <option key={ns}>{ns}</option>
              ))}
            </select>
          </label>
          {multiCluster && (
            <label className="field">
              <span className="field-label">Cluster</span>
              <select
                value={params.get('cluster') ?? ''}
                onChange={(e) => set({ cluster: e.target.value })}
              >
                <option value="">All</option>
                {clusters?.map((cluster) => (
                  <option key={cluster.name} value={cluster.name}>
                    {cluster.displayName}
                  </option>
                ))}
              </select>
            </label>
          )}
          <label className="field">
            <span className="field-label">Source</span>
            <select
              value={params.get('kind') ?? ''}
              onChange={(e) => set({ kind: e.target.value })}
            >
              <option value="">All</option>
              <option value="RayJob">RayJob CR</option>
              <option value="Submission">Jobs API submission</option>
              <option value="Driver">Interactive driver</option>
            </select>
          </label>
          <div className="segmented" role="group" aria-label="Created within">
            {TIME_WINDOWS.map((w) => (
              <button
                key={w}
                type="button"
                aria-pressed={w === window}
                onClick={() => set({ window: w === '7d' ? undefined : w })}
              >
                {WINDOW_LABELS[w]}
              </button>
            ))}
          </div>
        </div>

        <div className="table-scroll">
          <table>
            <caption id="jobs-table-title" className="visually-hidden">
              Jobs, newest first
            </caption>
            <thead>
              <tr>
                <th scope="col">Job</th>
                <th scope="col">K8s cluster</th>
                <th scope="col">Status</th>
                <th scope="col">Model</th>
                <th scope="col">Runtime</th>
                <th scope="col">GPU</th>
                <th scope="col">Created</th>
                <th scope="col" className="action">
                  <span className="visually-hidden">Ray Dashboard</span>
                </th>
              </tr>
            </thead>
            <tbody>
              {data?.items.map((job) => (
                <JobRow key={job.id} job={job} clusters={clusters} />
              ))}
              {data && data.items.length === 0 && (
                <tr>
                  <td colSpan={8} className="empty">
                    <EmptyState
                      status={sourceStatus}
                      source="RayJobs"
                      empty="No jobs match these filters."
                      hint="Jobs appear here within seconds of being created, however they were submitted."
                    />
                  </td>
                </tr>
              )}
              {!data && (
                <tr>
                  <td colSpan={8} className="empty" aria-busy={loading}>
                    {loading ? 'Loading jobs…' : 'No data.'}
                  </td>
                </tr>
              )}
            </tbody>
          </table>
        </div>

        <div className="table-footer">
          <span aria-live="polite">
            {updatedAt ? `Updated ${new Date(updatedAt).toLocaleTimeString()}` : ' '}
          </span>
          <span>
            <button
              type="button"
              className="button small"
              onClick={goNewer}
              disabled={!params.get('cursor')}
            >
              ← Newer
            </button>{' '}
            <button
              type="button"
              className="button small"
              onClick={goOlder}
              disabled={!data?.nextCursor}
            >
              Older →
            </button>
          </span>
        </div>
      </section>
    </>
  );
}

function JobRow({ job, clusters }: { job: JobView; clusters?: ClusterInfo[] }) {
  const location = [job.namespace, job.owner].filter(Boolean).join(' · ');
  return (
    <tr>
      <td>
        <Link className="row-link" href={jobPagePath(job.id)}>
          <span className="primary-text" title={job.name}>
            {job.name}
          </span>
        </Link>
        <span className="secondary-text">
          <span className="kind">{job.kind}</span>
          {location}
        </span>
      </td>
      <td>
        <ClusterCell name={job.cluster} clusters={clusters} />
      </td>
      <td>
        <StatusBadge
          label={job.status.toLowerCase()}
          tone={JOB_TONE[job.status]}
          title={job.reason}
        />
        <span className="secondary-text">{job.reason ?? job.phase}</span>
      </td>
      <td>
        <span className="primary-text">{job.model ?? '—'}</span>
        <span className="secondary-text" title={job.rayClusterName}>
          {job.rayClusterName ?? 'No Ray cluster yet'}
        </span>
      </td>
      <td className="numeric">
        <span className="primary-text">
          <Elapsed start={job.startedAt ?? job.createdAt} end={job.finishedAt} />
        </span>
        <span className="secondary-text">
          {job.finishedAt ? 'Total' : job.startedAt ? 'Elapsed' : 'Waiting'}
        </span>
      </td>
      <td>
        <GpuCell compute={job.compute} />
      </td>
      <td>
        <span className="primary-text" title={job.createdAt}>
          {formatRelative(job.createdAt)}
        </span>
        <span className="secondary-text">{formatDateTime(job.createdAt)}</span>
      </td>
      <td className="action">
        <DiagnosticsLink
          href={jobLinkPath(job.id)}
          kind={job.diagnostics}
          reason={job.diagnosticsReason}
          compact
        />
      </td>
    </tr>
  );
}
