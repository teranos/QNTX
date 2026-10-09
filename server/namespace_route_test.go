package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/teranos/QNTX/server/auth"
	"go.uber.org/zap"
)

func requestAs(caller auth.Admission) *http.Request {
	req := httptest.NewRequest(http.MethodGet, "/api/attestations", nil)
	return req.WithContext(auth.WithAdmission(req.Context(), caller))
}

// A node is handed its universes before it serves anything, so a node under
// test is handed one too: the default, and whatever a test adds to it.
func routeServer() *QNTXServer {
	return &QNTXServer{logger: zap.NewNop().Sugar(), held: servingStub(stubStore{})}
}

// A token minted for the duck pond wrote to the playground and was told it
// worked. Refusing is not the feature, but it is not a lie either.
func TestATokenOutsideAnyOpenNamespaceIsRefused(t *testing.T) {
	s := routeServer()
	_, err := s.storeFor(requestAs(auth.Admitted(auth.LevelAttestor, "pond")))
	if err == nil {
		t.Fatal("a caller in an unopened namespace got a store")
	}
	if !strings.Contains(err.Error(), "pond") {
		t.Fatalf("the refusal does not name what was asked for: %v", err)
	}
}

func TestTheDefaultNamespaceIsServed(t *testing.T) {
	s := routeServer()
	if _, err := s.storeFor(requestAs(auth.Admitted(auth.LevelAttestor, auth.NamespaceDefault))); err != nil {
		t.Fatalf("an admission naming default was refused: %v", err)
	}
}

// "nil is nil"

// Naming no namespace reaches none, at any level below SUPER.
func TestNamingNoNamespaceReachesNone(t *testing.T) {
	s := routeServer()
	_, err := s.storeFor(requestAs(auth.Admitted(auth.LevelAttestor)))
	if err == nil {
		t.Fatal("an admission naming no namespace got a store")
	}
}

// Namespaces are their own universes and nothing crosses (ADR-026). Where a
// request acts is a fact about the caller, and a request carries no say in it.
func TestNothingOnTheRequestNamesTheNamespace(t *testing.T) {
	s := routeServer()
	pond := auth.Admitted(auth.LevelAttestor, "pond")

	req := httptest.NewRequest(http.MethodGet, "/api/attestations?namespace=default", nil)
	req = req.WithContext(auth.WithAdmission(req.Context(), pond))

	_, err := s.storeFor(req)
	if err == nil {
		t.Fatal("a request talked its way into another namespace")
	}
	if !strings.Contains(err.Error(), "pond") {
		t.Fatalf("the caller acted somewhere other than its own namespace: %v", err)
	}
}

// system is not visible below SUPER (ADR-027).
func TestATokenCannotReachSystem(t *testing.T) {
	s := routeServer()
	s.held.SetSystem(oneNamespace("system", s.held.Served()))
	reach := auth.Admitted(auth.LevelAttestor, auth.NamespaceSystem)

	if _, err := s.storeFor(requestAs(reach)); err == nil {
		t.Fatal("a token reached the system namespace")
	}
}

// A request no gate admitted is a stranger on an ANYONE route, or a handler
// wired past the gate. Neither reaches a store.
func TestNoCallerReachesNoStore(t *testing.T) {
	s := routeServer()
	if _, err := s.storeFor(httptest.NewRequest(http.MethodGet, "/api/attestations", nil)); err == nil {
		t.Fatal("a request nobody admitted got the served store")
	}
	if _, _, refusal, _ := s.openCall(context.Background()); refusal == nil {
		t.Fatal("a plugin call nobody admitted was handed a store token")
	}
}

// A node without auth says who its one caller is, rather than saying nothing
// and having nothing read as everything.
func TestANodeWithoutAuthAdmitsItsOneCallerAsRoot(t *testing.T) {
	s := routeServer()
	var seen auth.Admission
	var admitted bool
	s.gate("/api/attestations", auth.Reach{}, func(_ http.ResponseWriter, r *http.Request) {
		seen, admitted = auth.AdmissionFrom(r.Context())
	})(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/api/attestations", nil))

	if !admitted {
		t.Fatal("the gate of a node without auth admitted nobody")
	}
	if !seen.ReachesEveryNamespace() || !seen.IsRoot() {
		t.Fatalf("the one caller of a node without auth is %s", seen.LevelName())
	}
}
