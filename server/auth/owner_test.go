package auth

import (
	"testing"

	"go.uber.org/zap"

	"github.com/go-webauthn/webauthn/webauthn"
	qntxtest "github.com/teranos/QNTX/internal/testing"
)

func credentialStoreForTest(t *testing.T) *credentialStore {
	t.Helper()
	return newCredentialStore(qntxtest.CreateTestDB(t), zap.NewNop().Sugar())
}

func credential(id string) webauthn.Credential {
	return webauthn.Credential{
		ID:              []byte(id),
		PublicKey:       []byte("pubkey-" + id),
		AttestationType: "none",
	}
}
