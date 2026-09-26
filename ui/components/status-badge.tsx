import type { Tone } from '@/lib/client/format';

export function StatusBadge({ label, tone, title }: { label: string; tone: Tone; title?: string }) {
  return (
    <span className={`badge tone-${tone}`} title={title}>
      {label}
    </span>
  );
}
