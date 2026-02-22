package qntxatproto

import (
	"github.com/teranos/QNTX/ats/types"
)

const atprotoContext = "atproto"

// attestSessionStatus records a PDS session event.
func (p *Plugin) attestSessionStatus(status, pdsHost, identity, errMsg string) {
	store := p.services.ATSStore()
	if store == nil {
		return
	}

	attrs := map[string]interface{}{
		"pds_host": pdsHost,
	}
	if errMsg != "" {
		attrs["error"] = errMsg
	}

	cmd := &types.AsCommand{
		Subjects:   []string{identity},
		Predicates: []string{status},
		Contexts:   []string{atprotoContext},
		Attributes: attrs,
	}
	if _, err := store.GenerateAndCreateAttestation(cmd); err != nil {
		logger := p.services.Logger("atproto")
		logger.Debugw("Failed to create session attestation", "status", status, "error", err)
	}
}

// attestPost records a post creation.
func (p *Plugin) attestPost(did, uri, cid, text string) {
	store := p.services.ATSStore()
	if store == nil {
		return
	}

	attrs := map[string]interface{}{
		"uri":  uri,
		"cid":  cid,
		"text": text,
	}

	cmd := &types.AsCommand{
		Subjects:   []string{did},
		Predicates: []string{"posted"},
		Contexts:   []string{atprotoContext},
		Attributes: attrs,
	}
	if _, err := store.GenerateAndCreateAttestation(cmd); err != nil {
		logger := p.services.Logger("atproto")
		logger.Debugw("Failed to create post attestation", "uri", uri, "error", err)
	}
}

// attestFollow records a follow action.
func (p *Plugin) attestFollow(actorDID, subjectDID, uri string) {
	store := p.services.ATSStore()
	if store == nil {
		return
	}

	cmd := &types.AsCommand{
		Subjects:   []string{actorDID},
		Predicates: []string{"following"},
		Contexts:   []string{atprotoContext},
		Attributes: map[string]interface{}{
			"subject": subjectDID,
			"uri":     uri,
		},
	}
	if _, err := store.GenerateAndCreateAttestation(cmd); err != nil {
		logger := p.services.Logger("atproto")
		logger.Debugw("Failed to create follow attestation", "subject", subjectDID, "error", err)
	}
}

// attestLike records a like action.
func (p *Plugin) attestLike(actorDID, subjectURI, uri string) {
	store := p.services.ATSStore()
	if store == nil {
		return
	}

	cmd := &types.AsCommand{
		Subjects:   []string{actorDID},
		Predicates: []string{"liked"},
		Contexts:   []string{atprotoContext},
		Attributes: map[string]interface{}{
			"subject_uri": subjectURI,
			"uri":         uri,
		},
	}
	if _, err := store.GenerateAndCreateAttestation(cmd); err != nil {
		logger := p.services.Logger("atproto")
		logger.Debugw("Failed to create like attestation", "subject_uri", subjectURI, "error", err)
	}
}

// attestResolve records a handle → DID resolution.
func (p *Plugin) attestResolve(handle, did string) {
	store := p.services.ATSStore()
	if store == nil {
		return
	}

	cmd := &types.AsCommand{
		Subjects:   []string{handle},
		Predicates: []string{"resolved-to"},
		Contexts:   []string{atprotoContext},
		Attributes: map[string]interface{}{
			"did": did,
		},
	}
	if _, err := store.GenerateAndCreateAttestation(cmd); err != nil {
		logger := p.services.Logger("atproto")
		logger.Debugw("Failed to create resolve attestation", "handle", handle, "error", err)
	}
}
