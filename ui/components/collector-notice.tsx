'use client';

import { useApi } from '@/lib/client/use-api';
import type { ClusterHealth, HealthView, SourceHealth } from '@/lib/domain/types';

const SOURCES: [keyof ClusterHealth, string][] = [
  ['rayJobs', 'RayJobs'],
  ['rayClusters', 'RayClusters'],
  ['rayServices', 'RayServices'],
  ['submissions', 'Ray Jobs API'],
  ['historyServer', 'Ray History Server'],
];

/** Shown only when a Kubernetes cluster or one of its data sources is failing. */
export function CollectorNotice() {
  const { data } = useApi<HealthView>('/api/health', 30_000);
  const problems = (data?.clusters ?? []).flatMap((cluster) =>
    SOURCES.flatMap(([key, label]) => {
      const source = cluster[key] as SourceHealth | undefined;
      return source?.error ? [`${cluster.displayName} · ${label}: ${source.error}`] : [];
    }),
  );
  if (!problems.length) return null;
  return (
    <div className="notice" role="status">
      <div>
        <strong>Some data may be stale.</strong>
        <ul className="notice-list">
          {problems.map((problem) => (
            <li key={problem}>{problem}</li>
          ))}
        </ul>
      </div>
    </div>
  );
}
