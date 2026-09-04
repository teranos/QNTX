package server

import (
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

func routeServer() *QNTXServer {
	return &QNTXServer{logger: zap.NewNop().Sugar()}
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
	// Naming it, and naming none — a session names none.
	for _, named := range [][]string{{auth.NamespaceDefault}, nil} {
		if _, err := s.storeFor(requestAs(auth.Admission{Namespaces: named})); err != nil {
			t.Fatalf("namespaces %v were refused: %v", named, err)
		}
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
	s.systemStore = s.atsStore
	reach := auth.Admitted(auth.LevelAttestor, auth.NamespaceSystem)

	if _, err := s.storeFor(requestAs(reach)); err == nil {
		t.Fatal("a token reached the system namespace")
	}
}

// A request that never reached auth has no caller and no namespace to check.
func TestNoCallerFallsToTheServedStore(t *testing.T) {
	s := routeServer()
	if _, err := s.storeFor(httptest.NewRequest(http.MethodGet, "/api/attestations", nil)); err != nil {
		t.Fatalf("a request with no caller was refused: %v", err)
	}
}
