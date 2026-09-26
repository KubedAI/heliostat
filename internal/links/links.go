// Package links decides where a job's deep link goes and builds Heliostat's proxy paths. It does
// no I/O.
package links

import (
	"net/url"

	"github.com/KubedAI/heliostat/internal/domain"
)

const (
	LivePrefix    = "/ray/live"
	HistoryPrefix = "/ray/history"
)

// LiveDashboardPath is where a live Ray cluster's dashboard is mounted, with a trailing slash so
// the dashboard's relative asset URLs resolve under it.
func LiveDashboardPath(cluster, namespace, rayCluster string) string {
	return LivePrefix + "/" + url.PathEscape(cluster) + "/" + url.PathEscape(namespace) + "/" + url.PathEscape(rayCluster) + "/"
}

// HistoryDashboardPath is where an archived session's dashboard is mounted.
func HistoryDashboardPath(cluster, namespace, rayCluster, session string) string {
	return HistoryPrefix + "/" + url.PathEscape(cluster) + "/" + url.PathEscape(namespace) + "/" +
		url.PathEscape(rayCluster) + "/" + url.PathEscape(session) + "/"
}

// JobFragment is the Ray Dashboard's hash route for a job, or its job list.
func JobFragment(jobID string) string {
	if jobID == "" {
		return "#/jobs"
	}
	return "#/jobs/" + url.PathEscape(jobID)
}

// JobPagePath is Heliostat's own job page.
func JobPagePath(id string) string { return "/job?id=" + url.QueryEscape(id) }

// Diagnostics is where a job's deep link goes.
type Diagnostics struct {
	Kind   domain.DiagnosticsKind
	Href   string
	Reason string
}

// Context answers the questions diagnostics needs without doing I/O.
type Context struct {
	// DashboardsEnabled is whether live Ray Dashboards are reachable through Heliostat.
	DashboardsEnabled bool
	// HasLiveHead reports whether the Ray cluster exists with a head service.
	HasLiveHead func(namespace, rayCluster string) bool
	// HistorySession returns the archived session for the Ray cluster, or "".
	HistorySession    func(namespace, rayCluster string) string
	HistoryConfigured bool
}

// For decides a job's link: the live dashboard while its Ray cluster runs, then the History Server
// archive, otherwise a reason for the UI.
func For(job domain.JobRecord, ctx Context) Diagnostics {
	rc := job.RayClusterName
	if rc == "" {
		return Diagnostics{Kind: domain.DiagnosticsNone, Reason: "No Ray cluster has been created for this job yet."}
	}
	if ctx.DashboardsEnabled && ctx.HasLiveHead(job.Namespace, rc) {
		id := job.SubmissionID
		if id == "" {
			id = job.RayJobID
		}
		return Diagnostics{Kind: domain.DiagnosticsLive, Href: LiveDashboardPath(job.Cluster, job.Namespace, rc) + JobFragment(id)}
	}
	if session := ctx.HistorySession(job.Namespace, rc); session != "" {
		// The archived dashboard can only open jobs by Ray's hex job ID.
		return Diagnostics{Kind: domain.DiagnosticsHistory,
			Href: HistoryDashboardPath(job.Cluster, job.Namespace, rc, session) + JobFragment(job.RayJobID)}
	}
	if !ctx.HistoryConfigured {
		return Diagnostics{Kind: domain.DiagnosticsNone, Reason: "The Ray cluster is gone and no Ray History Server is configured."}
	}
	return Diagnostics{Kind: domain.DiagnosticsNone, Reason: "The Ray cluster is gone and the History Server has no archive for it. " +
		"Enable historyServerOptions on the cluster spec to retain diagnostics."}
}
