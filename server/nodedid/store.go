package nodedid

import (
	"crypto/ed25519"
	"database/sql"

	"github.com/teranos/errors"
)

type store struct {
	db *sql.DB
}

// Load returns the stored identity, or nil when the node has none yet.
func (s *store) Load() (*Identity, error) {
	var privKey, pubKey []byte
	var did string
	err := s.db.QueryRow("SELECT private_key, public_key, did FROM node_identity WHERE id = 'self'").
		Scan(&privKey, &pubKey, &did)
	if err == sql.ErrNoRows {
		return nil, nil
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
