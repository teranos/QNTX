package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/teranos/QNTX/server/auth"
)

func requestAs(caller auth.Admission) *http.Request {
	req := httptest.NewRequest(http.MethodGet, "/api/attestations", nil)
	return req.WithContext(auth.WithAdmission(req.Context(), caller))
}

// A token minted for the duck pond wrote to the playground and was told it
// worked. Refusing is not the feature, but it is not a lie either.
func TestATokenOutsideTheServedNamespaceIsRefused(t *testing.T) {
	s := &QNTXServer{}
	_, err := s.storeFor(requestAs(auth.Admission{Level: auth.LevelToken, Namespace: "pond"}))
	if err == nil {
		t.Fatal("a caller in another namespace got the default store")
	}
	// What was asked for, and nothing about what this node happens to serve.
	if !strings.Contains(err.Error(), "pond") {
		t.Fatalf("the refusal does not name what was asked for: %v", err)
	}
	if strings.Contains(err.Error(), servedNamespace) {
		t.Fatalf("the refusal names what the node serves: %v", err)
	}
}

func TestTheServedNamespaceIsServed(t *testing.T) {
	s := &QNTXServer{}
	for _, ns := range []string{auth.NamespaceDefault, ""} {
		if _, err := s.storeFor(requestAs(auth.Admission{Namespace: ns})); err != nil {
			t.Fatalf("namespace %q was refused: %v", ns, err)
		}
	}
}

// A request that never reached auth has no caller and no namespace to check.
func TestNoCallerFallsToTheServedStore(t *testing.T) {
	s := &QNTXServer{}
	if _, err := s.storeFor(httptest.NewRequest(http.MethodGet, "/api/attestations", nil)); err != nil {
		t.Fatalf("a request with no caller was refused: %v", err)
	}
}
