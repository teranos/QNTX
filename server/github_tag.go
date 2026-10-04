package server

// "QNTX can know for sure, when a tag lands, and thus QNTX can do the dispatch
// to the QNTX App repo"

import (
	"strings"

	"github.com/teranos/QNTX/internal/sacred"
	"github.com/teranos/QNTX/plugin/grpc/protocol"
)

// A v* tag landing on appSource dispatches the app's build.
const (
	appSource   = "teranos/QNTX"
	appOwner    = "teranos"
	appRepo     = "QNTX-App"
	appWorkflow = "testflight.yml"
	appRef      = "main"
)

// tagLanded is the v* tag the push put on appSource, or empty.
func (p gitHubPush) tagLanded() string {
	tag, ok := strings.CutPrefix(p.Ref, "refs/tags/")
	if !ok || p.Deleted || !strings.EqualFold(p.Repository.FullName, appSource) || !strings.HasPrefix(tag, "v") {
		return ""
	}
	return tag
}

// dispatchApp dispatches the app's build for the tag that landed.
func (s *QNTXServer) dispatchApp(tag string) {
	logger := s.logger.Named("app")
	repo := appOwner + "/" + appRepo
	sacred.Go("app.dispatch."+tag, func() {
		said, _ := s.gitHubService().CreateAWorkflowDispatchEvent(s.lifetime(), &protocol.GitHubCreateAWorkflowDispatchEventRequest{
			Owner: appOwner, Repo: appRepo, WorkflowId: appWorkflow, Ref: appRef,
		})
		if !said.Success {
			logger.Errorw("A tag landed and the app's build was not dispatched", "tag", tag, "repo", repo, "workflow", appWorkflow, "error", said.Error)
			return
		}
		logger.Infow("A tag landed and the app's build was dispatched", "tag", tag, "repo", repo, "workflow", appWorkflow)
	})
}
