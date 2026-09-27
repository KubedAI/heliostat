'use client';

import Link from 'next/link';
import type { ReactNode } from 'react';
import { Elapsed } from '@/components/elapsed';
import { PageHeader } from '@/components/page-header';
import { StatusBadge } from '@/components/status-badge';
import {
  formatGpu,
  formatInstanceTypes,
  formatFullDateTime,
  formatNumber,
  formatUsd,
  JOB_TONE,
} from '@/lib/client/format';
import { useApi } from '@/lib/client/use-api';
import { isTerminal, type HealthView, type JobView } from '@/lib/domain/types';
import { jobLinkPath } from '@/lib/links';
import { DiagnosticsLink } from './diagnostics-link';

/** Display name and region of a configured Kubernetes cluster, falling back to its stable name. */
function clusterLabel(name: string, health?: HealthView): string {
  const info = health?.clusters.find((c) => c.name === name);
  if (!info) return name;
  return info.region ? `${info.displayName} (${info.region})` : info.displayName;
}

const SOURCE_LABEL: Record<JobView['kind'], string> = {
  RayJob: 'RayJob custom resource',
  Submission: 'Ray Jobs API submission',
  Driver: 'Interactive driver',
};

const DIAGNOSTICS_TEXT: Record<JobView['diagnostics'], string> = {
  live: 'The Ray cluster is running. Its dashboard has this job’s logs, tasks, actors, and metrics.',
  history:
    'The Ray cluster has been deleted. The Ray History Server archived its session: logs, tasks, and actors are available read-only.',
  none: '',
};

function Field({ label, children }: { label: string; children: ReactNode }) {
  return (
    <div>
      <dt>{label}</dt>
      <dd>{children ?? '—'}</dd>
    </div>
  );
}

export function JobDetail({ id }: { id: string }) {
  const {
    data: job,
    error,
    loading,
  } = useApi<JobView>(`/api/jobs/${encodeURIComponent(id)}`, 5_000);
  const { data: health } = useApi<HealthView>('/api/health', 60_000);

  if (!job) {
    return (
      <>
        <nav className="breadcrumb" aria-label="Breadcrumb">
          <Link href="/jobs">← Jobs</Link>
        </nav>
        {loading ? (
          <p aria-busy="true">Loading job…</p>
        ) : (
          <div className="notice error">{error ?? 'Job not found.'}</div>
        )}
      </>
    );
  }

  const failed = job.status === 'FAILED' || job.status === 'UNKNOWN' || job.status === 'STOPPED';
  const usage = job.reportedUsage;

  return (
    <>
      <nav className="breadcrumb" aria-label="Breadcrumb">
        <Link href="/jobs">← Jobs</Link>
      </nav>
      <PageHeader
        eyebrow={`${clusterLabel(job.cluster, health)} / ${job.namespace}`}
        title={job.name}
        lede={
          <>
            <StatusBadge label={job.status.toLowerCase()} tone={JOB_TONE[job.status]} /> {job.phase}
            {!job.present && ' · no longer in the cluster'}
          </>
        }
        actions={
          <DiagnosticsLink
            href={jobLinkPath(job.id)}
            kind={job.diagnostics}
            reason={job.diagnosticsReason}
          />
        }
      />

      {error && <div className="notice error">Could not refresh: {error}</div>}

      <section className="card">
        <dl className="detail-grid">
          <Field label="Source">{SOURCE_LABEL[job.kind]}</Field>
          <Field label="K8s cluster">
            {clusterLabel(job.cluster, health)}
            <span className="field-sub mono">{job.cluster}</span>
          </Field>
          <Field label="Model">{job.model}</Field>
          <Field label="Owner">{job.owner}</Field>
          <Field label="Workload type">{job.workloadType}</Field>
          <Field label="Ray cluster">{job.rayClusterName}</Field>
          <Field label="Submission ID">
            {job.submissionId && <span className="mono">{job.submissionId}</span>}
          </Field>
          <Field label="Ray job ID">
            {job.rayJobId && <span className="mono">{job.rayJobId}</span>}
          </Field>
          <Field label="GPU">{formatGpu(job.compute)}</Field>
          <Field label="Instance type">{formatInstanceTypes(job.compute)}</Field>
          <Field label="Created">{formatFullDateTime(job.createdAt)}</Field>
          <Field label="Started">{formatFullDateTime(job.startedAt)}</Field>
          <Field label="Finished">{formatFullDateTime(job.finishedAt)}</Field>
          <Field label={isTerminal(job.status) ? 'Total runtime' : 'Elapsed'}>
            <Elapsed start={job.startedAt ?? job.createdAt} end={job.finishedAt} />
          </Field>
        </dl>
      </section>

      {(job.message || job.reason) && (
        <section className="card section" aria-labelledby="message-title">
          <h2 className="section-title" id="message-title">
            {failed ? 'Failure details' : 'Status message'}
            {job.reason && <small>{job.reason}</small>}
          </h2>
          {job.message && <pre className="code-block">{job.message}</pre>}
        </section>
      )}

      <section className="card section" aria-labelledby="diagnostics-title">
        <h2 className="section-title" id="diagnostics-title">
          Ray diagnostics
        </h2>
        <div className="diagnostics">
          <p>
            {job.diagnostics === 'none' ? job.diagnosticsReason : DIAGNOSTICS_TEXT[job.diagnostics]}
          </p>
          <DiagnosticsLink
            href={jobLinkPath(job.id)}
            kind={job.diagnostics}
            reason={job.diagnosticsReason}
          />
        </div>
      </section>

      {job.entrypoint && (
        <section className="card section" aria-labelledby="entrypoint-title">
          <h2 className="section-title" id="entrypoint-title">
            Entrypoint
          </h2>
          <pre className="code-block">{job.entrypoint}</pre>
        </section>
      )}

      {usage && (
        <section className="card section" aria-labelledby="usage-title">
          <h2 className="section-title" id="usage-title">
            Reported usage <small>Self-reported by the workload · not measured</small>
          </h2>
          <dl className="detail-grid">
            <Field label="Input tokens">{formatNumber(usage.inputTokens)}</Field>
            <Field label="Output tokens">{formatNumber(usage.outputTokens)}</Field>
            <Field label="Total tokens">{formatNumber(usage.totalTokens)}</Field>
            <Field label="Estimated cost">{formatUsd(usage.estimatedCostUsd)}</Field>
          </dl>
        </section>
      )}
    </>
  );
}
