package grpc

import (
	"fmt"

	"github.com/teranos/QNTX/plugin/grpc/protocol"
	errors "github.com/teranos/sacred-error"
)

// endpointKind is whether a service answers, or the node runs without it.
type endpointKind int

const (
	// servingAt is a service answering at an address.
	servingAt endpointKind = iota + 1
	// notRunning is a service the node runs without, and why.
	notRunning
)

// ServiceEndpoint is where a service the node runs for plugins answers, or why
// the node runs without it. One made by neither names nothing, and reading it
// is refused.
type ServiceEndpoint struct {
	how endpointKind
	at  string
	why error
}

// ServingAt is a service answering at address (host:port).
func ServingAt(address string) ServiceEndpoint {
	return ServiceEndpoint{how: servingAt, at: address}
}

// NotRunning is a service the node runs without, because of why.
func NotRunning(why error) ServiceEndpoint {
	return ServiceEndpoint{how: notRunning, why: why}
}

// Address is where the service answers, or why it answers nowhere.
func (e ServiceEndpoint) Address() (string, error) {
	switch e.how {
	case servingAt:
		return e.at, nil
	case notRunning:
		return "", e.why
	}
	return "", errors.Newf("the endpoint names no address and no reason (kind %d)", e.how)
}

func (e ServiceEndpoint) String() string {
	switch e.how {
	case servingAt:
		return e.at
	case notRunning:
		return "not running: " + e.why.Error()
	}
	return fmt.Sprintf("names nothing (kind %d)", e.how)
}

// toProto is the endpoint as InitializeRequest hands it to a plugin, or why it
// cannot be handed.
func (e ServiceEndpoint) toProto(service string) (*protocol.ServiceEndpoint, error) {
	switch e.how {
	case servingAt:
		return &protocol.ServiceEndpoint{State: &protocol.ServiceEndpoint_Address{Address: e.at}}, nil
	case notRunning:
		return &protocol.ServiceEndpoint{State: &protocol.ServiceEndpoint_NotRunning{NotRunning: e.why.Error()}}, nil
	}
	return nil, errors.Newf("the %s endpoint names no address and no reason (kind %d)", service, e.how)
}

// EndpointFrom is the endpoint a plugin was handed for service, or why it can
// use none: the node runs without the service, or named nothing for it.
func EndpointFrom(service string, handed *protocol.ServiceEndpoint) ServiceEndpoint {
	switch state := handed.GetState().(type) {
	case *protocol.ServiceEndpoint_Address:
		return ServingAt(state.Address)
	case *protocol.ServiceEndpoint_NotRunning:
		return NotRunning(errors.Newf("the node runs without its %s service: %s", service, state.NotRunning))
	}
	return NotRunning(errors.Newf("the node named no %s endpoint: it is older than this plugin", service))
}
