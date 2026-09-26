import type { ClusterInfo } from '@/lib/domain/types';

/** Kubernetes cluster name with its region underneath, for table cells. */
export function ClusterCell({ name, clusters }: { name: string; clusters?: ClusterInfo[] }) {
  const info = clusters?.find((cluster) => cluster.name === name);
  return (
    <>
      <span className="primary-text" title={name}>
        {info?.displayName ?? name}
      </span>
      <span className="secondary-text">{info?.region ?? '—'}</span>
    </>
  );
}
