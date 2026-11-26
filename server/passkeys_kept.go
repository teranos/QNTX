package server

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"
	"time"

	"github.com/teranos/QNTX/ats"
	"github.com/teranos/QNTX/ats/identity"
	"github.com/teranos/QNTX/ats/types"
	"github.com/teranos/QNTX/server/auth"
	errors "github.com/teranos/sacred-error"
)

// "QNTX can be the thing that holds stuff durably"
//
// laye keeps a person's passkeys in etcd, under /passkeys/, so any laye signs
// them in while this node is down. The node keeps each record laye wrote, as
// an attestation in system: whatever etcd loses, the node still holds what
// laye kept and when. A record laye writes again (a sign-in moves its counter,
// a device joins it) is one more attestation, so the node holds its history.

// passkeysPrefix is where laye keeps passkeys (laye-front passkeys.rs PASSKEYS).
const passkeysPrefix = "/passkeys/"

// passkeyKept is what the node attests of one record laye kept.
const passkeyKept = "laye:passkey"

// passkeysLooked is how often the node reads what laye keeps.
const passkeysLooked = 30 * time.Second

// passkeysHeld is what laye keeps under /passkeys/, by credential id.
type passkeysHeld interface {
	Held() (map[string][]byte, error)
}

// passkeySubject names a passkey by its credential id, as laye keys it.
func passkeySubject(id string) string { return "passkey:" + id }

// passkeyRecordContext names one record of it by what it held.
func passkeyRecordContext(value []byte) string {
	sum := sha256.Sum256(value)
	return "kept:" + hex.EncodeToString(sum[:])
}

// keepPasskeys attests each record laye holds that the node does not yet, as
// actor. How many it attested now.
func keepPasskeys(held map[string][]byte, store ats.AttestationStore, actor string, now time.Time) (int, error) {
	ids := make([]string, 0, len(held))
	for id := range held {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	kept := 0
	for _, id := range ids {
		value := held[id]
		subject, context := passkeySubject(id), passkeyRecordContext(value)
		already, err := store.GetAttestations(ats.AttestationFilter{
			TimeStart: types.Unbounded(), TimeEnd: types.Unbounded(),
			Subjects: []string{subject}, Predicates: []string{passkeyKept}, Contexts: []string{context},
			Limit: 1,
		})
		if err != nil {
			return kept, errors.Wrapf(err, "whether the node holds laye's record of passkey %s was not read", id)
		}
		if len(already) > 0 {
			continue
		}
		var record map[string]any
		if err := json.Unmarshal(value, &record); err != nil {
			return kept, errors.Wrapf(err, "laye's record of passkey %s is not a JSON object", id)
		}
		record["etcd_key"] = passkeysPrefix + id
		asid, err := identity.GenerateASUIDWithRetry("AS", subject, passkeyKept, auth.NamespaceSystem, store.AttestationExists)
		if err != nil {
			return kept, errors.Wrapf(err, "the record of passkey %s was not named", id)
		}
		if err := store.CreateAttestation(&types.As{
			ID: asid, Subjects: []string{subject}, Predicates: []string{passkeyKept},
			Contexts: []string{context, auth.NamespaceSystem}, Actors: []string{actor},
			Timestamp: now, CreatedAt: now, Source: "laye", Attributes: record,
		}); err != nil {
			return kept, errors.Wrapf(err, "laye's record of passkey %s was not kept", id)
		}
		kept++
	}
	return kept, nil
}

// keepLayesPasskeys keeps what laye holds of passkeys until the node stops.
// A node whose am.toml names no etcd has nothing to read, and does nothing.
func (s *QNTXServer) keepLayesPasskeys() {
	held := s.layesPasskeys()
	if held == nil {
		return
	}
	tick := time.NewTicker(passkeysLooked)
	defer tick.Stop()
	for {
		select {
		case <-s.ctx.Done():
			return
		case <-tick.C:
			if err := s.keepPasskeysOnce(held); err != nil {
				s.logger.Errorw("What laye keeps of passkeys was not kept by the node", "error", err)
			}
		}
	}
}

func (s *QNTXServer) keepPasskeysOnce(held passkeysHeld) error {
	records, err := held.Held()
	if err != nil {
		return errors.Wrap(err, "what laye keeps of passkeys was not read")
	}
	if s.nodeDID == nil {
		return errors.New("the node holds no key, so there is nobody to keep laye's passkeys as")
	}
	store, err := s.held.WriteWhatTheNodeKnowsOfItself()
	if err != nil {
		return errors.Wrap(err, "no store to keep laye's passkeys in")
	}
	kept, err := keepPasskeys(records, store, s.nodeDID.DID, time.Now())
	if kept > 0 {
		s.logger.Infow("The node keeps what laye holds of passkeys", "kept", kept, "held", len(records))
	}
	return err
}
