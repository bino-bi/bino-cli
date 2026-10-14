package plugin

import (
	"context"
	"fmt"

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
// every plugin kind, satisfying config.DocumentValidatorProvider. It is built
// once per registry. Without plugin kinds it returns nil, so callers keep the
// embedded schema.
func (r *PluginRegistry) DocumentValidator(ctx context.Context) (*schema.Validator, error) {
	if r == nil || len(r.AllKinds()) == 0 {
		return nil, nil
	}
	r.validatorOnce.Do(func() {
		agg := NewSchemaAggregator(r)
		if err := agg.Build(ctx); err != nil {
			r.validatorErr = fmt.Errorf("merge plugin schemas: %w", err)
			return
		}
		r.validator, r.validatorErr = schema.CompileValidator(agg.MergedSchema())
	})
	return r.validator, r.validatorErr
}
