import type { Diagnostics } from '@/lib/domain/types';

const LABELS: Record<Exclude<Diagnostics['kind'], 'none'>, { full: string; compact: string }> = {
  live: { full: 'Open Ray Dashboard', compact: 'Dashboard ↗' },
  history: { full: 'Open archived dashboard', compact: 'History ↗' },
};

/**
 * Link to a job's Ray Dashboard page through Heliostat's `/go/job` resolver, or a disabled
 * control that explains why no diagnostics are available.
 */
export function DiagnosticsLink({
  href,
  kind,
  reason,
  compact = false,
}: {
  href: string;
  kind: Diagnostics['kind'];
  reason?: string;
  compact?: boolean;
}) {
  if (kind === 'none') {
    return (
      <span className="button small" aria-disabled="true" title={reason}>
        {compact ? 'No dashboard' : 'Dashboard unavailable'}
      </span>
    );
  }
  const label = LABELS[kind];
  return (
    <a
      className={`button small${kind === 'live' ? ' primary' : ''}`}
      href={href}
      target="_blank"
      rel="noopener noreferrer"
      title={
        kind === 'live'
          ? 'Live Ray Dashboard (opens in a new tab)'
          : 'Ray History Server archive (opens in a new tab)'
      }
    >
      {compact ? label.compact : label.full}
    </a>
  );
}
