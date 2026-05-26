package server

type watcherSubsystem struct{}

func (watcherSubsystem) Name() string { return "watcher" }

func (watcherSubsystem) Init(s *QNTXServer) error {
	if err := s.initWatcherEngine(); err != nil {
		return err
	}
	s.watcherHandler = NewWatcherHandler(s.watcherEngine, s.logger)
	return nil
}
