// Package proxy relays browser requests to Ray Dashboards (live clusters and the Ray History
// Server) under a Heliostat path, enforcing a read-only policy.
package proxy

import (
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"regexp"
	"strings"

	"github.com/KubedAI/heliostat/internal/config"
	"github.com/KubedAI/heliostat/internal/kube"
)

// denied lists Ray 2.x GET routes with side effects or more exposure than status. Route inventory:
// Ray 2.56 dashboard/ (http_server_head.py, modules/*_head.py). Write methods never get this far:
// only GET and HEAD are routed, and the API server transport rejects anything else.
var denied = []struct {
	pattern *regexp.Regexp
	reason  string
}{
	{regexp.MustCompile(`^/(worker|task)/(cpu_profile|gpu_profile|traceback)$`), "Profiling attaches py-spy or memray to live Ray workers."},
	{regexp.MustCompile(`^/memory_profile$`), "Profiling attaches memray to live Ray workers."},
	{regexp.MustCompile(`^/api/v0/delay/`), "Test endpoint that holds server resources."},
	{regexp.MustCompile(`^/api/packages/`), "Downloads user code packages uploaded with runtime_env."},
	{regexp.MustCompile(`^/api/job_agent/`), "Internal per-node job agent API."},
	{regexp.MustCompile(`^/api/jobs/[^/]+/logs/tail$`), "WebSocket log tailing is not proxied."},
	{regexp.MustCompile(`^/api/authenticate$`), "Ray token authentication is not proxied."},
}

var multiSlash = regexp.MustCompile(`/{2,}`)

// CheckPath validates a percent-encoded upstream path (no query). It rejects dot segments and
// encoded separators so a request cannot escape the route it was validated against.
func CheckPath(encodedPath string) (bool, string) {
	if !strings.HasPrefix(encodedPath, "/") {
		return false, "Path must be absolute."
	}
	lower := strings.ToLower(encodedPath)
	if strings.Contains(lower, "%2f") || strings.Contains(lower, "%5c") {
		return false, "Encoded path separators are not allowed."
	}
	decoded, err := url.PathUnescape(encodedPath)
	if err != nil {
		return false, "Malformed percent-encoding."
	}
	if strings.Contains(decoded, `\`) {
		return false, "Encoded path separators are not allowed."
	}
	for _, part := range strings.Split(decoded, "/") {
		if part == "." || part == ".." {
			return false, "Dot segments are not allowed."
		}
	}
	normalized := multiSlash.ReplaceAllString(decoded, "/")
	for _, d := range denied {
		if d.pattern.MatchString(normalized) {
			return false, d.reason
		}
	}
	return true, ""
}

// Request headers forwarded to Ray. Browser cookies and credentials are never sent.
var forwardedRequestHeaders = []string{"Accept", "Accept-Language", "If-None-Match", "If-Modified-Since", "Range"}

// Response headers relayed to the browser. Set-Cookie is intentionally absent.
var relayedResponseHeaders = []string{"Content-Type", "Cache-Control", "Etag", "Last-Modified", "Content-Range", "Accept-Ranges", "Content-Disposition"}

// SetSecurityHeaders adds headers applied to every proxied response.
func SetSecurityHeaders(h http.Header) {
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Referrer-Policy", "no-referrer")
	h.Set("X-Robots-Tag", "noindex")
	h.Set("X-Frame-Options", "SAMEORIGIN")
}

// Plain writes a short text response.
func Plain(w http.ResponseWriter, status int, message string) {
	SetSecurityHeaders(w.Header())
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(status)
	fmt.Fprintln(w, message)
}

// Target is one mounted Ray Dashboard.
type Target struct {
	Transport kube.ServiceTransport
	Service   config.ServiceRef
	// MountPath is the Heliostat path the dashboard is mounted at, with a trailing slash.
	MountPath string
	// Header is sent upstream, for example the History Server's session cookies.
	Header http.Header
}

// Serve relays one GET or HEAD under target.MountPath. It returns the upstream status (0 when no
// upstream response was relayed) so callers can retry, and writes nothing when retry is true and
// the upstream returned 503.
func Serve(w http.ResponseWriter, r *http.Request, target Target, log *slog.Logger, retryOn503 bool) int {
	escaped := r.URL.EscapedPath()
	mount := strings.TrimSuffix(target.MountPath, "/")
	if escaped == mount {
		location := target.MountPath
		if r.URL.RawQuery != "" {
			location += "?" + r.URL.RawQuery
		}
		SetSecurityHeaders(w.Header())
		http.Redirect(w, r, location, http.StatusPermanentRedirect)
		return 0
	}
	if !strings.HasPrefix(escaped, target.MountPath) {
		Plain(w, http.StatusNotFound, "Not found")
		return 0
	}
	upstreamPath := escaped[len(mount):]
	if ok, reason := CheckPath(upstreamPath); !ok {
		Plain(w, http.StatusForbidden, "Blocked by Heliostat's read-only policy: "+reason)
		return 0
	}

	header := http.Header{}
	for k, v := range target.Header {
		header[k] = v
	}
	for _, name := range forwardedRequestHeaders {
		if v := r.Header.Get(name); v != "" {
			header.Set(name, v)
		}
	}
	method := http.MethodGet
	if r.Method == http.MethodHead {
		method = http.MethodHead
	}
	pathAndQuery := upstreamPath
	if r.URL.RawQuery != "" {
		pathAndQuery += "?" + r.URL.RawQuery
	}
	resp, err := target.Transport.Do(r.Context(), target.Service, method, pathAndQuery, header)
	if err != nil {
		if r.Context().Err() != nil {
			return 0 // The browser went away; nothing to report.
		}
		log.Warn("dashboard upstream failed", "service", target.Service.Name, "path", upstreamPath, "error", err)
		Plain(w, http.StatusBadGateway, "Ray Dashboard is unreachable: "+err.Error())
		return 0
	}
	defer resp.Body.Close()
	if retryOn503 && resp.StatusCode == http.StatusServiceUnavailable {
		return resp.StatusCode
	}

	out := w.Header()
	SetSecurityHeaders(out)
	for _, name := range relayedResponseHeaders {
		if v := resp.Header.Get(name); v != "" {
			out.Set(name, v)
		}
	}
	if location := resp.Header.Get("Location"); location != "" {
		if strings.HasPrefix(location, "/") {
			location = mount + location
		}
		out.Set("Location", location)
	}
	w.WriteHeader(resp.StatusCode)
	if method == http.MethodHead || resp.StatusCode == http.StatusNoContent || resp.StatusCode == http.StatusNotModified {
		return resp.StatusCode
	}
	copyFlushing(w, resp.Body)
	return resp.StatusCode
}

// copyFlushing streams the body, flushing so log streams reach the browser promptly.
func copyFlushing(w http.ResponseWriter, body io.Reader) {
	flusher, _ := w.(http.Flusher)
	buf := make([]byte, 32<<10)
	for {
		n, err := body.Read(buf)
		if n > 0 {
			if _, werr := w.Write(buf[:n]); werr != nil {
				return
			}
			if flusher != nil {
				flusher.Flush()
			}
		}
		if err != nil {
			return
		}
	}
}
