package server

// A deploy is one plugin going from what is installed to what was pushed. The
// node holds each plugin's newest deploy, and each step is a line in system.

import (
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
	d.began = map[string]time.Time{}
	if queued, held := t.deploys[d.plugin]; held && queued.stage == deployQueued {
		d.rev, d.repo, d.ref, d.after = queued.rev, queued.repo, queued.ref, queued.after
		d.began, d.unwritten = queued.began, queued.unwritten
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
