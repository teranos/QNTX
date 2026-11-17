package server

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/teranos/QNTX/ats/storage"
	appcfg "github.com/teranos/QNTX/internal/config"
)

// amTomlNamespaces is a namespace store that keeps what it was told, so a second
// start reads what the first one made.
type amTomlNamespaces struct {
	held    []storage.Namespace
	created int
	given   int
}

func (f *amTomlNamespaces) List() ([]storage.Namespace, error) { return f.held, nil }

func (f *amTomlNamespaces) Create(name string, definition storage.NamespaceDefinition) error {
	f.created++
	f.held = append(f.held, storage.Namespace{Name: name, Definition: &definition})
	return nil
}

func (f *amTomlNamespaces) SetRecord(name string, record storage.NamespaceRecord) error {
	f.given++
	for i := range f.held {
		if f.held[i].Name == name {
			f.held[i].Definition.Record = record
		}
	}
	return nil
}

func (f *amTomlNamespaces) SetEnabled(string, bool) error { return nil }
func (f *amTomlNamespaces) Delete(string) error           { return nil }
func (f *amTomlNamespaces) Nuke() error                   { return nil }

func declaringConfig(namespaces ...appcfg.NamespaceConfig) *appcfg.Config {
	cfg := &appcfg.Config{}
	cfg.Auth.RootIdentities = []string{"https://chaos.example/@root"}
	cfg.Storage.Parquet.Namespaces = namespaces
	return cfg
}

// A namespace am.toml names and the node does not hold is created, owned by
// ROOT, and given its storage: what f42d's guests write in lands in parquet.
func TestANamespaceAmTomlNamesIsCreatedWithItsRecord(t *testing.T) {
	known := &amTomlNamespaces{held: []storage.Namespace{{Name: "default"}}}
	s := namespaceServer(t, known)
	require.NoError(t, s.serveDeclaredNamespaces(declaringConfig(appcfg.NamespaceConfig{Name: "fleetingmem", Record: "parquet"})))

	require.Len(t, known.held, 2)
	made := known.held[1]
	assert.Equal(t, "fleetingmem", made.Name)
	assert.Equal(t, "https://chaos.example/@root", made.Definition.Owner)
	assert.True(t, made.Definition.Enabled)
	assert.Equal(t, storage.RecordParquet, made.Definition.Record.Kind)
}

// The next start finds it held and makes nothing again.
func TestANamespaceAmTomlNamesIsMadeOnce(t *testing.T) {
	known := &amTomlNamespaces{}
	s := namespaceServer(t, known)
	cfg := declaringConfig(appcfg.NamespaceConfig{Name: "fleetingmem", Record: "parquet"})
	require.NoError(t, s.serveDeclaredNamespaces(cfg))
	require.NoError(t, s.serveDeclaredNamespaces(cfg))
	assert.Equal(t, 1, known.created)
	assert.Equal(t, 1, known.given)
}

// One the node holds with no storage yet is given what am.toml names; one
// that has storage keeps it, since storage is given once.
func TestANamespaceHeldIsGivenStorageOnlyFromNone(t *testing.T) {
	known := &amTomlNamespaces{held: []storage.Namespace{
		{Name: "FleetingMem", Definition: &storage.NamespaceDefinition{Record: storage.NamespaceRecord{Kind: storage.RecordNone}}},
		{Name: "garden", Definition: &storage.NamespaceDefinition{Record: storage.NamespaceRecord{Kind: storage.RecordSQLite}}},
	}}
	s := namespaceServer(t, known)
	require.NoError(t, s.serveDeclaredNamespaces(declaringConfig(
		appcfg.NamespaceConfig{Name: "fleetingmem", Record: "parquet"},
		appcfg.NamespaceConfig{Name: "garden", Record: "parquet"},
	)))
	assert.Equal(t, 0, known.created)
	assert.Equal(t, storage.RecordParquet, known.held[0].Definition.Record.Kind)
	assert.Equal(t, storage.RecordSQLite, known.held[1].Definition.Record.Kind)
}

// A list that does not hold is refused whole, and nothing is made.
func TestANamespacesListThatDoesNotHoldMakesNothing(t *testing.T) {
	for name, cfg := range map[string]*appcfg.Config{
		"no name":     declaringConfig(appcfg.NamespaceConfig{Record: "parquet"}),
		"twice":       declaringConfig(appcfg.NamespaceConfig{Name: "a", Record: "parquet"}, appcfg.NamespaceConfig{Name: "A", Record: "parquet"}),
		"system":      declaringConfig(appcfg.NamespaceConfig{Name: "system", Record: "parquet"}),
		"default":     declaringConfig(appcfg.NamespaceConfig{Name: "default", Record: "parquet"}),
		"no record":   declaringConfig(appcfg.NamespaceConfig{Name: "a"}),
		"postgres":    declaringConfig(appcfg.NamespaceConfig{Name: "a", Record: "postgres"}),
		"nobody root": {Storage: appcfg.StorageConfig{Parquet: appcfg.ParquetConfig{Namespaces: []appcfg.NamespaceConfig{{Name: "a", Record: "parquet"}}}}},
	} {
		known := &amTomlNamespaces{}
		s := namespaceServer(t, known)
		assert.Error(t, s.serveDeclaredNamespaces(cfg), name)
		assert.Zero(t, known.created, name)
	}
}

// A node that keeps no namespaces says so when am.toml names some.
func TestNamespacesOnANodeThatKeepsNoneIsRefused(t *testing.T) {
	s := namespaceServer(t, nil)
	assert.Error(t, s.serveDeclaredNamespaces(declaringConfig(appcfg.NamespaceConfig{Name: "fleetingmem", Record: "parquet"})))
	assert.NoError(t, s.serveDeclaredNamespaces(declaringConfig()), "naming none asks nothing of the node")
}
