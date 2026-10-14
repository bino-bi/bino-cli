package plugin

import (
	"context"
	"fmt"
	"sort"
	"time"

	"bino.bi/bino/internal/report/config"
	"bino.bi/bino/internal/schema"
)

// Ensure PluginRegistry satisfies config.KindProvider at compile time.
var (
	_ config.KindProvider              = (*PluginRegistry)(nil)
	_ config.DocumentValidatorProvider = (*PluginRegistry)(nil)
)

// GetKind returns a config.KindInfo for the given kind name, satisfying config.KindProvider.
func (r *PluginRegistry) GetKind(kindName string) (config.KindInfo, bool) {
	if r == nil {
		return config.KindInfo{}, false
	}
	r.mu.RLock()
	defer r.mu.RUnlock()

	k, ok := r.kinds[kindName]
	if !ok {
		return config.KindInfo{}, false
	}
	return config.KindInfo{
		KindName:       k.KindName,
		Category:       int(k.Category),
		DataSourceType: k.DataSourceType,
		PluginName:     k.PluginName,
	}, true
}

// DocumentValidator returns a validator for the built-in schema merged with
// every plugin kind, satisfying config.DocumentValidatorProvider. A success is
// kept for the registry's lifetime; a failure is not, so the next call retries.
// Without plugin kinds it returns nil, so callers keep the embedded schema.
func (r *PluginRegistry) DocumentValidator(ctx context.Context) (*schema.Validator, error) {
	if r == nil || len(r.AllKinds()) == 0 {
		return nil, nil
	}
	r.validatorMu.Lock()
	defer r.validatorMu.Unlock()
	if r.validator != nil {
		return r.validator, nil
	}

	// The result outlives this request, so a cancelled caller must not cut
	// the plugin calls short. The timeout keeps a stuck plugin from holding
	// the lock forever.
	bctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
	defer cancel()
	agg := NewSchemaAggregator(r)
	if err := agg.Build(bctx); err != nil {
		return nil, fmt.Errorf("merge plugin schemas: %w", err)
	}
	if err := agg.FetchErrors(); err != nil {
		return nil, err
	}
	v, err := schema.CompileValidator(agg.MergedSchema())
	if err != nil {
		return nil, r.blameSchema(agg, err)
	}
	r.validator = v
	return v, nil
}

// blameSchema names the plugin and kind whose schema does not compile on its
// own. It falls back to err when each schema compiles alone.
func (r *PluginRegistry) blameSchema(agg *SchemaAggregator, err error) error {
	names := make([]string, 0, len(agg.kindSchemas))
	for name := range agg.kindSchemas {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		if _, kerr := schema.CompileValidator(agg.kindSchemas[name]); kerr != nil {
			reg, _ := r.GetKindRegistration(name)
			return fmt.Errorf("plugin %s: schema for kind %q: %w", reg.PluginName, name, kerr)
		}
	}
	return fmt.Errorf("compile schema with plugin kinds: %w", err)
}
