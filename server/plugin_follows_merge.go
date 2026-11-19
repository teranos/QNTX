package server

// "i actually think QNTX should be less forgiving towards dead links"

// "like, in our case, why would QNTX not just be able to know it did merge, and adjust accordingly ?"

// "why at startup, why not let the webhook trigger it"

import (
	"encoding/json"
	"strings"

	errors "github.com/teranos/sacred-error"
)

// gitHubMergedPullRequest is the part of a pull_request event a plugin's tree URL follows.
type gitHubMergedPullRequest struct {
	Action      string `json:"action"`
	PullRequest struct {
		Merged bool `json:"merged"`
		Head   struct {
			Ref  string `json:"ref"`
			Repo struct {
				FullName string `json:"full_name"`
			} `json:"repo"`
		} `json:"head"`
		Base struct {
			Ref string `json:"ref"`
		} `json:"base"`
	} `json:"pull_request"`
	Repository struct {
		FullName string `json:"full_name"`
	} `json:"repository"`
}

// pluginsFollowMerge writes again, at the base branch, every plugin whose tree
// URL names the head branch of a pull request that merged, and names them. An
// event that is no merge names none.
func (s *QNTXServer) pluginsFollowMerge(event string, body []byte) ([]string, error) {
	if event != "pull_request" {
		return []string{}, nil
	}
	var e gitHubMergedPullRequest
	if err := json.Unmarshal(body, &e); err != nil {
		return nil, errors.Wrap(err, "the pull_request event is not readable JSON")
	}
	pr := e.PullRequest
	// A head on a fork is a branch of another repository than the one merged into.
	if e.Action != "closed" || !pr.Merged || !strings.EqualFold(pr.Head.Repo.FullName, e.Repository.FullName) {
		return []string{}, nil
	}
	records := s.pluginRecords()
	plugins, err := records.Plugins()
	if err != nil {
		return nil, err
	}
	followed := []string{}
	for _, record := range plugins {
		source, err := readGitHubSource(record.Repo)
		if err != nil || !source.RefNamed || source.Ref != pr.Head.Ref ||
			!strings.EqualFold(source.Owner+"/"+source.Repo, e.Repository.FullName) {
			continue
		}
		repo := "https://github.com/" + source.Owner + "/" + source.Repo + "/tree/" + pr.Base.Ref
		if source.PathNamed {
			repo += "/" + source.Path
		}
		was := record.Repo
		record.Repo = repo
		if err := records.pluginLine(pluginSource, record, map[string]any{}); err != nil {
			return followed, errors.Wrapf(err, "plugin %s was not moved from %s to %s", record.Name, was, repo)
		}
		followed = append(followed, record.Name)
	}
	return followed, nil
}
