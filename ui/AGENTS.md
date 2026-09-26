<!-- BEGIN:nextjs-agent-rules -->

# This is NOT the Next.js you know

This version has breaking changes — APIs, conventions, and file structure may all differ from your training data. Read the relevant guide in `node_modules/next/dist/docs/` (resolved from this file's directory; in monorepos the `next` package may not be visible from the repo root) before writing any code. Heed deprecation notices.

This block is written and re-added by `next dev` — verify at `node_modules/next/dist/server/lib/generate-agent-files.js`. Removing it from a diff only re-creates the uncommitted change; committing it with your work keeps the tree clean.

<!-- END:nextjs-agent-rules -->

# Heliostat web UI

Read the root [AGENTS.md](../AGENTS.md) first.

- The UI is a **static export** (`output: 'export'`): no server components that read data, no route handlers, no middleware. Every page fetches the Go backend's JSON API from the browser.
- Dynamic routes cannot be exported, so the job page is `/job?id=<id>` (`jobPagePath` in `lib/links.ts`).
- API types live in `lib/domain/types.ts` and must match `internal/domain/types.go`.
- `npm run dev` forwards `/api`, `/go`, and `/ray` to the backend (`make run`, default http://127.0.0.1:8080).
