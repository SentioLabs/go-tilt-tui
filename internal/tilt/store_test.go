package tilt

import (
	"slices"
	"testing"
	"time"
)

func res(name string, order int) Resource {
	return Resource{Metadata: ObjectMeta{Name: name}, Status: ResourceStatus{Order: order}}
}

func names(rs []Resource) []string {
	out := make([]string, len(rs))
	for i, r := range rs {
		out[i] = r.Name()
	}
	return out
}

func TestStoreApply(t *testing.T) {
	now := time.Now()
	deleted := res("web", 3)
	deleted.Metadata.DeletionTimestamp = &now

	updated := res("api", 2)
	updated.Status.UpdateStatus = UpdateInProgress

	tests := []struct {
		name  string
		views []View
		want  []string
	}{
		{
			name:  "snapshot sorted by order then name",
			views: []View{{IsComplete: true, UIResources: []Resource{res("web", 3), res("(Tiltfile)", 1), res("api", 2)}}},
			want:  []string{"(Tiltfile)", "api", "web"},
		},
		{
			name: "incremental update adds without dropping others",
			views: []View{
				{IsComplete: true, UIResources: []Resource{res("api", 2)}},
				{UIResources: []Resource{res("web", 3)}},
			},
			want: []string{"api", "web"},
		},
		{
			name: "deletionTimestamp removes resource",
			views: []View{
				{IsComplete: true, UIResources: []Resource{res("api", 2), res("web", 3)}},
				{UIResources: []Resource{deleted}},
			},
			want: []string{"api"},
		},
		{
			name: "new snapshot replaces stale state after reconnect",
			views: []View{
				{IsComplete: true, UIResources: []Resource{res("old", 2)}},
				{IsComplete: true, UIResources: []Resource{res("new", 2)}},
			},
			want: []string{"new"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := NewStore()
			for _, v := range tt.views {
				s.Apply(v)
			}
			if got := names(s.Resources()); !slices.Equal(got, tt.want) {
				t.Errorf("got %v, want %v", got, tt.want)
			}
		})
	}

	t.Run("update replaces fields", func(t *testing.T) {
		s := NewStore()
		s.Apply(View{IsComplete: true, UIResources: []Resource{res("api", 2)}})
		s.Apply(View{UIResources: []Resource{updated}})
		if got := s.Resources()[0].BuildStatus(); got != StatusBuilding {
			t.Errorf("status = %v, want building", got)
		}
	})
}
