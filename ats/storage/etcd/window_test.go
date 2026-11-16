package etcd

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/url"
	"testing"
	"time"

	"github.com/teranos/QNTX/ats/types"
	clientv3 "go.etcd.io/etcd/client/v3"
	"go.etcd.io/etcd/server/v3/embed"
)

// freeURL is an http URL on loopback no one is listening on yet.
func freeURL(t *testing.T) url.URL {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := l.Addr().String()
	l.Close()
	return url.URL{Scheme: "http", Host: addr}
}

// member runs a single etcd member in this process for the length of a test.
func member(t *testing.T) *clientv3.Client {
	t.Helper()
	cfg := embed.NewConfig()
	cfg.Dir = t.TempDir()
	cfg.LogLevel = "error"
	client, peer := freeURL(t), freeURL(t)
	cfg.ListenClientUrls, cfg.AdvertiseClientUrls = []url.URL{client}, []url.URL{client}
	cfg.ListenPeerUrls, cfg.AdvertisePeerUrls = []url.URL{peer}, []url.URL{peer}
	cfg.InitialCluster = fmt.Sprintf("%s=%s", cfg.Name, peer.String())

	e, err := embed.StartEtcd(cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(e.Close)
	select {
	case <-e.Server.ReadyNotify():
	case <-time.After(30 * time.Second):
		t.Fatal("etcd did not become ready in 30s")
	}

	c, err := clientv3.New(clientv3.Config{Endpoints: []string{client.String()}, DialTimeout: 5 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Close() })
	return c
}

func attestation() *types.As {
	return &types.As{
		ID:         "AS-window",
		Subjects:   []string{"mycelia"},
		Predicates: []string{"reads"},
		Contexts:   []string{"etcd"},
		Actors:     []string{"did:key:z6Mk"},
		Timestamp:  time.Date(2026, 11, 16, 12, 0, 0, 0, time.UTC),
		Source:     "test",
	}
}

func TestAnAttestationLandsInTheWindow(t *testing.T) {
	c := member(t)
	var failed []error
	w := New(c, "ground", func(err error) { failed = append(failed, err) })

	as := attestation()
	w.OnAttestationCreated(as)
	if len(failed) > 0 {
		t.Fatalf("the write failed: %v", failed)
	}

	ctx := context.Background()
	got, err := c.Get(ctx, "/attestations/ground/AS-window")
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Kvs) != 1 {
		t.Fatalf("want one key at /attestations/ground/AS-window, have %d", len(got.Kvs))
	}

	var back types.As
	if err := json.Unmarshal(got.Kvs[0].Value, &back); err != nil {
		t.Fatal(err)
	}
	if back.ID != as.ID || back.Subjects[0] != "mycelia" || !back.Timestamp.Equal(as.Timestamp) {
		t.Fatalf("the value is not the attestation: %+v", back)
	}

	lease := clientv3.LeaseID(got.Kvs[0].Lease)
	if lease == clientv3.NoLease {
		t.Fatal("the key carries no lease, so it never leaves the window")
	}
	ttl, err := c.TimeToLive(ctx, lease)
	if err != nil {
		t.Fatal(err)
	}
	if ttl.GrantedTTL != int64(Span/time.Second) {
		t.Fatalf("lease granted %ds, the window is %ds", ttl.GrantedTTL, int64(Span/time.Second))
	}
}

func TestAWriteThatFailsIsSaid(t *testing.T) {
	c := member(t)
	c.Close()
	var failed []error
	w := New(c, "ground", func(err error) { failed = append(failed, err) })

	w.OnAttestationCreated(attestation())
	if len(failed) != 1 {
		t.Fatalf("want the failed write said once, said %d times", len(failed))
	}
}
