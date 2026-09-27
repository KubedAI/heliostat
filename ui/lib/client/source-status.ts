'use client';

import type { ClusterHealth, HealthView } from '@/lib/domain/types';
import { useApi } from './use-api';

export type SourceKey = 'rayJobs' | 'rayClusters' | 'rayServices';

export type SourceStatus = {
  /** True until /api/health has answered once. */
  loading: boolean;
  /** Clusters whose source reports an error. */
  failing: string[];
  /** Clusters whose source has not finished its first sync (and has no error). */
  syncing: string[];
};

/**
 * Whether a data source is healthy in every cluster. Tables use it so an empty result is only
 * described as "nothing running" when every cluster has actually synced.
 */
export function useSourceStatus(key: SourceKey): SourceStatus {
  const { data } = useApi<HealthView>('/api/health', 15_000);
  const clusters: ClusterHealth[] = data?.clusters ?? [];
  return {
    loading: !data,
    failing: clusters.filter((c) => c[key].error).map((c) => c.displayName),
    syncing: clusters.filter((c) => !c[key].error && !c[key].synced).map((c) => c.displayName),
  };
}
