package server

import (
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"path"
	"slices"
	"strconv"
	"strings"
	"sync"
)

// standInRepo is abcd-nl/clean as the stand-in GitHub holds it: each branch's
// notes by path, the pull requests opened, and how many commits were made.
type standInRepo struct {
	mu            sync.Mutex
	defaultBranch string
	branches      map[string]map[string]string
	pulls         []map[string]any
	commits       []string
	// refuseRef is a branch GitHub will not make, as it will not beside one under it.
	refuseRef string
}

// stand is the repository the stand-in GitHub of vaultBindingServer holds.
var stand *standInRepo

// serve answers what is asked of abcd-nl/clean beyond its top folders, and
// says whether it answered.
func (repo *standInRepo) serve(w http.ResponseWriter, r *http.Request) bool {
	repo.mu.Lock()
	defer repo.mu.Unlock()
	give := func(status int, v any) {
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(v)
	}
	var sent map[string]string
	if r.Body != nil {
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &sent)
	}
	rest, ours := strings.CutPrefix(r.URL.Path, "/repos/abcd-nl/clean")
	switch {
	case !ours:
		return false
	case rest == "":
		give(http.StatusOK, map[string]any{"full_name": "abcd-nl/clean", "default_branch": repo.defaultBranch})
	case strings.HasPrefix(rest, "/branches/"):
		branch := strings.TrimPrefix(rest, "/branches/")
		if _, held := repo.branches[branch]; !held {
			give(http.StatusNotFound, map[string]string{"message": "Branch not found"})
			break
		}
		give(http.StatusOK, map[string]any{"name": branch, "commit": map[string]any{"sha": "sha-of-" + branch}})
	case rest == "/git/refs" && r.Method == http.MethodPost:
		branch := strings.TrimPrefix(sent["ref"], "refs/heads/")
		if branch == repo.refuseRef {
			give(http.StatusUnprocessableEntity, map[string]string{"message": "Reference update failed"})
			break
		}
		if _, held := repo.branches[branch]; held {
			give(http.StatusUnprocessableEntity, map[string]string{"message": "Reference already exists"})
			break
		}
		repo.branches[branch] = map[string]string{}
		for p, content := range repo.branches[repo.defaultBranch] {
			repo.branches[branch][p] = content
		}
		give(http.StatusCreated, map[string]any{"ref": sent["ref"]})
	case strings.HasPrefix(rest, "/contents/") && r.Method == http.MethodPut:
		file := strings.TrimPrefix(rest, "/contents/")
		notes, held := repo.branches[sent["branch"]]
		if !held {
			give(http.StatusNotFound, map[string]string{"message": "Branch " + sent["branch"] + " not found"})
			break
		}
		if was, exists := notes[file]; exists && sent["sha"] != gitBlobSHA([]byte(was)) {
			give(http.StatusConflict, map[string]string{"message": file + " does not match " + sent["sha"]})
			break
		}
		content, _ := base64.StdEncoding.DecodeString(sent["content"])
		notes[file] = string(content)
		repo.commits = append(repo.commits, sent["branch"]+": "+sent["message"])
		give(http.StatusOK, map[string]any{"content": map[string]any{"path": file}, "commit": map[string]any{"sha": "c"}})
	case strings.HasPrefix(rest, "/contents/") && r.Method == http.MethodDelete:
		file := strings.TrimPrefix(rest, "/contents/")
		notes := repo.branches[sent["branch"]]
		if was, exists := notes[file]; !exists || sent["sha"] != gitBlobSHA([]byte(was)) {
			give(http.StatusNotFound, map[string]string{"message": "Not Found"})
			break
		}
		delete(notes, file)
		repo.commits = append(repo.commits, sent["branch"]+": "+sent["message"])
		give(http.StatusOK, map[string]any{"commit": map[string]any{"sha": "c"}})
	case strings.HasPrefix(rest, "/contents/docs/adr"):
		repo.contents(strings.TrimPrefix(rest, "/contents/"), r.URL.Query().Get("ref"), give)
	case rest == "/pulls" && r.Method == http.MethodGet:
		open := []any{}
		for _, pull := range repo.pulls {
			if "abcd-nl:"+pull["head"].(map[string]any)["ref"].(string) == r.URL.Query().Get("head") {
				open = append(open, pull)
			}
		}
		give(http.StatusOK, open)
	case rest == "/pulls" && r.Method == http.MethodPost:
		number := len(repo.pulls) + 1
		pull := map[string]any{"number": number, "html_url": "https://github.com/abcd-nl/clean/pull/" + strconv.Itoa(number),
			"title": sent["title"], "base": map[string]any{"ref": sent["base"]}, "head": map[string]any{"ref": sent["head"]}}
		repo.pulls = append(repo.pulls, pull)
		give(http.StatusCreated, pull)
	case strings.HasPrefix(rest, "/compare/"):
		base, head, _ := strings.Cut(strings.TrimPrefix(rest, "/compare/"), "...")
		files := []any{}
		for _, p := range differ(repo.branches[base], repo.branches[head]) {
			files = append(files, map[string]any{"filename": p})
		}
		status := "identical"
		if len(files) > 0 {
			status = "ahead"
		}
		give(http.StatusOK, map[string]any{"status": status, "ahead_by": len(files), "files": files})
	default:
		return false
	}
	return true
}

// contents answers a folder or a note under docs/adr on a branch.
func (repo *standInRepo) contents(asked, ref string, give func(int, any)) {
	notes, held := repo.branches[ref]
	if !held {
		give(http.StatusNotFound, map[string]string{"message": "No commit found for the ref " + ref})
		return
	}
	if content, isNote := notes[asked]; isNote {
		give(http.StatusOK, map[string]any{"type": "file", "name": path.Base(asked), "path": asked, "sha": gitBlobSHA([]byte(content)),
			"encoding": "base64", "content": base64.StdEncoding.EncodeToString([]byte(content))})
		return
	}
	entries, listed := []any{}, map[string]bool{}
	for p, content := range notes {
		under, inside := strings.CutPrefix(p, asked+"/")
		if !inside {
			continue
		}
		if dir, _, deeper := strings.Cut(under, "/"); deeper {
			if !listed[dir] {
				listed[dir] = true
				entries = append(entries, map[string]any{"type": "dir", "name": dir, "path": asked + "/" + dir})
			}
			continue
		}
		entries = append(entries, map[string]any{"type": "file", "name": under, "path": p, "sha": gitBlobSHA([]byte(content))})
	}
	if len(entries) == 0 {
		give(http.StatusNotFound, map[string]string{"message": "Not Found"})
		return
	}
	give(http.StatusOK, entries)
}

// differ is every path one branch holds differently from the other.
func differ(a, b map[string]string) []string {
	var paths []string
	for p, content := range a {
		if other, held := b[p]; !held || other != content {
			paths = append(paths, p)
		}
	}
	for p := range b {
		if _, held := a[p]; !held {
			paths = append(paths, p)
		}
	}
	slices.Sort(paths)
	return paths
}
