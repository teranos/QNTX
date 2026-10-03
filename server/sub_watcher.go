package server

type watcherSubsystem struct{}

func (watcherSubsystem) Name() string { return "watcher" }

// The handler is made whether or not the engine came up: this subsystem is
// SubsystemWarn, so its routes are served either way, and a handler without
// an engine answers 503 where a nil handler panics.
func (watcherSubsystem) Init(s *QNTXServer) error {
	err := s.initWatcherEngine()
	s.watcherHandler = NewWatcherHandler(s.watcherEngine, s.logger, s.getAttestationsByIDs)
	return err
}
