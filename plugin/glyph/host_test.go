package glyph

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/teranos/QNTX/ats"
	"github.com/teranos/QNTX/ats/types"
	"github.com/teranos/QNTX/plugin"
	"go.uber.org/zap"
)

func quiet() *zap.SugaredLogger { return zap.NewNop().Sugar() }

// published is a store holding whatever has been published, newest first, the
// way storage answers: it orders timestamp DESC and the handler takes one.
type published struct {
	ats.AttestationStore
	held []*types.As
	// asked is what the last filter looked for, so a test can say the host
	// went looking for its own subject and not something near it.
	asked ats.AttestationFilter
	fails error
}

func (p *published) GetAttestations(filter ats.AttestationFilter) ([]*types.As, error) {
	p.asked = filter
	if p.fails != nil {
		return nil, p.fails
	}
	if len(p.held) == 0 {
		return nil, nil
	}
	if filter.Limit > 0 && filter.Limit < len(p.held) {
		return p.held[:filter.Limit], nil
	}
	return p.held, nil
}

// services hands a host the store and nothing else, which is all a glyph asks
// of the node.
type services struct {
	plugin.ServiceRegistry
	store ats.AttestationStore
}

func (s *services) ATSStore() ats.AttestationStore { return s.store }

func module(id, text string, at time.Time) *types.As {
	return &types.As{
		ID:         id,
		Subjects:   []string{Subject + "chart"},
		Predicates: []string{Predicate},
		Contexts:   []string{"_"},
		Actors:     []string{"did:key:zSomeone"},
		Timestamp:  at,
		Attributes: map[string]interface{}{SourceAttribute: text},
		SignerDID:  "did:key:zNode",
	}
}

// hosting builds a host over a store, initialised the way the node does it.
func hosting(t *testing.T, store ats.AttestationStore) *Host {
	t.Helper()
	host := New("chart", quiet())
	if err := host.Initialize(context.Background(), &services{store: store}); err != nil {
		t.Fatalf("Initialize: %v", err)
	}
	return host
}

func serve(t *testing.T, host *Host) *http.ServeMux {
	t.Helper()
	mux := http.NewServeMux()
	if err := host.RegisterHTTP(mux); err != nil {
		t.Fatalf("RegisterHTTP: %v", err)
	}
	return mux
}

func TestModuleIsServedAsJavaScript(t *testing.T) {
	text := "export const glyphDef = {symbol: 'x'}\nexport const render = () => {}\n"
	store := &published{held: []*types.As{module("AS-1", text, time.Now())}}

	rec := httptest.NewRecorder()
	serve(t, hosting(t, store)).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, ModuleRoute, nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	// Without this exact media type the browser refuses the import, and the
	// canvas reports a missing glyph rather than a wrong header.
	if got := rec.Header().Get("Content-Type"); got != ModuleContentType {
		t.Errorf("Content-Type = %q, want %q", got, ModuleContentType)
	}
	if rec.Body.String() != text {
		t.Errorf("body = %q, want %q", rec.Body.String(), text)
	}
}

func TestTheHostLooksForItsOwnSubject(t *testing.T) {
	store := &published{held: []*types.As{module("AS-1", "export const render = () => {}", time.Now())}}

	hosting(t, store).ModuleDigest()

	if want := []string{"glyph-chart"}; len(store.asked.Subjects) != 1 || store.asked.Subjects[0] != want[0] {
		t.Errorf("asked for subjects %v, want %v", store.asked.Subjects, want)
	}
	if len(store.asked.Predicates) != 1 || store.asked.Predicates[0] != Predicate {
		t.Errorf("asked for predicates %v, want [%s]", store.asked.Predicates, Predicate)
	}
	// One row is the current one, and asking for more would be asking the
	// store to hand back history nobody reads.
	if store.asked.Limit != 1 {
		t.Errorf("asked for %d rows, want 1", store.asked.Limit)
	}
}

// Publishing writes another attestation, and the newer one is what is served.
func TestPublishingSupersedes(t *testing.T) {
	store := &published{held: []*types.As{module("AS-1", "first", time.Now())}}
	host := hosting(t, store)
	mux := serve(t, host)

	first := httptest.NewRecorder()
	mux.ServeHTTP(first, httptest.NewRequest(http.MethodGet, ModuleRoute, nil))
	if first.Body.String() != "first" {
		t.Fatalf("first body = %q, want %q", first.Body.String(), "first")
	}
	was := host.ModuleDigest()

	store.held = []*types.As{
		module("AS-2", "second", time.Now().Add(time.Minute)),
		module("AS-1", "first", time.Now()),
	}

	second := httptest.NewRecorder()
	mux.ServeHTTP(second, httptest.NewRequest(http.MethodGet, ModuleRoute, nil))
	if second.Body.String() != "second" {
		t.Errorf("second body = %q, want %q", second.Body.String(), "second")
	}
	if now := host.ModuleDigest(); now == was {
		t.Errorf("digest stayed %q after another module was published", now)
	}
}

// The digest is the attestation's id, which is what makes a published module a
// URL the browser has not imported before.
func TestDigestIsTheAttestationID(t *testing.T) {
	store := &published{held: []*types.As{module("AS-KJ7QP", "export const render = () => {}", time.Now())}}

	if got := hosting(t, store).ModuleDigest(); got != "AS-KJ7QP" {
		t.Errorf("digest = %q, want the attestation id %q", got, "AS-KJ7QP")
	}
}

func TestNothingPublishedIsRefusedAndNamed(t *testing.T) {
	store := &published{}
	host := hosting(t, store)

	rec := httptest.NewRecorder()
	serve(t, host).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, ModuleRoute, nil))

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusServiceUnavailable)
	}
	// The subject is in the answer because the canvas only ever says the
	// import failed, and what it found nothing under is the whole question.
	if !strings.Contains(rec.Body.String(), "glyph-chart") {
		t.Errorf("body %q does not name the subject", rec.Body.String())
	}
	if got := host.ModuleDigest(); got != "" {
		t.Errorf("digest = %q with nothing published, want empty", got)
	}
}

// An attestation under the subject with no source is not a module. Serving its
// empty body would hand the canvas a module exporting nothing.
func TestAnAttestationWithoutSourceIsNotAModule(t *testing.T) {
	held := module("AS-1", "", time.Now())
	held.Attributes = map[string]interface{}{"note": "published by hand"}
	store := &published{held: []*types.As{held}}

	rec := httptest.NewRecorder()
	serve(t, hosting(t, store)).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, ModuleRoute, nil))

	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusServiceUnavailable)
	}
}

// Health answers about the module, not about the host. The host is always
// running; which module is published is the only thing worth asking.
func TestHealthReadsTheStore(t *testing.T) {
	store := &published{held: []*types.As{module("AS-1", "export const render = () => {}", time.Now())}}
	host := hosting(t, store)

	got := host.Health(context.Background())
	if !got.Healthy {
		t.Errorf("Healthy = false with a module published: %s", got.Message)
	}
	if got.Details["as"] != "AS-1" {
		t.Errorf("details name %v as the module, want AS-1", got.Details["as"])
	}

	store.held = nil
	if got := host.Health(context.Background()); got.Healthy {
		t.Error("Healthy = true with nothing published")
	}
}

// A store that cannot answer is not a store that answered nothing. The glyph
// is unhealthy either way, and the log line is what tells them apart.
func TestAStoreThatFailsIsNotAnEmptyStore(t *testing.T) {
	store := &published{fails: context.DeadlineExceeded}

	if got := hosting(t, store).ModuleDigest(); got != "" {
		t.Errorf("digest = %q when the store failed, want empty", got)
	}
}

// A glyph reads its module from the store, so a host given no services has
// nowhere to read from and says so rather than serving nothing.
func TestAHostWithoutServicesRefusesToInitialize(t *testing.T) {
	if err := New("chart", quiet()).Initialize(context.Background(), nil); err == nil {
		t.Error("Initialize accepted a nil registry")
	}
}

// Pause suspends a process and keeps what it was holding. A glyph is an
// attestation, so there is nothing to suspend and nothing held.
func TestAGlyphIsNotPausable(t *testing.T) {
	var host plugin.DomainPlugin = New("chart", quiet())

	if _, pausable := host.(plugin.PausablePlugin); pausable {
		t.Error("a glyph offers pause and resume; embedding plugin.Base is how that happens")
	}
}

func TestMetadataNameIsTheRoute(t *testing.T) {
	if got := New("chart", quiet()).Metadata().Name; got != "chart" {
		t.Errorf("Name = %q, want %q", got, "chart")
	}
}
