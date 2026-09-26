import type { Metadata } from 'next';
import { Suspense } from 'react';
import { JobsView } from '@/components/jobs/jobs-view';

export const metadata: Metadata = { title: 'Jobs' };

export default function JobsPage() {
  // useSearchParams() in JobsView requires a Suspense boundary.
  return (
    <Suspense>
      <JobsView />
    </Suspense>
  );
}
