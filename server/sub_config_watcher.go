package server

type configWatcherSubsystem struct{}

func (configWatcherSubsystem) Name() string { return "config-watcher" }

func (configWatcherSubsystem) Init(s *QNTXServer) error {
	setupConfigWatcher(s, s.nodeDB, s.logger)
	return nil
}
