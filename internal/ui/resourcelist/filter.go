package resourcelist

import "github.com/sentiolabs/go-tilt-tui/internal/tilt"

// Filter is the status filter. It reads Combined(), as the dots and the
// header counts do.
type Filter int

const (
	FilterAll Filter = iota
	FilterErrors
	FilterInProgress
	FilterHealthy
)

// String is the name the header shows.
func (f Filter) String() string {
	switch f {
	case FilterErrors:
		return "errors"
	case FilterInProgress:
		return "in progress"
	case FilterHealthy:
		return "healthy"
	default:
		return "all"
	}
}

// Next is the filter f cycles to: all, errors, in progress, healthy,
// then all again.
func (f Filter) Next() Filter {
	if f >= FilterHealthy || f < FilterAll {
		return FilterAll
	}
	return f + 1
}

// Match reports whether f lets r through. In progress covers pending and
// building, like Counts.Busy.
func (f Filter) Match(r tilt.Resource) bool {
	switch f {
	case FilterErrors:
		return r.Combined() == tilt.StatusUnhealthy
	case FilterInProgress:
		s := r.Combined()
		return s == tilt.StatusPending || s == tilt.StatusBuilding
	case FilterHealthy:
		return r.Combined() == tilt.StatusHealthy
	default:
		return true
	}
}

// Filter returns the active status filter.
func (m *Model) Filter() Filter { return m.filter }

// SetFilter changes the status filter. The resource under the cursor
// stays listed, dimmed and marked "(filtered)", until the cursor leaves
// it, so the selection and the logs don't change.
func (m *Model) SetFilter(f Filter) {
	if f == m.filter {
		return
	}
	m.filter = f
	m.free = false
	m.refilter()
}
