package server

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/KubedAI/heliostat/internal/app"
	"github.com/KubedAI/heliostat/internal/config"
	"github.com/KubedAI/heliostat/internal/domain"
)

func TestParseJobQuery(t *testing.T) {
	q, err := ParseJobQuery(url.Values{})
	if err != nil || q.Window != "7d" || q.Limit != 50 {
		t.Errorf("defaults: %+v %v", q, err)
	}
	q, err = ParseJobQuery(url.Values{"q": {" gemma "}, "status": {"FAILED"}, "kind": {"Submission"}, "window": {"all"}, "limit": {"10"}})
	if err != nil || q.Q != "gemma" || q.Status != domain.StatusFailed || q.Kind != domain.KindSubmission || q.Window != "all" || q.Limit != 10 {
		t.Errorf("parsed: %+v %v", q, err)
	}
	for _, bad := range []url.Values{{"status": {"failed"}}, {"window": {"1y"}}, {"kind": {"Pod"}}, {"limit": {"0"}}, {"limit": {"500"}}, {"limit": {"abc"}}} {
		if _, err := ParseJobQuery(bad); err == nil {
			t.Errorf("%v should be rejected", bad)
		}
	}
}

func testServer(t *testing.T) http.Handler {
	t.Helper()
	cfg := &config.Config{
		Clusters:     []config.Cluster{{Name: "c1", DisplayName: "c1", Region: "us-west-2", RayDashboards: true}},
		DatabasePath: ":memory:", RetentionDays: 90, ModelsPath: "../../config/models.yaml",
	}
	a, err := app.New(cfg, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { a.Store.Close() })
	_, _ = a.Store.Upsert(domain.JobRecord{ID: "j1", Kind: domain.KindRayJob, Cluster: "c1", Namespace: "ns", Name: "job-1",
		Status: domain.StatusSucceeded, Phase: "Complete", Compute: domain.ComputeIntent{InstanceTypes: []string{}},
		CreatedAt: "2026-09-25T00:00:00Z", Present: false})
	ui := fstest.MapFS{
		"index.html":            {Data: []byte("<html>home</html>")},
		"jobs.html":             {Data: []byte("<html>jobs</html>")},
		"404.html":              {Data: []byte("<html>missing</html>")},
		"_next/static/chunk.js": {Data: []byte("js")},
	}
	return New(a, ui, slog.New(slog.NewTextHandler(io.Discard, nil)))
}

func get(h http.Handler, method, target string, header http.Header) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, target, nil)
	for k, v := range header {
		req.Header[k] = v
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestRoutes(t *testing.T) {
	h := testServer(t)

	rec := get(h, "GET", "/api/jobs?window=all", nil)
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), `"name":"job-1"`) || !strings.Contains(rec.Body.String(), `"region":"us-west-2"`) {
		t.Errorf("jobs: %d %s", rec.Code, rec.Body)
	}
	if again := get(h, "GET", "/api/jobs?window=all", http.Header{"If-None-Match": {rec.Header().Get("ETag")}}); again.Code != 304 {
		t.Errorf("etag: %d", again.Code)
	}
	if rec := get(h, "GET", "/api/jobs?status=bogus", nil); rec.Code != 400 {
		t.Errorf("bad query: %d", rec.Code)
	}
	if rec := get(h, "GET", "/api/jobs/j1", nil); rec.Code != 200 || !strings.Contains(rec.Body.String(), `"diagnostics":"none"`) {
		t.Errorf("job: %d %s", rec.Code, rec.Body)
	}
	if rec := get(h, "GET", "/api/jobs/missing", nil); rec.Code != 404 {
		t.Errorf("missing job: %d", rec.Code)
	}
	if rec := get(h, "GET", "/go/job/j1", nil); rec.Code != 307 || rec.Header().Get("Location") != "/job?id=j1" {
		t.Errorf("go/job fallback: %d %s", rec.Code, rec.Header().Get("Location"))
	}
	if rec := get(h, "GET", "/api/models", nil); rec.Code != 200 || !strings.Contains(rec.Body.String(), `"items"`) {
		t.Errorf("models: %d %s", rec.Code, rec.Body)
	}
	if rec := get(h, "GET", "/api/readyz", nil); rec.Code != 200 {
		t.Errorf("readyz: %d", rec.Code)
	}
	if rec := get(h, "GET", "/ray/live/c1/ns/unknown/", nil); rec.Code != 404 {
		t.Errorf("unknown ray cluster: %d", rec.Code)
	}
	if rec := get(h, "GET", "/ray/history/c1/ns/rc/session_1/", nil); rec.Code != 404 {
		t.Errorf("unknown session: %d", rec.Code)
	}
}

func TestReadOnly(t *testing.T) {
	h := testServer(t)
	for _, method := range []string{"POST", "PUT", "PATCH", "DELETE"} {
		for _, target := range []string{"/api/jobs", "/ray/live/c1/ns/rc/api/jobs/", "/ray/history/c1/ns/rc/s/api/jobs/", "/go/job/j1"} {
			if rec := get(h, method, target, nil); rec.Code != http.StatusMethodNotAllowed {
				t.Errorf("%s %s = %d, want 405", method, target, rec.Code)
			}
		}
	}
}

func TestStaticUI(t *testing.T) {
	h := testServer(t)
	cases := []struct {
		path, body string
		code       int
	}{
		{"/", "home", 200},
		{"/jobs", "jobs", 200},
		{"/nope", "missing", 404},
		{"/_next/static/chunk.js", "js", 200},
	}
	for _, c := range cases {
		rec := get(h, "GET", c.path, nil)
		if rec.Code != c.code || !strings.Contains(rec.Body.String(), c.body) {
			t.Errorf("%s: %d %s", c.path, rec.Code, rec.Body)
		}
	}
	if rec := get(h, "GET", "/jobs/", nil); rec.Code != 308 || rec.Header().Get("Location") != "/jobs" {
		t.Errorf("trailing slash: %d %s", rec.Code, rec.Header().Get("Location"))
	}
	// Paths a browser would read as another host must never produce a redirect (open redirect).
	for _, p := range []string{"/\\evil.com/", "/%5Cevil.com/", "/%09/evil.com/", "/%0d%0a/evil.com/"} {
		if rec := get(h, "GET", p, nil); rec.Code == http.StatusPermanentRedirect || rec.Header().Get("Location") != "" {
			t.Errorf("%s redirected to %q", p, rec.Header().Get("Location"))
		}
	}
	if rec := get(h, "GET", "/_next/static/chunk.js", nil); !strings.Contains(rec.Header().Get("Cache-Control"), "immutable") {
		t.Errorf("asset caching: %s", rec.Header().Get("Cache-Control"))
	}
}
