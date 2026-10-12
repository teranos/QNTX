package auth

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
	errors "github.com/teranos/sacred-error"
)

// The primary's tokens are accepted here too (nodes.primary). A bearer this
// node did not issue is asked about there, this node signing as its own
// did:key, and the answer is admitted by this node's own lines.

// issuedAnswerTTL is how long an issuer's answer about one token is kept. The
// issuer's auth routes are rate limited, so asking on every request would be refused.
const issuedAnswerTTL = 30 * time.Second

// issuerTimeout bounds one question to an issuer, which a request waits on.
const issuerTimeout = 5 * time.Second

// issuerAnswerBytes bounds what is read of an issuer's answer.
const issuerAnswerBytes = 64 << 10

// issuedAnswer is what an issuer said about one token, and until when it is kept.
type issuedAnswer struct {
	grant Grant
	live  bool
	until time.Time
}

// issuedAnswers is the issuers' answers by token hash.
type issuedAnswers struct {
	byToken sync.Map
}

func (a *issuedAnswers) held(hash string, now time.Time) (issuedAnswer, bool) {
	kept, found := a.byToken.Load(hash)
	if !found {
		return issuedAnswer{}, false
	}
	answer, isAnswer := kept.(issuedAnswer)
	if !isAnswer || now.After(answer.until) {
		a.byToken.Delete(hash)
		return issuedAnswer{}, false
	}
	return answer, true
}

func (a *issuedAnswers) keep(hash string, answer issuedAnswer, now time.Time) {
	a.byToken.Range(func(held, kept any) bool {
		if was, isAnswer := kept.(issuedAnswer); !isAnswer || now.After(was.until) {
			a.byToken.Delete(held)
		}
		return true
	})
	a.byToken.Store(hash, answer)
}

// issuedElsewhere is the grant a token one of the issuers issued stands for,
// as that issuer says, or false when none says it is live.
func (h *Handler) issuedElsewhere(raw string) (Grant, bool) {
	hash := sha256Hex(raw)
	now := time.Now()
	if answer, held := h.issued.held(hash, now); held {
		return answer.grant, answer.live
	}
	for _, issuer := range h.issuers {
		grant, live, err := h.askIssuer(issuer, raw, now)
		if err != nil {
			// Not kept: the next request asks again rather than being refused for a while on one failure.
			h.logger.Errorw("An issuer did not answer about a token, so it is not admitted",
				"issuer", issuer, "error", err)
			return Grant{}, false
		}
		if live {
			h.issued.keep(hash, issuedAnswer{grant: grant, live: true, until: now.Add(issuedAnswerTTL)}, now)
			return grant, true
		}
	}
	h.issued.keep(hash, issuedAnswer{until: now.Add(issuedAnswerTTL)}, now)
	return Grant{}, false
}

// ownDID is the did:key this node signs as.
func (h *Handler) ownDID() (string, error) {
	if len(h.nodeKey) != ed25519.PrivateKeySize {
		return "", errors.New("this node has no DID key to sign as, so it cannot ask")
	}
	public, ok := h.nodeKey.Public().(ed25519.PublicKey)
	if !ok {
		return "", errors.New("this node's DID key has no ed25519 public half")
	}
	return EncodeDIDKey(public), nil
}

// asserted is this node's did and an assertion signed as it, made out to an
// issuer's token endpoint (RFC 7523 §2.2).
func (h *Handler) asserted(issuer string, now time.Time) (did, signed string, err error) {
	did, err = h.ownDID()
	if err != nil {
		return "", "", err
	}
	jti := make([]byte, assertionIDBytes)
	drawn, err := rand.Read(jti)
	if err != nil || drawn != assertionIDBytes {
		return "", "", errors.New("no assertion id could be drawn")
	}
	signed, err = jwt.NewWithClaims(jwt.SigningMethodEdDSA, jwt.RegisteredClaims{
		Issuer:    did,
		Subject:   did,
		Audience:  jwt.ClaimStrings{issuer + tokenPath},
		ExpiresAt: jwt.NewNumericDate(now.Add(time.Minute)),
		IssuedAt:  jwt.NewNumericDate(now),
		ID:        hex.EncodeToString(jti),
	}).SignedString(h.nodeKey)
	if err != nil {
		return "", "", errors.Wrap(err, "the assertion was not signed")
	}
	return did, signed, nil
}

// askIssuer introspects one token at an issuer, signing as this node.
func (h *Handler) askIssuer(issuer, raw string, now time.Time) (Grant, bool, error) {
	did, signed, err := h.asserted(issuer, now)
	if err != nil {
		return Grant{}, false, err
	}
	form := url.Values{}
	form.Set("token", raw)
	form.Set("client_id", did)
	form.Set("client_assertion_type", clientAssertionType)
	form.Set("client_assertion", signed)
	client := http.Client{Timeout: issuerTimeout}
	resp, err := client.PostForm(issuer+introspectPath, form)
	if err != nil {
		return Grant{}, false, errors.Wrapf(err, "%s did not answer", issuer+introspectPath)
	}
	body, readErr := io.ReadAll(io.LimitReader(resp.Body, issuerAnswerBytes))
	if closeErr := resp.Body.Close(); closeErr != nil {
		return Grant{}, false, errors.Wrap(closeErr, "the issuer's answer did not close")
	}
	if readErr != nil {
		return Grant{}, false, errors.Wrap(readErr, "the issuer's answer did not read")
	}
	if resp.StatusCode != http.StatusOK {
		return Grant{}, false, errors.Newf("%s answered %d: %s", issuer+introspectPath, resp.StatusCode, body)
	}
	var said introspection
	if err := json.Unmarshal(body, &said); err != nil {
		return Grant{}, false, errors.Wrapf(err, "the issuer's answer is not an introspection: %s", body)
	}
	if !said.Active {
		return Grant{}, false, nil
	}
	if said.Issuer != issuer {
		return Grant{}, false, errors.Newf("the answer says it is from %q, and this node asked %s", said.Issuer, issuer)
	}
	// The issuer's User is the issuer's record. Here the route reaches this
	// node's own, the one a session from logging in there is given.
	here, found, err := h.users.ByRoute(said.Subject)
	if err != nil {
		return Grant{}, false, errors.Wrapf(err, "the User store did not answer for %s", said.Subject)
	}
	if !found {
		here = User{}
	}
	return Grant{
		Label:               said.Label,
		DID:                 said.DID,
		MintedBy:            said.Subject,
		MintedByUser:        here.ID,
		MintedByDisplayName: said.Name,
		Level:               Level(said.Level),
		Namespaces:          said.Namespaces,
		ClientDID:           said.ClientID,
	}, true, nil
}
