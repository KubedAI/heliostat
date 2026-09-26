// Package history mirrors the KubeRay History Server's index of archived Ray sessions and knows
// how to open them.
package history

import (
	"context"
	"log/slog"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/KubedAI/heliostat/internal/config"
	"github.com/KubedAI/heliostat/internal/domain"
	"github.com/KubedAI/heliostat/internal/kube"
)

const (
	// Re-enter well within the History Server's cache lifetime so evicted sessions reload.
	sessionLoadTTL = 5 * time.Minute
	// Loading a session reads its archive from object storage, which can take a while.
	sessionLoadTimeout = 30 * time.Second
)

var sessionName = regexp.MustCompile(`^session_[\w.-]+$`)

// Session is one archived Ray session.
type Session struct {
	Namespace   string
	Name        string
	Session     string
	CreatedAtMs int64
	OwnerKind   string
	OwnerName   string
}

// ParseClusters parses the History Server's GET /clusters response.
func ParseClusters(payload any) []Session {
	items, _ := payload.([]any)
	var out []Session
	for _, raw := range items {
		e := domain.Dict(raw)
		s := Session{
			Namespace: domain.Str(e["namespace"]), Name: domain.Str(e["name"]), Session: domain.Str(e["sessionName"]),
			CreatedAtMs: int64(domain.Number(e["createTimeStamp"]) * 1000),
			OwnerKind:   domain.Str(e["ownerKind"]), OwnerName: domain.Str(e["ownerName"]),
		}
		if s.Namespace != "" && s.Name != "" && sessionName.MatchString(s.Session) {
			out = append(out, s)
		}
	}
	return out
}

// CookieHeader builds the cookies the History Server's dashboard reads to select a session
// (normally set by an interactive GET /enter_cluster/...). Sending them on every request makes
// history links stable and shareable.
func CookieHeader(s Session) string {
	var parts []string
	for _, kv := range [][2]string{
		{"cluster_name", s.Name}, {"cluster_namespace", s.Namespace}, {"session_name", s.Session},
		{"owner_kind", s.OwnerKind}, {"owner_name", s.OwnerName},
	} {
		if kv[1] != "" {
			parts = append(parts, kv[0]+"="+url.QueryEscape(kv[1]))
		}
	}
	return strings.Join(parts, "; ")
}

// Index periodically mirrors the History Server's /clusters list for one Kubernetes cluster.
type Index struct {
	Config    config.HistoryServer
	transport kube.ServiceTransport
	interval  time.Duration
	timeout   time.Duration
	onChange  func()
	log       *slog.Logger

	mu          sync.RWMutex
	sessions    map[string][]Session // "<ns>/<name>" → newest first
	fingerprint string
	health      domain.SourceHealth
	jobIDs      map[string]map[string]string // session → submission ID → Ray job ID
	loaded      map[string]time.Time
}

// NewIndex creates an index; call Run to start it.
func NewIndex(cfg config.HistoryServer, t kube.ServiceTransport, interval, timeout time.Duration, onChange func(), log *slog.Logger) *Index {
	return &Index{
		Config: cfg, transport: t, interval: interval, timeout: timeout, onChange: onChange, log: log,
		sessions: map[string][]Session{}, jobIDs: map[string]map[string]string{}, loaded: map[string]time.Time{},
	}
}

// Status reports the source's health.
func (x *Index) Status() domain.SourceHealth {
	x.mu.RLock()
	defer x.mu.RUnlock()
	return x.health
}

// Latest returns the newest archived session of a Ray cluster.
func (x *Index) Latest(namespace, name string) (Session, bool) {
	x.mu.RLock()
	defer x.mu.RUnlock()
	list := x.sessions[namespace+"/"+name]
	if len(list) == 0 {
		return Session{}, false
	}
	return list[0], true
}

// Find returns a specific archived session.
func (x *Index) Find(namespace, name, session string) (Session, bool) {
	x.mu.RLock()
	defer x.mu.RUnlock()
	for _, s := range x.sessions[namespace+"/"+name] {
		if s.Session == session {
			return s, true
		}
	}
	return Session{}, false
}

// Run refreshes the index until ctx is cancelled, backing off on errors.
func (x *Index) Run(ctx context.Context) {
	backoff := 5 * time.Second
	for {
		wait := x.interval
		if err := x.refresh(ctx); err != nil {
			x.mu.Lock()
			x.health.Error = err.Error()
			x.mu.Unlock()
			x.log.Warn("history server refresh failed", "error", err)
			wait, backoff = backoff, min(backoff*2, 5*time.Minute)
		} else {
			backoff = 5 * time.Second
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(wait):
		}
	}
}

func (x *Index) refresh(ctx context.Context) error {
	var payload any
	if err := kube.GetJSON(ctx, x.transport, x.Config.API, "/clusters", nil, x.timeout, &payload); err != nil {
		return err
	}
	sessions := ParseClusters(payload)
	next := map[string][]Session{}
	keys := make([]string, 0, len(sessions))
	for _, s := range sessions {
		k := s.Namespace + "/" + s.Name
		next[k] = append(next[k], s)
		keys = append(keys, k+"/"+s.Session)
	}
	for _, list := range next {
		sort.Slice(list, func(i, j int) bool { return list[i].CreatedAtMs > list[j].CreatedAtMs })
	}
	sort.Strings(keys)
	fingerprint := strings.Join(keys, "|")

	x.mu.Lock()
	changed := fingerprint != x.fingerprint
	x.sessions, x.fingerprint = next, fingerprint
	x.health = domain.SourceHealth{Synced: true, LastSuccessAt: time.Now().UTC().Format(time.RFC3339)}
	x.mu.Unlock()
	if changed {
		x.onChange()
	}
	return nil
}

// EnsureLoaded loads an archived session into the History Server's in-memory cache. Its dashboard
// answers "503 session snapshot not in cache" for sessions never entered or since evicted; the
// selection cookies alone do not load one. Memoized; pass force after a 503.
func (x *Index) EnsureLoaded(ctx context.Context, s Session, force bool) error {
	key := s.Namespace + "/" + s.Name + "/" + s.Session
	x.mu.RLock()
	loadedAt, ok := x.loaded[key]
	x.mu.RUnlock()
	if !force && ok && time.Since(loadedAt) < sessionLoadTTL {
		return nil
	}
	path := "/enter_cluster/" + url.PathEscape(s.Namespace) + "/raycluster/" + url.PathEscape(s.Name) + "/" + url.PathEscape(s.Session)
	var ignored any
	if err := kube.GetJSON(ctx, x.transport, x.Config.Dashboard, path, nil, sessionLoadTimeout, &ignored); err != nil {
		return err
	}
	x.mu.Lock()
	x.loaded[key] = time.Now()
	x.mu.Unlock()
	return nil
}

// RayJobIDFor resolves Ray's hex job ID for a submission in an archived session.
func (x *Index) RayJobIDFor(ctx context.Context, s Session, submissionID string) (string, error) {
	x.mu.RLock()
	cached := x.jobIDs[s.Session][submissionID]
	x.mu.RUnlock()
	if cached != "" {
		return cached, nil
	}
	if err := x.EnsureLoaded(ctx, s, false); err != nil {
		return "", err
	}
	var jobs []any
	header := http.Header{"Cookie": {CookieHeader(s)}}
	if err := kube.GetJSON(ctx, x.transport, x.Config.Dashboard, "/api/jobs/", header, x.timeout, &jobs); err != nil {
		return "", err
	}
	mapping := map[string]string{}
	for _, raw := range jobs {
		j := domain.Dict(raw)
		if sub, id := domain.Str(j["submission_id"]), domain.Str(j["job_id"]); sub != "" && id != "" {
			mapping[sub] = id
		}
	}
	x.mu.Lock()
	x.jobIDs[s.Session] = mapping
	x.mu.Unlock()
	return mapping[submissionID], nil
}
