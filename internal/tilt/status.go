package tilt

// Status is the simplified state shown next to each resource.
type Status int

const (
	StatusNone Status = iota
	StatusPending
	StatusBuilding
	StatusHealthy
	StatusUnhealthy
	StatusDisabled
)

func (s Status) String() string {
	switch s {
	case StatusPending:
		return "pending"
	case StatusBuilding:
		return "building"
	case StatusHealthy:
		return "ok"
	case StatusUnhealthy:
		return "error"
	case StatusDisabled:
		return "disabled"
	default:
		return "idle"
	}
}

// IsDisabled treats "Pending" as disabled too: the user asked for the
// resource to be disabled and Tilt is still tearing it down.
func (r Resource) IsDisabled() bool {
	ds := r.Status.DisableStatus
	return ds != nil && (ds.State == "Disabled" || ds.State == "Pending")
}

// BuildStatus reports the state of the most recent update (build + deploy).
// Ported from tilt-tui src/tilt/status-utils.ts buildStatus.
func (r Resource) BuildStatus() Status {
	if r.IsDisabled() {
		return StatusDisabled
	}
	switch r.Status.UpdateStatus {
	case UpdateInProgress:
		return StatusBuilding
	case UpdatePending:
		return StatusPending
	case UpdateError:
		return StatusUnhealthy
	case UpdateOK:
		return StatusHealthy
	default:
		return StatusNone
	}
}

// RuntimeState reports whether the deployed thing is up (pod ready, local
// serve_cmd running, ...). Ported from status-utils.ts runtimeStatus.
func (r Resource) RuntimeState() Status {
	if r.IsDisabled() {
		return StatusDisabled
	}
	switch r.Status.RuntimeStatus {
	case RuntimeError:
		return StatusUnhealthy
	case RuntimePending:
		return StatusPending
	case RuntimeOK:
		return StatusHealthy
	default:
		return StatusNone
	}
}

// Combined folds build and runtime state into the one status shown in the
// resource list.
//
// TODO(you): decide precedence. Today it only reports build state, so a
// resource whose build passed but whose pod is crash-looping shows "ok".
// Questions to answer: does an in-progress build outrank a runtime error?
// Should a pending runtime after a good build read as "pending" or "ok"?
// For a local_resource with no serve_cmd, runtime is StatusNone.
func (r Resource) Combined() Status {
	build, runtime := r.BuildStatus(), r.RuntimeState()
	_ = runtime
	return build
}
