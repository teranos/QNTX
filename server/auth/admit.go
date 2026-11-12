package auth

import (
	"net/http"
	"strings"
	"time"
)

// "「[Opus 5.5] key per session」"
// "「[Opus 5.5] I WANT A SESSION TO BE ABLE TO NAME ITSELF」"
// "「[Opus 5.5] SINCE THE EXISTENCE OF I NAME, I WANTED EXACTLY THIS」"
//
// 「[Opus 5.5] A session makes a key where it starts and keeps it there. A key the person」
// 「[Opus 5.5] already said yes to (device.go) admits it: the node holds a grant for the」
// 「[Opus 5.5] session's DID at the admitting key's level and namespaces, ending when the」
// 「[Opus 5.5] admitting key's grant ends, and no passkey is asked for a session (ADR-059).」
// 「[Opus 5.5] The session then signs as itself, and names itself with `i name`.」
//
// "「[Sonnet 5.5] A signature proves the key, and nothing reusable leaves the node.」"

// 「[Opus 5.5] admitPath is where a key that was said yes to admits a session's key.」
const admitPath = "/auth/admit"

// 「[Opus 5.5] handleAdmit answers POST /auth/admit: did, the session's did:key, and label,」
// 「[Opus 5.5] what the session is called; signed by the admitting key.」
func (h *Handler) handleAdmit(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if err := r.ParseForm(); err != nil {
		h.writeError(w, http.StatusBadRequest, "the form did not read: "+err.Error())
		return
	}
	if _, signed := signedAssertion(r); !signed {
		h.writeError(w, http.StatusUnauthorized, "a key admits a session's key by signing as itself")
		return
	}
	// 「[Fable 5.1] A signature is presented once (signedGrant spends its id): the gate's」
	// 「[Fable 5.1] admission is read when it made one, and the request is presented here」
	// 「[Fable 5.1] only when nothing did.」
	admitting, ok := AdmissionFrom(r.Context())
	if !ok {
		admitting, ok = h.admissionOf(h.presented(r))
	}
	if !ok {
		h.writeError(w, http.StatusUnauthorized, "the signing key is not admitted")
		return
	}
	// 「[Opus 5.5] The same key `i name` names: its own client, said yes to at the passkey.」
	// 「[Opus 5.5] A token issued through another client, or a person's session, admits none.」
	if admitting.Grant != nil || admitting.TokenDID == "" || admitting.TokenDID != admitting.ClientDID {
		h.writeError(w, http.StatusForbidden, "only a key the person said yes to admits a session's key")
		return
	}
	held, live := h.askingToken(admitting.TokenDID)
	if !live {
		h.writeError(w, http.StatusUnauthorized, "this node holds no live grant for "+admitting.TokenDID)
		return
	}
	did := strings.TrimSpace(r.PostForm.Get("did"))
	if _, err := DecodeUserDID(did); err != nil {
		h.writeError(w, http.StatusBadRequest, "did is no did:key: "+err.Error())
		return
	}
	if did == held.DID {
		h.writeError(w, http.StatusBadRequest, "a key does not admit itself")
		return
	}
	if _, already := h.tokenOf(did); already {
		h.writeError(w, http.StatusConflict, did+" is already held")
		return
	}
	label := strings.TrimSpace(r.PostForm.Get("label"))
	if label == "" {
		h.writeError(w, http.StatusBadRequest, "label is what the session is called, and is required")
		return
	}
	// 「[Fable 5.1] The session's grant ends with the admitting key's.」
	until := NeverEnds()
	if held.ExpiresAt != nil && *held.ExpiresAt != "" {
		ends, err := time.Parse(time.RFC3339Nano, *held.ExpiresAt)
		if err != nil {
			h.writeError(w, http.StatusInternalServerError, "the admitting key's expiry does not read: "+err.Error())
			return
		}
		until = &ends
	}
	id, err := h.tokens.Issue(IssuedToken{
		Hash:                signedOnly(did),
		DID:                 did,
		Label:               label,
		MintedBy:            held.MintedBy,
		MintedByUser:        held.MintedByUser,
		MintedByDisplayName: held.MintedByDisplayName,
		Level:               held.Level,
		Namespaces:          held.Namespaces,
		ExpiresAt:           until,
		ClientDID:           did,
	})
	if err != nil {
		h.attest(PredicateUnanswered, held.MintedBy, map[string]any{
			"asked": "token store", "doing": "issue", "label": label, "error": err.Error(),
		})
		h.writeError(w, http.StatusInternalServerError, "the session's key was not written: "+err.Error())
		return
	}
	h.attest(PredicateMinted, held.MintedBy, map[string]any{
		"token": id, "label": label, "level": string(held.Level), "did": did, "admitted_by": held.DID,
	})
	h.logger.Infow("A session's key was admitted", "by", held.DID, "label", label, "did", did, "grant", id)
	h.writeJSON(w, http.StatusOK, map[string]any{
		"did":        did,
		"token_type": strings.TrimSpace(signedScheme),
		"expires_in": int(time.Until(*until) / time.Second),
	})
}
