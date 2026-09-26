import { formatGpu, formatInstanceTypes } from '@/lib/client/format';
import type { ComputeIntent } from '@/lib/domain/types';

/** GPU count and model, with the requested instance type underneath. */
export function GpuCell({ compute }: { compute: ComputeIntent }) {
  const instances = formatInstanceTypes(compute);
  return (
    <>
      <span className="primary-text">{formatGpu(compute)}</span>
      <span className="secondary-text" title={instances}>
        {instances}
      </span>
    </>
  );
}
