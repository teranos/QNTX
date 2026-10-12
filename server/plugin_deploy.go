package server

// A deploy is one plugin going from what is installed to what was pushed. The
// node holds each plugin's newest deploy, and each step is a line in system.

import (
	"maps"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	errors "github.com/teranos/sacred-error"
)

// deployPredicate begins every deploy line; the stage, or deployChecked, ends it.
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
)

// deployChecked ends the line a check on the deploy's commit is.
const deployChecked = "checked"

// deployEnded is every stage a deploy stops at.
var deployEnded = []string{deployLive, deployUnchanged, deployDisabled, deployFailed}

// pluginDeploy is one plugin's deploy as far as it went.
type pluginDeploy struct {
	plugin string
	// rev is what the deploy's lines are about: plugin@rev.
	rev string
	// What the push said: the repository, the ref and the commit it moved to.
	repo, ref, after string
	// What it replaces, and what it installs.
	replacesRevs    []string
	replacesVersion string
	revs            []string
	version         string
	stage           string
	// The stage it failed at, and why.
	failedAt, failure string
	// Each check on the pushed commit, by name, as checkState draws it.
	checks map[string]string
	// When each stage began.
	began map[string]time.Time
	// The lines about it that were not written, and why.
	unwritten []string
}

// copied is d sharing nothing with the tracker.
func (d *pluginDeploy) copied() pluginDeploy {
	c := *d
	c.replacesRevs, c.revs, c.unwritten = slices.Clone(d.replacesRevs), slices.Clone(d.revs), slices.Clone(d.unwritten)
	c.checks, c.began = maps.Clone(d.checks), maps.Clone(d.began)
	return c
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
	subject := d.plugin + "@" + d.rev
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
	d.checks, d.began = map[string]string{}, map[string]time.Time{}
	if queued, held := t.deploys[d.plugin]; held && queued.stage == deployQueued {
		d.rev, d.repo, d.ref, d.after = queued.rev, queued.repo, queued.ref, queued.after
		d.checks, d.began, d.unwritten = queued.checks, queued.began, queued.unwritten
	}
	t.deploys[d.plugin] = &d
	t.moved(&d, d.stage, map[string]any{
		"repo": d.repo, "ref": d.ref, "after": d.after,
		"revs": strings.Join(d.revs, " "), "replaces_revs": strings.Join(d.replacesRevs, " "), "replaces_version": d.replacesVersion,
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
	t.mu.Lock()
	defer t.mu.Unlock()
	if d, flying := t.flying(plugin); flying {
		d.failedAt, d.failure = d.stage, failed.Error()
		t.moved(d, deployFailed, map[string]any{"failed_at": d.failedAt, "error": d.failure})
	}
}

// unchanged ends plugin's deploy: what it built is what was installed.
func (t *deployTracker) unchanged(plugin string) {
	t.stage(plugin, deployUnchanged)
}

// check writes a check run on every deploy of repo pushed to sha.
func (t *deployTracker) check(repo, sha, name, state string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	for _, d := range t.deploys {
		if d.repo != repo || d.after != sha {
			continue
		}
		d.checks[name] = state
		t.line(d, deployPredicate+deployChecked, map[string]any{"sha": sha, "name": name, "state": state})
	}
}

// active is every plugin's newest deploy, by the plugin's name.
func (t *deployTracker) active() []pluginDeploy {
	t.mu.Lock()
	defer t.mu.Unlock()
	held := make([]pluginDeploy, 0, len(t.deploys))
	for _, d := range t.deploys {
		held = append(held, d.copied())
	}
	sort.Slice(held, func(i, j int) bool { return held[i].plugin < held[j].plugin })
	return held
}
