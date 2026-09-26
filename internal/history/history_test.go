package history

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/KubedAI/heliostat/internal/config"
)

func TestParseClusters(t *testing.T) {
	data, err := os.ReadFile("../../testdata/history-clusters.json")
	if err != nil {
		t.Fatal(err)
	}
	var payload any
	if err := json.Unmarshal(data, &payload); err != nil {
		t.Fatal(err)
	}
	want := []Session{{Namespace: "raydata", Name: "rayjob-history-demo-cvjsx", Session: "session_2026-09-24_10-55-21_731289_1",
		CreatedAtMs: 1790247321000, OwnerKind: "rayjob", OwnerName: "rayjob-history-demo"}}
	if got := ParseClusters(payload); !reflect.DeepEqual(got, want) {
		t.Errorf("got %+v", got)
	}
	bad := []any{map[string]any{"namespace": "n", "name": "c", "sessionName": "../etc"}}
	if got := ParseClusters(bad); len(got) != 0 {
		t.Errorf("invalid session accepted: %+v", got)
	}
}

func TestCookieHeader(t *testing.T) {
	got := CookieHeader(Session{Namespace: "raydata", Name: "rc", Session: "session_1", OwnerKind: "rayjob"})
	if got != "cluster_name=rc; cluster_namespace=raydata; session_name=session_1; owner_kind=rayjob" {
		t.Errorf("got %s", got)
	}
}

type recorder struct{ paths []string }

func (r *recorder) Do(_ context.Context, _ config.ServiceRef, _, path string, _ http.Header) (*http.Response, error) {
	r.paths = append(r.paths, path)
	return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"result":"success"}`))}, nil
}

func TestEnsureLoadedIsMemoized(t *testing.T) {
	rec := &recorder{}
	x := NewIndex(config.HistoryServer{}, rec, time.Minute, time.Second, func() {}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	s := Session{Namespace: "raydata", Name: "rc-1", Session: "session_2026-09-25_1"}
	for i := 0; i < 2; i++ {
		if err := x.EnsureLoaded(context.Background(), s, false); err != nil {
			t.Fatal(err)
		}
	}
	if !reflect.DeepEqual(rec.paths, []string{"/enter_cluster/raydata/raycluster/rc-1/session_2026-09-25_1"}) {
		t.Errorf("paths: %v", rec.paths)
	}
	_ = x.EnsureLoaded(context.Background(), s, true)
	if len(rec.paths) != 2 {
		t.Errorf("force should reload: %v", rec.paths)
	}
}
