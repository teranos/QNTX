package server

import (
	"time"

	"github.com/teranos/QNTX/server/namespaces"
	"github.com/teranos/QNTX/sym"
)

// startNamespace runs what a namespace starts for itself.
//
// "The namespace is its own universe running." A namespace the node already
// held starts at boot; one created since starts on being opened, which is the
// first request that reaches it. Either way it is the same list in the same
// order, so a namespace opened on a Tuesday is the same universe as one the
// node booted with.
//
// A step is handed the namespace it runs in, which is what it is a step of.
func (s *QNTXServer) startNamespace(u *namespaces.Universe) {
	if u == nil || u.Store() == nil {
		return
	}

	for _, entry := range namespaceSubsystems {
		started := time.Now()
		err := entry.sub.Start(u)
		if err == nil {
			s.logger.Debugw(sym.Type+" A namespace started a step",
				"namespace", u.Name(), "step", entry.sub.Name(), "took", time.Since(started))
			continue
		}
		if entry.policy == SubsystemFatal {
			s.logger.Errorw(sym.Type+" A namespace could not start",
				"namespace", u.Name(), "step", entry.sub.Name(), "error", err)
			continue
		}
		s.logger.Warnw(sym.Type+" A namespace started without one of its steps",
			"namespace", u.Name(), "step", entry.sub.Name(), "error", err)
	}
}
