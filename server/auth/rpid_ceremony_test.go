package auth

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/fxamacker/cbor/v2"
	"github.com/stretchr/testify/require"

	qntxtest "github.com/teranos/QNTX/internal/testing"
)

// TestRegistrationCeremonyUsesConfiguredRPID drives a full WebAuthn
// registration ceremony end-to-end through the actual HTTP handlers with a
// simulated authenticator that computes rpIdHash = sha256(<configured rp_id>).
//
// This is the exact production failure for any public deployment: auth.New once
// hardcodes RPID = "localhost". A real browser on https://qntx.example.com refuses
// the ceremony because rp.id must be a registrable domain suffix of the
// origin's effective domain. Server-side, go-webauthn's FinishRegistration
// checks that the authenticatorData's rpIdHash equals sha256(configured RPID);
// if the config didn't propagate, the check fails and the ceremony aborts.
//
// This test fails today because auth.New does not accept rp_id / rp_origins
// — the call below does not compile. Once auth.New takes those parameters
// and threads them into webauthn.Config, the full ceremony completes.
func TestRegistrationCeremonyUsesConfiguredRPID(t *testing.T) {
	const (
		rpID   = "qntx.example.com"
		origin = "https://qntx.example.com"
	)

	db := qntxtest.CreateTestDB(t)
	passthroughCors := func(h http.HandlerFunc) http.HandlerFunc { return h }

	h, err := New(
		db,
		rpID,
		[]string{origin},
		8770,
		8820,
		24,
		testLogger(),
		passthroughCors,
		nil,   // token store not exercised by RPID ceremony tests
		false, // secureCookies — cookie flag not exercised here
		nil,   // rootIdentities — laye login not exercised by RPID ceremony tests
		nil,   // bindingSigners
	)
	require.NoError(t, err)

	// --- POST /auth/register/begin ---
	beginReq := httptest.NewRequest(http.MethodPost, "/auth/register/begin", nil)
	beginReq.Header.Set("Origin", origin)
	beginRec := httptest.NewRecorder()
	h.handleRegisterBegin(beginRec, beginReq)
	require.Equal(t, http.StatusOK, beginRec.Code,
		"begin failed, body=%s", beginRec.Body.String())

	var beginResp struct {
		PublicKey struct {
			Challenge string `json:"challenge"`
			RP        struct {
				ID string `json:"id"`
			} `json:"rp"`
		} `json:"publicKey"`
	}
	require.NoError(t, json.Unmarshal(beginRec.Body.Bytes(), &beginResp))
	require.Equal(t, rpID, beginResp.PublicKey.RP.ID,
		"browser will refuse ceremony if rp.id doesn't match origin's registrable domain")

	// --- Simulated authenticator: build a valid "none" attestation ---
	credentialID := []byte("qntx-owner-cred-id")

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)

	// COSE_Key EC2/ES256/P-256 (labels: 1=kty, 3=alg, -1=crv, -2=x, -3=y)
	coseKey := map[int]any{
		1:  2,
		3:  -7,
		-1: 1,
		-2: key.PublicKey.X.FillBytes(make([]byte, 32)),
		-3: key.PublicKey.Y.FillBytes(make([]byte, 32)),
	}
	coseKeyBytes, err := cbor.Marshal(coseKey)
	require.NoError(t, err)

	// authenticatorData = rpIdHash(32) | flags(1) | signCount(4)
	//                     | AAGUID(16) | credIDLen(2) | credID | credPubKey
	rpIDHash := sha256.Sum256([]byte(rpID))
	flags := byte(0x41) // UP (0x01) + AT (0x40)
	signCount := make([]byte, 4)
	aaguid := make([]byte, 16)
	credIDLen := make([]byte, 2)
	binary.BigEndian.PutUint16(credIDLen, uint16(len(credentialID)))

	authData := bytes.Join([][]byte{
		rpIDHash[:], {flags}, signCount, aaguid, credIDLen, credentialID, coseKeyBytes,
	}, nil)

	attObj := map[string]any{
		"fmt":      "none",
		"attStmt":  map[string]any{},
		"authData": authData,
	}
	attObjBytes, err := cbor.Marshal(attObj)
	require.NoError(t, err)

	clientData, err := json.Marshal(map[string]any{
		"type":      "webauthn.create",
		"challenge": beginResp.PublicKey.Challenge,
		"origin":    origin,
	})
	require.NoError(t, err)

	// The browser derives this from the PRF output and signs the challenge
	// with it. A credential cannot be stored without one, so the ceremony
	// carries it even in a test that is about the RPID.
	ownerPub, ownerPriv, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	ownerSig := ed25519.Sign(ownerPriv, []byte(beginResp.PublicKey.Challenge))

	finishBody, err := json.Marshal(map[string]any{
		"id":    base64.RawURLEncoding.EncodeToString(credentialID),
		"rawId": base64.RawURLEncoding.EncodeToString(credentialID),
		"type":  "public-key",
		"response": map[string]any{
			"attestationObject": base64.RawURLEncoding.EncodeToString(attObjBytes),
			"clientDataJSON":    base64.RawURLEncoding.EncodeToString(clientData),
		},
		"user_did":           EncodeDIDKey(ownerPub),
		"user_did_signature": base64.RawURLEncoding.EncodeToString(ownerSig),
	})
	require.NoError(t, err)

	// Enrolment records the identity whose session authorized it, so the
	// ceremony needs one to speak for.
	sessionToken, err := h.sessions.create("https://chaos.social/@onf")
	require.NoError(t, err)

	// --- POST /auth/register/finish ---
	finishReq := httptest.NewRequest(http.MethodPost, "/auth/register/finish", bytes.NewReader(finishBody))
	finishReq.Header.Set("Origin", origin)
	finishReq.Header.Set("Content-Type", "application/json")
	finishReq.AddCookie(&http.Cookie{Name: sessionCookieName, Value: sessionToken})
	finishRec := httptest.NewRecorder()
	h.handleRegisterFinish(finishRec, finishReq)

	require.Equal(t, http.StatusOK, finishRec.Code,
		"server-side WebAuthn verification rejected the ceremony; body=%s", finishRec.Body.String())
}
