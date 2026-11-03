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

// --- Contract assertions ---

var (
	_ func(*Store, string) []Button       = (*Store).Buttons
	_ func(*Store, string) (Button, bool) = (*Store).DisableButton
	_ func(*Store, string) (Button, bool) = (*Store).Button
	_ string                              = ComponentTypeResource
	_ string                              = AnnotationButtonType
	_ string                              = ButtonTypeDisableToggle
)

func button(name, componentType, componentID, buttonType string) Button {
	b := Button{Metadata: ObjectMeta{Name: name}}
	if componentType != "" {
		b.Spec.Location = &ButtonLocation{ComponentType: componentType, ComponentID: componentID}
	}
	if buttonType != "" {
		b.Metadata.Annotations = map[string]string{AnnotationButtonType: buttonType}
	}
	return b
}

func buttonNames(bs []Button) []string {
	out := make([]string, len(bs))
	for i, b := range bs {
		out[i] = b.Metadata.Name
	}
	return out
}

func TestStoreButtons(t *testing.T) {
	now := time.Now()
	gone := button("api-gone", "Resource", "api", "")
	gone.Metadata.DeletionTimestamp = &now

	s := NewStore()
	s.Apply(View{IsComplete: true, UIButtons: []Button{
		button("api-zz-migrate", "Resource", "api", ""),
		button("api-seed", "Resource", "api", ""),
		button("api-stopbuild", "Resource", "api", "StopBuild"),
		button("toggle-api-disable", "Resource", "api", ButtonTypeDisableToggle),
		button("web-seed", "Resource", "web", ""),
		button("global-nav", "Global", "nav", ""),
		button("api-nowhere", "", "", ""),
		button("api-gone", "Resource", "api", ""),
	}})
	s.Apply(View{UIButtons: []Button{gone}})

	want := []string{"api-seed", "api-stopbuild", "api-zz-migrate"}
	if got := buttonNames(s.Buttons("api")); !slices.Equal(got, want) {
		t.Errorf("Buttons(api) = %v, want %v", got, want)
	}
	if got := s.Buttons("unknown"); len(got) != 0 {
		t.Errorf("Buttons(unknown) = %v, want none", buttonNames(got))
	}

	d, ok := s.DisableButton("api")
	if !ok || d.Metadata.Name != "toggle-api-disable" {
		t.Errorf("DisableButton(api) = %q, %v", d.Metadata.Name, ok)
	}
	if _, ok := s.DisableButton("web"); ok {
		t.Error("DisableButton(web) found a toggle web doesn't have")
	}

	if b, ok := s.Button("web-seed"); !ok || b.Spec.Location.ComponentID != "web" {
		t.Errorf("Button(web-seed) = %+v, %v", b, ok)
	}
	if _, ok := s.Button("api-gone"); ok {
		t.Error("Button returned a deleted button")
	}
}

func TestRecordedButtons(t *testing.T) {
	s := NewStore()
	for _, v := range loadFixture(t) {
		s.Apply(v)
	}
	d, ok := s.DisableButton("hello")
	if !ok || d.Metadata.Name != "toggle-hello-disable" {
		t.Fatalf("DisableButton(hello) = %q, %v", d.Metadata.Name, ok)
	}
	if len(d.Spec.Inputs) != 1 || d.Spec.Inputs[0].Name != "action" || d.Spec.Inputs[0].Hidden == nil {
		t.Fatalf("toggle inputs = %+v, want one hidden input named action", d.Spec.Inputs)
	}
	for _, b := range s.Buttons("hello") {
		if b.Metadata.Name == d.Metadata.Name {
			t.Fatal("Buttons(hello) includes the disable toggle")
		}
	}
	if got := buttonNames(s.Buttons("hello")); !slices.Contains(got, "hello-stopbuild") {
		t.Fatalf("Buttons(hello) = %v, want hello-stopbuild among them", got)
	}
}
