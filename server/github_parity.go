package server

import (
	"sync"

	"github.com/teranos/QNTX/plugin/grpc/protocol"
	"github.com/teranos/QNTX/server/parity"
	"github.com/teranos/errors"
)

// GitHubService (ADR-043) is GitHub's REST API as GitHub describes it: each
// request message names the operation it is, // GET /issues, and each of its
// fields is the parameter or body property of that name. The github signum
// follows the description parity pins, read from those messages and nothing
// written out here. namespace, success and error are GitHubService's own.

var (
	githubFollows     *protocol.Follows
	githubFollowsErr  error
	githubFollowsOnce sync.Once
)

// githubFollowGitHub is what the GitHubService messages follow of GitHub's
// description, read once.
func githubFollowGitHub() (*protocol.Follows, error) {
	githubFollowsOnce.Do(func() {
		schema, refused := parity.Reference("github")
		if refused != nil {
			githubFollowsErr = errors.Newf("the github reference did not read: %s", refused.GetSays())
			return
		}
		githubFollows, githubFollowsErr = parity.OperationFollows("github", schema, "github_", "namespace", "success", "error")
	})
	return githubFollows, githubFollowsErr
}
