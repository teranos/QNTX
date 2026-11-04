package server

// "Can an agent submit a PR via sigil ?"

// The code forges the node reaches, as a signum (ADR-043): what is asked is
// said in a forge's terms and of no forge by name. The repository says its
// host, and the forge on that host answers; GitHub is the one behind it now.
//
// Whoever proposes holds no credential of the forge (ADR-048, Its git): the
// node carries the change as the application it is installed as, and hands
// nothing back but what became of it.

import (
	"context"
	"encoding/json"
	"net/http"
	"path"
	"slices"
	"strings"

	"github.com/teranos/QNTX/internal/forge"
	"github.com/teranos/QNTX/plugin/grpc/protocol"
	"github.com/teranos/QNTX/server/sigil"
	errors "github.com/teranos/sacred-error"
)

const codeForgePath = "/api/codeforge"

func (s *QNTXServer) codeForgeSignum() sigil.Signum {
	return sigil.Signum{
		Signum: &protocol.Signum{
			Name:        "codeforge",
			Description: "The code forges the node reaches, asked in a forge's terms and of no forge by name: a repository says its host, and the forge on that host answers.",
			Tags:        []string{"forge", "pull request", "merge request"},
			Sigils: []*protocol.Sigil{
				{
					Name: "propose",
					Does: "Propose a change to a repository, carried by the node as the application it is installed as where the repository is: the branch is made from the branch it is proposed into when the forge has none, each file is committed to it, and the change that proposes it, a pull request or a merge request, is opened, or the one already open is kept. Whoever asks holds no credential of the forge.",
					Takes: []*protocol.Param{
						{Name: "repo", Required: true, Says: "The repository, with its host: github.com/owner/repo."},
						{Name: "branch", Required: true, Says: "The branch that holds the change: one of its own, not the branch it is proposed into and not the default branch."},
						{Name: "title", Required: true, Says: "What the change is called."},
						{Name: "body", Says: "What the change says of itself."},
						{Name: "into", Says: "The branch the change is proposed into. Left out, the repository's default branch."},
						{Name: "files", Says: "What the branch is to hold, as a JSON object of each file's path to its text: {\"docs/a.md\": \"# A\\n\"}. A file the branch holds as given is left as it is."},
						{Name: "removes", Says: "What the branch is to hold no longer, as a JSON array of paths: [\"docs/old.md\"]. A file the branch does not hold is left out."},
						{Name: "message", Says: "What each commit says. Left out, the title."},
					},
					Gives: []*protocol.Field{
						{Name: "number", Says: "The change's number on its forge."},
						{Name: "url", Says: "Where the change is read."},
						{Name: "branch", Says: "The branch that holds the change."},
						{Name: "into", Says: "The branch it is proposed into."},
						{Name: "committed", Says: "Each file this put on the branch, by path."},
						{Name: "removed", Says: "Each file this took off the branch, by path."},
					},
					Http: &protocol.Endpoint{Method: http.MethodPost, Path: codeForgePath + "/propose"},
				},
			},
		},
		Answers: map[string]sigil.Answer{
			"propose": s.codeForgePropose,
		},
	}
}

// proposedFiles reads what a proposal is to hold and to hold no longer, as
// they were sent: a JSON object of paths to text, and a JSON array of paths.
func proposedFiles(sent sigil.Sent) (map[string]string, []string, *protocol.Refusal) {
	puts := map[string]string{}
	if said := sent["files"]; said != "" {
		if err := json.Unmarshal([]byte(said), &puts); err != nil {
			return nil, nil, &protocol.Refusal{Why: sigil.Invalid, Param: "files",
				Says: "files is a JSON object of each file's path to its text, and what was sent does not read as one: " + err.Error()}
		}
	}
	removes := []string{}
	if said := sent["removes"]; said != "" {
		if err := json.Unmarshal([]byte(said), &removes); err != nil {
			return nil, nil, &protocol.Refusal{Why: sigil.Invalid, Param: "removes",
				Says: "removes is a JSON array of paths, and what was sent does not read as one: " + err.Error()}
		}
	}
	if len(puts)+len(removes) == 0 {
		return nil, nil, &protocol.Refusal{Why: sigil.Missing, Param: "files",
			Says: "propose needs files or removes: a change that changes no file is not proposed"}
	}
	for file := range puts {
		if why := notARepoFile(file); why != "" {
			return nil, nil, &protocol.Refusal{Why: sigil.Invalid, Param: "files", Says: why}
		}
	}
	for _, file := range removes {
		if why := notARepoFile(file); why != "" {
			return nil, nil, &protocol.Refusal{Why: sigil.Invalid, Param: "removes", Says: why}
		}
		if _, put := puts[file]; put {
			return nil, nil, &protocol.Refusal{Why: sigil.Invalid, Param: "removes", Says: file + " is in files and in removes: the branch holds it or does not"}
		}
	}
	return puts, removes, nil
}

// notARepoFile is why a path names no file inside a repository, or empty.
func notARepoFile(file string) string {
	switch {
	case file == "", file == forge.Root:
		return "a file has a path inside the repository, and one was sent with none"
	case strings.HasPrefix(file, "/"), path.Clean(file) != file, strings.HasPrefix(file, "../"), file == "..":
		return file + " is no path inside a repository: it is written from the top, with no leading /, no . or .. and no doubled /"
	case file == ".git", strings.HasPrefix(file, ".git/"):
		return file + " is git's own, and no file of the repository"
	}
	return ""
}

// "Can an agent submit a PR via sigil ?"
func (s *QNTXServer) codeForgePropose(ctx context.Context, sent sigil.Sent) (any, *protocol.Refusal) {
	repo, refused := repoOf(sent["repo"])
	if refused != nil {
		return nil, refused
	}
	host, err := s.forges().For(repo)
	if err != nil {
		return nil, &protocol.Refusal{Why: sigil.NotFound, Param: "repo", Says: err.Error()}
	}
	puts, removes, refused := proposedFiles(sent)
	if refused != nil {
		return nil, refused
	}
	branch, into := strings.TrimSpace(sent["branch"]), strings.TrimSpace(sent["into"])
	if branch == "" {
		return nil, &protocol.Refusal{Why: sigil.Missing, Param: "branch", Says: "propose needs branch: the branch that holds the change"}
	}
	if why := notABranchName(branch); why != "" {
		return nil, &protocol.Refusal{Why: sigil.Invalid, Param: "branch", Says: why}
	}
	if why := notABranchName(into); why != "" {
		return nil, &protocol.Refusal{Why: sigil.Invalid, Param: "into", Says: why}
	}

	defaultBranch, err := s.repoDefaultBranch(ctx, repo)
	var notReached forge.Refused
	if errors.As(err, &notReached) {
		return nil, &protocol.Refusal{Why: sigil.NotFound, Param: "repo", Says: err.Error()}
	}
	if err != nil {
		return nil, &protocol.Refusal{Why: sigil.Failed, Says: err.Error()}
	}
	if into == "" {
		into = defaultBranch
	}
	switch branch {
	case into:
		return nil, &protocol.Refusal{Why: sigil.Invalid, Param: "branch",
			Says: branch + " is the branch the change is proposed into; a change is proposed from a branch of its own"}
	case defaultBranch:
		return nil, &protocol.Refusal{Why: sigil.Invalid, Param: "branch",
			Says: branch + " is the default branch of " + repo.String() + ", which a change reaches by being merged; a change is proposed from a branch of its own"}
	}

	proposal := forge.Proposal{Repo: repo, Branch: branch, Into: into}
	if err := proposal.HoldBranch(ctx, host, forge.AsApp); err != nil {
		return nil, &protocol.Refusal{Why: sigil.Failed, Says: err.Error()}
	}
	files := make([]string, 0, len(puts))
	for file := range puts {
		files = append(files, file)
	}
	slices.Sort(files)
	held, err := proposal.Holds(ctx, host, forge.AsApp, append(slices.Clone(files), removes...))
	if err != nil {
		return nil, &protocol.Refusal{Why: sigil.Failed, Says: err.Error()}
	}
	title := sent["title"]
	message := sent["message"]
	if message == "" {
		message = title
	}
	edits := make([]forge.Edit, 0, len(files)+len(removes))
	for _, file := range files {
		content := []byte(puts[file])
		blob, there := held[file]
		switch {
		case there && blob == gitBlobSHA(content):
			// The branch holds it as given.
		case there:
			edits = append(edits, forge.Replaced(file, content, blob, message))
		default:
			edits = append(edits, forge.Added(file, content, message))
		}
	}
	for _, file := range removes {
		if blob, there := held[file]; there {
			edits = append(edits, forge.Removed(file, blob, message))
		}
	}
	done, err := proposal.Commit(ctx, host, forge.AsApp, edits)
	if err != nil {
		says := err.Error()
		if reached := append(slices.Clone(done.Put), done.Removed...); len(reached) > 0 {
			says += "; before it, " + branch + " took " + strings.Join(reached, ", ")
		}
		return nil, &protocol.Refusal{Why: sigil.Failed, Says: says}
	}
	change, err := proposal.Propose(ctx, host, forge.AsApp, title, sent["body"])
	if err != nil {
		return nil, &protocol.Refusal{Why: sigil.Failed, Says: err.Error()}
	}
	s.logger.Named("codeforge").Infow("A change was proposed", "repo", repo.String(), "branch", branch, "into", into,
		"committed", done.Put, "removed", done.Removed, "change", change.URL, "by", actorOf(ctx))
	return map[string]any{
		"number": change.Number, "url": change.URL, "branch": branch, "into": into,
		"committed": done.Put, "removed": done.Removed,
	}, nil
}
