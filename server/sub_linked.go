package server

import (
	"context"
	"slices"

	"github.com/teranos/QNTX/ats"
	"github.com/teranos/QNTX/ats/storage"
	"github.com/teranos/QNTX/ats/watcher"
	"github.com/teranos/QNTX/pulse/async"
	"github.com/teranos/QNTX/server/namespaces"
	"github.com/teranos/QNTX/server/sigil"
	"github.com/teranos/QNTX/server/statusline"
	"github.com/teranos/QNTX/server/subsystem"
	errors "github.com/teranos/sacred-error"
	"go.uber.org/zap"
)

// The subsystems linked into this binary (ADR-056, server/subsystem). Each
// registered itself from the package that links it; this is where the node
// reads them: their rows stand, each is started with the node as its host,
// and what each declared it registers is checked to be there.
//
// Fatal: a subsystem is the build's own, and a build whose subsystem does not
// start is a build that is wrong, the way a line in the tablet naming a path
// nothing answers is.
type linkedSubsystem struct{}

func (linkedSubsystem) Name() string { return "linked" }

func (linkedSubsystem) Init(s *QNTXServer) error {
	for _, sub := range subsystem.Registered() {
		decl := sub.Declare()
		rows, err := subsystem.Standing(decl)
		if err != nil {
			return errors.Wrapf(err, "the subsystem %s declares a standing row that cannot stand", decl.GetName())
		}
		if err := watcher.Stand(rows...); err != nil {
			return errors.Wrapf(err, "the subsystem %s stood a row that cannot stand", decl.GetName())
		}
		if err := sub.Start(linkedHost{s}); err != nil {
			return errors.Wrapf(err, "the subsystem %s did not start", decl.GetName())
		}
		for _, name := range decl.GetHandlerNames() {
			if !s.daemon.Registry().Has(name) {
				return errors.Newf("the subsystem %s declares the built-in %s and registered none by that name", decl.GetName(), name)
			}
		}
		s.logger.Infow("A subsystem linked into this build started",
			"subsystem", decl.GetName(), "signa", len(decl.GetSigna()), "standing", len(rows), "handlers", decl.GetHandlerNames(), "github_events", decl.GetGithubEvents())
	}
	return nil
}

// linkedSigna is every linked subsystem's signa, with their answers, served
// as the node's own (signa.go).
func (s *QNTXServer) linkedSigna() []sigil.Signum {
	var signa []sigil.Signum
	for _, sub := range subsystem.Registered() {
		signa = append(signa, subsystem.Signa(sub)...)
	}
	return signa
}

// deliveries is what each linked subsystem that reads this event answered to
// one delivery, under its name. A listener that failed is said under its name
// too, in the log and in the answer, and is not a failed delivery GitHub
// would hand in again.
func (s *QNTXServer) deliveries(ctx context.Context, event string, body []byte) map[string]any {
	var said map[string]any
	for _, sub := range subsystem.Registered() {
		decl := sub.Declare()
		if !slices.Contains(decl.GetGithubEvents(), event) {
			continue
		}
		listener, reads := sub.(subsystem.Delivered)
		if !reads {
			s.logger.Errorw("A subsystem names GitHub events and reads no delivery", "subsystem", decl.GetName(), "event", event)
			continue
		}
		if said == nil {
			said = map[string]any{}
		}
		answered, err := listener.Delivered(ctx, event, body)
		if err != nil {
			s.logger.Errorw("A subsystem did not read a delivery", "subsystem", decl.GetName(), "event", event, "error", err)
			said[decl.GetName()] = "error: " + err.Error()
			continue
		}
		said[decl.GetName()] = answered
	}
	return said
}

// linkedHost is the node as a subsystem is handed it: the methods of
// subsystem.Host, each over what the server already holds.
type linkedHost struct{ s *QNTXServer }

func (h linkedHost) Read(namespace string) (namespaces.Reading, error) {
	if h.s.held == nil {
		return nil, errors.New("no namespaces are held to read from")
	}
	return h.s.held.Read(namespace)
}

func (h linkedHost) Known() storage.Namespaces {
	if h.s.held == nil {
		return nil
	}
	return h.s.held.Known()
}

func (h linkedHost) StoreFor(ctx context.Context) (ats.AttestationStore, error) {
	caller := sigil.Caller(ctx)
	if caller == nil {
		return nil, errors.New("the asking carried no request to find the caller's namespace by")
	}
	return h.s.storeFor(caller)
}

func (h linkedHost) Registry() *async.HandlerRegistry { return h.s.daemon.Registry() }

func (h linkedHost) Enqueue(job *async.Job) error {
	if h.s.daemon == nil {
		return errors.New("the node has no daemon to enqueue a job on")
	}
	return h.s.daemon.GetQueue().Enqueue(job)
}

func (h linkedHost) News() *statusline.NewsLog { return h.s.news }

func (h linkedHost) MintedBy(did string) (string, bool) { return h.s.authHandler.MintedBy(did) }

func (h linkedHost) AskGitHub(ctx context.Context, operation string, request any) ([]byte, error) {
	return h.s.askGitHub(ctx, operation, request)
}

func (h linkedHost) Logger() *zap.SugaredLogger { return h.s.logger }
