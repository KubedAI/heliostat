'use client';

import { useEffect, useState } from 'react';
import { elapsedSeconds, formatDuration } from '@/lib/client/format';

/** Duration from `start` to `end`; ticks every second while `end` is unset. */
export function Elapsed({ start, end }: { start?: string; end?: string }) {
  const [now, setNow] = useState(() => Date.now());
  const live = Boolean(start && !end);

  useEffect(() => {
    if (!live) return;
    const timer = setInterval(() => setNow(Date.now()), 1000);
    return () => clearInterval(timer);
  }, [live]);

  return <span suppressHydrationWarning>{formatDuration(elapsedSeconds(start, end, now))}</span>;
}
