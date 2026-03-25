package plugin

import "bino.bi/bino/internal/report/config"

// Ensure PluginRegistry satisfies config.KindProvider at compile time.
var _ config.KindProvider = (*PluginRegistry)(nil)

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
