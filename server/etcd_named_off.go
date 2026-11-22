//go:build !(cgo && rustetcd)

package server

import (
	appcfg "github.com/teranos/QNTX/internal/config"
	"github.com/teranos/QNTX/server/namespaces"
	errors "github.com/teranos/sacred-error"
)

// etcdNamedSubsystem in a build without ats-etcd: a node whose am.toml names
// an etcd is refused, rather than run keeping names and what is collected
// anywhere but there.
type etcdNamedSubsystem struct{}

func (etcdNamedSubsystem) Name() string { return "etcd" }

func (etcdNamedSubsystem) Start(u *namespaces.Universe) error {
	cfg, err := appcfg.Load()
	if err != nil {
		return errors.Wrap(err, "am.toml was not read for the etcd it names")
	}
	if cfg.Etcd.Endpoint == "" {
		return nil
	}
	return errors.Newf("am.toml names etcd at %s, and this build carries no ats-etcd: build with the rustetcd tag", cfg.Etcd.Endpoint)
}
