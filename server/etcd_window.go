//go:build cgo && rustetcd

package server

import (
	"github.com/teranos/QNTX/ats/storage"
	"github.com/teranos/QNTX/ats/storage/etcdcgo"
	appcfg "github.com/teranos/QNTX/internal/config"
	"github.com/teranos/QNTX/server/namespaces"
	"github.com/teranos/QNTX/server/statusline"
	errors "github.com/teranos/sacred-error"
)

// 「[Opus 5.5] etcdWindowSubsystem is the step a namespace runs when it starts:
// 「[Opus 5.5] when am.toml names an etcd, each attestation the namespace creates from then」
// 「[Opus 5.5] on is put into it. A write etcd does not take is said where failing handlers」
// 「[Opus 5.5] are, and in the log.」」
type etcdWindowSubsystem struct{}

func (etcdWindowSubsystem) Name() string { return "etcd-window" }

func (etcdWindowSubsystem) Start(u *namespaces.Universe) error {
	s := GetDefaultServer()
	if s == nil || u == nil {
		return nil
	}
	cfg, err := appcfg.Load()
	if err != nil {
		return errors.Wrap(err, "the etcd window could not read am.toml")
	}
	endpoint := cfg.Etcd.Endpoint
	if endpoint == "" {
		return nil
	}
	store, err := etcdcgo.Open(endpoint, u.Name())
	if err != nil {
		return err
	}
	logger := s.logger.Named("etcd-window")
	namespace := u.Name()
	storage.RegisterObserver(namespace, etcdcgo.NewWindow(store, func(err error) {
		logger.Errorw("etcd did not take an attestation", "namespace", namespace, "endpoint", endpoint, "error", err)
		s.noteHandlerFailure(statusline.HandlerFailure{Handler: "etcd-window", Error: err.Error()})
	}))
	logger.Infow("The etcd window holds what this namespace creates", "namespace", namespace, "endpoint", endpoint)
	return nil
}
