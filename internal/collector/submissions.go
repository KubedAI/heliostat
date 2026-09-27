package collector

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/KubedAI/heliostat/internal/config"
	"github.com/KubedAI/heliostat/internal/domain"
	"github.com/KubedAI/heliostat/internal/kube"
	"github.com/KubedAI/heliostat/internal/store"
)

// HeadService is a Ray cluster's dashboard service, if its head is up.
func HeadService(rc domain.RayClusterRecord) (config.ServiceRef, bool) {
	if rc.HeadServiceName == "" {
		return config.ServiceRef{}, false
	}
	return config.ServiceRef{Namespace: rc.Namespace, Name: rc.HeadServiceName, Port: rc.DashboardPort}, true
}

// Pollable reports whether a Ray cluster's Jobs API is worth polling: a ready head not managed
// by a RayService (Serve clusters run no batch jobs).
func Pollable(rc domain.RayClusterRecord) bool {
	return rc.State == "ready" && rc.HeadServiceName != "" && (rc.Owner == nil || rc.Owner.Kind != "RayService")
}

// SubmissionPoller polls GET /api/jobs/ on every live Ray head. This is the only way to see work
// submitted to an existing cluster with `ray job submit`, the Jobs SDK, or an interactive driver,
// because none of those create Kubernetes objects. It also learns Ray's hex job ID for
// RayJob-managed submissions, which the History Server needs for job links.
type SubmissionPoller struct {
	cluster     string
	transport   kube.ServiceTransport
	store       *store.Store
	onChange    func()
	rayClusters func() []domain.RayClusterRecord
	rayJobs     func() []domain.JobRecord
	// inventory reports the health of the RayCluster informer that rayClusters reads from. While it
	// is not synced, an empty target list means "unknown", not "nothing to poll".
	inventory   func() domain.SourceHealth
	timeout     time.Duration
	concurrency int
	log         *slog.Logger

	mu     sync.Mutex
	health domain.SourceHealth
}

// Status reports the poller's health.
func (p *SubmissionPoller) Status() domain.SourceHealth {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.health
}

// Run polls every interval until ctx is cancelled.
func (p *SubmissionPoller) Run(ctx context.Context, interval time.Duration) {
	for {
		p.Tick(ctx)
		select {
		case <-ctx.Done():
			return
		case <-time.After(interval):
		}
	}
}

// Tick polls every eligible Ray head once.
func (p *SubmissionPoller) Tick(ctx context.Context) {
	var targets []domain.RayClusterRecord
	for _, rc := range p.rayClusters() {
		if Pollable(rc) {
			targets = append(targets, rc)
		}
	}
	owningRayJob := map[string]domain.JobRecord{}
	for _, job := range p.rayJobs() {
		if job.RayClusterName != "" {
			owningRayJob[job.Namespace+"/"+job.RayClusterName] = job
		}
	}

	var mu sync.Mutex
	var failures []string
	changed := false
	sem := make(chan struct{}, max(1, p.concurrency))
	var wg sync.WaitGroup
	for _, rc := range targets {
		wg.Add(1)
		sem <- struct{}{}
		go func(rc domain.RayClusterRecord) {
			defer wg.Done()
			defer func() { <-sem }()
			svc, _ := HeadService(rc)
			var entries []any
			if err := kube.GetJSON(ctx, p.transport, svc, "/api/jobs/", nil, p.timeout, &entries); err != nil {
				mu.Lock()
				failures = append(failures, fmt.Sprintf("%s/%s: %v", rc.Namespace, rc.Name, err))
				mu.Unlock()
				return
			}
			var owner *domain.JobRecord
			if rc.Owner != nil && rc.Owner.Kind == "RayJob" {
				if j, ok := owningRayJob[rc.Namespace+"/"+rc.Name]; ok {
					owner = &j
				}
			}
			if p.ingest(rc, entries, owner) {
				mu.Lock()
				changed = true
				mu.Unlock()
			}
		}(rc)
	}
	wg.Wait()

	if changed {
		p.onChange()
	}
	now := time.Now().UTC().Format(time.RFC3339)
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.inventory != nil {
		if inv := p.inventory(); !inv.Synced {
			reason := inv.Error
			if reason == "" {
				reason = "not synced yet"
			}
			p.health = domain.SourceHealth{LastSuccessAt: p.health.LastSuccessAt, Error: "RayCluster inventory unavailable: " + reason}
			return
		}
	}
	if len(failures) == 0 {
		p.health = domain.SourceHealth{Synced: true, LastSuccessAt: now}
		return
	}
	p.log.Warn("some Ray heads could not be polled", "failures", failures)
	partial := len(failures) < len(targets)
	p.health = domain.SourceHealth{
		Synced:        partial,
		LastSuccessAt: p.health.LastSuccessAt,
		Error:         fmt.Sprintf("%d of %d Ray heads unreachable (first: %s)", len(failures), len(targets), failures[0]),
	}
	if partial {
		p.health.LastSuccessAt = now
	}
}

func (p *SubmissionPoller) ingest(rc domain.RayClusterRecord, entries []any, owner *domain.JobRecord) bool {
	changed := false
	ctx := domain.RayClusterContext{
		Cluster: p.cluster, Namespace: rc.Namespace, RayClusterName: rc.Name, RayClusterUID: rc.ID, Compute: rc.Compute,
	}
	for _, raw := range entries {
		job, ok := domain.NormalizeRayAPIJob(domain.Dict(raw), ctx, time.Now())
		if !ok {
			continue
		}
		if owner != nil && job.SubmissionID != "" && job.SubmissionID == owner.SubmissionID {
			// Same job as the RayJob CR; only borrow the hex job ID it lacks.
			if stored, found := p.store.Get(owner.ID); found && job.RayJobID != "" && stored.RayJobID != job.RayJobID {
				stored.RayJobID = job.RayJobID
				if ok, err := p.store.Upsert(stored); err == nil && ok {
					changed = true
				}
			}
			continue
		}
		if ok, err := p.store.Upsert(job); err == nil && ok {
			changed = true
		}
	}
	return changed
}
