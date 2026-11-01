// Package subsystem is the contract a compiled-in subsystem keeps with the
// node (ADR-056).
//
// "QNTX needs a formal contract for pluggable, optional, compiled-in
// subsystems."
//
// "not a fucking plugin"
//
// A plugin is another process, on a node through its record, and a person
// holds it and takes it away (ADR-002). A subsystem is linked into the binary
// by the main that builds it, and is there or not for the life of the node.
// What it declares is protocol.Subsystem; what it does is Go behind the names
// there, registered from the package that links it:
//
//	func init() {
//		if err := subsystem.Register(quotes{}); err != nil {
//			panic(err)
//		}
//	}
//
// The node reads the registered at boot (server/linked.go): their signa are
// served as its own, their standing rows stand with its own, each is started
// with the node as its Host, and each that reads deliveries is handed them.
//
// "And subsystems are also where you should be more free to encode org
// specific policy and descision."
//
// QNTX names no subsystem and decides none of their policy. This package
// imports nothing of package server, so a subsystem's own module imports it.
package subsystem

import (
	"context"
	"slices"
	"sort"
	"sync"

	"github.com/teranos/QNTX/ats"
	"github.com/teranos/QNTX/ats/storage"
	"github.com/teranos/QNTX/ats/types"
	"github.com/teranos/QNTX/ats/watcher"
	"github.com/teranos/QNTX/plugin/grpc/protocol"
	"github.com/teranos/QNTX/pulse/async"
	"github.com/teranos/QNTX/server/namespaces"
	"github.com/teranos/QNTX/server/sigil"
	"github.com/teranos/QNTX/server/statusline"
	errors "github.com/teranos/sacred-error"
	"go.uber.org/zap"
)

// A Subsystem is one compiled in: what it declares, the functions behind its
// sigils, and its start.
type Subsystem interface {
	// Declare is what the subsystem is, as data: its name, its signa, its
	// standing rows, its built-ins' names, what its messages say, the
	// deliveries it reads.
	Declare() *protocol.Subsystem
	// Answers is the function behind each sigil, by signum name then sigil
	// name. A sigil declared with no answer is refused when the signa are
	// checked (sigil.Signum.Check), and the signum is served nowhere.
	Answers() map[string]map[string]sigil.Answer
	// Start is the subsystem's boot step, handed the node. What it registers
	// in the host's registry are the built-ins it declared.
	Start(Host) error
}

// Host is what a subsystem may ask of the node. Each method is one the first
// subsystem reached into the server for; nothing here is new code, each is an
// existing method or field given a name on an interface.
type Host interface {
	// Read is a namespace's store to read, by name.
	Read(namespace string) (namespaces.Reading, error)
	// Known is the namespaces the node keeps, to list; nil is a backend that
	// keeps one.
	Known() storage.Namespaces
	// StoreFor is the store of the namespace the caller of a sigil stands in,
	// from the context the sigil's Answer was handed.
	StoreFor(ctx context.Context) (ats.AttestationStore, error)
	// Registry is where a built-in is registered, by its name.
	Registry() *async.HandlerRegistry
	// Enqueue hands a job to the daemon, for a built-in to run off the
	// caller's time.
	Enqueue(job *async.Job) error
	// News is what built-ins leave on the status row, per caller.
	News() *statusline.NewsLog
	// MintedBy is who a token's DID speaks for, when the node knows.
	MintedBy(did string) (string, bool)
	// AskGitHub asks GitHubService one operation as the App's installation,
	// and hands back GitHub's answer as JSON.
	AskGitHub(ctx context.Context, operation string, request any) ([]byte, error)
	// Logger is the node's, for the subsystem to name its own under.
	Logger() *zap.SugaredLogger
}

// Delivered is a subsystem that reads the App's webhook deliveries. The node
// hands it each event its declaration names (protocol.Subsystem.github_events),
// and puts what it answered in the webhook's response under the subsystem's
// name; an error there is said under the name too, not as a failed delivery
// GitHub would hand in again.
type Delivered interface {
	Delivered(ctx context.Context, event string, body []byte) (any, error)
}

var (
	mu         sync.RWMutex
	registered = map[string]Subsystem{}
)

// Register makes a subsystem one the node reads at boot. A second of one name
// is refused: the build that linked both learns it when the first init runs.
func Register(s Subsystem) error {
	if s == nil {
		return errors.New("no subsystem to register")
	}
	name := s.Declare().GetName()
	if name == "" {
		return errors.New("a subsystem with no name cannot register")
	}
	mu.Lock()
	defer mu.Unlock()
	if _, twice := registered[name]; twice {
		return errors.Newf("a subsystem is registered as %s already", name)
	}
	registered[name] = s
	return nil
}

// Withdraw forgets a registered subsystem, by name. For a test that registered
// one; a node forgets nothing it was linked with.
func Withdraw(name string) {
	mu.Lock()
	defer mu.Unlock()
	delete(registered, name)
}

// Registered is every subsystem registered, by name.
func Registered() []Subsystem {
	mu.RLock()
	defer mu.RUnlock()
	names := make([]string, 0, len(registered))
	for name := range registered {
		names = append(names, name)
	}
	sort.Strings(names)
	held := make([]Subsystem, 0, len(names))
	for _, name := range names {
		held = append(held, registered[name])
	}
	return held
}

// Signa is a subsystem's signa with their answers: each declared signum paired
// with the functions Answers gives for it, as the node serves a signum.
func Signa(s Subsystem) []sigil.Signum {
	answers := s.Answers()
	var signa []sigil.Signum
	for _, declared := range s.Declare().GetSigna() {
		signa = append(signa, sigil.Signum{Signum: declared, Answers: answers[declared.GetName()]})
	}
	return signa
}

// Standing is a declaration's standing rows as rows of the standing table:
// each a built-in by its handler's name, the structural filter as declared,
// over every time, enabled, at its rate. A row naming no filter would match
// every attestation; one naming a handler the subsystem does not declare
// would reach nothing. Both are refused by the row's id.
func Standing(decl *protocol.Subsystem) ([]storage.Watcher, error) {
	rows := make([]storage.Watcher, 0, len(decl.GetStanding()))
	for _, reg := range decl.GetStanding() {
		if reg.GetId() == "" {
			return nil, errors.Newf("a standing row of %s has no id", decl.GetName())
		}
		named := len(reg.GetSubjects()) + len(reg.GetPredicates()) + len(reg.GetContexts()) + len(reg.GetActors())
		if named == 0 {
			return nil, errors.Newf("the standing row %s of %s names nothing to watch, so it would match every attestation", reg.GetId(), decl.GetName())
		}
		if !slices.Contains(decl.GetHandlerNames(), reg.GetHandlerName()) {
			return nil, errors.Newf("the standing row %s of %s reaches %s, which %s does not declare among its built-ins", reg.GetId(), decl.GetName(), reg.GetHandlerName(), decl.GetName())
		}
		if reg.GetMaxFiresPerSecond() <= 0 {
			return nil, errors.Newf("the standing row %s of %s has a fire rate of %d, which is never", reg.GetId(), decl.GetName(), reg.GetMaxFiresPerSecond())
		}
		rows = append(rows, storage.Watcher{
			ID:   reg.GetId(),
			Name: reg.GetId(),
			Filter: types.AxFilter{
				TimeStart: types.Unbounded(), TimeEnd: types.Unbounded(),
				Subjects: reg.GetSubjects(), Predicates: reg.GetPredicates(), Contexts: reg.GetContexts(), Actors: reg.GetActors(),
			},
			ActionType:        storage.ActionTypeBuiltinExecute,
			ActionData:        watcher.BuiltinActionData(reg.GetHandlerName()),
			MaxFiresPerSecond: int(reg.GetMaxFiresPerSecond()),
			Enabled:           true,
		})
	}
	return rows, nil
}
