Build output of the web UI, embedded into the Go binary by `ui/embed.go`.

`make ui` fills this directory from `ui/out` (the Next.js static export). Everything here except
this file is ignored by git.
