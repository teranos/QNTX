package server

import (
	"net/http"
	"slices"
	"strings"

	a2asdk "github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2acompat/a2av0"
	a2agrpcv0 "github.com/a2aproject/a2a-go/v2/a2agrpc/v0"
	a2agrpc "github.com/a2aproject/a2a-go/v2/a2agrpc/v1"
	"github.com/a2aproject/a2a-go/v2/a2asrv"
	"google.golang.org/grpc"
)

// "「[Fable 5.2] WHAT IS THE STRUCTURALLY ELEGANT AND FUTURE PROOF WAY TO GO ABOUT THIS」"
//
// "「[Fable 5.2] I WANRED THE ELEGANT ONE FROM THE  START」"
//
// 「[Fable 5.2] Every A2A version the node speaks is a row: a2a-go's bindings for it, and」
// 「[Fable 5.2] where each is mounted. Where a request arrives is the version it is in, as」
// 「[Fable 5.2] a2a-go's own compat tests mount each version at a URL of its own, so the」
// 「[Fable 5.2] version a caller names, or names none, does not matter. The routes, the」
// 「[Fable 5.2] cards' interfaces and the gRPC services are read from this table, and parity」
// 「[Fable 5.2] holds it to what the pinned a2a-go serves (a2a.serves.json): a pin that」
// 「[Fable 5.2] brings a version is one row more.」

// 「[Fable 5.2] a2aVersion is one A2A version the node speaks.」
type a2aVersion struct {
	version a2asdk.ProtocolVersion
	// 「[Fable 5.2] base is where its HTTP bindings are mounted, and grpcService where gRPC」
	// 「[Fable 5.2] asks for its A2AService: /<package>.<service>/.」
	base, grpcService string
	// 「[Fable 5.2] tenantField is whether its requests carry the tenant they are for. A」
	// 「[Fable 5.2] version whose requests do not is reached at a tenant under the tenant's」
	// 「[Fable 5.2] path, and each request is given the tenant its path named.」
	tenantField bool
	// 「[Fable 5.2] operation is whether a path beneath base is an operation at no tenant.」
	operation func(method, path string) bool
	// 「[Fable 5.2] a2a-go's bindings: HTTP+JSON, at no tenant and at one, JSON-RPC and gRPC.」
	rest, restAtTenant, jsonRPC func(a2asrv.RequestHandler, ...a2asrv.TransportOption) http.Handler
	grpc                        func(a2asrv.RequestHandler, *grpc.Server)
	// 「[Fable 5.2] answeredBy is the handler its bindings are given, made of the node's.」
	answeredBy func(a2asrv.RequestHandler, *QNTXServer) a2asrv.RequestHandler
}

// 「[Fable 5.2] a2aSpoken is every A2A version the node speaks, the newest first: the order」
// 「[Fable 5.2] a card lists them in.」
var a2aSpoken = []a2aVersion{
	{
		version: a2asdk.Version, base: "/a2a", grpcService: "/lf.a2a.v1.A2AService/", tenantField: true,
		operation: a2aRoute,
		rest:      a2asrv.NewRESTHandler,
		restAtTenant: func(h a2asrv.RequestHandler, opts ...a2asrv.TransportOption) http.Handler {
			return a2asrv.NewTenantRESTHandler("/{*}", h, opts...)
		},
		jsonRPC: a2asrv.NewJSONRPCHandler,
		grpc:    func(h a2asrv.RequestHandler, g *grpc.Server) { a2agrpc.NewHandler(h).RegisterWith(g) },
		answeredBy: func(h a2asrv.RequestHandler, _ *QNTXServer) a2asrv.RequestHandler {
			return h
		},
	},
	{
		version: a2av0.Version, base: "/a2a/0.3", grpcService: "/a2a.v1.A2AService/",
		operation: func(_, path string) bool {
			return strings.HasPrefix(path, a2av0.RESTPathPrefix+"/")
		},
		rest:    a2av0.NewRESTHandler,
		jsonRPC: a2av0.NewJSONRPCHandler,
		grpc:    func(h a2asrv.RequestHandler, g *grpc.Server) { a2agrpcv0.NewHandler(h).RegisterWith(g) },
		answeredBy: func(h a2asrv.RequestHandler, s *QNTXServer) a2asrv.RequestHandler {
			return tenantIn03{h, s}
		},
	},
}

// 「[Fable 5.2] bindings is each binding v is served over, as a2a-go names it.」
func (v a2aVersion) bindings() []a2asdk.TransportProtocol {
	var over []a2asdk.TransportProtocol
	if v.rest != nil {
		over = append(over, a2asdk.TransportProtocolHTTPJSON)
	}
	if v.jsonRPC != nil {
		over = append(over, a2asdk.TransportProtocolJSONRPC)
	}
	if v.grpc != nil {
		over = append(over, a2asdk.TransportProtocolGRPC)
	}
	return over
}

// 「[Fable 5.2] spokenAt is the version whose base path is beneath, the longest base that」
// 「[Fable 5.2] holds it, and path past that base.」
func spokenAt(path string) (a2aVersion, string, bool) {
	at := -1
	for i, v := range a2aSpoken {
		if (path == v.base || strings.HasPrefix(path, v.base+"/")) && (at < 0 || len(v.base) > len(a2aSpoken[at].base)) {
			at = i
		}
	}
	if at < 0 {
		return a2aVersion{}, "", false
	}
	return a2aSpoken[at], "/" + strings.TrimPrefix(strings.TrimPrefix(path, a2aSpoken[at].base), "/"), true
}

// 「[Fable 5.2] spokenOverGRPC is the version whose A2AService path is beneath.」
func spokenOverGRPC(path string) (a2aVersion, bool) {
	at := slices.IndexFunc(a2aSpoken, func(v a2aVersion) bool { return strings.HasPrefix(path, v.grpcService) })
	if at < 0 {
		return a2aVersion{}, false
	}
	return a2aSpoken[at], true
}

// 「[Fable 5.2] tenantAt is the tenant a path beneath v's base names, and the operation's」
// 「[Fable 5.2] path after it. A path that is an operation at no tenant names none; any」
// 「[Fable 5.2] other's first segment is a tenant, a namespace named tasks among them.」
func (v a2aVersion) tenantAt(method, path string) (string, string, bool) {
	if v.operation(method, path) {
		return "", path, false
	}
	first, _, _ := strings.Cut(strings.TrimPrefix(path, "/"), "/")
	if first == "" {
		return "", path, false
	}
	return first, "/" + strings.TrimPrefix(strings.TrimPrefix(path, "/"+first), "/"), true
}

// 「[Fable 5.2] interfacesOf is what a card says the agent at tenant is reached at: each」
// 「[Fable 5.2] version's HTTP bindings beneath node, then each version's gRPC at grpcAt when」
// 「[Fable 5.2] the node is reached over gRPC. A version whose requests carry no tenant is」
// 「[Fable 5.2] reached at a tenant under the tenant's path, and over gRPC, whose path is」
// 「[Fable 5.2] its service's, at none.」
func interfacesOf(node, grpcAt, tenant string) []*a2asdk.AgentInterface {
	var over []*a2asdk.AgentInterface
	for _, overGRPC := range []bool{false, true} {
		for _, v := range a2aSpoken {
			url, named := node+v.base, tenant
			if !v.tenantField && tenant != "" {
				url, named = url+"/"+tenant, ""
			}
			for _, binding := range v.bindings() {
				if (binding == a2asdk.TransportProtocolGRPC) != overGRPC {
					continue
				}
				if overGRPC {
					if grpcAt == "" || (!v.tenantField && tenant != "") {
						continue
					}
					url = grpcAt
				}
				over = append(over, &a2asdk.AgentInterface{URL: url, ProtocolBinding: binding, ProtocolVersion: v.version, Tenant: named})
			}
		}
	}
	return over
}
