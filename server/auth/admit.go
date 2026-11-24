package auth

import (
	"net/http"
	"strings"
	"time"
)

// "key per session"
// "I WANT A SESSION TO BE ABLE TO NAME ITSELF"
// "SINCE THE EXISTENCE OF I NAME, I WANTED EXACTLY THIS"
//
// A session makes a key where it starts and keeps it there. A key the person
// already said yes to (device.go) admits it: the node holds a grant for the
// session's DID at the admitting key's level and namespaces, ending when the
// admitting key's grant ends, and no passkey is asked for a session (ADR-059).
// The session then signs as itself, and names itself with `i name`.
//
// "A signature proves the key, and nothing reusable leaves the node."

// admitPath is where a key that was said yes to admits a session's key.
const admitPath = "/auth/admit"

// handleAdmit answers POST /auth/admit: did, the session's did:key, and label,
// what the session is called; signed by the admitting key.
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
	// A signature is presented once (signedGrant spends its id): the gate's
	// admission is read when it made one, and the request is presented here
	// only when nothing did.
	admitting, ok := AdmissionFrom(r.Context())
	if !ok {
		admitting, ok = h.admissionOf(h.presented(r))
	}
	if !ok {
		h.writeError(w, http.StatusUnauthorized, "the signing key is not admitted")
		return
	}
	held, asThePerson, status, why := h.admitter(admitting)
	if why != "" {
		h.writeError(w, status, why)
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
	// The session's grant ends with the admitting key's.
	until := NeverEnds()
	if held.ExpiresAt != nil && *held.ExpiresAt != "" {
		ends, err := time.Parse(time.RFC3339Nano, *held.ExpiresAt)
		if err != nil {
			h.writeError(w, http.StatusInternalServerError, "the admitting key's expiry does not read: "+err.Error())
			return
		}
		until = &ends
	}
	// A session's key is its own client, and stands as the person who said
	// yes; a key the person admitted stands at its own level.
	client := did
	if !asThePerson {
		client = ""
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
		ClientDID:           client,
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

// admitter is who admits a key, and the grant it is held on: a key the
// person said yes to, signing as itself, or the person, carried by this
// node's primary from where they approved it (laye_admit.go). Whether the
// key admitted stands as the person, as a session's key does.
//
// "CAN WE PLEASE STOP EXPECTING AN AGENT TO REQUEST IT OWN PERMISSIONS AND ALSO BE PERFECT?"
func (h *Handler) admitter(admitting Admission) (TokenInfo, bool, int, string) {
	// The person, carried: ROOT here, by the word of this node's primary. The
	// key is theirs at SUPER, as the primary carried them (LevelSuper in
	// handleLayeAdmit), until it is revoked.
	if admitting.Grant == nil && admitting.TokenDID != "" && admitting.TokenDID != admitting.ClientDID &&
		h.signsForItsPeople(admitting.TokenDID) {
		if !admitting.IsRoot() {
			return TokenInfo{}, false, http.StatusForbidden, admitting.Identity + " is not ROOT here, and admitting a key is ROOT's"
		}
		return TokenInfo{
			DID:                 admitting.TokenDID,
			MintedBy:            admitting.Identity,
			MintedByUser:        admitting.UserID,
			MintedByDisplayName: admitting.DisplayName,
			Level:               LevelSuper,
			Namespaces:          admitting.Namespaces,
		}, false, 0, ""
	}
	// The same key `i name` names: its own client, said yes to at the passkey.
	// A token issued through another client, or a person's session, admits none.
	if admitting.Grant != nil || admitting.TokenDID == "" || admitting.TokenDID != admitting.ClientDID {
		return TokenInfo{}, false, http.StatusForbidden, "only a key the person said yes to admits a session's key"
	}
	held, live := h.askingToken(admitting.TokenDID)
	if !live {
		return TokenInfo{}, false, http.StatusUnauthorized, "this node holds no live grant for " + admitting.TokenDID
	}
	return held, true, 0, ""
}
