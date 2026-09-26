package collector

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/KubedAI/heliostat/internal/config"
	"github.com/KubedAI/heliostat/internal/domain"
	"github.com/KubedAI/heliostat/internal/store"
)

type fakeHeads map[string]string // head service name → JSON body; missing means unreachable

func (f fakeHeads) Do(_ context.Context, svc config.ServiceRef, _, _ string, _ http.Header) (*http.Response, error) {
	body, ok := f[svc.Name]
	if !ok {
		return nil, errUnreachable
	}
	return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body))}, nil
}

var errUnreachable = errors.New("connect ECONNREFUSED")

func rayCluster(mod func(*domain.RayClusterRecord)) domain.RayClusterRecord {
	rc := domain.RayClusterRecord{ID: "rc-uid", Cluster: "c1", Namespace: "team-a", Name: "shared", State: "ready",
		HeadServiceName: "shared-head-svc", DashboardPort: 8265,
		Compute: domain.ComputeIntent{InstanceTypes: []string{"g6e.xlarge"}, GPUs: 1, GPUModel: "L40S"}}
	if mod != nil {
		mod(&rc)
	}
	return rc
}

func poller(t *testing.T, heads fakeHeads, clusters []domain.RayClusterRecord, jobs []domain.JobRecord) (*SubmissionPoller, *store.Store, *int) {
	t.Helper()
	st, err := store.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	for _, j := range jobs {
		if _, err := st.Upsert(j); err != nil {
			t.Fatal(err)
		}
	}
	changes := 0
	return &SubmissionPoller{
		cluster: "c1", transport: heads, store: st, onChange: func() { changes++ },
		rayClusters: func() []domain.RayClusterRecord { return clusters },
		rayJobs:     func() []domain.JobRecord { return jobs },
		timeout:     time.Second, concurrency: 2, log: slog.New(slog.NewTextHandler(io.Discard, nil)),
	}, st, &changes
}

func TestPollable(t *testing.T) {
	cases := map[string]bool{
		"standalone":  Pollable(rayCluster(nil)),
		"rayjob":      Pollable(rayCluster(func(r *domain.RayClusterRecord) { r.Owner = &domain.Owner{Kind: "RayJob"} })),
		"rayservice":  !Pollable(rayCluster(func(r *domain.RayClusterRecord) { r.Owner = &domain.Owner{Kind: "RayService"} })),
		"not ready":   !Pollable(rayCluster(func(r *domain.RayClusterRecord) { r.State = "provisioning" })),
		"no head svc": !Pollable(rayCluster(func(r *domain.RayClusterRecord) { r.HeadServiceName = "" })),
	}
	for name, ok := range cases {
		if !ok {
			t.Errorf("%s", name)
		}
	}
}

func TestSubmissionsAndDrivers(t *testing.T) {
	p, st, changes := poller(t, fakeHeads{"shared-head-svc": `[
		{"type":"SUBMISSION","submission_id":"raysubmit_abc","job_id":"02000000","status":"RUNNING","start_time":1},
		{"type":"DRIVER","job_id":"03000000","status":"SUCCEEDED","start_time":2,"end_time":3}]`},
		[]domain.RayClusterRecord{rayCluster(nil)}, nil)
	p.Tick(context.Background())
	sub, _ := st.Get("rc-uid.raysubmit_abc")
	driver, _ := st.Get("rc-uid.03000000")
	if sub.Kind != domain.KindSubmission || sub.RayJobID != "02000000" || sub.Compute.GPUModel != "L40S" ||
		driver.Kind != domain.KindDriver || driver.Status != domain.StatusSucceeded || *changes != 1 {
		t.Errorf("sub %+v driver %+v changes %d", sub, driver, *changes)
	}
	if h := p.Status(); !h.Synced || h.Error != "" {
		t.Errorf("health: %+v", h)
	}
}

func TestOwningRayJobIsEnrichedNotDuplicated(t *testing.T) {
	owner := domain.JobRecord{ID: "rayjob-uid", Kind: domain.KindRayJob, Cluster: "c1", Namespace: "team-a",
		RayClusterName: "owned", SubmissionID: "my-job-xyz", Status: domain.StatusRunning, CreatedAt: "2026-09-25T00:00:00Z", Present: true}
	p, st, _ := poller(t, fakeHeads{"owned-head-svc": `[{"type":"SUBMISSION","submission_id":"my-job-xyz","job_id":"04000000","status":"RUNNING"}]`},
		[]domain.RayClusterRecord{rayCluster(func(r *domain.RayClusterRecord) {
			r.ID, r.Name, r.HeadServiceName, r.Owner = "owned-uid", "owned", "owned-head-svc", &domain.Owner{Kind: "RayJob", Name: "my-job"}
		})}, []domain.JobRecord{owner})
	p.Tick(context.Background())
	if got, _ := st.Get("rayjob-uid"); got.RayJobID != "04000000" {
		t.Errorf("owner not enriched: %+v", got)
	}
	if _, found := st.Get("owned-uid.my-job-xyz"); found {
		t.Error("duplicate submission recorded")
	}
}

func TestPartialFailure(t *testing.T) {
	p, st, _ := poller(t, fakeHeads{"shared-head-svc": `[{"submission_id":"raysubmit_ok","status":"SUCCEEDED"}]`},
		[]domain.RayClusterRecord{rayCluster(nil), rayCluster(func(r *domain.RayClusterRecord) {
			r.ID, r.Name, r.HeadServiceName = "down-uid", "down", "down-head-svc"
		})}, nil)
	p.Tick(context.Background())
	if _, found := st.Get("rc-uid.raysubmit_ok"); !found {
		t.Error("reachable head not ingested")
	}
	if h := p.Status(); !h.Synced || !strings.Contains(h.Error, "1 of 2 Ray heads unreachable") || !strings.Contains(h.Error, "team-a/down") {
		t.Errorf("health: %+v", h)
	}
}
