//go:build cgo && rustetcd

package server

import "github.com/teranos/QNTX/server/namespaces"

// "names and collections and just smaller things, messages say's status"
//
// etcdNamedSubsystem in a build with ats-etcd: etcd holds names, what is
// collected and a message's say and status, kept where each is kept
// (names_at_laye_etcd.go, collected_keys_etcd.go). A namespace's attestations
// are not put there.
type etcdNamedSubsystem struct{}

func (etcdNamedSubsystem) Name() string { return "etcd" }

func (etcdNamedSubsystem) Start(*namespaces.Universe) error { return nil }
