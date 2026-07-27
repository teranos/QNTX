package main

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

// TestUnimplementedContracts_NoBackendHoldsIt is the NO/NO case, and the
// reason this pass exists. A contract nothing satisfies is a thing QNTX needs
// and nowhere keeps — invisible to anything that reads the backends, because
// there is nothing in either backend to read.
func TestUnimplementedContracts_NoBackendHoldsIt(t *testing.T) {
	root := fixture(t, map[string]string{
		"auth/tokens.go": `package auth

type TokenStore interface {
	Lookup(hash string) bool
	Create(label string) (string, error)
	Revoke(id string) error
}

type Handler struct {
	tokens TokenStore
}`,
	})

	names := contracts(t, root)
	if !slices.Contains(names, "tokens") {
		t.Errorf("tokens missing from %v: the contract exists and nothing satisfies it", names)
	}
}

// TestUnimplementedContracts_ImplementedIsNotAGap: once a type carries the
// methods, the thing is held and the contract is not work waiting.
func TestUnimplementedContracts_ImplementedIsNotAGap(t *testing.T) {
	root := fixture(t, map[string]string{
		"storage/watchers.go": `package storage

type WatcherStore interface {
	Get(id string) error
	Put(id string) error
}

type holder struct {
	store WatcherStore
}

type sqliteWatchers struct{}

func (s *sqliteWatchers) Get(id string) error { return nil }
func (s *sqliteWatchers) Put(id string) error { return nil }`,
	})

	names := contracts(t, root)
	if slices.Contains(names, "watchers") {
		t.Errorf("watchers reported in %v, but sqliteWatchers satisfies WatcherStore", names)
	}
}

// TestUnimplementedContracts_TestDoublesDoNotCount is why the scan skips
// _test.go. memTokenStore in server/auth/auth_test.go satisfies TokenStore
// completely; counting it would erase the only line that shows the work.
func TestUnimplementedContracts_TestDoublesDoNotCount(t *testing.T) {
	root := fixture(t, map[string]string{
		"auth/tokens.go": `package auth

type TokenStore interface {
	Lookup(hash string) bool
	Revoke(id string) error
}

type Handler struct {
	tokens TokenStore
}`,
		"auth/tokens_test.go": `package auth

type memTokenStore struct{}

func (m *memTokenStore) Lookup(hash string) bool { return false }
func (m *memTokenStore) Revoke(id string) error  { return nil }`,
	})

	names := contracts(t, root)
	if !slices.Contains(names, "tokens") {
		t.Errorf("tokens missing from %v: a test double is not a backend", names)
	}
}

// TestUnimplementedContracts_AbandonedInterfaceIsNotWork: ats.BoundedStore is
// declared and referenced by nothing. An interface no code depends on is dead,
// and putting it on the picture invents a thing QNTX does not persist.
func TestUnimplementedContracts_AbandonedInterfaceIsNotWork(t *testing.T) {
	root := fixture(t, map[string]string{
		"ats/store.go": `package ats

type AbandonedStore interface {
	Create(id string) error
	Delete(id string) error
}`,
	})

	names := contracts(t, root)
	if slices.Contains(names, "abandoneds") {
		t.Errorf("abandoneds reported in %v: nothing declares a field, parameter or variable of it", names)
	}
}

// TestUnimplementedContracts_SameNameDifferentPackage: ats.BoundedStore (an
// interface nobody uses) and storage.BoundedStore (a struct used everywhere)
// share a bare name. Matching on the name alone credits the interface with the
// struct's popularity and puts a line on the picture for a thing that is not
// one.
func TestUnimplementedContracts_SameNameDifferentPackage(t *testing.T) {
	root := fixture(t, map[string]string{
		"ats/store.go": `package ats

type BoundedStore interface {
	Create(id string) error
	Delete(id string) error
}`,
		"storage/bounded.go": `package storage

type BoundedStore struct{}`,
		"server/wiring.go": `package server

import "example.com/parityfixture/storage"

type Server struct {
	rich *storage.BoundedStore
}`,
	})

	names := contracts(t, root)
	if slices.Contains(names, "boundeds") {
		t.Errorf("boundeds reported in %v: the consumed BoundedStore is the struct in storage, not the interface in ats", names)
	}
}

// TestUnimplementedContracts_EmbeddedMethodsCount: a contract that embeds
// another is satisfied only by carrying both method sets.
func TestUnimplementedContracts_EmbeddedMethodsCount(t *testing.T) {
	root := fixture(t, map[string]string{
		"ats/store.go": `package ats

type AttestationStore interface {
	Create(id string) error
}

type QuotaStore interface {
	AttestationStore
	CreateWithLimits(id string) error
}

type holder struct {
	store QuotaStore
}

type partial struct{}

func (p *partial) CreateWithLimits(id string) error { return nil }`,
	})

	names := contracts(t, root)
	if !slices.Contains(names, "quotas") {
		t.Errorf("quotas missing from %v: partial lacks Create, so it does not satisfy QuotaStore", names)
	}
}

// fixture writes a throwaway module. go.mod is required: without the module
// path there is no way to tell an in-module import from a dependency, and
// qualified type names stop resolving.
func fixture(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	write(t, root, "go.mod", "module example.com/parityfixture\n\ngo 1.24\n")
	for name, body := range files {
		if dir := filepath.Dir(name); dir != "." {
			if err := os.MkdirAll(filepath.Join(root, dir), 0o755); err != nil {
				t.Fatalf("mkdir %s: %v", dir, err)
			}
		}
		write(t, root, name, body)
	}
	return root
}

func contracts(t *testing.T, root string) []string {
	t.Helper()
	names, err := UnimplementedContracts(root)
	if err != nil {
		t.Fatalf("UnimplementedContracts: %v", err)
	}
	return names
}
