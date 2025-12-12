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
