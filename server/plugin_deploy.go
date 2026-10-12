package server

// A deploy is one plugin going from what is installed to what was pushed. The
// node holds each plugin's newest deploy, and each step is a line in system.

import (
	"maps"
	"slices"
	"strings"
	"sync"
	"time"

	errors "github.com/teranos/sacred-error"
)

// deployPredicate begins every deploy line; the stage ends it.
const deployPredicate = "deploy:"

// The stages of a deploy.
const (
	deployQueued     = "queued"
	deployBuilding   = "building"
	deployRestarting = "restarting"
	deployLive       = "live"
	deployUnchanged  = "unchanged"
	deployDisabled   = "disabled"
	deployFailed     = "failed"
	// Ended from outside the deploy; the next build takes it up again.
	deployInterrupted = "interrupted"
)

// deployEnded is every stage a deploy stops at.
var deployEnded = []string{deployLive, deployUnchanged, deployDisabled, deployFailed, deployInterrupted}

// pluginDeploy is one plugin's deploy as far as it went.
type pluginDeploy struct {
	plugin string
	// rev is what the deploy's lines are about: plugin@rev, the rev short.
	rev string
	// What the push said: the repository, the ref and the commit it moved to.
	repo, ref, after string
	// pushed is a deploy a push began, at a commit checks run on.
	pushed bool
	// The revs it replaces, and the revs it installs.
	replacesRevs []string
	revs         []string
	stage        string
	// The stage it failed or was interrupted at, and why.
	failedAt, failure string
	// When each stage began.
	began map[string]time.Time
	// The lines about it that were not written, and why.
	unwritten []string
	// What each check run on the commit pushed last said, by its name.
	checks map[string]string
}

// shortRev is how much of a rev names it in a deploy's subject.
const shortRev = 7

// subject is what d's lines are about: the plugin at its short rev. A full rev
// is a run of hex the store takes for an identifier, not a name.
func (d *pluginDeploy) subject() string {
	rev := d.rev
	if len(rev) > shortRev {
		rev = rev[:shortRev]
	}
	return d.plugin + "@" + rev
}

// deployWriter writes one line about a deploy, in the plugin's context.
type deployWriter func(subject, predicate, plugin string, attributes map[string]any) error

// deployTracker is each plugin's newest deploy, by plugin.
type deployTracker struct {
	mu      sync.Mutex
	deploys map[string]*pluginDeploy
	write   deployWriter
}

// writesWith readies the tracker to hold deploys, writing their lines with write.
func (t *deployTracker) writesWith(write deployWriter) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.deploys, t.write = map[string]*pluginDeploy{}, write
}

// deployLine writes one deploy line by the node, where it keeps its own.
func (s *QNTXServer) deployLine(subject, predicate, plugin string, attributes map[string]any) error {
	return s.nodeRecords().nodeRecord(s.nodeActor(), subject, predicate, plugin, attributes)
}

// line writes what d did, and holds on d what was not written. Called with mu
// held.
func (t *deployTracker) line(d *pluginDeploy, predicate string, attributes map[string]any) {
	subject := d.subject()
	if err := t.write(subject, predicate, d.plugin, attributes); err != nil {
		d.unwritten = append(d.unwritten, errors.Wrapf(err, "the %s line about %s was not written", predicate, subject).Error())
	}
}

// moved is d at stage from now.
func (t *deployTracker) moved(d *pluginDeploy, stage string, attributes map[string]any) {
	d.stage = stage
	d.began[stage] = time.Now()
	t.line(d, deployPredicate+stage, attributes)
}

// begin starts d at its stage. A deploy the push queued is taken up by its
// build: what the push said of it stays.
func (t *deployTracker) begin(d pluginDeploy) {
	t.mu.Lock()
	defer t.mu.Unlock()
	d.began, d.checks = map[string]time.Time{}, map[string]string{}
	if queued, held := t.deploys[d.plugin]; held && queued.stage == deployQueued {
		d.rev, d.repo, d.ref, d.after, d.pushed = queued.rev, queued.repo, queued.ref, queued.after, queued.pushed
		d.began, d.unwritten, d.checks = queued.began, queued.unwritten, queued.checks
	}
	t.deploys[d.plugin] = &d
	t.moved(&d, d.stage, map[string]any{
		"rev": d.rev, "repo": d.repo, "ref": d.ref, "after": d.after,
		"revs": strings.Join(d.revs, " "), "replaces_revs": strings.Join(d.replacesRevs, " "),
	})
}

// flying is plugin's deploy while it has not stopped at a stage. Called with
// mu held.
func (t *deployTracker) flying(plugin string) (*pluginDeploy, bool) {
	d, held := t.deploys[plugin]
	return d, held && !slices.Contains(deployEnded, d.stage)
}

// inFlight is whether plugin's deploy has not stopped at a stage.
func (t *deployTracker) inFlight(plugin string) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	d, held := t.deploys[plugin]
	return held && !slices.Contains(deployEnded, d.stage)
}

// stage moves plugin's deploy to stage. A deploy that stopped stays where it
// stopped.
func (t *deployTracker) stage(plugin, stage string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if d, flying := t.flying(plugin); flying {
		t.moved(d, stage, map[string]any{})
	}
}

// fail ends plugin's deploy at the stage it was at, saying why.
func (t *deployTracker) fail(plugin string, failed error) {
	t.endShort(plugin, deployFailed, failed)
}

// interrupt ends plugin's deploy at the stage it was at: something outside it
// ended it, and why is said.
func (t *deployTracker) interrupt(plugin string, ended error) {
	t.endShort(plugin, deployInterrupted, ended)
}

// endShort ends plugin's deploy at end, before it went live, saying the stage
// it was at and why.
func (t *deployTracker) endShort(plugin, end string, why error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if d, flying := t.flying(plugin); flying {
		d.failedAt, d.failure = d.stage, why.Error()
		t.moved(d, end, map[string]any{end + "_at": d.failedAt, "error": d.failure})
	}
}

// unchanged ends plugin's deploy: what it built is what was installed.
func (t *deployTracker) unchanged(plugin string) {
	t.stage(plugin, deployUnchanged)
}

// waiting is plugin's deploy while it waits on the checks of sha. Called with
// mu held.
func (t *deployTracker) waiting(plugin, sha string) (*pluginDeploy, bool) {
	return t.deploys[plugin], t.waits(plugin, sha)
}

// waits is whether plugin's deploy waits on the checks of sha. Called with mu
// held.
func (t *deployTracker) waits(plugin, sha string) bool {
	d, held := t.deploys[plugin]
	return held && d.stage == deployChecking && d.after == sha
}

// awaiting is whether plugin's deploy still waits on the checks of sha: a
// newer push, or an end, is a deploy that does not.
func (t *deployTracker) awaiting(plugin, sha string) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.waits(plugin, sha)
}

// checksAre is what each check run on sha last said, held on plugin's deploy
// while it waits on them.
func (t *deployTracker) checksAre(plugin, sha string, states map[string]string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if d, waits := t.waiting(plugin, sha); waits {
		d.checks = maps.Clone(states)
	}
}

// checksPassed queues plugin's deploy for its build: every check on sha
// passed. False is a deploy that no longer waits on them, and is not built.
func (t *deployTracker) checksPassed(plugin, sha string) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	d, waits := t.waiting(plugin, sha)
	if waits {
		t.moved(d, deployQueued, map[string]any{"checks": len(d.checks)})
	}
	return waits
}

// held is each plugin's newest deploy, as it is now.
func (t *deployTracker) held() []pluginDeploy {
	t.mu.Lock()
	defer t.mu.Unlock()
	out := make([]pluginDeploy, 0, len(t.deploys))
	for _, d := range t.deploys {
		copied := *d
		copied.began = maps.Clone(d.began)
		copied.checks = maps.Clone(d.checks)
		out = append(out, copied)
	}
	return out
}
