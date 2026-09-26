'use client';

import { useSearchParams } from 'next/navigation';
import { Suspense } from 'react';
import { JobDetail } from '@/components/jobs/job-detail';

function JobPageContent() {
  const id = useSearchParams().get('id');
  if (!id) return <div className="notice error">No job selected.</div>;
  return <JobDetail id={id} />;
}

export default function JobPage() {
  // useSearchParams() requires a Suspense boundary in statically exported pages.
  return (
    <Suspense>
      <JobPageContent />
    </Suspense>
  );
}
