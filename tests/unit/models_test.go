package unit_test

import (
	"encoding/json"
	"reflect"
	"testing"

	"mugi/internal/models"
)

// TestStepDependsOnAcceptsIntsAndStrings verifies the IntList custom unmarshal
// accepts both bare ints (compliant models) and numeric strings (qwen2.5-coder
// emitted depends_on: ["1","2"], which a plain []int rejected).
func TestStepDependsOnAcceptsIntsAndStrings(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want models.IntList
	}{
		{"ints", `{"id":2,"depends_on":[1,2]}`, models.IntList{1, 2}},
		{"strings", `{"id":2,"depends_on":["1","2"]}`, models.IntList{1, 2}},
		{"mixed", `{"id":2,"depends_on":[1,"2",3]}`, models.IntList{1, 2, 3}},
		{"empty", `{"id":2,"depends_on":[]}`, models.IntList{}},
		{"omitted", `{"id":2}`, nil},
		{"null", `{"id":2,"depends_on":null}`, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var s models.Step
			if err := json.Unmarshal([]byte(tc.in), &s); err != nil {
				t.Fatalf("unmarshal failed: %v", err)
			}
			if !reflect.DeepEqual(s.DependsOn, tc.want) {
				t.Fatalf("depends_on = %#v, want %#v", s.DependsOn, tc.want)
			}
		})
	}
}

// TestStepDependsOnRejectsNonNumericString ensures we don't silently swallow a
// genuinely malformed dependency value.
func TestStepDependsOnRejectsNonNumericString(t *testing.T) {
	var s models.Step
	if err := json.Unmarshal([]byte(`{"id":1,"depends_on":["abc"]}`), &s); err == nil {
		t.Fatal("expected error for non-numeric depends_on string, got nil")
	}
}
