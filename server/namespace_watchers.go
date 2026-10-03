package server

import (
	"fmt"
	"maps"

	"github.com/teranos/QNTX/ats/storage"
	"github.com/teranos/QNTX/ats/watcher"
	"github.com/teranos/QNTX/internal/config"
	"github.com/teranos/QNTX/server/namespaces"
	"github.com/teranos/errors"
)

// keepEngine holds the watcher engine of one namespace.
func (s *QNTXServer) keepEngine(namespace string, engine *watcher.Engine) {
	s.enginesMu.Lock()
	defer s.enginesMu.Unlock()
	if s.engines == nil {
		s.engines = map[string]*watcher.Engine{}
	}
	s.engines[namespace] = engine
}

// engineIn is the watcher engine of the namespace named, nil when that
// namespace has not started one.
func (s *QNTXServer) engineIn(namespace string) *watcher.Engine {
	s.enginesMu.Lock()
	defer s.enginesMu.Unlock()
	return s.engines[namespace]
}

// allEngines is every namespace's watcher engine, by namespace.
func (s *QNTXServer) allEngines() map[string]*watcher.Engine {
	s.enginesMu.Lock()
	defer s.enginesMu.Unlock()
	return maps.Clone(s.engines)
}

// watcherEngineSubsystem is the step a namespace runs when it starts: its
// watchers run against its attestations, and what they find goes to it.
type watcherEngineSubsystem struct{}

func (watcherEngineSubsystem) Name() string { return "watcher-engine" }

func (watcherEngineSubsystem) Start(u *namespaces.Universe) error {
	s := GetDefaultServer()
	if s == nil || u == nil {
		return nil
	}
	// The default's engine is made at boot by initWatcherEngine.
	if u.Name() == s.held.ServedUniverse().Name() {
		return nil
	}
	return s.startEngineIn(u)
}

// startEngineIn makes, wires and starts the watcher engine of one namespace.
func (s *QNTXServer) startEngineIn(u *namespaces.Universe) error {
	name := u.Name()
	reader, ok := u.Store().(watcher.AttestationReader)
	if !ok {
		return errors.Newf("attestation store %T of %s cannot read for a watcher engine", u.Store(), name)
	}
	if u.Sqlite() == nil {
		return errors.Newf("%s has no sqlite of its own to keep its watcher queue in", name)
	}

	// Switched off and on again, a namespace gets a new engine and the one
	// before stops hearing it.
	if before := s.engineIn(name); before != nil {
		storage.UnregisterObserver(name, before)
		before.Stop()
	}

	apiBaseURL := fmt.Sprintf("http://127.0.0.1:%d", config.GetServerPort())
	engine := watcher.NewEngine(u.Sqlite(), reader, apiBaseURL, s.logger.With("namespace", name))
	engine.SetWatcherStore(u.Watchers())
	engine.SetAvailableElementTypes([]string{"prompt", "se"})
	engine.SetBroadcastCallback(s.watcherMatchesIn(name, engine))
	engine.SetElementFiredCallback(s.elementsFiredIn(name))
	engine.SetPluginExecutor(&watcherPluginAdapter{server: s})
	if s.builtin != nil {
		engine.SetBuiltinExecutor(s.builtin)
	}
	if s.pythonClient != nil {
		engine.AddElementType("py")
		engine.SetPythonExecutor(&grpcPythonExecutor{client: s.pythonClient})
	}
	if s.watcherEngine != nil {
		engine.SetDilation(s.watcherEngine.Dilation())
	}

	if err := engine.Start(); err != nil {
		return errors.Wrapf(err, "failed to start the watcher engine of %s", name)
	}
	storage.RegisterObserver(name, engine)
	s.keepEngine(name, engine)
	return nil
}
