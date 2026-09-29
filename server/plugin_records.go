package server

// The plugins this node runs, kept where the node keeps what it knows about
// itself. Each change is a new line, and the newest line about a plugin holds.

import (
	"context"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/teranos/QNTX/ats"
	"github.com/teranos/QNTX/ats/identity"
	"github.com/teranos/QNTX/ats/storage"
	"github.com/teranos/QNTX/ats/types"
	"github.com/teranos/QNTX/internal/config"
	"github.com/teranos/QNTX/internal/measure"
	grpcplugin "github.com/teranos/QNTX/plugin/grpc"
	"github.com/teranos/QNTX/server/auth"
	"github.com/teranos/errors"
)

const (
	// A PLUGIN line's predicate is the plugin's name and its context the
	// repository it was added from.
	pluginSubject = "PLUGIN"
	pluginSource  = "qntx"
)

// PluginRecords reads and writes the PLUGIN lines.
type PluginRecords struct{ s *QNTXServer }

func (s *QNTXServer) pluginRecords() PluginRecords { return PluginRecords{s: s} }

// PluginRecords is where the plugin layer reads which plugins run.
func (s *QNTXServer) PluginRecords() grpcplugin.PluginRecords { return s.pluginRecords() }

// actorOf is who a line written for this request is by: the route a person
// came in by, or the admission's identity.
func actorOf(ctx context.Context) string {
	admitted, gated := auth.AdmissionFrom(ctx)
	if !gated {
		return pluginSource
	}
	if actor := admitted.ActsAs(); actor != "" {
		return actor
	}
	return admitted.Identity
}

// pluginSearchPaths is where a plugin's binary is looked for: [plugin] paths.
func pluginSearchPaths() []string {
	return config.GetStringSlice("plugin.paths")
}

// newest is the newest PLUGIN line per plugin.
func (r PluginRecords) newest() (map[string]*types.As, error) {
	if r.s.held == nil {
		return nil, errors.New("this node holds no store to keep its plugins in")
	}
	// Read where pluginLine writes: system, or default on a backend with none.
	where := auth.NamespaceDefault
	if r.s.held.KeepsSystem() {
		where = auth.NamespaceSystem
	}
	reading, err := r.s.held.Read(where)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to read the %s lines in %s", pluginSubject, where)
	}
	found, err := reading.GetAttestations(ats.AttestationFilter{
		Subjects: []string{pluginSubject},
		Limit:    storage.MaxAttestationLimit,
	})
	if err != nil {
		return nil, errors.Wrapf(err, "failed to read the %s lines in %s", pluginSubject, where)
	}
	newest := map[string]*types.As{}
	for _, as := range found {
		if len(as.Predicates) == 0 {
			continue
		}
		name := as.Predicates[0]
		if held, ok := newest[name]; ok && !as.Timestamp.After(held.Timestamp) {
			continue
		}
		newest[name] = as
	}
	return newest, nil
}

// pluginLine writes one plugin whole, as a new line.
func (r PluginRecords) pluginLine(actor string, record grpcplugin.PluginRecord) error {
	config := make(map[string]any, len(record.Config))
	for key, value := range record.Config {
		config[key] = value
	}
	store := r.s.held.TheNodesOwnRecords()
	id, err := identity.GenerateASUIDWithRetry("AS", pluginSubject, record.Name, record.Repo, store.AttestationExists)
	if err != nil {
		return errors.Wrapf(err, "failed to name the %s line about %s", pluginSubject, record.Name)
	}
	at := time.Now()
	if err := store.CreateAttestation(&types.As{
		ID: id, Subjects: []string{pluginSubject}, Predicates: []string{record.Name}, Contexts: []string{record.Repo},
		Actors: []string{actor}, Timestamp: at, CreatedAt: at, Source: pluginSource,
		Attributes: map[string]any{"enabled": record.Enabled, "config": config},
	}); err != nil {
		return errors.Wrapf(err, "the %s line about %s was not written", pluginSubject, record.Name)
	}
	measure.Count(measure.AttestationsWritten, 1)
	return nil
}

func asPluginRecord(as *types.As) grpcplugin.PluginRecord {
	record := grpcplugin.PluginRecord{Name: as.Predicates[0], Config: map[string]string{}}
	if len(as.Contexts) > 0 {
		record.Repo = as.Contexts[0]
	}
	if enabled, ok := as.Attributes["enabled"].(bool); ok {
		record.Enabled = enabled
	}
	if held, ok := as.Attributes["config"].(map[string]any); ok {
		for key, value := range held {
			if text, ok := value.(string); ok {
				record.Config[key] = text
			}
		}
	}
	return record
}

// Plugins is every plugin the node knows, by name.
func (r PluginRecords) Plugins() ([]grpcplugin.PluginRecord, error) {
	newest, err := r.newest()
	if err != nil {
		return nil, err
	}
	records := make([]grpcplugin.PluginRecord, 0, len(newest))
	for _, as := range newest {
		records = append(records, asPluginRecord(as))
	}
	slices.SortFunc(records, func(a, b grpcplugin.PluginRecord) int { return strings.Compare(a.Name, b.Name) })
	return records, nil
}

// Plugin is one plugin the node knows. False is a plugin nobody added.
func (r PluginRecords) Plugin(name string) (grpcplugin.PluginRecord, bool, error) {
	newest, err := r.newest()
	if err != nil {
		return grpcplugin.PluginRecord{}, false, err
	}
	as, ok := newest[name]
	if !ok {
		return grpcplugin.PluginRecord{}, false, nil
	}
	return asPluginRecord(as), true, nil
}

// AddPlugin records a plugin by its repository URL. It starts disabled.
func (r PluginRecords) AddPlugin(actor, repo string) (grpcplugin.PluginRecord, error) {
	repo = strings.TrimSpace(repo)
	parsed, err := url.Parse(repo)
	if err != nil || (parsed.Scheme != "https" && parsed.Scheme != "http") || parsed.Host == "" {
		return grpcplugin.PluginRecord{}, errors.Newf("%q is not a repository URL", repo)
	}
	name := config.PluginNameFromRepo(repo)
	if name == "" || name == "." || name == "/" {
		return grpcplugin.PluginRecord{}, errors.Newf("%q names no plugin", repo)
	}
	if held, found, err := r.Plugin(name); err != nil {
		return grpcplugin.PluginRecord{}, err
	} else if found {
		return grpcplugin.PluginRecord{}, errors.Newf("plugin %s is already added, from %s", name, held.Repo)
	}
	record := grpcplugin.PluginRecord{Name: name, Repo: repo, Config: map[string]string{}}
	if err := r.pluginLine(actor, record); err != nil {
		return grpcplugin.PluginRecord{}, err
	}
	return record, nil
}

// added is a plugin somebody added, or an error naming the one nobody did.
func (r PluginRecords) added(name string) (grpcplugin.PluginRecord, error) {
	record, found, err := r.Plugin(name)
	if err != nil {
		return grpcplugin.PluginRecord{}, err
	}
	if !found {
		return grpcplugin.PluginRecord{}, errors.Newf("plugin %s was never added: press + in the plugin element", name)
	}
	return record, nil
}

// EnablePlugin switches a plugin on or off, keeping its config.
func (r PluginRecords) EnablePlugin(actor, name string, enabled bool) error {
	record, err := r.added(name)
	if err != nil {
		return err
	}
	record.Enabled = enabled
	return r.pluginLine(actor, record)
}

// ConfigurePlugin replaces a plugin's config, keeping whether it is enabled.
func (r PluginRecords) ConfigurePlugin(actor, name string, settings map[string]string) error {
	record, err := r.added(name)
	if err != nil {
		return err
	}
	record.Config = settings
	return r.pluginLine(actor, record)
}
