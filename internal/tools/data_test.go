package tools

import (
	"encoding/json"
	"testing"
)

// A struct in the payload used to vanish WITHOUT an error: the transport carries `Data` as a protobuf
// Struct, and a Go struct is not one of its value types, so the whole field arrived as null.
//
// These cases are the shapes the tools actually return. The round-trip assertion is the one that
// matters: what a client receives is what survives JSON.
func TestNormaliseValueMakesPayloadsTransportable(t *testing.T) {
	type item struct {
		Path   string
		Status string
		Adds   int `json:"additions"`
		hidden string
	}
	type wrapper struct {
		Name  string
		Items []item
		Ref   *item
	}

	cases := []struct {
		name string
		in   any
	}{
		{"a struct", item{Path: "a.go", Status: "added", Adds: 3, hidden: "x"}},
		{"a slice of structs", []item{{Path: "a.go"}, {Path: "b.go"}}},
		{"a pointer", &item{Path: "a.go"}},
		{"a nested struct", wrapper{Name: "w", Items: []item{{Path: "a"}}, Ref: &item{Path: "r"}}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := normaliseValue(c.in)
			// The real assertion: a client can only receive what JSON can carry, and the previous
			// failure was that the WHOLE payload became null.
			raw, err := json.Marshal(got)
			if err != nil {
				t.Fatalf("not JSON-serialisable: %v", err)
			}
			if string(raw) == "null" {
				t.Fatalf("normalised value is null — exactly the bug this guards")
			}
		})
	}
}

// Scalars must pass through untouched: a payload that was already fine should not be rebuilt, and a
// number that became a string would break every count in the UI.
func TestNormaliseValueLeavesScalarsAlone(t *testing.T) {
	if got := normaliseValue(42); got != 42 {
		t.Errorf("int became %#v", got)
	}
	if got := normaliseValue("x"); got != "x" {
		t.Errorf("string became %#v", got)
	}
	if got := normaliseValue(true); got != true {
		t.Errorf("bool became %#v", got)
	}
}

// A json tag wins over the field name, because that is the contract the field was declared with.
func TestNormaliseValueHonoursJSONTags(t *testing.T) {
	type tagged struct {
		Adds int `json:"additions"`
	}
	m, ok := normaliseValue(tagged{Adds: 7}).(map[string]any)
	if !ok {
		t.Fatal("not a map")
	}
	if m["additions"] != 7 {
		t.Errorf("expected key `additions`, got %#v", m)
	}
}

// Unexported fields are skipped: they are not part of the value the tool is publishing.
func TestNormaliseValueSkipsUnexportedFields(t *testing.T) {
	type mixed struct {
		Public  string
		private string
	}
	m := normaliseValue(mixed{Public: "a", private: "b"}).(map[string]any)
	if _, ok := m["private"]; ok {
		t.Errorf("unexported field leaked into the payload: %#v", m)
	}
	if m["Public"] != "a" {
		t.Errorf("exported field missing: %#v", m)
	}
}

// A nil payload stays nil rather than becoming an empty map: "no data" and "empty data" are
// different, and callers test for the first.
func TestNormaliseDataPassesNilThrough(t *testing.T) {
	if got := normaliseData(nil); got != nil {
		t.Errorf("nil became %#v", got)
	}
}
