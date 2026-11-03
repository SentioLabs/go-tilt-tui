// Package tilt is a minimal client for the Tilt HUD server API
// (the same endpoints the Tilt web UI uses on port 10350).
//
// The structs here mirror the JSON shapes of Tilt's webview API but only
// declare the fields this program reads. encoding/json ignores the rest,
// which keeps us tolerant of Tilt adding fields between releases.
package tilt

import "time"

// View is one message from the /ws/view WebSocket.
//
// Tilt sends a full snapshot first (IsComplete == true) and then incremental
// updates that contain only the resources and buttons that changed.
type View struct {
	TiltStartTime time.Time  `json:"tiltStartTime"`
	UISession     *Session   `json:"uiSession,omitempty"`
	UIResources   []Resource `json:"uiResources,omitempty"`
	UIButtons     []Button   `json:"uiButtons,omitempty"`
	LogList       *LogList   `json:"logList,omitempty"`
	IsComplete    bool       `json:"isComplete"`
}

type ObjectMeta struct {
	Name              string            `json:"name"`
	ResourceVersion   string            `json:"resourceVersion,omitempty"`
	Labels            map[string]string `json:"labels,omitempty"`
	Annotations       map[string]string `json:"annotations,omitempty"`
	DeletionTimestamp *time.Time        `json:"deletionTimestamp,omitempty"`
}

type Session struct {
	Metadata ObjectMeta `json:"metadata"`
	Status   struct {
		TiltStartTime time.Time `json:"tiltStartTime"`
		TiltfileKey   string    `json:"tiltfileKey"`
	} `json:"status"`
}

// Resource is a UIResource: one row in the Tilt sidebar.
type Resource struct {
	Metadata ObjectMeta     `json:"metadata"`
	Status   ResourceStatus `json:"status"`
}

func (r Resource) Name() string { return r.Metadata.Name }

type ResourceStatus struct {
	Order             int              `json:"order"`
	UpdateStatus      UpdateStatus     `json:"updateStatus,omitempty"`
	RuntimeStatus     RuntimeStatus    `json:"runtimeStatus,omitempty"`
	BuildHistory      []BuildRecord    `json:"buildHistory,omitempty"`
	PendingBuildSince *time.Time       `json:"pendingBuildSince,omitempty"`
	DisableStatus     *DisableStatus   `json:"disableStatus,omitempty"`
	Waiting           *Waiting         `json:"waiting,omitempty"`
	EndpointLinks     []EndpointLink   `json:"endpointLinks,omitempty"`
	Specs             []TargetSpec     `json:"specs,omitempty"`
	K8sResourceInfo   *K8sResourceInfo `json:"k8sResourceInfo,omitempty"`
	TriggerMode       int              `json:"triggerMode,omitempty"`
}

// UpdateStatus mirrors pkg/model.UpdateStatus in Tilt.
type UpdateStatus string

const (
	UpdateOK            UpdateStatus = "ok"
	UpdatePending       UpdateStatus = "pending"
	UpdateInProgress    UpdateStatus = "in_progress"
	UpdateError         UpdateStatus = "error"
	UpdateNotApplicable UpdateStatus = "not_applicable"
	UpdateNone          UpdateStatus = "none"
)

// RuntimeStatus mirrors pkg/model.RuntimeStatus in Tilt.
type RuntimeStatus string

const (
	RuntimeOK            RuntimeStatus = "ok"
	RuntimePending       RuntimeStatus = "pending"
	RuntimeError         RuntimeStatus = "error"
	RuntimeNotApplicable RuntimeStatus = "not_applicable"
	RuntimeNone          RuntimeStatus = "none"
)

type BuildRecord struct {
	StartTime  *time.Time `json:"startTime,omitempty"`
	FinishTime *time.Time `json:"finishTime,omitempty"`
	Error      string     `json:"error,omitempty"`
	Warnings   []string   `json:"warnings,omitempty"`
}

type DisableStatus struct {
	EnabledCount  int    `json:"enabledCount"`
	DisabledCount int    `json:"disabledCount"`
	State         string `json:"state"` // "Enabled" | "Disabled" | "Pending" | "Error"
}

type Waiting struct {
	Reason string `json:"reason"`
	On     []struct {
		Kind string `json:"kind"`
		Name string `json:"name"`
	} `json:"on,omitempty"`
}

type EndpointLink struct {
	URL  string `json:"url"`
	Name string `json:"name,omitempty"`
}

type TargetSpec struct {
	ID   string `json:"id"`
	Type string `json:"type"` // "local" | "k8s" | "image" | ...
}

type K8sResourceInfo struct {
	PodName     string `json:"podName"`
	PodStatus   string `json:"podStatus"`
	PodRestarts int    `json:"podRestarts"`
}

// Button is a UIButton. Decoded now so the store can hold them; the shell
// does not render them yet.
type Button struct {
	Metadata ObjectMeta `json:"metadata"`
	Spec     struct {
		Location *struct {
			ComponentID   string `json:"componentID"`
			ComponentType string `json:"componentType"`
		} `json:"location,omitempty"`
		Text string `json:"text"`
	} `json:"spec"`
}

// LogList carries new log segments. Decoded but unused until the log store
// lands.
type LogList struct {
	Segments []struct {
		SpanID string    `json:"spanId,omitempty"`
		Time   time.Time `json:"time"`
		Text   string    `json:"text"`
		Level  string    `json:"level,omitempty"`
	} `json:"segments,omitempty"`
	Spans map[string]struct {
		ManifestName string `json:"manifestName,omitempty"`
	} `json:"spans,omitempty"`
	FromCheckpoint int `json:"fromCheckpoint,omitempty"`
	ToCheckpoint   int `json:"toCheckpoint,omitempty"`
}

// Kind returns a short label for the resource type ("k8s", "local", ...).
// The Tiltfile resource is always order 1 and has no specs.
func (r Resource) Kind() string {
	for _, s := range r.Status.Specs {
		if s.Type != "" {
			return s.Type
		}
	}
	if r.Status.Order == 1 {
		return "tiltfile"
	}
	return ""
}

// LastBuildError returns the error from the most recent build, if any.
// Tilt orders BuildHistory newest first.
func (r Resource) LastBuildError() string {
	if len(r.Status.BuildHistory) == 0 {
		return ""
	}
	return r.Status.BuildHistory[0].Error
}
