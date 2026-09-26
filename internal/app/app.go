// Package app wires configuration, the job store, and one collector per Kubernetes cluster, and
// builds the views the HTTP API serves. API handlers read from here and never call clusters.
package app

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync/atomic"
	"time"

	"github.com/KubedAI/heliostat/internal/collector"
	"github.com/KubedAI/heliostat/internal/config"
	"github.com/KubedAI/heliostat/internal/domain"
	"github.com/KubedAI/heliostat/internal/links"
	"github.com/KubedAI/heliostat/internal/store"
)

const retentionInterval = time.Hour

// App is the process-wide Heliostat instance.
type App struct {
	Config    *config.Config
	Store     *store.Store
	Clusters  []*collector.Cluster
	byName    map[string]*collector.Cluster
	startedAt time.Time
	changes   atomic.Int64
	log       *slog.Logger
}

// New opens the store and creates (but does not start) the collectors.
func New(cfg *config.Config, log *slog.Logger) (*App, error) {
	st, err := store.Open(cfg.DatabasePath)
	if err != nil {
		return nil, fmt.Errorf("open job store: %w", err)
	}
	a := &App{Config: cfg, Store: st, byName: map[string]*collector.Cluster{}, startedAt: time.Now(), log: log}
	for _, c := range cfg.Clusters {
		col := collector.New(c, cfg.Collector, st, func() { a.changes.Add(1) }, log)
		a.Clusters = append(a.Clusters, col)
		a.byName[c.Name] = col
	}
	return a, nil
}

// Run starts every collector and the retention loop, returning when ctx is cancelled.
func (a *App) Run(ctx context.Context) {
	for _, c := range a.Clusters {
		go c.Run(ctx)
	}
	names := make([]string, 0, len(a.Clusters))
	for _, c := range a.Clusters {
		names = append(names, c.Config.Name)
	}
	a.log.Info("started", "clusters", names)
	for {
		if n, err := a.Store.Purge(a.Config.RetentionDays, time.Now()); err != nil {
			a.log.Error("retention purge failed", "error", err)
		} else if n > 0 {
			a.log.Info("purged expired jobs", "removed", n, "retentionDays", a.Config.RetentionDays)
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(retentionInterval):
		}
	}
}

// Cluster returns a configured cluster's collector.
func (a *App) Cluster(name string) (*collector.Cluster, bool) {
	c, ok := a.byName[name]
	return c, ok
}

// Revision changes whenever observable state changes; used for HTTP ETags.
func (a *App) Revision() string {
	return fmt.Sprintf("%d.%d", a.changes.Load(), a.Store.Revision())
}

// ClusterInfo describes every configured cluster.
func (a *App) ClusterInfo() []domain.ClusterInfo {
	out := make([]domain.ClusterInfo, 0, len(a.Clusters))
	for _, c := range a.Clusters {
		out = append(out, c.Info())
	}
	return out
}

func (a *App) diagnostics(job domain.JobRecord) links.Diagnostics {
	c, ok := a.byName[job.Cluster]
	if !ok {
		return links.Diagnostics{Kind: domain.DiagnosticsNone, Reason: fmt.Sprintf("Cluster %q is no longer configured.", job.Cluster)}
	}
	hist := c.History()
	return links.For(job, links.Context{
		DashboardsEnabled: c.Config.RayDashboards,
		HasLiveHead: func(ns, name string) bool {
			rc, ok := c.RayCluster(ns, name)
			return ok && rc.HeadServiceName != ""
		},
		HistorySession: func(ns, name string) string {
			if hist == nil {
				return ""
			}
			s, _ := hist.Latest(ns, name)
			return s.Session
		},
		HistoryConfigured: hist != nil,
	})
}

func (a *App) view(job domain.JobRecord) domain.JobView {
	d := a.diagnostics(job)
	return domain.JobView{JobRecord: job, Diagnostics: d.Kind, DiagnosticsReason: d.Reason}
}

// Jobs serves /api/jobs.
func (a *App) Jobs(q domain.JobQuery) (domain.JobPage, error) {
	page, err := a.Store.Query(q, time.Now())
	if err != nil {
		return domain.JobPage{}, err
	}
	items := make([]domain.JobView, 0, len(page.Items))
	for _, j := range page.Items {
		items = append(items, a.view(j))
	}
	return domain.JobPage{
		Items: items, NextCursor: page.NextCursor,
		Facets:   domain.Facets{Status: page.StatusCounts, Namespaces: page.Namespaces},
		Clusters: a.ClusterInfo(),
	}, nil
}

// Job serves /api/jobs/{id}.
func (a *App) Job(id string) (domain.JobView, bool) {
	job, ok := a.Store.Get(id)
	if !ok {
		return domain.JobView{}, false
	}
	return a.view(job), true
}

// JobDashboardHref resolves a job's Ray Dashboard URL at click time: live, then archived. For
// archives it may ask the History Server for Ray's hex job ID, and stores it for next time.
func (a *App) JobDashboardHref(ctx context.Context, id string) (string, bool) {
	job, ok := a.Store.Get(id)
	if !ok {
		return "", false
	}
	d := a.diagnostics(job)
	if d.Kind == domain.DiagnosticsNone {
		return "", false
	}
	if d.Kind != domain.DiagnosticsHistory || job.RayJobID != "" || job.SubmissionID == "" {
		return d.Href, true
	}
	hist := a.byName[job.Cluster].History()
	session, ok := hist.Latest(job.Namespace, job.RayClusterName)
	if !ok {
		return d.Href, true
	}
	rayJobID, err := hist.RayJobIDFor(ctx, session, job.SubmissionID)
	if err != nil {
		a.log.Warn("history job lookup failed", "job", id, "error", err)
		return d.Href, true
	}
	if rayJobID == "" {
		return d.Href, true
	}
	job.RayJobID = rayJobID
	_, _ = a.Store.Upsert(job)
	return d.Href[:strings.Index(d.Href, "#")] + links.JobFragment(rayJobID), true
}

// RayClusters serves /api/clusters.
func (a *App) RayClusters() []domain.RayClusterView {
	active, err := a.Store.ActiveJobsByRayCluster()
	if err != nil {
		a.log.Error("store read failed", "error", err)
	}
	out := []domain.RayClusterView{}
	for _, c := range a.Clusters {
		for _, rc := range c.RayClusters() {
			v := domain.RayClusterView{RayClusterRecord: rc, ActiveJobs: active[store.RayClusterKey(rc.Cluster, rc.Namespace, rc.Name)]}
			if c.Config.RayDashboards && rc.HeadServiceName != "" {
				v.DashboardHref = links.LiveDashboardPath(rc.Cluster, rc.Namespace, rc.Name)
			}
			out = append(out, v)
		}
	}
	return out
}

// Endpoints serves /api/endpoints.
func (a *App) Endpoints() []domain.EndpointView {
	out := []domain.EndpointView{}
	for _, c := range a.Clusters {
		for _, ep := range c.Endpoints() {
			v := domain.EndpointView{EndpointRecord: ep}
			if rc, ok := c.RayCluster(ep.Namespace, ep.ActiveRayClusterName); ok && c.Config.RayDashboards && rc.HeadServiceName != "" {
				v.DashboardHref = links.LiveDashboardPath(ep.Cluster, ep.Namespace, rc.Name)
			}
			out = append(out, v)
		}
	}
	return out
}

// Health serves /api/health.
func (a *App) Health() domain.HealthView {
	jobs, active, err := a.Store.Counts()
	if err != nil {
		a.log.Error("store read failed", "error", err)
	}
	h := domain.HealthView{StartedAt: a.startedAt.UTC().Format(time.RFC3339), Counts: domain.Counts{Jobs: jobs, ActiveJobs: active}}
	for _, c := range a.Clusters {
		h.Clusters = append(h.Clusters, c.Health())
		h.Counts.RayClusters += len(c.RayClusters())
		h.Counts.Endpoints += len(c.Endpoints())
	}
	return h
}
