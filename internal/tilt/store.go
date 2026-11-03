package tilt

import (
	"cmp"
	"slices"
)

// Store holds the merged view state built from a stream of View messages.
// It is not safe for concurrent use; the UI owns it on its update loop.
type Store struct {
	resources map[string]Resource
	buttons   map[string]Button
	session   *Session
}

func NewStore() *Store {
	return &Store{
		resources: map[string]Resource{},
		buttons:   map[string]Button{},
	}
}

// Apply merges one View message into the store.
//
// A complete snapshot replaces everything. Incremental messages upsert by
// name, and an object carrying a deletionTimestamp is removed. This matches
// how Tilt's own web UI merges /ws/view updates.
func (s *Store) Apply(v View) {
	if v.IsComplete {
		clear(s.resources)
		clear(s.buttons)
	}
	if v.UISession != nil {
		s.session = v.UISession
	}
	for _, r := range v.UIResources {
		if r.Metadata.DeletionTimestamp != nil {
			delete(s.resources, r.Name())
			continue
		}
		s.resources[r.Name()] = r
	}
	for _, b := range v.UIButtons {
		if b.Metadata.DeletionTimestamp != nil {
			delete(s.buttons, b.Metadata.Name)
			continue
		}
		s.buttons[b.Metadata.Name] = b
	}
}

// Resources returns the resources in Tilt's display order.
func (s *Store) Resources() []Resource {
	out := make([]Resource, 0, len(s.resources))
	for _, r := range s.resources {
		out = append(out, r)
	}
	slices.SortFunc(out, func(a, b Resource) int {
		if c := cmp.Compare(a.Status.Order, b.Status.Order); c != 0 {
			return c
		}
		return cmp.Compare(a.Name(), b.Name())
	})
	return out
}

// TiltfilePath returns the Tiltfile this Tilt instance is running, if known.
func (s *Store) TiltfilePath() string {
	if s.session == nil {
		return ""
	}
	return s.session.Status.TiltfileKey
}
