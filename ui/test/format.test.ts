import { describe, expect, it } from 'vitest';
import { formatGpu, formatInstanceTypes } from '@/lib/client/format';

describe('GPU column formatting', () => {
  it.each([
    [{ instanceTypes: ['g6e.12xlarge'], gpus: 4, gpuModel: 'L40S' }, '4 × L40S', 'g6e.12xlarge'],
    [{ instanceTypes: ['g6e.*'], gpus: 4 }, '4 GPU', 'g6e.*'],
    [{ instanceTypes: [], gpus: 0 }, 'CPU only', 'any instance type'],
  ])('%j', (compute, gpu, instances) => {
    expect(formatGpu(compute)).toBe(gpu);
    expect(formatInstanceTypes(compute)).toBe(instances);
  });
});
