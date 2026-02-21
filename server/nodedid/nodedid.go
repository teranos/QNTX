package nodedid

import (
	"crypto/ed25519"
	"crypto/rand"
	"database/sql"
	"encoding/json"

	"github.com/mr-tron/base58"
	"github.com/teranos/QNTX/errors"
	"go.uber.org/zap"
)

// Handler holds the node's DID identity and serves the DID document.
type Handler struct {
	DID         string
	PublicKey   ed25519.PublicKey
	PrivateKey  ed25519.PrivateKey
	didDocument []byte
}

// New loads the node identity from the database, or generates one on first boot.
func New(db *sql.DB, logger *zap.SugaredLogger) (*Handler, error) {
	s := &store{db: db}

	id, err := s.load()
	if err != nil {
		return nil, err
	}

	if id == nil {
		id, err = generate()
		if err != nil {
			return nil, err
		}
		if err := s.save(id); err != nil {
			return nil, err
		}
		logger.Infow("Generated node DID", "did", id.did)
	} else {
		logger.Infow("Loaded node DID", "did", id.did)
	}

	doc, err := buildDIDDocument(id.did, id.publicKey)
	if err != nil {
		return nil, errors.Wrap(err, "failed to build DID document")
	}

	// TODO(#580): Resolve peer-attested vanity name for this node DID
	return &Handler{
		DID:         id.did,
		PublicKey:   id.publicKey,
		PrivateKey:  id.privateKey,
		didDocument: doc,
	}, nil
}

func generate() (*identity, error) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, errors.Wrap(err, "failed to generate ed25519 keypair")
	}
	did := encodeDIDKey(pub)
	return &identity{
		privateKey: priv,
		publicKey:  pub,
		did:        did,
	}, nil
}

// encodeDIDKey encodes an ed25519 public key as a did:key identifier.
// Format: did:key:z + base58btc(0xed 0x01 + 32-byte pubkey)
func encodeDIDKey(pub ed25519.PublicKey) string {
	// Multicodec prefix for ed25519-pub: 0xed, 0x01
	buf := make([]byte, 2+len(pub))
	buf[0] = 0xed
	buf[1] = 0x01
	copy(buf[2:], pub)
	return "did:key:z" + base58.Encode(buf)
}

type didDocument struct {
	Context            string               `json:"@context"`
	ID                 string               `json:"id"`
	VerificationMethod []verificationMethod `json:"verificationMethod"`
	Authentication     []string             `json:"authentication"`
}

type verificationMethod struct {
	ID                 string `json:"id"`
	Type               string `json:"type"`
	Controller         string `json:"controller"`
	PublicKeyMultibase string `json:"publicKeyMultibase"`
}

func buildDIDDocument(did string, pub ed25519.PublicKey) ([]byte, error) {
	// The fragment is the multibase-encoded public key (same as the method-specific-id)
	fragment := did[len("did:key:"):]
	vmID := did + "#" + fragment

	doc := didDocument{
		Context: "https://www.w3.org/ns/did/v1",
		ID:      did,
		VerificationMethod: []verificationMethod{{
			ID:                 vmID,
			Type:               "Ed25519VerificationKey2020",
			Controller:         did,
			PublicKeyMultibase: fragment,
		}},
		Authentication: []string{vmID},
	}

	data, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return nil, errors.Wrap(err, "failed to marshal DID document")
	}
	return data, nil
}
