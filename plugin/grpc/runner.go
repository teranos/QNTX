package grpc

// The Actions runner on this box builds plugins, and QNTX takes each build
// the moment it lands (ADR-043). Nothing is downloaded from GitHub.
// "A plugin's new build lands under the runner, and QNTX has it running as fast as it can. Nothing polls."

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/teranos/QNTX/internal/sacred"
	"github.com/teranos/QNTX/internal/sqlclose"
	"github.com/teranos/errors"
	"go.uber.org/zap"
)

const (
	// runnerFile is what a registered runner writes into its directory.
	runnerFile = ".runner"
	// runnerWork is where the runner checks out and builds, one directory per repository.
	runnerWork = "_work"
	// digestSuffix names the digest the package step writes beside an archive.
	digestSuffix = ".sha256"
	// buildSettle is how long a digest file is left to finish being written.
	buildSettle = 300 * time.Millisecond
	// workspaceDepth is how deep under _work a build is looked for: _work/<repo>/<repo>.
	workspaceDepth = 2
)

// The runner's own directories under _work: what every job starts from, never a build.
var runnerOwn = map[string]bool{"_actions": true, "_tool": true, "_temp": true}

// BuildPlugin is the plugin a build archive is of, when it is a build for this
// platform: qntx-<name>-plugin-<version>-<os>-<arch>.tar.gz.
func BuildPlugin(file string) (string, bool) {
	rest, ok := strings.CutPrefix(filepath.Base(file), "qntx-")
	if !ok || !strings.HasSuffix(rest, pluginAssetSuffix()) {
		return "", false
	}
	at := strings.LastIndex(rest, "-plugin-")
	if at <= 0 {
		return "", false
	}
	return rest[:at], true
}

// Runner is an Actions runner's directory on this box.
type Runner struct {
	path string

	mu    sync.Mutex
	taken []TakenBuild
}

// TakenBuild is a build QNTX installed from the runner.
type TakenBuild struct {
	Plugin  string    `json:"plugin"`
	Archive string    `json:"archive"`
	Digest  string    `json:"digest"`
	At      time.Time `json:"at"`
	Changed bool      `json:"changed"`
	// Older is a build that landed before the installed one was installed, and
	// so is not installed over it.
	Older bool `json:"older"`
}

// OpenRunner is the runner at path, or an error naming why there is none.
func OpenRunner(path string) (*Runner, error) {
	path = filepath.Clean(strings.TrimSpace(path))
	if _, err := os.Stat(filepath.Join(path, runnerFile)); err != nil {
		return nil, errors.Wrapf(err, "no runner at %s: %s is not there", path, filepath.Join(path, runnerFile))
	}
	return &Runner{path: path}, nil
}

// Path is the runner's directory.
func (r *Runner) Path() string { return r.path }

// Take verifies the build a digest file names and installs it, unless it is
// the build already installed.
func (r *Runner) Take(digestFile string, logger *zap.SugaredLogger) (TakenBuild, error) {
	archivePath := strings.TrimSuffix(digestFile, digestSuffix)
	name, ok := BuildPlugin(archivePath)
	if !ok {
		return TakenBuild{}, errors.Newf("%s is not a plugin build for %s", archivePath, strings.TrimPrefix(pluginAssetSuffix(), "-"))
	}
	said, err := os.ReadFile(digestFile)
	if err != nil {
		return TakenBuild{}, errors.Wrapf(err, "failed to read the digest %s", digestFile)
	}
	fields := strings.Fields(string(said))
	if len(fields) == 0 || len(fields[0]) != sha256.Size*2 {
		return TakenBuild{}, errors.Newf("%s holds no sha256 digest", digestFile)
	}
	want := strings.ToLower(fields[0])

	archive, err := os.ReadFile(archivePath)
	if err != nil {
		return TakenBuild{}, errors.Wrapf(err, "failed to read the build %s", archivePath)
	}
	got := hex.EncodeToString(sha256Of(archive))
	if got != want {
		return TakenBuild{}, errors.Newf("build %s has digest %s and %s says %s; nothing was installed", archivePath, got, digestFile, want)
	}

	taken := TakenBuild{Plugin: name, Archive: archivePath, Digest: got, At: time.Now()}
	dir, err := PluginInstallPath(name)
	if err != nil {
		return TakenBuild{}, err
	}
	// A build already installed is still one the runner delivered, and is shown as unchanged.
	if installed, ok := installedDigest(dir); ok && installed == got {
		r.record(taken)
		return taken, nil
	}
	// An archive left in a workspace from an earlier job is older than what is
	// installed, and never replaces it: the newest build is the one that runs.
	older, err := landedBeforeInstall(archivePath, dir)
	if err != nil {
		return TakenBuild{}, err
	}
	if older {
		taken.Older = true
		logger.Infow("A build under the runner is older than the installed one; it is not installed",
			"plugin", name, "archive", archivePath, "sha256", got)
		r.record(taken)
		return taken, nil
	}
	binary, files, err := install(archive, dir, PluginBinaryName(name))
	if err != nil {
		return TakenBuild{}, errors.Wrapf(err, "failed to install plugin %s to %s from %s", name, dir, archivePath)
	}
	if err := recordInstalledDigest(dir, got); err != nil {
		return TakenBuild{}, err
	}
	if err := removeLegacyInstall(name, logger); err != nil {
		return TakenBuild{}, err
	}
	taken.Changed = true
	logger.Infow("Installed plugin build from the runner",
		"plugin", name, "archive", archivePath, "binary", binary, "files", files, "sha256", got)

	r.record(taken)
	return taken, nil
}

// landedBeforeInstall is whether archive was written before dir's build was
// installed. Nothing installed yet is never older.
func landedBeforeInstall(archive, dir string) (bool, error) {
	installed, err := os.Stat(filepath.Join(dir, installedDigestFile))
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, errors.Wrapf(err, "could not tell when the build in %s was installed", dir)
	}
	landed, err := os.Stat(archive)
	if err != nil {
		return false, errors.Wrapf(err, "could not tell when %s landed", archive)
	}
	return landed.ModTime().Before(installed.ModTime()), nil
}

// record keeps one row per build: a build seen again replaces its row, and a
// row that installed it stays marked as having changed the plugin.
func (r *Runner) record(taken TakenBuild) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for i, held := range r.taken {
		if held.Plugin == taken.Plugin && held.Digest == taken.Digest {
			taken.Changed = taken.Changed || held.Changed
			r.taken[i] = taken
			return
		}
	}
	r.taken = append(r.taken, taken)
}

// Watch takes every build that lands under the runner until ctx ends, and
// hands each plugin whose installed build changed to landed.
func (r *Runner) Watch(ctx context.Context, landed func(name string), logger *zap.SugaredLogger) error {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return errors.Wrapf(err, "failed to watch the runner at %s", r.path)
	}
	work := filepath.Join(r.path, runnerWork)
	var already []string
	if err := r.watchUnder(watcher, work, 0, &already); err != nil {
		return sqlclose.With(err, watcher.Close(), "the runner watcher on "+work)
	}

	sacred.Go("plugin.runner.watch", func() {
		defer func() {
			if err := watcher.Close(); err != nil {
				logger.Warnw("The runner watch did not close", "runner", r.path, "error", err)
			}
		}()
		pending := map[string]*time.Timer{}
		take := func(digestFile string) {
			if timer, ok := pending[digestFile]; ok {
				timer.Stop()
			}
			pending[digestFile] = time.AfterFunc(buildSettle, func() {
				taken, err := r.Take(digestFile, logger)
				if err != nil {
					logger.Errorw("A build landed under the runner and was not installed", "digest", digestFile, "error", err)
					return
				}
				if taken.Changed {
					landed(taken.Plugin)
				}
			})
		}
		for _, digestFile := range already {
			take(digestFile)
		}
		for {
			select {
			case <-ctx.Done():
				for _, timer := range pending {
					timer.Stop()
				}
				return
			case err, open := <-watcher.Errors:
				if !open {
					return
				}
				logger.Errorw("The runner watch reported an error", "runner", r.path, "error", err)
			case event, open := <-watcher.Events:
				if !open {
					return
				}
				if event.Op&fsnotify.Create != 0 {
					if info, err := os.Stat(event.Name); err == nil && info.IsDir() {
						// What landed in it before its watch was added raised no event.
						var inside []string
						if err := r.watchUnder(watcher, event.Name, depthUnder(work, event.Name), &inside); err != nil {
							logger.Errorw("A new workspace under the runner is not watched", "path", event.Name, "error", err)
						}
						for _, digestFile := range inside {
							take(digestFile)
						}
						continue
					}
				}
				if event.Op&(fsnotify.Create|fsnotify.Write) == 0 || !isBuildDigest(event.Name) {
					continue
				}
				take(event.Name)
			}
		}
	})
	logger.Infow("Watching the runner for plugin builds", "runner", r.path, "work", work)
	return nil
}

// isBuildDigest is whether a file is the .sha256 of a plugin build for this platform.
func isBuildDigest(path string) bool {
	if !strings.HasSuffix(path, digestSuffix) {
		return false
	}
	_, ok := BuildPlugin(strings.TrimSuffix(path, digestSuffix))
	return ok
}

// watchUnder watches dir and the directories under it down to workspaceDepth,
// and adds to found every build digest already in them.
func (r *Runner) watchUnder(watcher *fsnotify.Watcher, dir string, depth int, found *[]string) error {
	if depth > workspaceDepth || runnerOwn[filepath.Base(dir)] {
		return nil
	}
	if err := watcher.Add(dir); err != nil {
		return errors.Wrapf(err, "failed to watch %s", dir)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return errors.Wrapf(err, "failed to list %s", dir)
	}
	for _, entry := range entries {
		path := filepath.Join(dir, entry.Name())
		if entry.IsDir() {
			if err := r.watchUnder(watcher, path, depth+1, found); err != nil {
				return err
			}
			continue
		}
		if isBuildDigest(path) {
			*found = append(*found, path)
		}
	}
	return nil
}

// depthUnder is how many directories below work path is.
func depthUnder(work, path string) int {
	rel, err := filepath.Rel(work, path)
	if err != nil {
		return workspaceDepth + 1
	}
	return len(strings.Split(rel, string(filepath.Separator)))
}

// RunnerStats is what the GitHub element shows of the runner, read off its directory.
type RunnerStats struct {
	Path       string       `json:"path"`
	Name       string       `json:"name"`
	GitHubURL  string       `json:"github_url"`
	Workspaces []string     `json:"workspaces"`
	Jobs       int          `json:"jobs"`
	LastJob    *time.Time   `json:"last_job,omitempty"`
	Taken      []TakenBuild `json:"taken"`
}

// Stats is the runner as its directory says it is now.
func (r *Runner) Stats() RunnerStats {
	stats := RunnerStats{Path: r.path, Workspaces: []string{}, Taken: []TakenBuild{}}
	if raw, err := os.ReadFile(filepath.Join(r.path, runnerFile)); err == nil {
		var registered struct {
			AgentName string `json:"agentName"`
			GitHubURL string `json:"gitHubUrl"`
		}
		// The runner is a .NET program, and a UTF-8 byte order mark is not JSON.
		if json.Unmarshal(bytes.TrimPrefix(raw, []byte{0xEF, 0xBB, 0xBF}), &registered) == nil {
			stats.Name, stats.GitHubURL = registered.AgentName, registered.GitHubURL
		}
	}
	if entries, err := os.ReadDir(filepath.Join(r.path, runnerWork)); err == nil {
		for _, entry := range entries {
			if entry.IsDir() && !runnerOwn[entry.Name()] {
				stats.Workspaces = append(stats.Workspaces, entry.Name())
			}
		}
	}
	// A job leaves a Worker_<time>.log in _diag.
	if entries, err := os.ReadDir(filepath.Join(r.path, "_diag")); err == nil {
		var jobs []time.Time
		for _, entry := range entries {
			if !strings.HasPrefix(entry.Name(), "Worker_") {
				continue
			}
			if info, err := entry.Info(); err == nil {
				jobs = append(jobs, info.ModTime())
			}
		}
		sort.Slice(jobs, func(i, j int) bool { return jobs[i].Before(jobs[j]) })
		stats.Jobs = len(jobs)
		if len(jobs) > 0 {
			last := jobs[len(jobs)-1]
			stats.LastJob = &last
		}
	}
	r.mu.Lock()
	stats.Taken = append(stats.Taken, r.taken...)
	r.mu.Unlock()
	return stats
}
