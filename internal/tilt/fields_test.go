package tilt

import (
	"encoding/json"
	"testing"
)

func TestSegmentFieldsSurviveDecoding(t *testing.T) {
	var v View
	raw := `{"logList":{"segments":[{"spanId":"s","text":"x","fields":{"progressID":"layer-1"}}]}}`
	if err := json.Unmarshal([]byte(raw), &v); err != nil {
		t.Fatal(err)
	}
	if got := v.LogList.Segments[0].Fields["progressID"]; got != "layer-1" {
		t.Fatalf("progressID = %q, want layer-1", got)
	}
}

func TestButtonInputsDecode(t *testing.T) {
	var b Button
	raw := `{"metadata":{"name":"api-disable"},"spec":{"text":"Disable","inputs":[{"name":"action","hidden":{"value":"on"}}]},"status":{"inputs":[{"name":"action","hidden":{"value":"on"}}]}}`
	if err := json.Unmarshal([]byte(raw), &b); err != nil {
		t.Fatal(err)
	}
	if got := b.Spec.Inputs[0].Hidden.Value; got != "on" {
		t.Fatalf("spec hidden value = %q, want on", got)
	}
	if got := b.Status.Inputs[0].Hidden.Value; got != "on" {
		t.Fatalf("status hidden value = %q, want on", got)
	}
}
