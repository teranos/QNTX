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
	return askingAs("", caller)
}

// askingAs is a request naming a namespace, the way a token reaching several
// says which one this one is.
func askingAs(namespace string, caller auth.Admission) *http.Request {
	url := "/api/attestations"
	if namespace != "" {
		url += "?namespace=" + namespace
	}
	req := httptest.NewRequest(http.MethodGet, url, nil)
	return req.WithContext(auth.WithAdmission(req.Context(), caller))
}

func routeServer() *QNTXServer {
	return &QNTXServer{logger: zap.NewNop().Sugar()}
}

// A token minted for the duck pond wrote to the playground and was told it
// worked. Refusing is not the feature, but it is not a lie either.
func TestATokenOutsideAnyOpenNamespaceIsRefused(t *testing.T) {
	s := routeServer()
	_, err := s.storeFor(requestAs(auth.Admission{Level: auth.LevelToken, Namespaces: []string{"pond"}}))
	if err == nil {
		t.Fatal("a caller in an unopened namespace got a store")
	}
	if !strings.Contains(err.Error(), "pond") {
		t.Fatalf("the refusal does not name what was asked for: %v", err)
	}
}

func TestTheDefaultNamespaceIsServed(t *testing.T) {
	s := routeServer()
	// Naming it, and naming none — a session names none, which is every
	// namespace the node serves.
	for _, named := range [][]string{{auth.NamespaceDefault}, nil} {
		if _, err := s.storeFor(requestAs(auth.Admission{Namespaces: named})); err != nil {
			t.Fatalf("namespaces %v were refused: %v", named, err)
		}
	}
}

// A write lands somewhere definite or nowhere. Picking the first of several
// would put an attestation in a namespace nobody named.
func TestATokenReachingSeveralHasToSayWhichOne(t *testing.T) {
	s := routeServer()
	reach := auth.Admission{Level: auth.LevelToken, Namespaces: []string{auth.NamespaceDefault, "pond"}}

	_, err := s.storeFor(requestAs(reach))
	if err == nil {
		t.Fatal("a token reaching two namespaces was served without naming one")
	}
	for _, name := range reach.Namespaces {
		if !strings.Contains(err.Error(), name) {
			t.Errorf("the refusal does not name %s, so the caller cannot pick: %v", name, err)
		}
	}

	if _, err := s.storeFor(askingAs(auth.NamespaceDefault, reach)); err != nil {
		t.Fatalf("naming one of its own namespaces was refused: %v", err)
	}
}

// Naming one it was not minted for is the same lie as before, said explicitly.
func TestANamespaceOutsideTheTokenIsRefused(t *testing.T) {
	s := routeServer()
	reach := auth.Admission{Level: auth.LevelToken, Namespaces: []string{auth.NamespaceDefault}}

	_, err := s.storeFor(askingAs("pond", reach))
	if err == nil {
		t.Fatal("a token was served a namespace it does not reach")
	}
	if !strings.Contains(err.Error(), "pond") {
		t.Fatalf("the refusal does not name what was asked for: %v", err)
	}
}

// system is not visible below SUPER (ADR-027), and a token is below it.
func TestATokenCannotReachSystem(t *testing.T) {
	s := routeServer()
	s.systemStore = s.atsStore
	reach := auth.Admission{Level: auth.LevelToken, Namespaces: []string{auth.NamespaceSystem}}

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
