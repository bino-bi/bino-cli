package lsp

import (
	"slices"
	"testing"
)

func TestCompleteVariances(t *testing.T) {
	labels := func(scenarios []string, snippets bool) []string {
		t.Helper()
		items := completeVariances(scenarios, snippets)
		out := make([]string, 0, len(items))
		for _, it := range items {
			out = append(out, it.Label)
		}
		return out
	}

	t.Run("offers absolute and relative variances per pair", func(t *testing.T) {
		got := labels([]string{"ac1", "pp1"}, false)
		want := []string{"dac1_pp1_pos", "drac1_pp1_pos", "dpp1_ac1_pos", "drpp1_ac1_pos"}
		if !slices.Equal(got, want) {
			t.Fatalf("labels = %v, want %v", got, want)
		}
	})

	t.Run("snippet builder only for snippet-capable clients", func(t *testing.T) {
		if got := labels([]string{"ac1", "pp1"}, true); got[0] != "d…/dr…_…_… (variance builder)" {
			t.Fatalf("first item = %q, want variance builder snippet", got[0])
		}
		for _, l := range labels([]string{"ac1", "pp1"}, false) {
			if l == "d…/dr…_…_… (variance builder)" {
				t.Fatal("snippet item offered to non-snippet client")
			}
		}
	})

	t.Run("interleaves prefixes so dr survives the cap", func(t *testing.T) {
		scenarios := []string{"ac1", "ac2", "ac3", "ac4", "pp1", "pp2", "pp3", "pp4", "fc1", "fc2", "fc3", "fc4", "pl1", "pl2", "pl3", "pl4"}
		got := labels(scenarios, false)
		if len(got) > 48 {
			t.Fatalf("cap exceeded: %d items", len(got))
		}
		if !slices.Contains(got, "drac1_ac2_pos") {
			t.Fatalf("relative variance missing under cap: %v", got)
		}
	})

	t.Run("no items without scenarios", func(t *testing.T) {
		if got := labels(nil, false); len(got) != 0 {
			t.Fatalf("labels = %v, want none", got)
		}
	})
}
