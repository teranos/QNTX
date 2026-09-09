package server

import (
	"time"

	"github.com/teranos/QNTX/server/namespaces"
)

// BootBudget is how long the subsystems together may take before the boot is
// an error. Sixty seconds is the wait the operator called long (ADR-024, The floor).
const BootBudget = 60 * time.Second

// Subsystem is a discrete initialization step for QNTXServer.
// Each subsystem lives in its own file and is responsible for one
// area of server setup (auth, plugins, embeddings, etc.).
//
// Subsystems run in the order they appear in the subsystems slice.
// A subsystem may read fields set by earlier subsystems.
type Subsystem interface {
	Name() string
	Init(s *QNTXServer) error
}

// SubsystemPolicy controls whether Init failure is fatal or non-fatal.
type SubsystemPolicy int

const (
	// SubsystemFatal — Init error aborts server startup.
	SubsystemFatal SubsystemPolicy = iota
	// SubsystemWarn — Init error is logged but startup continues.
	SubsystemWarn
)

type subsystemEntry struct {
	sub    Subsystem
	policy SubsystemPolicy
}

// subsystems defines the initialization order. Dependencies flow downward:
// later subsystems may read fields set by earlier ones.
var subsystems = []subsystemEntry{
	// The node DID names the system namespace, and namespace is the top-level
	// prefix every store writes under, so identity resolves before any store.
	{sub: nodeDIDSubsystem{}, policy: SubsystemFatal},
	// Before the door opens: a node whose store will not take a write has
	// nothing to admit anyone into.
	{sub: storeProofSubsystem{}, policy: SubsystemFatal},
	{sub: authSubsystem{}, policy: SubsystemFatal},
	{sub: pluginServicesSubsystem{}, policy: SubsystemWarn},
	{sub: tickerSubsystem{}, policy: SubsystemFatal},
	{sub: watcherSubsystem{}, policy: SubsystemWarn},
	{sub: canvasSubsystem{}, policy: SubsystemFatal},
	{sub: embeddingSubsystem{}, policy: SubsystemWarn},
	{sub: configWatcherSubsystem{}, policy: SubsystemWarn},
}

// NamespaceSubsystem is one step of a namespace starting.
//
// "The namespace is its own universe inside of QNTX" (ADR-026), so a namespace
// starts the way QNTX starts: it runs this list, in this order, for itself.
//
// A Subsystem is the host's — its DID, its doors, its plugins, its HTTP server,
// one of each however many namespaces it runs. A NamespaceSubsystem is a
// namespace's, and it is handed the namespace it is a step of.
type NamespaceSubsystem interface {
	Name() string
	Start(u *namespaces.Universe) error
}

// namespaceSubsystems is what a namespace starts, in order, whenever one
// starts: at boot for the ones the node already holds, and on being opened for
// one created since.
var namespaceSubsystems = []struct {
	sub    NamespaceSubsystem
	policy SubsystemPolicy
}{
	{sub: typeRegistrationSubsystem{}, policy: SubsystemWarn},
}
