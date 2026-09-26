/** Stable deep link to a job's Ray Dashboard page; the backend resolves it at click time. */
export function jobLinkPath(id: string): string {
  return `/go/job/${encodeURIComponent(id)}`;
}

/** The job detail page. A query parameter, because the UI is exported as static files. */
export function jobPagePath(id: string): string {
  return `/job?id=${encodeURIComponent(id)}`;
}
