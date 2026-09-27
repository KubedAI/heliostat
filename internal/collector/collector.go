// Package collector observes one Kubernetes cluster: KubeRay RayJobs, RayClusters, and
// RayServices through shared informers, jobs submitted directly to Ray heads through their Jobs
// API, and (optionally) the Ray History Server's archive index.
package collector

import (
	"context"
	"log/slog"
	"sort"
	"sync"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/dynamic/dynamicinformer"
	"k8s.io/client-go/tools/cache"

	"github.com/KubedAI/heliostat/internal/config"
	"github.com/KubedAI/heliostat/internal/domain"
	"github.com/KubedAI/heliostat/internal/history"
	"github.com/KubedAI/heliostat/internal/kube"
	"github.com/KubedAI/heliostat/internal/store"
)

var (
	rayJobsGVR     = schema.GroupVersionResource{Group: "ray.io", Version: "v1", Resource: "rayjobs"}
	rayClustersGVR = schema.GroupVersionResource{Group: "ray.io", Version: "v1", Resource: "rayclusters"}
	rayServicesGVR = schema.GroupVersionResource{Group: "ray.io", Version: "v1", Resource: "rayservices"}

	deletedRayJob = store.CloseAs{
		Status: domain.StatusStopped, Reason: "Deleted",
		Message: "The RayJob was deleted before it reached a terminal state.",
	}
	deletedRayCluster = store.CloseAs{
		Status: domain.StatusUnknown, Reason: "ClusterDeleted",
		Message: "The Ray cluster was deleted before this job reported a terminal state. " +
			"Check the Ray History Server for its final state.",
	}
)

// source tracks the health of one informer.
type source struct {
	mu        sync.Mutex
	hasSynced func() bool
	// listedRV is the informer's last synced resource version. A watch error is cleared once it
	// moves past the version seen at failure, so a recovered but empty resource (no events to
	// call ok) does not report the old error forever.
	listedRV func() string
	failedRV string
	last     time.Time
	err      string
}

func (s *source) ok() {
	s.mu.Lock()
	s.last, s.err = time.Now(), ""
	s.mu.Unlock()
}

func (s *source) fail(err error) {
	s.mu.Lock()
	s.err = err.Error()
	if s.listedRV != nil {
		s.failedRV = s.listedRV()
	}
	s.mu.Unlock()
}

func (s *source) status() domain.SourceHealth {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.err != "" && s.listedRV != nil {
		if rv := s.listedRV(); rv != "" && rv != s.failedRV {
			s.last, s.err = time.Now(), ""
		}
	}
	h := domain.SourceHealth{Error: s.err}
	if s.hasSynced != nil && s.hasSynced() {
		h.Synced = s.err == ""
	}
	if !s.last.IsZero() {
		h.LastSuccessAt = s.last.UTC().Format(time.RFC3339)
	}
	return h
}

// Cluster collects everything Heliostat shows for one Kubernetes cluster.
type Cluster struct {
	Config    config.Cluster
	collector config.Collector
	store     *store.Store
	onChange  func()
	log       *slog.Logger

	mu          sync.RWMutex
	transport   kube.ServiceTransport
	history     *history.Index
	rayJobs     map[string]domain.JobRecord        // live RayJobs by UID
	rayClusters map[string]domain.RayClusterRecord // by "<namespace>/<name>"
	endpoints   map[string]domain.EndpointRecord   // by UID
	connectErr  string

	jobsSource, clustersSource, servicesSource source
	poller                                     *SubmissionPoller
}

// New creates a collector; Run connects and starts it.
func New(cfg config.Cluster, collectorCfg config.Collector, st *store.Store, onChange func(), log *slog.Logger) *Cluster {
	return &Cluster{
		Config: cfg, collector: collectorCfg, store: st, onChange: onChange,
		log:         log.With("cluster", cfg.Name),
		rayJobs:     map[string]domain.JobRecord{},
		rayClusters: map[string]domain.RayClusterRecord{},
		endpoints:   map[string]domain.EndpointRecord{},
	}
}

// Info describes the cluster for the UI.
func (c *Cluster) Info() domain.ClusterInfo {
	return domain.ClusterInfo{Name: c.Config.Name, DisplayName: c.Config.DisplayName, Region: c.Config.Region}
}

// Transport is nil until the cluster is connected.
func (c *Cluster) Transport() kube.ServiceTransport {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.transport
}

// History is nil when no History Server is configured or before connecting.
func (c *Cluster) History() *history.Index {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.history
}

// Run connects (retrying with backoff) and runs until ctx is cancelled.
func (c *Cluster) Run(ctx context.Context) {
	backoff := 2 * time.Second
	for {
		err := c.connectAndStart(ctx)
		if err == nil {
			<-ctx.Done()
			return
		}
		c.mu.Lock()
		c.connectErr = err.Error()
		c.mu.Unlock()
		c.log.Warn("cannot connect to cluster", "error", err, "retryIn", backoff)
		select {
		case <-ctx.Done():
			return
		case <-time.After(backoff):
		}
		backoff = min(backoff*2, 2*time.Minute)
	}
}

func (c *Cluster) connectAndStart(ctx context.Context) error {
	restCfg, err := kube.RESTConfig(ctx, c.Config.Connection)
	if err != nil {
		return err
	}
	transport, err := kube.NewServiceTransport(c.Config.Transport, restCfg)
	if err != nil {
		return err
	}
	dyn, err := dynamic.NewForConfig(restCfg)
	if err != nil {
		return err
	}
	c.log.Info("connecting", "connection", c.Config.Connection.Type, "serviceTransport", c.Config.Transport,
		"rayDashboards", c.Config.RayDashboards, "historyServer", c.Config.HistoryServer != nil)

	timeout := time.Duration(c.collector.RequestTimeoutSeconds) * time.Second
	c.mu.Lock()
	c.transport, c.connectErr = transport, ""
	if c.Config.HistoryServer != nil {
		c.history = history.NewIndex(*c.Config.HistoryServer, transport,
			time.Duration(c.collector.HistoryPollIntervalSeconds)*time.Second, timeout, c.onChange, c.log.With("source", "history"))
	}
	c.mu.Unlock()

	factory := dynamicinformer.NewDynamicSharedInformerFactory(dyn, time.Duration(c.collector.ResyncIntervalSeconds)*time.Second)
	jobs := c.informer(factory, rayJobsGVR, &c.jobsSource, cache.ResourceEventHandlerFuncs{
		AddFunc:    func(obj any) { c.upsertRayJob(obj) },
		UpdateFunc: func(_, obj any) { c.upsertRayJob(obj) },
		DeleteFunc: c.deleteRayJob,
	})
	clusters := c.informer(factory, rayClustersGVR, &c.clustersSource, cache.ResourceEventHandlerFuncs{
		AddFunc:    func(obj any) { c.upsertRayCluster(obj) },
		UpdateFunc: func(_, obj any) { c.upsertRayCluster(obj) },
		DeleteFunc: c.deleteRayCluster,
	})
	c.informer(factory, rayServicesGVR, &c.servicesSource, cache.ResourceEventHandlerFuncs{
		AddFunc:    func(obj any) { c.upsertEndpoint(obj) },
		UpdateFunc: func(_, obj any) { c.upsertEndpoint(obj) },
		DeleteFunc: c.deleteEndpoint,
	})
	factory.Start(ctx.Done())

	if c.Config.RayDashboards {
		c.poller = &SubmissionPoller{
			cluster: c.Config.Name, transport: transport, store: c.store, onChange: c.onChange,
			rayClusters: c.RayClusters, rayJobs: c.liveRayJobs, inventory: c.clustersSource.status,
			timeout: timeout, concurrency: c.collector.MaxConcurrentRequests, log: c.log.With("source", "submissions"),
		}
		go c.poller.Run(ctx, time.Duration(c.collector.SubmissionPollIntervalSeconds)*time.Second)
	}
	if h := c.History(); h != nil {
		go h.Run(ctx)
	}

	go func() {
		if cache.WaitForCacheSync(ctx.Done(), jobs.HasSynced, clusters.HasSynced) {
			c.reconcile()
		}
	}()
	return nil
}

func (c *Cluster) informer(factory dynamicinformer.DynamicSharedInformerFactory, gvr schema.GroupVersionResource,
	src *source, handler cache.ResourceEventHandlerFuncs) cache.SharedIndexInformer {
	inf := factory.ForResource(gvr).Informer()
	// Only normalized records are kept; drop the bulkiest metadata from the informer cache.
	_ = inf.SetTransform(func(obj any) (any, error) {
		if u, ok := obj.(*unstructured.Unstructured); ok {
			u.SetManagedFields(nil)
			if a := u.GetAnnotations(); a != nil {
				delete(a, "kubectl.kubernetes.io/last-applied-configuration")
				u.SetAnnotations(a)
			}
		}
		return obj, nil
	})
	_ = inf.SetWatchErrorHandler(func(_ *cache.Reflector, err error) {
		src.fail(err)
		c.log.Warn("watch error", "resource", gvr.Resource, "error", err)
	})
	src.hasSynced = inf.HasSynced
	src.listedRV = inf.LastSyncResourceVersion
	wrap := func(fn func(any)) func(any) {
		return func(obj any) { src.ok(); fn(obj) }
	}
	_, _ = inf.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    wrap(handler.AddFunc),
		UpdateFunc: func(old, obj any) { src.ok(); handler.UpdateFunc(old, obj) },
		DeleteFunc: wrap(handler.DeleteFunc),
	})
	return inf
}

func objectOf(obj any) (domain.Obj, bool) {
	if tomb, ok := obj.(cache.DeletedFinalStateUnknown); ok {
		obj = tomb.Obj
	}
	u, ok := obj.(*unstructured.Unstructured)
	if !ok {
		return nil, false
	}
	return u.Object, true
}

func (c *Cluster) changed(changed bool, err error) {
	if err != nil {
		c.log.Error("store write failed", "error", err)
		return
	}
	if changed {
		c.onChange()
	}
}

func (c *Cluster) upsertRayJob(obj any) {
	o, ok := objectOf(obj)
	if !ok {
		return
	}
	job, ok := domain.NormalizeRayJob(o, c.Config.Name)
	if !ok {
		return
	}
	c.mu.Lock()
	c.rayJobs[job.ID] = job
	c.mu.Unlock()
	c.changed(c.store.Upsert(job))
}

func (c *Cluster) deleteRayJob(obj any) {
	o, ok := objectOf(obj)
	if !ok {
		return
	}
	job, ok := domain.NormalizeRayJob(o, c.Config.Name)
	if !ok {
		return
	}
	c.mu.Lock()
	delete(c.rayJobs, job.ID)
	c.mu.Unlock()
	// Record the final observed state, then close it if it never finished.
	_, _ = c.store.Upsert(job)
	c.changed(c.store.MarkGone(job.ID, deletedRayJob, time.Now()))
}

func (c *Cluster) upsertRayCluster(obj any) {
	o, ok := objectOf(obj)
	if !ok {
		return
	}
	rc, ok := domain.NormalizeRayCluster(o, c.Config.Name)
	if !ok {
		return
	}
	c.mu.Lock()
	c.rayClusters[rc.Namespace+"/"+rc.Name] = rc
	c.mu.Unlock()
	c.onChange()
}

func (c *Cluster) deleteRayCluster(obj any) {
	o, ok := objectOf(obj)
	if !ok {
		return
	}
	rc, ok := domain.NormalizeRayCluster(o, c.Config.Name)
	if !ok {
		return
	}
	key := rc.Namespace + "/" + rc.Name
	c.mu.Lock()
	delete(c.rayClusters, key)
	c.mu.Unlock()
	submissions, err := c.store.PresentSubmissionsByRayCluster(c.Config.Name)
	if err != nil {
		c.log.Error("store read failed", "error", err)
	}
	for _, id := range submissions[key] {
		_, _ = c.store.MarkGone(id, deletedRayCluster, time.Now())
	}
	c.onChange()
}

func (c *Cluster) upsertEndpoint(obj any) {
	if o, ok := objectOf(obj); ok {
		if ep, ok := domain.NormalizeRayService(o, c.Config.Name); ok {
			c.mu.Lock()
			c.endpoints[ep.ID] = ep
			c.mu.Unlock()
			c.onChange()
		}
	}
}

func (c *Cluster) deleteEndpoint(obj any) {
	if o, ok := objectOf(obj); ok {
		if ep, ok := domain.NormalizeRayService(o, c.Config.Name); ok {
			c.mu.Lock()
			delete(c.endpoints, ep.ID)
			c.mu.Unlock()
			c.onChange()
		}
	}
}

// reconcile closes records whose objects disappeared while Heliostat was not running.
func (c *Cluster) reconcile() {
	c.mu.RLock()
	liveJobs := map[string]bool{}
	for id := range c.rayJobs {
		liveJobs[id] = true
	}
	liveClusters := map[string]bool{}
	for key := range c.rayClusters {
		liveClusters[key] = true
	}
	c.mu.RUnlock()

	changed := false
	ids, err := c.store.PresentIDs(c.Config.Name, domain.KindRayJob)
	if err != nil {
		c.log.Error("reconcile failed", "error", err)
		return
	}
	for _, id := range ids {
		if !liveJobs[id] {
			ok, _ := c.store.MarkGone(id, deletedRayJob, time.Now())
			changed = changed || ok
		}
	}
	submissions, _ := c.store.PresentSubmissionsByRayCluster(c.Config.Name)
	for key, ids := range submissions {
		if liveClusters[key] {
			continue
		}
		for _, id := range ids {
			ok, _ := c.store.MarkGone(id, deletedRayCluster, time.Now())
			changed = changed || ok
		}
	}
	if changed {
		c.onChange()
	}
	c.log.Info("synced", "rayJobs", len(liveJobs), "rayClusters", len(liveClusters))
}

// RayCluster returns a live Ray cluster by namespace and name.
func (c *Cluster) RayCluster(namespace, name string) (domain.RayClusterRecord, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	rc, ok := c.rayClusters[namespace+"/"+name]
	return rc, ok
}

// RayClusters returns every live Ray cluster, newest first.
func (c *Cluster) RayClusters() []domain.RayClusterRecord {
	c.mu.RLock()
	out := make([]domain.RayClusterRecord, 0, len(c.rayClusters))
	for _, rc := range c.rayClusters {
		out = append(out, rc)
	}
	c.mu.RUnlock()
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt > out[j].CreatedAt })
	return out
}

// Endpoints returns every RayService, by name.
func (c *Cluster) Endpoints() []domain.EndpointRecord {
	c.mu.RLock()
	out := make([]domain.EndpointRecord, 0, len(c.endpoints))
	for _, ep := range c.endpoints {
		out = append(out, ep)
	}
	c.mu.RUnlock()
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

func (c *Cluster) liveRayJobs() []domain.JobRecord {
	c.mu.RLock()
	defer c.mu.RUnlock()
	out := make([]domain.JobRecord, 0, len(c.rayJobs))
	for _, j := range c.rayJobs {
		out = append(out, j)
	}
	return out
}

// Health reports every source's state.
func (c *Cluster) Health() domain.ClusterHealth {
	h := domain.ClusterHealth{
		ClusterInfo: c.Info(),
		RayJobs:     c.jobsSource.status(),
		RayClusters: c.clustersSource.status(),
		RayServices: c.servicesSource.status(),
	}
	c.mu.RLock()
	connectErr := c.connectErr
	c.mu.RUnlock()
	if connectErr != "" {
		for _, s := range []*domain.SourceHealth{&h.RayJobs, &h.RayClusters, &h.RayServices} {
			s.Synced, s.Error = false, connectErr
		}
	}
	if c.poller != nil {
		s := c.poller.Status()
		h.Submissions = &s
	}
	if hist := c.History(); hist != nil {
		s := hist.Status()
		h.HistoryServer = &s
	}
	return h
}
