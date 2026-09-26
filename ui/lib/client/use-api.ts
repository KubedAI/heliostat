'use client';

import { useCallback, useEffect, useState } from 'react';

export type ApiState<T> = {
  data?: T;
  error?: string;
  loading: boolean;
  /** Timestamp of the last successful response (including 304s). */
  updatedAt?: number;
  refresh(): void;
};

/**
 * Fetches a JSON endpoint and re-polls it while the tab is visible. Sends `If-None-Match`, so
 * unchanged data costs a `304` and no re-render. Pass `null` as the URL to pause.
 */
export function useApi<T>(url: string | null, intervalMs = 10_000): ApiState<T> {
  const [state, setState] = useState<Omit<ApiState<T>, 'refresh'>>({ loading: url !== null });
  const [tick, setTick] = useState(0);
  const refresh = useCallback(() => setTick((n) => n + 1), []);

  // A new URL is a new resource: drop the cached validator and show loading.
  const [previousUrl, setPreviousUrl] = useState(url);
  if (previousUrl !== url) {
    setPreviousUrl(url);
    setState((s) => ({ ...s, loading: url !== null }));
  }

  useEffect(() => {
    if (url === null) return;
    // The validator belongs to this effect run: a cancelled run must not leave one behind, or
    // the next run would get a 304 without ever having received the body.
    let etag: string | null = null;
    let cancelled = false;
    let timer: ReturnType<typeof setTimeout> | undefined;
    const controller = new AbortController();

    const load = async () => {
      if (document.visibilityState === 'hidden') return schedule();
      try {
        const response = await fetch(url, {
          headers: etag ? { 'if-none-match': etag } : {},
          cache: 'no-store',
          signal: controller.signal,
        });
        if (cancelled) return;
        if (response.status === 304) {
          setState((s) => ({ ...s, loading: false, error: undefined, updatedAt: Date.now() }));
        } else if (response.ok) {
          const data = (await response.json()) as T;
          if (cancelled) return;
          etag = response.headers.get('etag');
          setState({ data, loading: false, updatedAt: Date.now() });
        } else {
          const body = (await response.json().catch(() => ({}))) as { error?: string };
          if (!cancelled) {
            setState((s) => ({
              ...s,
              loading: false,
              error: body.error ?? `Request failed (${response.status})`,
            }));
          }
        }
      } catch (error) {
        if (!cancelled && !controller.signal.aborted) {
          setState((s) => ({
            ...s,
            loading: false,
            error: error instanceof Error ? error.message : 'Network error',
          }));
        }
      }
      schedule();
    };
    const schedule = () => {
      if (!cancelled) timer = setTimeout(load, intervalMs);
    };
    const onVisible = () => {
      if (document.visibilityState === 'visible') {
        clearTimeout(timer);
        void load();
      }
    };

    void load();
    document.addEventListener('visibilitychange', onVisible);
    return () => {
      cancelled = true;
      controller.abort();
      clearTimeout(timer);
      document.removeEventListener('visibilitychange', onVisible);
    };
  }, [url, intervalMs, tick]);

  return { ...state, refresh };
}
