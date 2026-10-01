// Package a2a is A2A as the node reads it from the pinned spec
// (server/parity/a2a_v1.0.1_3303592): the operations once, so that a binding
// is an adapter onto them and another can be added later.
//
// "Can we be relatively agnostic so I can always switch or add another later?"
package a2a

import (
	"slices"
	"strings"

	"github.com/teranos/QNTX/server/parity"
	"github.com/teranos/errors"
	"google.golang.org/genproto/googleapis/api/annotations"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/descriptorpb"
)

// Service is the A2A service the operations are read from.
const Service = "lf.a2a.v1.A2AService"

// OutOfScope is what the table leaves out: "The parts where you think that the
// spec disagrees with itself, those specific parts will not be in scope".
// SubscribeToTask is get in a2a.proto:78 and POST in specification.md:1169 and
// 2795; the push notification config operations are the others.
var OutOfScope = []string{
	"SubscribeToTask",
	"CreateTaskPushNotificationConfig",
	"GetTaskPushNotificationConfig",
	"ListTaskPushNotificationConfigs",
	"DeleteTaskPushNotificationConfig",
}

// Route is one HTTP method and path. A path parameter is written as the proto
// names it, {id=*} as {id}.
type Route struct {
	Method string
	Path   string
}

// Operation is one rpc of A2AService as the pinned a2a.proto declares it.
type Operation struct {
	Name     string
	Request  protoreflect.FullName
	Response protoreflect.FullName
	// Streams is a server-streaming rpc.
	Streams bool
	// HTTP is the HTTP+JSON route, and Tenant the same under /{tenant}, from
	// the rpc's google.api.http annotation.
	HTTP   Route
	Tenant Route
	// JSONRPC is the JSON-RPC method: the rpc's own name (§9.1).
	JSONRPC string
}

// Operations is every operation in scope, in the order the service declares
// them.
func Operations() ([]Operation, error) {
	service, err := service()
	if err != nil {
		return nil, err
	}
	var operations []Operation
	methods := service.Methods()
	for i := 0; i < methods.Len(); i++ {
		method := methods.Get(i)
		name := string(method.Name())
		if slices.Contains(OutOfScope, name) {
			continue
		}
		rule, err := httpRule(method)
		if err != nil {
			return nil, err
		}
		if len(rule.GetAdditionalBindings()) != 1 {
			return nil, errors.Newf("%s has %d additional HTTP bindings, and the tenant route is the one", name, len(rule.GetAdditionalBindings()))
		}
		operations = append(operations, Operation{
			Name:     name,
			Request:  method.Input().FullName(),
			Response: method.Output().FullName(),
			Streams:  method.IsStreamingServer(),
			HTTP:     routeOf(rule),
			Tenant:   routeOf(rule.GetAdditionalBindings()[0]),
			JSONRPC:  name,
		})
	}
	return operations, nil
}

// Declared is the name of every rpc A2AService declares, in or out of scope.
func Declared() ([]string, error) {
	service, err := service()
	if err != nil {
		return nil, err
	}
	var names []string
	for i := 0; i < service.Methods().Len(); i++ {
		names = append(names, string(service.Methods().Get(i).Name()))
	}
	return names, nil
}

func service() (protoreflect.ServiceDescriptor, error) {
	files, refused := parity.Descriptors("a2a")
	if refused != nil {
		return nil, errors.New(refused.GetSays())
	}
	for _, file := range files {
		if found := file.Services().ByName(protoreflect.FullName(Service).Name()); found != nil && found.FullName() == Service {
			return found, nil
		}
	}
	return nil, errors.Newf("the pinned a2a declares no %s", Service)
}

// httpRule reads google.api.http off a method. The compiled options carry the
// extension as bytes, so they are read again against the registry this binary
// links, where google.api.http is known.
func httpRule(method protoreflect.MethodDescriptor) (*annotations.HttpRule, error) {
	raw, err := proto.Marshal(method.Options())
	if err != nil {
		return nil, errors.Wrapf(err, "the options of %s did not marshal", method.FullName())
	}
	options := &descriptorpb.MethodOptions{}
	if err := proto.Unmarshal(raw, options); err != nil {
		return nil, errors.Wrapf(err, "the options of %s did not read", method.FullName())
	}
	rule, ok := proto.GetExtension(options, annotations.E_Http).(*annotations.HttpRule)
	if !ok || rule == nil || rule.GetPattern() == nil {
		return nil, errors.Newf("%s has no google.api.http", method.FullName())
	}
	return rule, nil
}

func routeOf(rule *annotations.HttpRule) Route {
	switch pattern := rule.GetPattern().(type) {
	case *annotations.HttpRule_Get:
		return Route{Method: "GET", Path: plain(pattern.Get)}
	case *annotations.HttpRule_Post:
		return Route{Method: "POST", Path: plain(pattern.Post)}
	case *annotations.HttpRule_Put:
		return Route{Method: "PUT", Path: plain(pattern.Put)}
	case *annotations.HttpRule_Delete:
		return Route{Method: "DELETE", Path: plain(pattern.Delete)}
	case *annotations.HttpRule_Patch:
		return Route{Method: "PATCH", Path: plain(pattern.Patch)}
	case *annotations.HttpRule_Custom:
		return Route{Method: pattern.Custom.GetKind(), Path: plain(pattern.Custom.GetPath())}
	}
	return Route{}
}

// plain writes a path parameter as its name alone: {id=*} is {id}. A segment
// that matches more than one segment, {name=**}, is kept as it is written.
func plain(path string) string {
	var b strings.Builder
	for {
		open := strings.Index(path, "{")
		if open < 0 {
			b.WriteString(path)
			return b.String()
		}
		closing := strings.Index(path[open:], "}")
		if closing < 0 {
			b.WriteString(path)
			return b.String()
		}
		closing += open
		b.WriteString(path[:open])
		param := path[open+1 : closing]
		if name, pattern, found := strings.Cut(param, "="); found && pattern == "*" {
			param = name
		}
		b.WriteString("{" + param + "}")
		path = path[closing+1:]
	}
}
