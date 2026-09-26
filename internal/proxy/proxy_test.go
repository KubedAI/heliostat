package proxy

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/KubedAI/heliostat/internal/config"
)

func TestCheckPath(t *testing.T) {
	for _, p := range []string{"/", "/api/jobs/", "/api/jobs/raysubmit_1", "/api/v0/logs/file", "/static/js/main.js", "/nodes"} {
		if ok, reason := CheckPath(p); !ok {
			t.Errorf("%s should be allowed: %s", p, reason)
		}
	}
	for _, p := range []string{
		"/worker/traceback", "/worker/cpu_profile", "/worker/gpu_profile", "/task/traceback", "/task/cpu_profile",
		"/memory_profile", "//memory_profile", "/api/v0/delay/100", "/api/packages/gcs/pkg.zip",
		"/api/job_agent/jobs/x/logs", "/api/jobs/x/logs/tail", "/api/authenticate",
		"/static/../worker/traceback", "/static/%2e%2e/worker/traceback", "/api/jobs%2fx", "/api/%5cjobs",
		"relative", "/%E0%A4%A",
	} {
		if ok, _ := CheckPath(p); ok {
			t.Errorf("%s should be denied", p)
		}
	}
}

type call struct {
	method, path string
	header       http.Header
}

type fakeTransport struct {
	calls  []call
	status int
	header http.Header
	body   string
	err    error
}

func (f *fakeTransport) Do(_ context.Context, _ config.ServiceRef, method, path string, header http.Header) (*http.Response, error) {
	f.calls = append(f.calls, call{method, path, header})
	if f.err != nil {
		return nil, f.err
	}
	status := f.status
	if status == 0 {
		status = 200
	}
	h := f.header
	if h == nil {
		h = http.Header{"Content-Type": {"text/html"}}
	}
	return &http.Response{StatusCode: status, Header: h, Body: io.NopCloser(strings.NewReader(f.body))}, nil
}

var quiet = slog.New(slog.NewTextHandler(io.Discard, nil))

func serve(f *fakeTransport, url string, header http.Header, extra http.Header) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, url, nil)
	for k, v := range header {
		req.Header[k] = v
	}
	rec := httptest.NewRecorder()
	Serve(rec, req, Target{Transport: f, Service: config.ServiceRef{Namespace: "ns", Name: "rc-head-svc", Port: 8265},
		MountPath: "/ray/live/c1/ns/rc/", Header: extra}, quiet, false)
	return rec
}

func TestServe(t *testing.T) {
	f := &fakeTransport{}
	if rec := serve(f, "/ray/live/c1/ns/rc?x=1", nil, nil); rec.Code != 308 || rec.Header().Get("Location") != "/ray/live/c1/ns/rc/?x=1" || len(f.calls) != 0 {
		t.Errorf("mount redirect: %d %s", rec.Code, rec.Header().Get("Location"))
	}

	f = &fakeTransport{body: "[]", header: http.Header{"Content-Type": {"application/json"}, "Set-Cookie": {"x=1"}, "Server": {"aiohttp"}}}
	rec := serve(f, "/ray/live/c1/ns/rc/api/jobs/?limit=5",
		http.Header{"Cookie": {"session=secret"}, "Authorization": {"Bearer t"}, "Accept": {"application/json"}},
		http.Header{"Cookie": {"cluster_name=rc"}})
	got := f.calls[0]
	if got.path != "/api/jobs/?limit=5" || got.method != "GET" || got.header.Get("Cookie") != "cluster_name=rc" ||
		got.header.Get("Authorization") != "" || got.header.Get("Accept") != "application/json" {
		t.Errorf("upstream call: %+v", got)
	}
	if rec.Header().Get("Set-Cookie") != "" || rec.Header().Get("Server") != "" || rec.Header().Get("X-Content-Type-Options") != "nosniff" || rec.Body.String() != "[]" {
		t.Errorf("response headers: %v", rec.Header())
	}

	f = &fakeTransport{}
	if rec := serve(f, "/ray/live/c1/ns/rc/worker/traceback?pid=1", nil, nil); rec.Code != 403 || len(f.calls) != 0 {
		t.Errorf("denied path: %d", rec.Code)
	}

	f = &fakeTransport{status: 302, header: http.Header{"Location": {"/#/overview"}}}
	if rec := serve(f, "/ray/live/c1/ns/rc/x", nil, nil); rec.Header().Get("Location") != "/ray/live/c1/ns/rc/#/overview" {
		t.Errorf("location rewrite: %s", rec.Header().Get("Location"))
	}

	f = &fakeTransport{err: errors.New("connect ECONNREFUSED")}
	if rec := serve(f, "/ray/live/c1/ns/rc/", nil, nil); rec.Code != 502 || !strings.Contains(rec.Body.String(), "ECONNREFUSED") {
		t.Errorf("unreachable: %d %s", rec.Code, rec.Body)
	}
}
