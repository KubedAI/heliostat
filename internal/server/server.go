// Package server exposes Heliostat over HTTP: the JSON API, the job link resolver, the read-only
// Ray Dashboard proxy, and the embedded web UI. Every route is GET or HEAD.
package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"mime"
	"net/http"
	"net/url"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/KubedAI/heliostat/internal/app"
	"github.com/KubedAI/heliostat/internal/collector"
	"github.com/KubedAI/heliostat/internal/domain"
	"github.com/KubedAI/heliostat/internal/history"
	"github.com/KubedAI/heliostat/internal/links"
	"github.com/KubedAI/heliostat/internal/models"
	"github.com/KubedAI/heliostat/internal/proxy"
)

const (
	defaultLimit = 50
	maxLimit     = 200
)

// Server routes HTTP requests.
type Server struct {
	app *app.App
	ui  fs.FS
	log *slog.Logger
	mux *http.ServeMux
}

// New builds the handler. ui holds the statically exported web UI (may be empty in development).
func New(a *app.App, ui fs.FS, log *slog.Logger) http.Handler {
	s := &Server{app: a, ui: ui, log: log, mux: http.NewServeMux()}
	s.mux.HandleFunc("GET /api/jobs", s.jobs)
	s.mux.HandleFunc("GET /api/jobs/{id}", s.job)
	s.mux.HandleFunc("GET /api/clusters", s.etag(func(*http.Request) (any, error) {
		return domain.ListResponse[domain.RayClusterView]{Items: a.RayClusters(), Clusters: a.ClusterInfo()}, nil
	}))
	s.mux.HandleFunc("GET /api/endpoints", s.etag(func(*http.Request) (any, error) {
		return domain.ListResponse[domain.EndpointView]{Items: a.Endpoints(), Clusters: a.ClusterInfo()}, nil
	}))
	s.mux.HandleFunc("GET /api/models", s.models)
	s.mux.HandleFunc("GET /api/health", s.health)
	s.mux.HandleFunc("GET /api/livez", func(w http.ResponseWriter, _ *http.Request) { fmt.Fprintln(w, "ok") })
	s.mux.HandleFunc("GET /api/readyz", s.readyz)
	s.mux.HandleFunc("GET /go/job/{id}", s.goJob)
	s.mux.HandleFunc("GET "+links.LivePrefix+"/", s.liveDashboard)
	s.mux.HandleFunc("GET "+links.HistoryPrefix+"/", s.historyDashboard)
	s.mux.HandleFunc("GET /", s.static)
	return s
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h := w.Header()
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Referrer-Policy", "no-referrer")
	s.mux.ServeHTTP(w, r)
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func problem(w http.ResponseWriter, status int, detail string) {
	writeJSON(w, status, map[string]string{"error": detail})
}

// respondETag serves JSON with a weak ETag so polling clients get cheap 304 replies.
func (s *Server) respondETag(w http.ResponseWriter, r *http.Request, revision string, body func() (any, error)) {
	etag := `W/"` + revision + `"`
	w.Header().Set("ETag", etag)
	w.Header().Set("Cache-Control", "no-cache")
	if r.Header.Get("If-None-Match") == etag {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	v, err := body()
	if err != nil {
		s.log.Error("request failed", "path", r.URL.Path, "error", err)
		problem(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, v)
}

func (s *Server) etag(body func(*http.Request) (any, error)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		s.respondETag(w, r, s.app.Revision(), func() (any, error) { return body(r) })
	}
}

// ParseJobQuery validates /api/jobs query parameters.
func ParseJobQuery(v url.Values) (domain.JobQuery, error) {
	q := domain.JobQuery{Window: "7d", Limit: defaultLimit,
		Cluster: v.Get("cluster"), Namespace: v.Get("namespace"), Cursor: v.Get("cursor")}
	if s := strings.TrimSpace(v.Get("q")); s != "" {
		q.Q = s[:min(len(s), 200)]
	}
	if s := v.Get("limit"); s != "" {
		n, err := strconv.Atoi(s)
		if err != nil || n < 1 || n > maxLimit {
			return q, fmt.Errorf("limit must be an integer between 1 and %d", maxLimit)
		}
		q.Limit = n
	}
	if s := v.Get("status"); s != "" {
		if !contains(domain.JobStatuses, domain.JobStatus(s)) {
			return q, errors.New("status must be one of PENDING, RUNNING, SUSPENDED, SUCCEEDED, FAILED, STOPPED, UNKNOWN")
		}
		q.Status = domain.JobStatus(s)
	}
	if s := v.Get("kind"); s != "" {
		if !contains([]domain.JobKind{domain.KindRayJob, domain.KindSubmission, domain.KindDriver}, domain.JobKind(s)) {
			return q, errors.New("kind must be one of RayJob, Submission, Driver")
		}
		q.Kind = domain.JobKind(s)
	}
	if s := v.Get("window"); s != "" {
		if !contains(domain.TimeWindows, domain.TimeWindow(s)) {
			return q, errors.New("window must be one of 24h, 7d, 30d, all")
		}
		q.Window = domain.TimeWindow(s)
	}
	return q, nil
}

func contains[T comparable](list []T, v T) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}

func (s *Server) jobs(w http.ResponseWriter, r *http.Request) {
	q, err := ParseJobQuery(r.URL.Query())
	if err != nil {
		problem(w, http.StatusBadRequest, err.Error())
		return
	}
	s.respondETag(w, r, s.app.Revision()+":"+r.URL.RawQuery, func() (any, error) { return s.app.Jobs(q) })
}

func (s *Server) job(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	job, ok := s.app.Job(id)
	if !ok {
		problem(w, http.StatusNotFound, "Job "+id+" not found")
		return
	}
	s.respondETag(w, r, s.app.Revision()+":"+id, func() (any, error) { return job, nil })
}

func (s *Server) models(w http.ResponseWriter, _ *http.Request) {
	items, err := models.Load(s.app.Config.ModelsPath)
	if err != nil {
		problem(w, http.StatusInternalServerError, "Unable to load the model catalog: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) health(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, s.app.Health())
}

// readyz checks only that the job store works: one unreachable cluster must never take the UI
// out of service. Collector errors are reported in the UI instead.
func (s *Server) readyz(w http.ResponseWriter, _ *http.Request) {
	if err := s.app.Store.Ping(); err != nil {
		http.Error(w, "not ready: "+err.Error(), http.StatusServiceUnavailable)
		return
	}
	fmt.Fprintln(w, "ok")
}

// goJob is a stable deep link resolved at click time: the live dashboard while the Ray cluster
// exists, then the History Server archive, otherwise Heliostat's own job page.
func (s *Server) goJob(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	target, ok := s.app.JobDashboardHref(r.Context(), id)
	if !ok {
		target = links.JobPagePath(id)
	}
	w.Header().Set("Cache-Control", "no-store")
	http.Redirect(w, r, target, http.StatusTemporaryRedirect)
}

// mountSegments splits "/ray/<kind>/a/b/c/..." into its first n unescaped segments.
func mountSegments(r *http.Request, prefix string, n int) ([]string, bool) {
	rest := strings.TrimPrefix(r.URL.EscapedPath(), prefix+"/")
	parts := strings.SplitN(rest, "/", n+1)
	if len(parts) < n {
		return nil, false
	}
	out := make([]string, n)
	for i := 0; i < n; i++ {
		v, err := url.PathUnescape(parts[i])
		if err != nil || v == "" {
			return nil, false
		}
		out[i] = v
	}
	return out, true
}

// liveDashboard proxies a live Ray cluster's dashboard. Only clusters Heliostat observes are
// reachable, so the proxy cannot be pointed at arbitrary services.
func (s *Server) liveDashboard(w http.ResponseWriter, r *http.Request) {
	seg, ok := mountSegments(r, links.LivePrefix, 3)
	if !ok {
		proxy.Plain(w, http.StatusNotFound, "Not found")
		return
	}
	clusterName, namespace, name := seg[0], seg[1], seg[2]
	c, ok := s.app.Cluster(clusterName)
	var target proxy.Target
	if ok && c.Config.RayDashboards && c.Transport() != nil {
		if rc, found := c.RayCluster(namespace, name); found {
			if svc, hasHead := collector.HeadService(rc); hasHead {
				target = proxy.Target{Transport: c.Transport(), Service: svc, MountPath: links.LiveDashboardPath(clusterName, namespace, name)}
			}
		}
	}
	if target.Transport == nil {
		proxy.Plain(w, http.StatusNotFound, fmt.Sprintf("Ray cluster %s/%s is not running in %s.", namespace, name, clusterName))
		return
	}
	proxy.Serve(w, r, target, s.log, false)
}

// historyDashboard proxies the Ray History Server's dashboard for one archived session. The
// session is part of the path and is sent upstream as the cookies the History Server expects.
func (s *Server) historyDashboard(w http.ResponseWriter, r *http.Request) {
	seg, ok := mountSegments(r, links.HistoryPrefix, 4)
	if !ok {
		proxy.Plain(w, http.StatusNotFound, "Not found")
		return
	}
	clusterName, namespace, name, session := seg[0], seg[1], seg[2], seg[3]
	c, ok := s.app.Cluster(clusterName)
	var hist *history.Index
	var archived history.Session
	found := false
	if ok {
		if hist = c.History(); hist != nil {
			archived, found = hist.Find(namespace, name, session)
		}
	}
	if !found || c.Transport() == nil {
		proxy.Plain(w, http.StatusNotFound, fmt.Sprintf("No archived session %s for Ray cluster %s/%s in %s.", session, namespace, name, clusterName))
		return
	}
	target := proxy.Target{
		Transport: c.Transport(), Service: hist.Config.Dashboard,
		MountPath: links.HistoryDashboardPath(clusterName, namespace, name, session),
		Header:    http.Header{"Cookie": {history.CookieHeader(archived)}},
	}
	if err := hist.EnsureLoaded(r.Context(), archived, false); err != nil {
		proxy.Plain(w, http.StatusServiceUnavailable, "Could not load the archived session from the Ray History Server: "+err.Error())
		return
	}
	if status := proxy.Serve(w, r, target, s.log, true); status == http.StatusServiceUnavailable {
		// The History Server evicted the session since it was loaded; reload once and retry.
		if err := hist.EnsureLoaded(r.Context(), archived, true); err != nil {
			proxy.Plain(w, http.StatusServiceUnavailable, "Could not reload the archived session: "+err.Error())
			return
		}
		proxy.Serve(w, r, target, s.log, false)
	}
}

// static serves the exported UI: "/" → index.html, "/jobs" → jobs.html, assets as-is.
func (s *Server) static(w http.ResponseWriter, r *http.Request) {
	p := r.URL.Path
	if p != "/" && strings.HasSuffix(p, "/") {
		http.Redirect(w, r, strings.TrimRight(p, "/"), http.StatusPermanentRedirect)
		return
	}
	clean := strings.TrimPrefix(path.Clean(p), "/")
	candidates := []string{clean, clean + ".html"}
	if clean == "" {
		candidates = []string{"index.html"}
	}
	for _, name := range candidates {
		if s.serveFile(w, r, name, http.StatusOK) {
			return
		}
	}
	if !s.serveFile(w, r, "404.html", http.StatusNotFound) {
		if _, err := fs.Stat(s.ui, "index.html"); err != nil {
			http.Error(w, "The web UI is not built into this binary. Run `make ui` (or `make build`).", http.StatusNotFound)
			return
		}
		http.NotFound(w, r)
	}
}

func (s *Server) serveFile(w http.ResponseWriter, r *http.Request, name string, status int) bool {
	data, err := fs.ReadFile(s.ui, name)
	if err != nil {
		return false
	}
	h := w.Header()
	if ct := mime.TypeByExtension(path.Ext(name)); ct != "" {
		h.Set("Content-Type", ct)
	} else {
		h.Set("Content-Type", "text/plain; charset=utf-8")
	}
	if strings.HasPrefix(name, "_next/static/") {
		h.Set("Cache-Control", "public, max-age=31536000, immutable")
	} else {
		h.Set("Cache-Control", "no-cache")
	}
	if strings.HasSuffix(name, ".html") {
		h.Set("X-Frame-Options", "DENY")
	}
	if status == http.StatusOK {
		http.ServeContent(w, r, name, time.Time{}, strings.NewReader(string(data)))
		return true
	}
	w.WriteHeader(status)
	if r.Method != http.MethodHead {
		_, _ = w.Write(data)
	}
	return true
}
