'use client';

import Link from 'next/link';
import { LogoMark } from '@/components/logo';
import { ThemeSwitch } from '@/components/theme-switch';
import { usePathname } from 'next/navigation';
import type { HealthView } from '@/lib/domain/types';
import { useApi } from '@/lib/client/use-api';

type NavItem = {
  href: string;
  label: string;
  count?: (h: HealthView) => number;
  countLabel?: string;
};

const NAV: NavItem[] = [
  { href: '/', label: 'Overview' },
  { href: '/jobs', label: 'Jobs', count: (h) => h.counts.activeJobs, countLabel: 'active' },
  {
    href: '/clusters',
    label: 'Ray clusters',
    count: (h) => h.counts.rayClusters,
    countLabel: 'live',
  },
  { href: '/endpoints', label: 'Endpoints', count: (h) => h.counts.endpoints, countLabel: 'live' },
  { href: '/models', label: 'Models' },
];

export function Sidebar() {
  const pathname = usePathname();
  const { data: health } = useApi<HealthView>('/api/health', 15_000);

  return (
    <aside className="sidebar">
      <Link href="/" className="brand">
        <LogoMark size={30} />
        <span>
          Heliostat
          <span className="experimental-tag" title="Experimental: not production ready">
            Experimental
          </span>
        </span>
      </Link>
      <nav className="nav" aria-label="Primary">
        {NAV.map((item) => {
          const count = health && item.count?.(health);
          return (
            <Link
              key={item.href}
              href={item.href}
              aria-current={
                (item.href === '/' ? pathname === '/' : pathname.startsWith(item.href))
                  ? 'page'
                  : undefined
              }
            >
              {item.label}
              {count !== undefined && (
                <span className="nav-count" aria-label={`${count} ${item.countLabel}`}>
                  {count}
                </span>
              )}
            </Link>
          );
        })}
      </nav>
      <ThemeSwitch />
    </aside>
  );
}
