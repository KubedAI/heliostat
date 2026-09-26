import { CollectorNotice } from '@/components/collector-notice';
import { Sidebar } from '@/components/sidebar';

export default function ConsoleLayout({ children }: Readonly<{ children: React.ReactNode }>) {
  return (
    <>
      <a className="skip-link" href="#main">
        Skip to content
      </a>
      <div className="shell">
        <Sidebar />
        <main id="main" className="workspace">
          <div className="page">
            <CollectorNotice />
            {children}
          </div>
        </main>
      </div>
    </>
  );
}
