package server

import (
	"time"

	appcfg "github.com/teranos/QNTX/internal/config"
	"github.com/teranos/QNTX/server/reach"
	errors "github.com/teranos/sacred-error"
)

// "open /a2a/ and /am/statusline/{name} to GROUND on f42d's node"
//
// 「[Opus 5.5] auth.reach is the REACH lines ROOT says in am.toml rather than in the store,」
// 「[Opus 5.5] as auth.words is the WRITE and READ lines: a node that comes up with nobody」
// 「[Opus 5.5] signed in as ROOT still serves them. Each is read as a stored line is」
// 「[Opus 5.5] (reach.ReadLine), so am.toml can say nothing the store could not.」

// 「[Opus 5.5] declaredReachActor is who the lines are by: am.toml, which is not ROOT, so a」
// 「[Opus 5.5] line ROOT writes in the store about the same path and role outranks one said」
// 「[Opus 5.5] here, whenever it was written (reach's outranks).」
const declaredReachActor = "am.toml"

// 「[Opus 5.5] declaredReach is auth.reach, as runtime reach lines. A line that does not」
// 「[Opus 5.5] read is the whole list refused, as a bad auth.grants is.」
func declaredReach(cfg *appcfg.Config) ([]reach.Line, error) {
	lines := make([]reach.Line, 0, len(cfg.Auth.Reach))
	for i, said := range cfg.Auth.Reach {
		line, err := reach.ReadLine([]string{reach.Subject}, said.Paths, said.To, []string{declaredReachActor}, time.Time{})
		if err != nil {
			return nil, errors.Wrapf(err, "auth.reach[%d]", i)
		}
		// 「[Opus 5.5] A level opens only a plugin's route, which am.toml cannot know of」
		// 「[Opus 5.5] before the plugin runs: auth.reach opens to roles.」
		if outside := line.Unopenable(nil); len(outside) > 0 {
			return nil, errors.Newf("auth.reach[%d] opens %v to a level; auth.reach opens paths to roles", i, outside)
		}
		lines = append(lines, line)
	}
	return lines, nil
}

// 「[Opus 5.5] setDeclaredReach holds auth.reach for what the node serves, and serves it」
// 「[Opus 5.5] now when the node already serves something.」
func (s *QNTXServer) setDeclaredReach(lines []reach.Line) error {
	s.opening.Lock()
	defer s.opening.Unlock()
	s.declaredReach = lines
	if s.served == nil {
		return nil
	}
	_, err := s.reopenHeld()
	return err
}
