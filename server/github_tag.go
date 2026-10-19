package server

// "QNTX can know for sure, when a tag lands, and thus QNTX can do the dispatch
// to the QNTX App repo"

import (
	"strings"

	"github.com/teranos/QNTX/internal/forge"
	"github.com/teranos/QNTX/internal/sacred"
	"github.com/teranos/QNTX/plugin/grpc/protocol"
	"github.com/teranos/QNTX/server/auth"
)

// A v* tag landing on appSource dispatches the app's build.
const (
	appOwner    = "teranos"
	appRepo     = "QNTX-App"
	appWorkflow = "testflight.yml"
	appRef      = "main"
)

// appSource is the repository whose tags build the app.
var appSource = forge.Repo{Host: forge.GitHub, Path: "teranos/QNTX"}

// tagLanded is the v* tag the push put on appSource, or empty.
func tagLanded(p forge.Push) string {
	tag := p.Tag()
	if p.Deleted || !p.Repo.Is(appSource) || !strings.HasPrefix(tag, "v") {
		return ""
	}
	return tag
}

// dispatchApp dispatches the app's build for the tag that landed.
func (g *gitHub) dispatchApp(tag string) {
	logger := g.host.log().Named("app")
	repo := appOwner + "/" + appRepo
	sacred.Go("app.dispatch."+tag, func() {
		said, err := g.gitHubService().CreateAWorkflowDispatchEvent(g.host.lifetime(), &protocol.GitHubCreateAWorkflowDispatchEventRequest{
			Namespace: auth.NamespaceSystem, Owner: appOwner, Repo: appRepo, WorkflowId: appWorkflow, Ref: appRef,
		})
		if err != nil {
			logger.Errorw("A tag landed and the app's build was not dispatched", "tag", tag, "repo", repo, "workflow", appWorkflow, "error", err)
			return
		}
		if !said.Success {
			logger.Errorw("A tag landed and the app's build was not dispatched", "tag", tag, "repo", repo, "workflow", appWorkflow, "error", said.Error)
			return
		}
		logger.Infow("A tag landed and the app's build was dispatched", "tag", tag, "repo", repo, "workflow", appWorkflow)
	})
}
