import type { ComputeIntent, EndpointStatus, JobStatus } from '@/lib/domain/types';

export type Tone = 'ok' | 'run' | 'wait' | 'bad' | 'idle';

export const JOB_TONE: Record<JobStatus, Tone> = {
  SUCCEEDED: 'ok',
  RUNNING: 'run',
  PENDING: 'wait',
  SUSPENDED: 'wait',
  FAILED: 'bad',
  STOPPED: 'idle',
  UNKNOWN: 'idle',
};

export const ENDPOINT_TONE: Record<EndpointStatus, Tone> = {
  READY: 'ok',
  UPGRADING: 'run',
  DEPLOYING: 'wait',
  UNHEALTHY: 'bad',
  UNKNOWN: 'idle',
};

export function clusterStateTone(state: string): Tone {
  if (state === 'ready') return 'ok';
  if (state === 'failed') return 'bad';
  if (state === 'suspended') return 'idle';
  return 'wait';
}

export function formatDuration(seconds: number | undefined): string {
  if (seconds === undefined || !Number.isFinite(seconds) || seconds < 0) return '—';
  const s = Math.floor(seconds);
  const days = Math.floor(s / 86_400);
  const hours = Math.floor((s % 86_400) / 3600);
  const minutes = Math.floor((s % 3600) / 60);
  const secs = s % 60;
  if (days) return `${days}d ${hours}h`;
  if (hours) return `${hours}h ${minutes}m`;
  if (minutes) return `${minutes}m ${secs}s`;
  return `${secs}s`;
}

/** Seconds between `start` and `end` (or now). Undefined when `start` is missing. */
export function elapsedSeconds(start?: string, end?: string, now = Date.now()): number | undefined {
  if (!start) return undefined;
  const from = Date.parse(start);
  const to = end ? Date.parse(end) : now;
  return Number.isNaN(from) || Number.isNaN(to) ? undefined : Math.max(0, (to - from) / 1000);
}

const dateTime = new Intl.DateTimeFormat(undefined, {
  month: 'short',
  day: 'numeric',
  hour: 'numeric',
  minute: '2-digit',
});
const fullDateTime = new Intl.DateTimeFormat(undefined, { dateStyle: 'medium', timeStyle: 'long' });

export function formatDateTime(iso?: string): string {
  return iso ? dateTime.format(new Date(iso)) : '—';
}

export function formatFullDateTime(iso?: string): string {
  return iso ? fullDateTime.format(new Date(iso)) : '—';
}

const relative = new Intl.RelativeTimeFormat(undefined, { numeric: 'auto' });

export function formatRelative(iso?: string, now = Date.now()): string {
  if (!iso) return '—';
  const seconds = (Date.parse(iso) - now) / 1000;
  const abs = Math.abs(seconds);
  if (abs < 45) return 'just now';
  if (abs < 3600) return relative.format(Math.round(seconds / 60), 'minute');
  if (abs < 86_400) return relative.format(Math.round(seconds / 3600), 'hour');
  return relative.format(Math.round(seconds / 86_400), 'day');
}

export function formatNumber(value?: number): string {
  if (value === undefined) return '—';
  return new Intl.NumberFormat(undefined, {
    notation: value >= 100_000 ? 'compact' : 'standard',
    maximumFractionDigits: 1,
  }).format(value);
}

export function formatUsd(value?: number): string {
  if (value === undefined) return '—';
  return new Intl.NumberFormat(undefined, {
    style: 'currency',
    currency: 'USD',
    maximumFractionDigits: value < 1 ? 4 : 2,
  }).format(value);
}

/** Main line of the GPU column: `4 × L40S`, `4 GPU`, or `CPU only`. */
export function formatGpu(compute: ComputeIntent): string {
  if (!compute.gpus) return 'CPU only';
  return compute.gpuModel ? `${compute.gpus} × ${compute.gpuModel}` : `${compute.gpus} GPU`;
}

export function formatInstanceTypes(compute: ComputeIntent): string {
  return compute.instanceTypes.length ? compute.instanceTypes.join(', ') : 'any instance type';
}
