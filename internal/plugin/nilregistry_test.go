package plugin

import (
	"context"
	"testing"
)

// A nil registry (no plugins configured) must not panic when aggregating the
// schema — it simply contributes no plugin kinds.
func TestSchemaAggregatorNilRegistry(t *testing.T) {
	agg := NewSchemaAggregator(nil)
	if err := agg.Build(context.Background()); err != nil {
		t.Fatalf("Build(nil registry) error = %v", err)
	}
	if len(agg.MergedSchema()) == 0 {
		t.Error("MergedSchema() empty; want the built-in schema")
	}
}

func TestRegistryNilReceivers(t *testing.T) {
	var r *PluginRegistry
	if r.AllKinds() != nil {
		t.Error("nil.AllKinds() should be nil")
	}
	if r.AllPlugins() != nil {
		t.Error("nil.AllPlugins() should be nil")
	}
}
