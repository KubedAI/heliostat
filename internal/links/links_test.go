package links

import (
	"strings"
	"testing"

	"github.com/KubedAI/heliostat/internal/domain"
)

func TestFor(t *testing.T) {
	job := domain.JobRecord{Cluster: "c1", Namespace: "team-a", RayClusterName: "rc-abc", SubmissionID: "rc-sub", RayJobID: "03000000"}
	ctx := func(dashboards, live bool, session string, history bool) Context {
		return Context{
			DashboardsEnabled: dashboards,
			HasLiveHead:       func(string, string) bool { return live },
			HistorySession:    func(string, string) string { return session },
			HistoryConfigured: history,
		}
	}
	cases := []struct {
		name string
		job  domain.JobRecord
		ctx  Context
		kind domain.DiagnosticsKind
		href string
		why  string
	}{
		{"live by submission id", job, ctx(true, true, "", true), domain.DiagnosticsLive, "/ray/live/c1/team-a/rc-abc/#/jobs/rc-sub", ""},
		{"history by ray job id", job, ctx(true, false, "session_1", true), domain.DiagnosticsHistory, "/ray/history/c1/team-a/rc-abc/session_1/#/jobs/03000000", ""},
		{"history list when id unknown", func() domain.JobRecord { j := job; j.RayJobID = ""; return j }(), ctx(true, false, "s", true), domain.DiagnosticsHistory, "/ray/history/c1/team-a/rc-abc/s/#/jobs", ""},
		{"dashboards disabled", job, ctx(false, true, "", true), domain.DiagnosticsNone, "", "historyServerOptions"},
		{"no ray cluster", domain.JobRecord{}, ctx(true, true, "", true), domain.DiagnosticsNone, "", "No Ray cluster"},
		{"no history server", job, ctx(true, false, "", false), domain.DiagnosticsNone, "", "no Ray History Server is configured"},
	}
	for _, c := range cases {
		got := For(c.job, c.ctx)
		if got.Kind != c.kind || got.Href != c.href || !strings.Contains(got.Reason, c.why) {
			t.Errorf("%s: %+v", c.name, got)
		}
	}
}
