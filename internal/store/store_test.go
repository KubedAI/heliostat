package store

import (
	"reflect"
	"testing"
	"time"

	"github.com/KubedAI/heliostat/internal/domain"
)

var now = time.Date(2026, 9, 26, 0, 0, 0, 0, time.UTC)

func open(t *testing.T) *Store {
	t.Helper()
	s, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func job(mod func(*domain.JobRecord)) domain.JobRecord {
	j := domain.JobRecord{
		ID: "uid-1", Kind: domain.KindRayJob, Cluster: "c1", Namespace: "team-a", Name: "job-1",
		Status: domain.StatusRunning, Phase: "Running · RUNNING", Compute: domain.ComputeIntent{InstanceTypes: []string{}},
		CreatedAt: "2026-09-25T10:00:00Z", Present: true,
	}
	if mod != nil {
		mod(&j)
	}
	return j
}

func must[T any](v T, err error) T {
	if err != nil {
		panic(err)
	}
	return v
}

func TestUpsertChangeDetectionAndMerge(t *testing.T) {
	s := open(t)
	if !must(s.Upsert(job(nil))) {
		t.Fatal("first write should change")
	}
	rev := s.Revision()
	if must(s.Upsert(job(nil))) || s.Revision() != rev {
		t.Fatal("identical write should be a no-op")
	}
	must(s.Upsert(job(func(j *domain.JobRecord) { j.RayJobID = "03000000" })))
	must(s.Upsert(job(func(j *domain.JobRecord) { j.Status = domain.StatusSucceeded })))
	if got, _ := s.Get("uid-1"); got.RayJobID != "03000000" || got.Status != domain.StatusSucceeded {
		t.Errorf("merge lost rayJobId: %+v", got)
	}

	must(s.Upsert(job(func(j *domain.JobRecord) {
		j.ID, j.Kind, j.CreatedAt = "s", domain.KindSubmission, "2026-09-25T10:00:00Z"
	})))
	must(s.Upsert(job(func(j *domain.JobRecord) {
		j.ID, j.Kind, j.CreatedAt = "s", domain.KindSubmission, "2026-09-25T10:05:00Z"
	})))
	if got, _ := s.Get("s"); got.CreatedAt != "2026-09-25T10:00:00Z" {
		t.Errorf("submission createdAt changed: %s", got.CreatedAt)
	}
}

func TestMarkGone(t *testing.T) {
	s := open(t)
	must(s.Upsert(job(nil)))
	closeAs := CloseAs{Status: domain.StatusStopped, Reason: "Deleted", Message: "gone"}
	if !must(s.MarkGone("uid-1", closeAs, now)) {
		t.Fatal("expected change")
	}
	got, _ := s.Get("uid-1")
	if got.Present || got.Status != domain.StatusStopped || got.Phase != "Deleted" || got.FinishedAt == "" {
		t.Errorf("closed: %+v", got)
	}
	if must(s.MarkGone("uid-1", closeAs, now)) {
		t.Error("second MarkGone should be a no-op")
	}

	must(s.Upsert(job(func(j *domain.JobRecord) {
		j.ID, j.Status, j.FinishedAt = "done", domain.StatusSucceeded, "2026-09-25T11:00:00Z"
	})))
	must(s.MarkGone("done", closeAs, now))
	if got, _ := s.Get("done"); got.Status != domain.StatusSucceeded || got.Present {
		t.Errorf("finished job must keep its status: %+v", got)
	}
}

func TestReconciliationQueries(t *testing.T) {
	s := open(t)
	must(s.Upsert(job(func(j *domain.JobRecord) { j.ID = "a" })))
	must(s.Upsert(job(func(j *domain.JobRecord) { j.ID, j.Cluster = "b", "c2" })))
	must(s.Upsert(job(func(j *domain.JobRecord) { j.ID, j.Kind, j.RayClusterName = "s", domain.KindSubmission, "rc" })))
	if ids := must(s.PresentIDs("c1", domain.KindRayJob)); !reflect.DeepEqual(ids, []string{"a"}) {
		t.Errorf("present ids: %v", ids)
	}
	if m := must(s.PresentSubmissionsByRayCluster("c1")); !reflect.DeepEqual(m, map[string][]string{"team-a/rc": {"s"}}) {
		t.Errorf("submissions: %v", m)
	}
	if m := must(s.ActiveJobsByRayCluster()); !reflect.DeepEqual(m, map[string]int{"c1/team-a/rc": 1}) {
		t.Errorf("active: %v", m)
	}
}

func seeded(t *testing.T) *Store {
	s := open(t)
	for _, j := range []domain.JobRecord{
		job(func(j *domain.JobRecord) {
			j.ID, j.Name, j.Status, j.CreatedAt = "old-done", "nightly", domain.StatusSucceeded, "2026-09-01T00:00:00Z"
		}),
		job(func(j *domain.JobRecord) {
			j.ID, j.Name, j.CreatedAt = "old-running", "marathon", "2026-08-01T00:00:00Z"
		}),
		job(func(j *domain.JobRecord) {
			j.ID, j.Name, j.Status, j.CreatedAt, j.Model = "new-failed", "extract", domain.StatusFailed, "2026-09-25T12:00:00Z", "Gemma-4"
		}),
		job(func(j *domain.JobRecord) {
			j.ID, j.Name, j.Status, j.CreatedAt, j.Namespace = "new-ok", "summarize_100%", domain.StatusSucceeded, "2026-09-25T13:00:00Z", "team-b"
		}),
	} {
		must(s.Upsert(j))
	}
	return s
}

func ids(p Page) []string {
	var out []string
	for _, j := range p.Items {
		out = append(out, j.ID)
	}
	return out
}

func TestQuery(t *testing.T) {
	s := seeded(t)
	if got := ids(must(s.Query(domain.JobQuery{Window: "24h", Limit: 10}, now))); !reflect.DeepEqual(got, []string{"new-ok", "new-failed", "old-running"}) {
		t.Errorf("window keeps unfinished jobs: %v", got)
	}
	failed := must(s.Query(domain.JobQuery{Window: "all", Limit: 10, Status: domain.StatusFailed}, now))
	if !reflect.DeepEqual(ids(failed), []string{"new-failed"}) ||
		!reflect.DeepEqual(failed.StatusCounts, map[domain.JobStatus]int{"SUCCEEDED": 2, "RUNNING": 1, "FAILED": 1}) ||
		!reflect.DeepEqual(failed.Namespaces, []string{"team-a", "team-b"}) {
		t.Errorf("facets: %+v", failed)
	}
	for q, want := range map[string]string{"gemma": "new-failed", "100%": "new-ok", "_": "new-ok"} {
		if got := ids(must(s.Query(domain.JobQuery{Window: "all", Limit: 10, Q: q}, now))); !reflect.DeepEqual(got, []string{want}) {
			t.Errorf("search %q: %v", q, got)
		}
	}
	first := must(s.Query(domain.JobQuery{Window: "all", Limit: 3}, now))
	second := must(s.Query(domain.JobQuery{Window: "all", Limit: 3, Cursor: first.NextCursor}, now))
	if len(first.Items) != 3 || !reflect.DeepEqual(ids(second), []string{"old-running"}) || second.NextCursor != "" {
		t.Errorf("pagination: %v then %v", ids(first), ids(second))
	}
	if n := len(must(s.Query(domain.JobQuery{Window: "all", Limit: 10, Cursor: "garbage"}, now)).Items); n != 4 {
		t.Errorf("bad cursor should restart: %d", n)
	}
}

func TestPurge(t *testing.T) {
	s := open(t)
	must(s.Upsert(job(func(j *domain.JobRecord) {
		j.ID, j.Status, j.FinishedAt = "live", domain.StatusSucceeded, "2026-01-01T00:00:00Z"
	})))
	must(s.Upsert(job(func(j *domain.JobRecord) {
		j.ID, j.Status, j.FinishedAt, j.Present = "expired", domain.StatusSucceeded, "2026-01-01T00:00:00Z", false
	})))
	must(s.Upsert(job(func(j *domain.JobRecord) {
		j.ID, j.Status, j.FinishedAt, j.Present = "recent", domain.StatusSucceeded, "2026-09-20T00:00:00Z", false
	})))
	if n := must(s.Purge(30, now)); n != 1 {
		t.Errorf("purged %d", n)
	}
	if _, ok := s.Get("expired"); ok {
		t.Error("expired job kept")
	}
}

// The same namespace and Ray cluster name in two Kubernetes clusters must never mix, and a
// Ray cluster filter must not match names that merely share a prefix.
func TestRayClusterFilterAcrossClusters(t *testing.T) {
	s := open(t)
	for _, j := range []domain.JobRecord{
		job(func(j *domain.JobRecord) { j.ID, j.Cluster, j.RayClusterName = "east", "eks-east", "shared" }),
		job(func(j *domain.JobRecord) { j.ID, j.Cluster, j.RayClusterName = "west", "eks-west", "shared" }),
		job(func(j *domain.JobRecord) { j.ID, j.Cluster, j.RayClusterName = "west-2", "eks-west", "shared-2" }),
	} {
		must(s.Upsert(j))
	}
	scoped := must(s.Query(domain.JobQuery{Window: "all", Limit: 10, Cluster: "eks-west", Namespace: "team-a", RayCluster: "shared"}, now))
	if got := ids(scoped); len(got) != 1 || got[0] != "west" {
		t.Errorf("cluster-scoped: %v", got)
	}
	byName := ids(must(s.Query(domain.JobQuery{Window: "all", Limit: 10, RayCluster: "shared"}, now)))
	if len(byName) != 2 || contains(byName, "west-2") {
		t.Errorf("name-only filter must match exactly %q in every cluster: %v", "shared", byName)
	}
}

func contains(list []string, v string) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}
