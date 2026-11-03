package tilt

import "testing"

func TestHasWarning(t *testing.T) {
	tests := []struct {
		name    string
		history []BuildRecord
		want    bool
	}{
		{"no builds", nil, false},
		{"clean build", []BuildRecord{{}}, false},
		{"warnings only", []BuildRecord{{Warnings: []string{"w"}}}, true},
		{"warnings and error", []BuildRecord{{Warnings: []string{"w"}, Error: "e"}}, false},
		{"only latest build counts", []BuildRecord{{}, {Warnings: []string{"old"}}}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := Resource{Status: ResourceStatus{BuildHistory: tt.history}}
			if got := r.HasWarning(); got != tt.want {
				t.Errorf("HasWarning() = %v, want %v", got, tt.want)
			}
		})
	}
}
