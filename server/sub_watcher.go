package server

type watcherSubsystem struct{}

func (watcherSubsystem) Name() string { return "watcher" }

func (watcherSubsystem) Init(s *QNTXServer) error {
	return s.initWatcherEngine()
}
