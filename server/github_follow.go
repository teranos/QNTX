package server

// "web root follows main from the webhook QNTX already gets"

// A follow is ROOT's line that a push to one repo's branch dispatches a
// workflow, by the node's own GitHub, when the push reaches the App's webhook.

import (
	"context"
	"encoding/json"
	"net/http"
	"slices"
	"strconv"
	"strings"

	"github.com/teranos/QNTX/internal/forge"
	"github.com/teranos/QNTX/internal/sacred"
	"github.com/teranos/QNTX/plugin/grpc/protocol"
	"github.com/teranos/QNTX/server/auth"
	"github.com/teranos/QNTX/server/sigil"
	errors "github.com/teranos/sacred-error"
	"go.uber.org/zap"
	"google.golang.org/protobuf/types/known/structpb"
)

// A follow's line is GITHUB follow:<repo>@<branch>><dispatches>/<workflow>,
// so one push can dispatch more than one workflow.
const githubFollowLine = "follow:"

// TODO(forge): A follow is not GitHub's: Follow. Its record is the GITHUB subject with a follow: predicate (see node_records.go), so naming it anew is a record migration like forge_names.go.
// GitHubFollow is a push to Repo's Branch dispatching Workflow of Dispatches at
// Ref. Repo and Dispatches name their forge: github.com/owner/repo.
type GitHubFollow struct {
	Repo       string            `json:"repo"`
	Branch     string            `json:"branch"`
	Dispatches string            `json:"dispatches"`
	Workflow   string            `json:"workflow"`
	Ref        string            `json:"ref"`
	Inputs     map[string]string `json:"inputs"`
	Enabled    bool              `json:"enabled"`
	// kept is the line it was read under, which an older follow names otherwise.
	kept string
}

func (f GitHubFollow) line() string {
	return githubFollowLine + f.Repo + "@" + f.Branch + ">" + f.Dispatches + "/" + f.Workflow
}

// Follows are the node's follows, the newest line per repo, branch and workflow it dispatches.
func (r NodeRecords) Follows() ([]GitHubFollow, error) {
	newest, err := r.newest(githubSubject)
	if err != nil {
		return nil, err
	}
	follows := []GitHubFollow{}
	for line, as := range newest {
		if !strings.HasPrefix(line, githubFollowLine) {
			continue
		}
		if retired, said := as.Attributes[retiredLine].(bool); said && retired {
			continue
		}
		raw, err := json.Marshal(as.Attributes)
		if err != nil {
			return nil, errors.Wrapf(err, "the %s line %s did not read", githubSubject, line)
		}
		var f GitHubFollow
		if err := json.Unmarshal(raw, &f); err != nil {
			return nil, errors.Wrapf(err, "the %s line %s did not read", githubSubject, line)
		}
		f.kept = line
		follows = append(follows, f)
	}
	slices.SortFunc(follows, func(a, b GitHubFollow) int { return strings.Compare(a.line(), b.line()) })
	return follows, nil
}

// SetFollow writes one follow whole.
func (r NodeRecords) SetFollow(actor string, f GitHubFollow) error {
	return r.nodeRecord(actor, githubSubject, f.line(), "_", map[string]any{
		"repo":       f.Repo,
		"branch":     f.Branch,
		"dispatches": f.Dispatches,
		"workflow":   f.Workflow,
		"ref":        f.Ref,
		"inputs":     f.inputs(),
		"enabled":    f.Enabled,
	})
}

func (f GitHubFollow) inputs() map[string]any {
	inputs := map[string]any{}
	for k, v := range f.Inputs {
		inputs[k] = v
	}
	return inputs
}

// followed is whether the push lands on f's repo and branch.
func followed(p forge.Push, f GitHubFollow) (bool, error) {
	repo, err := forge.ParseRepo(f.Repo)
	if err != nil {
		return false, errors.Wrapf(err, "the follow %s names no repository", f.line())
	}
	return f.Enabled && !p.Deleted && repo.Is(p.Repo) && p.Branch() == f.Branch, nil
}

// followsMovedBy dispatches every follow the push lands on, and names them.
func (g *gitHub) followsMovedBy(push forge.Push) []string {
	logger := g.host.log().Named("follow")
	follows, err := g.host.nodeRecords().Follows()
	if err != nil {
		logger.Errorw("A push arrived and the follows were not read", "repo", push.Repo.String(), "ref", push.Ref, "error", err)
		return []string{}
	}
	names := []string{}
	for _, f := range follows {
		lands, err := followed(push, f)
		if err != nil {
			logger.Errorw("A push arrived and a follow could not be matched against it", "repo", push.Repo.String(), "error", err)
			continue
		}
		if !lands {
			continue
		}
		names = append(names, f.line())
		sacred.Go("github."+f.line(), func() { g.dispatchFollow(f, push.After, logger) })
	}
	return names
}

// dispatchFollow dispatches f's workflow for the push that landed at after.
func (g *gitHub) dispatchFollow(f GitHubFollow, after string, logger *zap.SugaredLogger) {
	dispatches, err := forge.ParseRepo(f.Dispatches)
	if err != nil {
		logger.Errorw("A push landed and its follow names no repo to dispatch", "follow", f.line(), "after", after, "dispatches", f.Dispatches, "error", err)
		return
	}
	// TODO(forge): The follow names its forge in Dispatches. Look the forge up with s.forges().For(dispatches) and dispatch through it; onGitHub is the adapter's.
	on, err := onGitHub(dispatches)
	if err != nil {
		logger.Errorw("A push landed and its follow dispatches where GitHub does not", "follow", f.line(), "after", after, "dispatches", f.Dispatches, "error", err)
		return
	}
	owner, repo := on.Owner, on.Name
	inputs, err := structpb.NewStruct(f.inputs())
	if err != nil {
		logger.Errorw("A push landed and its follow's inputs are not a workflow's", "follow", f.line(), "after", after, "error", err)
		return
	}
	// TODO(forge): Dispatching a pipeline is a Forge method (Writer), not GitHubService's CreateAWorkflowDispatchEvent.
	said, err := g.gitHubService().CreateAWorkflowDispatchEvent(g.host.lifetime(), &protocol.GitHubCreateAWorkflowDispatchEventRequest{
		Namespace: auth.NamespaceSystem, Owner: owner, Repo: repo, WorkflowId: f.Workflow, Ref: f.Ref, Inputs: inputs,
	})
	if err != nil {
		logger.Errorw("A push landed and its follow was not dispatched", "follow", f.line(), "after", after, "dispatches", f.Dispatches, "workflow", f.Workflow, "error", err)
		return
	}
	if !said.Success {
		logger.Errorw("A push landed and its follow was not dispatched", "follow", f.line(), "after", after, "dispatches", f.Dispatches, "workflow", f.Workflow, "error", said.Error)
		return
	}
	logger.Infow("A push landed and its follow was dispatched", "follow", f.line(), "after", after, "dispatches", f.Dispatches, "workflow", f.Workflow, "ref", f.Ref, "run", said.HtmlUrl)
}

func githubFollowSigil() *protocol.Sigil {
	return &protocol.Sigil{
		Name: "follow",
		Does: "Set what a push to a repo's branch dispatches: a workflow, run by the node's own GitHub when the push reaches the App's webhook. Turned off, the push dispatches nothing.",
		Takes: []*protocol.Param{
			{Name: "repo", Required: true, Says: "The repository the push lands on, with its host: github.com/owner/repo."},
			{Name: "branch", Required: true, Says: "The branch the push lands on."},
			{Name: "dispatches", Required: true, Says: "The repository whose workflow is dispatched, with its host: github.com/owner/repo."},
			{Name: "workflow", Required: true, Says: "The workflow's file name, as in deploy.yml."},
			{Name: "ref", Required: true, Says: "The branch of dispatches the workflow runs from."},
			{Name: "inputs", Required: true, Says: "The workflow's inputs as a JSON object of strings; {} for none."},
			{Name: "enabled", Required: true, Says: "true or false."},
		},
		Gives: []*protocol.Field{{Name: "follow", Says: "The follow as it now stands."}},
		Http:  &protocol.Endpoint{Method: http.MethodPost, Path: githubPath + "/follow"},
	}
}

func (g *gitHub) githubFollow(ctx context.Context, sent sigil.Sent) (any, *protocol.Refusal) {
	on, err := strconv.ParseBool(sent["enabled"])
	if err != nil {
		return nil, &protocol.Refusal{Why: sigil.Invalid, Param: "enabled", Says: "enabled is true or false, and this said " + sent["enabled"]}
	}
	f := GitHubFollow{Branch: sent["branch"], Workflow: sent["workflow"], Ref: sent["ref"], Enabled: on}
	for param, into := range map[string]*string{"repo": &f.Repo, "dispatches": &f.Dispatches} {
		named, err := forge.ParseRepo(sent[param])
		if err != nil {
			return nil, &protocol.Refusal{Why: sigil.Invalid, Param: param, Says: err.Error()}
		}
		*into = named.String()
	}
	notObject := &protocol.Refusal{Why: sigil.Invalid, Param: "inputs", Says: "inputs is a JSON object of strings, and this said " + sent["inputs"]}
	opens, err := json.NewDecoder(strings.NewReader(sent["inputs"])).Token()
	if err != nil || opens != json.Delim('{') {
		return nil, notObject
	}
	if err := json.Unmarshal([]byte(sent["inputs"]), &f.Inputs); err != nil {
		return nil, notObject
	}
	if err := g.host.nodeRecords().SetFollow(actorOf(ctx), f); err != nil {
		return nil, &protocol.Refusal{Why: sigil.Failed, Says: err.Error()}
	}
	return map[string]GitHubFollow{"follow": f}, nil
}
