package grpc

import "sync"

// PluginRecord is a plugin as the node knows it. The plugin element is where
// one is added, configured and enabled.
type PluginRecord struct {
	Name    string            `json:"name"`
	Repo    string            `json:"repo"`
	Enabled bool              `json:"enabled"`
	Config  map[string]string `json:"config"`
}

// PluginRecords is where the node reads which plugins there are, where each
// comes from and what each is configured with.
type PluginRecords interface {
	Plugins() ([]PluginRecord, error)
	Plugin(name string) (PluginRecord, bool, error)
}

var (
	recordsMu sync.RWMutex
	records   PluginRecords
)

// SetPluginRecords hands the plugin layer the node's records. The server sets
// them once its store is open.
func SetPluginRecords(r PluginRecords) {
	recordsMu.Lock()
	defer recordsMu.Unlock()
	records = r
}

// pluginRecord is one plugin's record. False is a plugin with no record, or a
// node whose records are not set yet.
func pluginRecord(name string) (PluginRecord, bool) {
	recordsMu.RLock()
	held := records
	recordsMu.RUnlock()
	if held == nil {
		return PluginRecord{}, false
	}
	record, found, err := held.Plugin(name)
	if err != nil || !found {
		return PluginRecord{}, false
	}
	return record, true
}

// pluginRepo is the repository a plugin was added from; empty for a plugin
// with no record.
func pluginRepo(name string) string {
	record, _ := pluginRecord(name)
	return record.Repo
}
