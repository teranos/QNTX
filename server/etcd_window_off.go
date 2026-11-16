//go:build !(cgo && rustetcd)

package server

import (
	appcfg "github.com/teranos/QNTX/internal/config"
	"github.com/teranos/QNTX/server/namespaces"
	errors "github.com/teranos/sacred-error"
)

// 「[Opus 5.5] etcdWindowSubsystem in a build without ats-etcd: a node whose
// am.toml names an etcd is refused, rather than run without the window it was
// told to keep.」
type etcdWindowSubsystem struct{}

func (etcdWindowSubsystem) Name() string { return "etcd-window" }

func (etcdWindowSubsystem) Start(u *namespaces.Universe) error {
	cfg, err := appcfg.Load()
	if err != nil {
		return errors.Wrap(err, "the etcd window could not read am.toml")
	}
	if cfg.Etcd.Endpoint == "" {
		return nil
	}
	return errors.Newf("am.toml names etcd at %s, and this build carries no ats-etcd: build with the rustetcd tag", cfg.Etcd.Endpoint)
}
