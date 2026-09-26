# syntax=docker/dockerfile:1

# 1. Web UI: Next.js static export.
FROM --platform=$BUILDPLATFORM node:22-alpine AS ui
WORKDIR /src/ui
ENV NEXT_TELEMETRY_DISABLED=1
COPY ui/package.json ui/package-lock.json ./
RUN npm ci
COPY ui/ ./
RUN npm run build

# 2. Go binary with the UI embedded. Cross-compiles natively for each target platform.
FROM --platform=$BUILDPLATFORM golang:1.26-alpine AS build
ARG TARGETOS TARGETARCH VERSION=dev
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd/ cmd/
COPY internal/ internal/
COPY ui/embed.go ui/
COPY ui/dist/README.md ui/dist/
COPY --from=ui /src/ui/out/ ui/dist/
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH \
    go build -trimpath -ldflags "-s -w -X main.version=${VERSION}" -o /out/heliostat ./cmd/heliostat

# 3. Runtime: distroless, non-root, no shell.
FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/heliostat /heliostat
COPY config/models.yaml /config/models.yaml
WORKDIR /
USER 65532:65532
EXPOSE 3000
ENTRYPOINT ["/heliostat"]
