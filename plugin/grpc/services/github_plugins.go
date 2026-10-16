package services

// A plugin calls GitHubService as the namespace its record names (ADR-046), and
// holds no token of its own (ADR-043).
// "a plugin in my namespace calls it for GitHub things and holds no token of its own"

import (
	"context"
	"strings"

	errors "github.com/teranos/sacred-error"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
)

// PluginNamespaces is the namespace a plugin stands in, by the token the node
// handed it at Initialize. False is a token the node handed no plugin.
type PluginNamespaces func(token string) (string, bool)

// ForPlugins is what stands between a plugin and GitHubService: the plugin's
// token names its namespace, and every call it makes is that namespace's.
func (s *GitHubServer) ForPlugins(namespaces PluginNamespaces) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		rpc := info.FullMethod[strings.LastIndex(info.FullMethod, "/")+1:]
		route, found := githubRoutes[rpc]
		if !found {
			return nil, status.Errorf(codes.Unimplemented, "GitHubService %s has no route", rpc)
		}
		// The App's own routes, and those that answer with a credential, are the node's.
		if route.asApp || route.mints {
			return nil, status.Errorf(codes.PermissionDenied, "GitHubService %s is the node's own to call, not a plugin's", rpc)
		}
		token, err := bearer(ctx)
		if err != nil {
			return nil, status.Error(codes.Unauthenticated, err.Error())
		}
		namespace, standing := namespaces(token)
		if !standing {
			return nil, status.Error(codes.Unauthenticated, "the token is none the node handed a plugin")
		}
		asked, ok := req.(proto.Message)
		if !ok {
			return nil, status.Errorf(codes.Internal, "GitHubService %s was asked with a %T", rpc, req)
		}
		if err := standIn(asked.ProtoReflect(), namespace); err != nil {
			return nil, status.Errorf(codes.PermissionDenied, "GitHubService %s: %v", rpc, err)
		}
		return handler(ctx, req)
	}
}

// bearer is the token a call carries as its authorization metadata.
func bearer(ctx context.Context) (string, error) {
	md, carried := metadata.FromIncomingContext(ctx)
	if !carried {
		return "", errors.New("the call carries no metadata, so no token")
	}
	said := md.Get("authorization")
	if len(said) != 1 {
		return "", errors.Newf("the call carries %d authorization values, not one", len(said))
	}
	token, isBearer := strings.CutPrefix(said[0], "Bearer ")
	if !isBearer {
		return "", errors.New("the call's authorization is not a bearer token")
	}
	return token, nil
}

// standIn refuses a request that names any namespace but the one the plugin
// stands in. Nothing named is refused too: the node does not name it for it.
func standIn(msg protoreflect.Message, namespace string) error {
	field, err := githubField(msg.Descriptor(), "namespace")
	if err != nil {
		return err
	}
	if named := msg.Get(field).String(); named != namespace {
		return errors.Newf("the plugin stands in %q and asked as %q", namespace, named)
	}
	return nil
}
