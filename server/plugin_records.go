package server

// The plugins this node runs, kept where the node keeps what it knows about
// itself. Each change is a new line, and the newest line about a plugin holds.

import (
	"context"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/teranos/QNTX/ats/identity"
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
	return newestLines(r.s, pluginSubject, "plugins")
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

// asPluginRecord reads a PLUGIN line. A line that does not read whole is an
// error naming the line, never a plugin missing part of what was written.
func asPluginRecord(as *types.As) (grpcplugin.PluginRecord, error) {
	record := grpcplugin.PluginRecord{Name: as.Predicates[0], Config: map[string]string{}}
	// pluginLine writes the repository a plugin was added from as the one context.
	if len(as.Contexts) != 1 {
		return grpcplugin.PluginRecord{}, errors.Newf("%s line %s about %s: contexts are %v, not the one repository it was added from",
			pluginSubject, as.ID, record.Name, as.Contexts)
	}
	record.Repo = as.Contexts[0]
	enabled, ok := as.Attributes["enabled"].(bool)
	if !ok {
		return grpcplugin.PluginRecord{}, errors.Newf("%s line %s about %s: enabled is %v, not true or false",
			pluginSubject, as.ID, record.Name, as.Attributes["enabled"])
	}
	record.Enabled = enabled
	held, ok := as.Attributes["config"].(map[string]any)
	if !ok {
		return grpcplugin.PluginRecord{}, errors.Newf("%s line %s about %s: config is %v, not a map",
			pluginSubject, as.ID, record.Name, as.Attributes["config"])
	}
	for key, value := range held {
		text, ok := value.(string)
		if !ok {
			return grpcplugin.PluginRecord{}, errors.Newf("%s line %s about %s: config %s is %v, not text",
				pluginSubject, as.ID, record.Name, key, value)
		}
		record.Config[key] = text
	}
	return record, nil
}

// Plugins is every plugin the node knows, by name.
func (r PluginRecords) Plugins() ([]grpcplugin.PluginRecord, error) {
	newest, err := r.newest()
	if err != nil {
		return nil, err
	}
	records := make([]grpcplugin.PluginRecord, 0, len(newest))
	for _, as := range newest {
		record, err := asPluginRecord(as)
		if err != nil {
			return nil, err
		}
		records = append(records, record)
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
	record, err := asPluginRecord(as)
	if err != nil {
		return grpcplugin.PluginRecord{}, false, err
	}
	return record, true, nil
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
