package server

import (
	"slices"
	"strings"
	"time"

	"github.com/teranos/QNTX/ats/storage"
	appcfg "github.com/teranos/QNTX/internal/config"
	"github.com/teranos/QNTX/internal/slug"
	"github.com/teranos/QNTX/server/auth"
	errors "github.com/teranos/sacred-error"
)

// A fresh node has nobody signed in as ROOT to create a namespace with, and
// its guests' grants (auth.grants) act in one. So am.toml, which is ROOT's
// word, names the namespaces the node serves, the way it names the grants.

// declaredNamespace is one namespace storage.parquet.namespaces names, read.
type declaredNamespace struct {
	name   string
	record storage.RecordKind
}

// declaredNamespacesOf reads storage.parquet.namespaces, refusing the whole
// list when any line does not hold.
func declaredNamespacesOf(cfg *appcfg.Config) ([]declaredNamespace, error) {
	listed := cfg.Storage.Parquet.Namespaces
	declared := make([]declaredNamespace, 0, len(listed))
	for _, line := range listed {
		name := strings.TrimSpace(line.Name)
		if name == "" {
			return nil, errors.New("storage.parquet.namespaces names a namespace without a name")
		}
		if slug.Of(name) == slug.Of(auth.NamespaceSystem) || slug.Of(name) == slug.Of(auth.NamespaceDefault) {
			return nil, errors.Newf("storage.parquet.namespaces names %s, which every node serves already", name)
		}
		if slices.ContainsFunc(declared, func(d declaredNamespace) bool { return slug.Of(d.name) == slug.Of(name) }) {
			return nil, errors.Newf("storage.parquet.namespaces names %s twice", name)
		}
		record := storage.RecordKind(strings.TrimSpace(line.Record))
		if record != storage.RecordParquet && record != storage.RecordSQLite {
			return nil, errors.Newf("storage.parquet.namespaces gives %s the record %q; am.toml gives parquet or sqlite, and a postgres record is given at /api/namespaces/{name}/store", name, line.Record)
		}
		declared = append(declared, declaredNamespace{name: name, record: record})
	}
	return declared, nil
}

// serveDeclaredNamespaces creates each namespace am.toml names that the node
// does not hold, owned by the first of auth.root_identities and given its
// storage, as POST /api/namespaces and /store would. One the node holds is left
// as it stands: switched off stays off, and storage is given once, so one that
// has some keeps it. Nothing here ends a namespace.
func (s *QNTXServer) serveDeclaredNamespaces(cfg *appcfg.Config) error {
	declared, err := declaredNamespacesOf(cfg)
	if err != nil || len(declared) == 0 {
		return err
	}
	known := s.held.Known()
	if known == nil {
		return errors.New("storage.parquet.namespaces names namespaces, and this node keeps none: namespaces exist only on the parquet backend")
	}
	if len(cfg.Auth.RootIdentities) == 0 {
		return errors.New("storage.parquet.namespaces names namespaces, and auth.root_identities names nobody to own them")
	}
	owner := cfg.Auth.RootIdentities[0]

	held, err := known.List()
	if err != nil {
		return errors.Wrap(err, "the namespaces this node holds were not listed, so none am.toml names was created")
	}
	for _, want := range declared {
		i := slices.IndexFunc(held, func(n storage.Namespace) bool { return slug.Of(n.Name) == slug.Of(want.name) })
		// A namespace keeps the name it was created with, so one held is asked
		// after by that name and not as am.toml happens to write it.
		name := want.name
		if i >= 0 {
			name = held[i].Name
		}
		if i < 0 {
			definition := storage.NamespaceDefinition{
				Owner:     owner,
				Enabled:   true,
				CreatedAt: time.Now().UTC().Format(time.RFC3339),
				Record:    storage.NamespaceRecord{Kind: storage.RecordNone},
			}
			if err := known.Create(want.name, definition); err != nil {
				return errors.Wrapf(err, "%s, which am.toml names, was not created", want.name)
			}
			s.logger.Infow("namespace created, as am.toml says", "namespace", want.name, "by", owner)
		} else if d := held[i].Definition; d != nil && d.Record.Kind != storage.RecordNone && d.Record.Kind != "" {
			if d.Record.Kind != want.record {
				s.logger.Warnw("am.toml gives a namespace storage it already has other storage for, which stands",
					"namespace", held[i].Name, "has", string(d.Record.Kind), "am.toml", string(want.record))
			}
			continue
		}
		if err := known.SetRecord(name, storage.NamespaceRecord{Kind: want.record}); err != nil {
			return errors.Wrapf(err, "%s, which am.toml names, was not given its %s record", name, want.record)
		}
		s.held.Forget(name)
		s.logger.Infow("namespace given storage, as am.toml says", "namespace", name, "kind", string(want.record), "by", owner)
	}
	return nil
}
