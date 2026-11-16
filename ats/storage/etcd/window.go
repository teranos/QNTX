// 「[Opus 5.5] Package etcd is a window onto attestations.」
package etcd

import (
	"context"
	"encoding/json"
	"time"

	"github.com/teranos/QNTX/ats/storage"
	"github.com/teranos/QNTX/ats/types"
	errors "github.com/teranos/sacred-error"
	clientv3 "go.etcd.io/etcd/client/v3"
)

// 「[Opus 5.5] Span is how long an attestation stays in the window.」
//
// "which would be around 3 days"
const Span = 72 * time.Hour

// 「[Opus 5.5] writeTimeout bounds one attestation's grant and put.」
const writeTimeout = 5 * time.Second

// 「[Opus 5.5] Window puts the attestations one namespace creates into etcd.」
//
// "isnt the right framing to think of etcd another place where attestations can go? while QNTX holds it durably?"
type Window struct {
	client    *clientv3.Client
	namespace string
	failed    func(error)
}

var _ storage.AttestationObserver = (*Window)(nil)

// 「[Opus 5.5] New is the window for one namespace. failed is handed every write etcd did not take.」
func New(client *clientv3.Client, namespace string, failed func(error)) *Window {
	return &Window{client: client, namespace: namespace, failed: failed}
}

// 「[Opus 5.5] Key is where an attestation is in the window.」
func Key(namespace, id string) string {
	return "/attestations/" + namespace + "/" + id
}

// 「[Opus 5.5] OnAttestationCreated puts the attestation under a lease of Span.」
func (w *Window) OnAttestationCreated(as *types.As) {
	if err := w.put(as); err != nil {
		w.failed(err)
	}
}

func (w *Window) put(as *types.As) error {
	body, err := json.Marshal(as)
	if err != nil {
		return errors.Wrapf(err, "attestation %s is not JSON", as.ID)
	}
	ctx, cancel := context.WithTimeout(context.Background(), writeTimeout)
	defer cancel()
	lease, err := w.client.Grant(ctx, int64(Span/time.Second))
	if err != nil {
		return errors.Wrapf(err, "etcd granted no lease for attestation %s", as.ID)
	}
	key := Key(w.namespace, as.ID)
	if _, err := w.client.Put(ctx, key, string(body), clientv3.WithLease(lease.ID)); err != nil {
		return errors.Wrapf(err, "etcd did not take %s", key)
	}
	return nil
}
