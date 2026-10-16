package services

import (
	"context"
	"net"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/teranos/QNTX/plugin/grpc/protocol"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

// standing is the namespace each plugin token the node handed stands in.
var standing = map[string]string{"tok-garden": "garden"}

// pluginGitHub is GitHubService as a plugin reaches it, over gRPC, behind ForPlugins.
func pluginGitHub(t *testing.T) (protocol.GitHubServiceClient, *[]seenRequest) {
	t.Helper()
	s, seen := fakeGitHub(t, answerJSON(200, `{"type":"file","name":"README.md","path":"README.md"}`))
	server := grpc.NewServer(grpc.UnaryInterceptor(s.ForPlugins(func(token string) (string, bool) {
		namespace, found := standing[token]
		return namespace, found
	})))
	protocol.RegisterGitHubServiceServer(server, s)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(server.Stop)
	conn, err := grpc.NewClient(listener.Addr().String(), grpc.WithTransportCredentials(insecure.NewCredentials()))
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close() })
	return protocol.NewGitHubServiceClient(conn), seen
}

func as(token string) context.Context {
	return metadata.AppendToOutgoingContext(context.Background(), "authorization", "Bearer "+token)
}

func contentOf(namespace string) *protocol.GitHubGetRepositoryContentRequest {
	return &protocol.GitHubGetRepositoryContentRequest{Namespace: namespace, Owner: "teranos", Repo: "QNTX", Path: "README.md"}
}

// "a plugin in my namespace calls it for GitHub things and holds no token of its own"
func TestAPluginCallsGitHubAsTheNamespaceItStandsIn(t *testing.T) {
	client, seen := pluginGitHub(t)

	answered, err := client.GetRepositoryContent(as("tok-garden"), contentOf("garden"))
	require.NoError(t, err)
	assert.True(t, answered.Success, answered.Error)
	require.Len(t, *seen, 1)
	assert.Equal(t, "Bearer ghp_garden", (*seen)[0].Header.Get("Authorization"))
}

func TestAPluginIsRefusedAnyOtherNamespace(t *testing.T) {
	client, seen := pluginGitHub(t)

	for _, named := range []string{"orchard", ""} {
		_, err := client.GetRepositoryContent(as("tok-garden"), contentOf(named))
		assert.Equal(t, codes.PermissionDenied, status.Code(err), "asked as %q", named)
	}
	assert.Empty(t, *seen, "GitHub was never asked")
}

func TestACallWithoutAPluginsTokenIsRefused(t *testing.T) {
	client, seen := pluginGitHub(t)

	_, err := client.GetRepositoryContent(context.Background(), contentOf("garden"))
	assert.Equal(t, codes.Unauthenticated, status.Code(err))

	_, err = client.GetRepositoryContent(as("tok-nobody"), contentOf("garden"))
	assert.Equal(t, codes.Unauthenticated, status.Code(err))
	assert.Empty(t, *seen, "GitHub was never asked")
}

func TestTheAppsOwnRoutesAreNotAPluginsToCall(t *testing.T) {
	client, seen := pluginGitHub(t)

	_, err := client.CreateAnInstallationAccessTokenForAnApp(as("tok-garden"), &protocol.GitHubCreateAnInstallationAccessTokenForAnAppRequest{})
	assert.Equal(t, codes.PermissionDenied, status.Code(err))
	assert.Empty(t, *seen, "GitHub was never asked")
}
