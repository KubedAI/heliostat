package domain

import "strings"

// RayJobStatus maps a Ray Jobs API status (PENDING, RUNNING, STOPPED, SUCCEEDED, FAILED).
func RayJobStatus(value string) JobStatus {
	switch s := JobStatus(strings.ToUpper(value)); s {
	case StatusPending, StatusRunning, StatusStopped, StatusSucceeded, StatusFailed:
		return s
	}
	return StatusUnknown
}

// RayJobResourceStatus combines a RayJob's jobDeploymentStatus (KubeRay lifecycle) with its
// jobStatus (Ray job). The deployment status wins for terminal and suspended states: a RayJob that
// exceeds activeDeadlineSeconds, or whose submitter fails, is Failed while jobStatus can still read
// RUNNING or be empty.
func RayJobResourceStatus(deploymentStatus, jobStatus string) JobStatus {
	var job JobStatus
	if jobStatus != "" {
		job = RayJobStatus(jobStatus)
	}
	orDefault := func(fallback JobStatus) JobStatus {
		if job != "" {
			return job
		}
		return fallback
	}
	switch deploymentStatus {
	case "Complete":
		if job == StatusFailed || job == StatusStopped {
			return job
		}
		return StatusSucceeded
	case "Failed", "ValidationFailed":
		if job == StatusStopped {
			return StatusStopped
		}
		return StatusFailed
	case "Suspended", "Suspending":
		return StatusSuspended
	case "New", "Initializing", "Waiting", "Retrying", "":
		return StatusPending
	case "Running":
		return orDefault(StatusPending)
	default:
		return orDefault(StatusUnknown)
	}
}

// RayJobPhase is the raw phase shown under the status badge.
func RayJobPhase(deploymentStatus, jobStatus string) string {
	switch {
	case deploymentStatus != "" && jobStatus != "":
		return deploymentStatus + " · " + jobStatus
	case deploymentStatus != "":
		return deploymentStatus
	case jobStatus != "":
		return jobStatus
	}
	return "New"
}

// Condition is a Kubernetes status condition.
type Condition struct {
	Type, Status, Reason, Message string
}

// EndpointHealth derives RayService health from the Ready and UpgradeInProgress conditions
// (KubeRay >= 1.3), falling back to Serve application statuses for older operators.
func EndpointHealth(conditions []Condition, applicationStatuses []string) (EndpointStatus, string) {
	var ready, upgrading *Condition
	for i := range conditions {
		switch conditions[i].Type {
		case "Ready":
			ready = &conditions[i]
		case "UpgradeInProgress":
			upgrading = &conditions[i]
		}
	}
	unhealthy := false
	for _, s := range applicationStatuses {
		if s == "DEPLOY_FAILED" || s == "UNHEALTHY" {
			unhealthy = true
		}
	}
	firstOf := func(values ...string) string {
		for _, v := range values {
			if v != "" {
				return v
			}
		}
		return ""
	}

	switch {
	case upgrading != nil && upgrading.Status == "True":
		return EndpointUpgrading, firstOf(upgrading.Message, upgrading.Reason)
	case ready != nil && ready.Status == "True":
		return EndpointReady, ready.Reason
	case unhealthy:
		if ready != nil {
			return EndpointUnhealthy, firstOf(ready.Message, ready.Reason)
		}
		return EndpointUnhealthy, ""
	case ready != nil:
		return EndpointDeploying, firstOf(ready.Message, ready.Reason)
	case len(applicationStatuses) == 0:
		return EndpointUnknown, ""
	}
	for _, s := range applicationStatuses {
		if s != "RUNNING" {
			return EndpointDeploying, ""
		}
	}
	return EndpointReady, ""
}
