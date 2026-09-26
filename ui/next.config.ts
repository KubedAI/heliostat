import type { NextConfig } from 'next';

/**
 * The UI is exported as static files and embedded in the Go binary (`ui/embed.go`), which also
 * serves the JSON API. In development, `next dev` forwards API and proxy paths to a locally
 * running backend (`HELIOSTAT_BACKEND`, default http://127.0.0.1:8080).
 */
const backend = process.env.HELIOSTAT_BACKEND ?? 'http://127.0.0.1:8080';
const isDev = process.env.NODE_ENV !== 'production';

const nextConfig: NextConfig = {
  output: isDev ? undefined : 'export',
  images: { unoptimized: true },
  poweredByHeader: false,
  ...(isDev && {
    skipTrailingSlashRedirect: true,
    async rewrites() {
      return ['/api/:path*', '/go/:path*', '/ray/:path*'].map((source) => ({
        source,
        destination: `${backend}${source.replace('/:path*', '')}/:path*`,
      }));
    },
  }),
};

export default nextConfig;
