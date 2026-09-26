'use client';

import Link from 'next/link';
import { useApi } from '@/lib/client/use-api';
import type { HealthView, JobPage, JobStatus } from '@/lib/domain/types';

const STATUS_TILES: { status: JobStatus; label: string; color: string }[] = [
  { status: 'RUNNING', label: 'Running', color: 'var(--blue)' },
  { status: 'PENDING', label: 'Pending', color: 'var(--accent)' },
  { status: 'SUCCEEDED', label: 'Succeeded', color: 'var(--mint)' },
  { status: 'FAILED', label: 'Failed', color: 'var(--coral)' },
];

/** Below-the-fold totals: jobs by status over the last 24 hours, plus live cluster counts. */
export function JobSummary() {
  const { data: health } = useApi<HealthView>('/api/health', 15_000);
  const { data: today } = useApi<JobPage>('/api/jobs?window=24h&limit=1');
  const clustersUp = health?.clusters.filter((c) => c.rayJobs.synced && !c.rayJobs.error).length;

  return (
    <section aria-labelledby="summary-title">
      <header className="summary-header">
        <h2 id="summary-title">Job summary</h2>
        <span className="field-label">Last 24 hours</span>
      </header>
      <nav className="metrics home-stats" aria-label="Jobs by status, last 24 hours">
        {STATUS_TILES.map((tile) => (
          <Link
            key={tile.status}
            href={`/jobs?status=${tile.status}&window=24h`}
            className="metric"
            style={{ '--metric-color': tile.color } as React.CSSProperties}
          >
            <span>{tile.label}</span>
            <strong>{today ? (today.facets.status[tile.status] ?? 0) : '—'}</strong>
          </Link>
        ))}
        <Link
          href="/clusters"
          className="metric"
          style={{ '--metric-color': 'var(--ink)' } as React.CSSProperties}
        >
          <span>Ray / K8s clusters</span>
          <strong>{health ? `${health.counts.rayClusters} / ${clustersUp}` : '—'}</strong>
        </Link>
      </nav>
    </section>
  );
}
