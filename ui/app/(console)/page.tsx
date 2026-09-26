import type { Metadata } from 'next';
import { ArchitectureDiagram } from '@/components/home/architecture-diagram';
import { JobSummary } from '@/components/home/job-summary';
import { PageHeader } from '@/components/page-header';

export const metadata: Metadata = { title: 'Overview' };

export default function OverviewPage() {
  return (
    <>
      <PageHeader eyebrow="Ray fleet console" title="Every Ray workload, wherever it runs." />
      <ArchitectureDiagram />
      <JobSummary />
    </>
  );
}
