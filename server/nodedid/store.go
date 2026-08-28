package nodedid

import (
	"crypto/ed25519"
	"database/sql"

	"github.com/teranos/errors"
)

type store struct {
	db *sql.DB
}

// ErrNoIdentity is a node that has never minted a key — the first-boot
// answer, named, so it can never be confused with a store that failed.
var ErrNoIdentity = errors.New("no node identity stored")

// Load returns the stored identity, or ErrNoIdentity when the node has none yet.
func (s *store) Load() (*Identity, error) {
	var privKey, pubKey []byte
	var did string
	err := s.db.QueryRow("SELECT private_key, public_key, did FROM node_identity WHERE id = 'self'").
		Scan(&privKey, &pubKey, &did)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNoIdentity
	}
	if err != nil {
		return nil, errors.Wrap(err, "failed to load node identity")
	}
	return &Identity{
		PrivateKey: ed25519.PrivateKey(privKey),
		PublicKey:  ed25519.PublicKey(pubKey),
		DID:        did,
	}, nil
}

func (s *store) Save(id *Identity) error {
	_, err := s.db.Exec(
		"INSERT INTO node_identity (id, private_key, public_key, did) VALUES ('self', ?, ?, ?)",
		[]byte(id.PrivateKey), []byte(id.PublicKey), id.DID,
	)
	if err != nil {
		return errors.Wrap(err, "failed to save node identity")
	}
	return nil
}
