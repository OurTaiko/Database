package httpapi

import (
	"encoding/json"
	"reflect"
	"testing"
)

// No category chosen is Variety, as on Fanmade; a choice is sorted and deduplicated.
func TestCategorySelection(t *testing.T) {
	for raw, want := range map[string][]string{
		"":                      {"variety"},
		"null":                  {"variety"},
		"[]":                    {"variety"},
		`["pop","anime","pop"]`: {"anime", "pop"},
	} {
		got, err := categorySelection(json.RawMessage(raw))
		if err != nil || !reflect.DeepEqual(got, want) {
			t.Fatalf("%q: %v %v", raw, got, err)
		}
	}
	for _, raw := range []string{`"pop"`, `["missing"]`, `[1]`, `{}`} {
		if _, err := categorySelection(json.RawMessage(raw)); err == nil {
			t.Fatalf("%q accepted", raw)
		}
	}
}
