package server

import (
	"time"

	appcfg "github.com/teranos/QNTX/internal/config"
	errors "github.com/teranos/sacred-error"
)

// "and as a human i can dial it"
// "remove this for this f42d, because i need2getrunning"
//
// collected.wait in am.toml is how long what an agent says to another waits
// on this node, for every model alike. It is still collected, seen, held and
// released as ever: only the wait is dialled. Under a minute, the node looks
// for what is due more often than once a minute, or the minute would be the
// wait.

// collectedLookedMost is how often the node looks for what is due when the
// wait is dialled under a minute.
const collectedLookedMost = 5 * time.Second

// dialCollected sets the wait am.toml says, before anything is collected or
// looked for. Saying nothing leaves each model's own.
func dialCollected(cfg *appcfg.Config) error {
	said := cfg.Collected.Wait
	if said == "" {
		return nil
	}
	wait, err := time.ParseDuration(said)
	if err != nil {
		return errors.Wrapf(err, "collected.wait %q is no duration", said)
	}
	if wait < 0 {
		return errors.Newf("collected.wait %q is less than nothing", said)
	}
	collectedForFable, collectedForOpus, collectedForSonnet = wait, wait, wait
	if wait < collectedLooked {
		collectedLooked = max(min(wait, collectedLookedMost), time.Second)
	}
	return nil
}
