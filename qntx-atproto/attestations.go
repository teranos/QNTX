package qntxatproto

import (
	"context"
	"strings"

	"github.com/teranos/QNTX/ats/types"
)

// collectionFromURI extracts the collection from an AT URI.
// at://did:plc:xxx/app.bsky.feed.post/abc123 → app.bsky.feed.post
func collectionFromURI(atURI string) string {
	// Strip "at://" prefix, then split: [did, collection, rkey]
	trimmed := strings.TrimPrefix(atURI, "at://")
	parts := strings.SplitN(trimmed, "/", 3)
	if len(parts) >= 2 {
		return parts[1]
	}
	return ""
}

// attestSessionStatus records a PDS session event.
func (p *Plugin) attestSessionStatus(status, pdsHost, identity, errMsg string) {
	store := p.Services().ATSStore()
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
		Subjects:      []string{identity},
		Predicates:    []string{status},
		Source:        p.Metadata().Name,
		SourceVersion: p.Metadata().Version,
		Attributes:    attrs,
	}
	if _, err := store.GenerateAndCreateAttestation(context.Background(), cmd); err != nil {
		logger := p.Services().Logger("atproto")
		logger.Debugw("Failed to create session attestation", "status", status, "error", err)
	}
}

// attestPost records a post creation.
func (p *Plugin) attestPost(did, uri, cid, text string) {
	store := p.Services().ATSStore()
	if store == nil {
		return
	}

	cmd := &types.AsCommand{
		Actors:        []string{did},
		Subjects:      []string{uri},
		Predicates:    []string{"posted"},
		Contexts:      []string{collectionFromURI(uri)},
		Source:        p.Metadata().Name,
		SourceVersion: p.Metadata().Version,
		Attributes: map[string]interface{}{
			"cid":  cid,
			"text": text,
		},
	}
	if _, err := store.GenerateAndCreateAttestation(context.Background(), cmd); err != nil {
		logger := p.Services().Logger("atproto")
		logger.Debugw("Failed to create post attestation", "uri", uri, "error", err)
	}
}

// attestFollow records a follow action.
func (p *Plugin) attestFollow(actorDID, subjectDID, uri string) {
	store := p.Services().ATSStore()
	if store == nil {
		return
	}

	cmd := &types.AsCommand{
		Actors:        []string{actorDID},
		Subjects:      []string{subjectDID},
		Predicates:    []string{"following"},
		Contexts:      []string{collectionFromURI(uri)},
		Source:        p.Metadata().Name,
		SourceVersion: p.Metadata().Version,
		Attributes: map[string]interface{}{
			"uri": uri,
		},
	}
	if _, err := store.GenerateAndCreateAttestation(context.Background(), cmd); err != nil {
		logger := p.Services().Logger("atproto")
		logger.Debugw("Failed to create follow attestation", "subject", subjectDID, "error", err)
	}
}

// attestLike records a like action.
func (p *Plugin) attestLike(actorDID, subjectURI, uri string) {
	store := p.Services().ATSStore()
	if store == nil {
		return
	}

	cmd := &types.AsCommand{
		Actors:        []string{actorDID},
		Subjects:      []string{subjectURI},
		Predicates:    []string{"liked"},
		Contexts:      []string{collectionFromURI(uri)},
		Source:        p.Metadata().Name,
		SourceVersion: p.Metadata().Version,
		Attributes: map[string]interface{}{
			"uri": uri,
		},
	}
	if _, err := store.GenerateAndCreateAttestation(context.Background(), cmd); err != nil {
		logger := p.Services().Logger("atproto")
		logger.Debugw("Failed to create like attestation", "subject_uri", subjectURI, "error", err)
	}
}

// attestResolve records a handle → DID resolution.
func (p *Plugin) attestResolve(handle, did string) {
	store := p.Services().ATSStore()
	if store == nil {
		return
	}

	cmd := &types.AsCommand{
		Subjects:      []string{handle},
		Predicates:    []string{"resolved-to"},
		Source:        p.Metadata().Name,
		SourceVersion: p.Metadata().Version,
		Attributes: map[string]interface{}{
			"did": did,
		},
	}
	if _, err := store.GenerateAndCreateAttestation(context.Background(), cmd); err != nil {
		logger := p.Services().Logger("atproto")
		logger.Debugw("Failed to create resolve attestation", "handle", handle, "error", err)
	}
}

// attestTimelinePost records a post appearing in the authenticated user's timeline.
func (p *Plugin) attestTimelinePost(ctx context.Context, uri, authorDID, authorHandle, text, cid string) error {
	store := p.Services().ATSStore()
	if store == nil {
		return nil // Store not available
	}

	attrs := map[string]interface{}{
		"author_handle": authorHandle,
		"cid":           cid,
	}
	if text != "" {
		attrs["text"] = text
	}

	cmd := &types.AsCommand{
		Actors:        []string{authorDID},
		Subjects:      []string{uri},
		Predicates:    []string{"appeared-in-timeline"},
		Contexts:      []string{collectionFromURI(uri)},
		Source:        p.Metadata().Name,
		SourceVersion: p.Metadata().Version,
		Attributes:    attrs,
	}
	if _, err := store.GenerateAndCreateAttestation(ctx, cmd); err != nil {
		return err
	}
	return nil
}
