import type { SourceStatus } from '@/lib/client/source-status';

/**
 * Empty-table message that never claims "nothing here" while a cluster is still syncing or
 * failing, which would present an unknown state as a healthy one.
 */
export function EmptyState({
  status,
  source,
  empty,
  hint,
}: {
  status: SourceStatus;
  /** Human name of the source, for example "RayClusters". */
  source: string;
  empty: string;
  hint?: string;
}) {
  if (status.failing.length > 0) {
    return (
      <>
        <strong>
          Could not read {source} from {status.failing.join(', ')}.
        </strong>
        Results may be incomplete; see the notice at the top of the page.
      </>
    );
  }
  if (status.loading || status.syncing.length > 0) {
    return (
      <>
        <strong>Syncing {source}…</strong>
        {status.syncing.length > 0 && `Waiting for ${status.syncing.join(', ')}.`}
      </>
    );
  }
  return (
    <>
      <strong>{empty}</strong>
      {hint}
    </>
  );
}
