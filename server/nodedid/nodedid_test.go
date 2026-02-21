package nodedid

import (
	"crypto/ed25519"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	qntxtest "github.com/teranos/QNTX/internal/testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func testLogger() *zap.SugaredLogger {
	return zap.NewNop().Sugar()
}

func TestGenerateAndPersist(t *testing.T) {
	db := qntxtest.CreateTestDB(t)

	// First call generates a new identity
	h1, err := New(db, testLogger())
	require.NoError(t, err)
	assert.True(t, strings.HasPrefix(h1.DID, "did:key:z"))
	assert.Len(t, h1.PublicKey, ed25519.PublicKeySize)
	assert.Len(t, h1.PrivateKey, ed25519.PrivateKeySize)

	// Second call loads the same identity
	h2, err := New(db, testLogger())
	require.NoError(t, err)
	assert.Equal(t, h1.DID, h2.DID)
	assert.Equal(t, h1.PublicKey, h2.PublicKey)
	assert.Equal(t, h1.PrivateKey, h2.PrivateKey)
}

func TestDIDKeyEncoding(t *testing.T) {
	pub, _, err := ed25519.GenerateKey(nil)
	require.NoError(t, err)

	did := encodeDIDKey(pub)
	assert.True(t, strings.HasPrefix(did, "did:key:z"))
	// z + base58btc(2 byte prefix + 32 byte key) should be reasonably long
	assert.Greater(t, len(did), 40)
}

func TestDIDDocument(t *testing.T) {
	db := qntxtest.CreateTestDB(t)
	h, err := New(db, testLogger())
	require.NoError(t, err)

	var doc didDocument
	err = json.Unmarshal(h.didDocument, &doc)
	require.NoError(t, err)

	assert.Equal(t, "https://www.w3.org/ns/did/v1", doc.Context)
	assert.Equal(t, h.DID, doc.ID)
	require.Len(t, doc.VerificationMethod, 1)
	assert.Equal(t, "Ed25519VerificationKey2020", doc.VerificationMethod[0].Type)
	assert.Equal(t, h.DID, doc.VerificationMethod[0].Controller)
	require.Len(t, doc.Authentication, 1)
	assert.Equal(t, doc.VerificationMethod[0].ID, doc.Authentication[0])
}

func TestHandleDIDDocument(t *testing.T) {
	db := qntxtest.CreateTestDB(t)
	h, err := New(db, testLogger())
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodGet, "/.well-known/did.json", nil)
	rec := httptest.NewRecorder()

	h.HandleDIDDocument(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "application/did+json", rec.Header().Get("Content-Type"))

	var doc didDocument
	err = json.Unmarshal(rec.Body.Bytes(), &doc)
	require.NoError(t, err)
	assert.Equal(t, h.DID, doc.ID)
}

func TestSignAndVerify(t *testing.T) {
	db := qntxtest.CreateTestDB(t)
	h, err := New(db, testLogger())
	require.NoError(t, err)

	msg := []byte("hello from this node")
	sig := ed25519.Sign(h.PrivateKey, msg)
	assert.True(t, ed25519.Verify(h.PublicKey, msg, sig))
}
