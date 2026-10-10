package grpc

import (
	"sync"

	"github.com/teranos/errors"
)

// PluginRecord is a plugin as the node knows it (ADR-043). The plugin element
// is the only place one is added, configured and enabled.
// "that means no am.toml"
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
	records   PluginRecords = recordsNotHanded{}
)

// recordsNotHanded is the plugin layer before the server hands it the node's
// records: it knows no plugin, and asking it about one is refused.
type recordsNotHanded struct{}

func (recordsNotHanded) Plugins() ([]PluginRecord, error) {
	return nil, errors.New("the node has not handed the plugin layer its plugin records yet")
}

func (recordsNotHanded) Plugin(name string) (PluginRecord, bool, error) {
	return PluginRecord{}, false, errors.Newf("the node has not handed the plugin layer its plugin records yet, so plugin %s has none to read", name)
}

// SetPluginRecords hands the plugin layer the node's records. The server sets
// them once its store is open.
func SetPluginRecords(r PluginRecords) {
	recordsMu.Lock()
	defer recordsMu.Unlock()
	records = r
}

// pluginRecord is one plugin's record. False is a plugin with no record; a
// record that could not be read, or a node that has not handed its records
// over yet, is the error.
func pluginRecord(name string) (PluginRecord, bool, error) {
	recordsMu.RLock()
	held := records
	recordsMu.RUnlock()
	record, found, err := held.Plugin(name)
	if err != nil {
		return PluginRecord{}, false, errors.Wrapf(err, "failed to read the record of plugin %s", name)
	}
	return record, found, nil
}
