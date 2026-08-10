package server

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
	{sub: authSubsystem{}, policy: SubsystemFatal},
	{sub: typeRegistrationSubsystem{}, policy: SubsystemWarn},
	{sub: pluginServicesSubsystem{}, policy: SubsystemWarn},
	{sub: tickerSubsystem{}, policy: SubsystemFatal},
	{sub: watcherSubsystem{}, policy: SubsystemWarn},
	{sub: canvasSubsystem{}, policy: SubsystemFatal},
	{sub: embeddingSubsystem{}, policy: SubsystemWarn},
	{sub: configWatcherSubsystem{}, policy: SubsystemWarn},
}
