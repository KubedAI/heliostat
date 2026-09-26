import type { Metadata } from 'next';
import { ClustersView } from '@/components/clusters/clusters-view';

export const metadata: Metadata = { title: 'Clusters' };

export default function ClustersPage() {
  return <ClustersView />;
}
