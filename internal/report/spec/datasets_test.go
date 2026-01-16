package spec

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestDatasetListUnmarshalString(t *testing.T) {
	var got DatasetList
	if err := json.Unmarshal([]byte(`"$sales"`), &got); err != nil {
		t.Fatalf("unmarshal string: %v", err)
	}
	want := []string{"$sales"}
	if !reflect.DeepEqual(got.Strings(), want) {
		t.Fatalf("Strings() = %v, want %v", got.Strings(), want)
	}
	if got.Join(",") != "$sales" {
		t.Fatalf("Join() mismatch: %q", got.Join(","))
	}
}

func TestDatasetListUnmarshalArray(t *testing.T) {
	var got DatasetList
	if err := json.Unmarshal([]byte(`["$sales"," dataset_a "]`), &got); err != nil {
		t.Fatalf("unmarshal array: %v", err)
	}
	want := []string{"$sales", "dataset_a"}
	if !reflect.DeepEqual(got.Strings(), want) {
		t.Fatalf("Strings() = %v, want %v", got.Strings(), want)
	}
	if got.Join(",") != "$sales,dataset_a" {
		t.Fatalf("Join() mismatch: %q", got.Join(","))
	}
}

func TestDatasetListUnmarshalNullAndEmpty(t *testing.T) {
	cases := []string{"null", `""`, `[]`}
	for _, input := range cases {
		t.Run(input, func(t *testing.T) {
			var got DatasetList
			if err := json.Unmarshal([]byte(input), &got); err != nil {
				t.Fatalf("unmarshal %s: %v", input, err)
			}
			if !got.Empty() {
				t.Fatalf("expected list to be empty for %s", input)
			}
		})
	}
}

func TestDatasetListUnmarshalInvalid(t *testing.T) {
	var got DatasetList
	if err := json.Unmarshal([]byte(`123`), &got); err == nil {
		t.Fatalf("expected error for invalid dataset value")
	}
}

func TestDatasetListUnmarshalInlineObject(t *testing.T) {
	input := `{"query": "SELECT * FROM sales", "dependencies": ["raw_sales"]}`
	var got DatasetList
	if err := json.Unmarshal([]byte(input), &got); err != nil {
		t.Fatalf("unmarshal inline object: %v", err)
	}

	if got.Empty() {
		t.Fatal("expected non-empty list")
	}
	if !got.HasInline() {
		t.Fatal("expected HasInline() to be true")
	}
	if got.InlineCount() != 1 {
		t.Fatalf("InlineCount() = %d, want 1", got.InlineCount())
	}

	// Strings() should return empty since there are no string refs
	if len(got.Strings()) != 0 {
		t.Fatalf("Strings() should be empty for inline-only list, got %v", got.Strings())
	}

	entries := got.Entries()
	if len(entries) != 1 {
		t.Fatalf("Entries() length = %d, want 1", len(entries))
	}
	if !entries[0].IsInline() {
		t.Fatal("expected first entry to be inline")
	}
	if entries[0].Inline.Query.Inline != "SELECT * FROM sales" {
		t.Fatalf("query = %q, want %q", entries[0].Inline.Query.Inline, "SELECT * FROM sales")
	}
}

func TestDatasetListUnmarshalMixedArray(t *testing.T) {
	input := `["named_dataset", {"query": "SELECT * FROM inline_data"}]`
	var got DatasetList
	if err := json.Unmarshal([]byte(input), &got); err != nil {
		t.Fatalf("unmarshal mixed array: %v", err)
	}

	if got.Empty() {
		t.Fatal("expected non-empty list")
	}
	if !got.HasInline() {
		t.Fatal("expected HasInline() to be true")
	}
	if got.InlineCount() != 1 {
		t.Fatalf("InlineCount() = %d, want 1", got.InlineCount())
	}

	// Strings() should only return the named reference
	strings := got.Strings()
	if len(strings) != 1 || strings[0] != "named_dataset" {
		t.Fatalf("Strings() = %v, want [named_dataset]", strings)
	}

	entries := got.Entries()
	if len(entries) != 2 {
		t.Fatalf("Entries() length = %d, want 2", len(entries))
	}
	if !entries[0].IsRef() || entries[0].Ref != "named_dataset" {
		t.Fatal("expected first entry to be string ref 'named_dataset'")
	}
	if !entries[1].IsInline() {
		t.Fatal("expected second entry to be inline")
	}
}

func TestDatasetListUnmarshalInlineWithDependencies(t *testing.T) {
	input := `{
		"query": "SELECT * FROM @inline(0)",
		"dependencies": [
			{"type": "csv", "path": "./data.csv"}
		]
	}`
	var got DatasetList
	if err := json.Unmarshal([]byte(input), &got); err != nil {
		t.Fatalf("unmarshal inline with dependencies: %v", err)
	}

	entries := got.Entries()
	if len(entries) != 1 {
		t.Fatalf("Entries() length = %d, want 1", len(entries))
	}

	inline := entries[0].Inline
	if inline == nil {
		t.Fatal("expected inline definition")
	}
	if len(inline.Dependencies) != 1 {
		t.Fatalf("dependencies length = %d, want 1", len(inline.Dependencies))
	}
	if !inline.Dependencies[0].IsInline() {
		t.Fatal("expected dependency to be inline")
	}
	if inline.Dependencies[0].Inline.Type != "csv" {
		t.Fatalf("dependency type = %q, want %q", inline.Dependencies[0].Inline.Type, "csv")
	}
}

func TestDatasetListMarshalRoundTrip(t *testing.T) {
	cases := []struct {
		name  string
		input string
	}{
		{"string", `"sales"`},
		{"array", `["ds1","ds2"]`},
		{"inline", `{"query":"SELECT 1"}`},
		{"mixed", `["named",{"query":"SELECT 2"}]`},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var original DatasetList
			if err := json.Unmarshal([]byte(tc.input), &original); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}

			marshaled, err := json.Marshal(original)
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}

			var roundTrip DatasetList
			if err := json.Unmarshal(marshaled, &roundTrip); err != nil {
				t.Fatalf("unmarshal roundtrip: %v", err)
			}

			// Compare entries
			origEntries := original.Entries()
			rtEntries := roundTrip.Entries()
			if len(origEntries) != len(rtEntries) {
				t.Fatalf("entries length mismatch: %d vs %d", len(origEntries), len(rtEntries))
			}
		})
	}
}

func TestDatasetListSetResolvedNames(t *testing.T) {
	// Start with an inline definition
	input := `{"query": "SELECT * FROM @inline(0)", "dependencies": [{"type": "csv", "path": "./data.csv"}]}`
	var list DatasetList
	if err := json.Unmarshal([]byte(input), &list); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	// Verify it has inline
	if !list.HasInline() {
		t.Fatal("expected HasInline() to be true")
	}

	// Simulate materialization by setting resolved names
	list.SetResolvedNames([]string{"_inline_dataset_abc123"})

	// Now it should be all string refs
	if list.HasInline() {
		t.Fatal("expected HasInline() to be false after SetResolvedNames")
	}
	strings := list.Strings()
	if len(strings) != 1 || strings[0] != "_inline_dataset_abc123" {
		t.Fatalf("Strings() = %v, want [_inline_dataset_abc123]", strings)
	}
}
