package nodedid

import (
	"crypto/ed25519"
	"database/sql"

	"github.com/teranos/QNTX/errors"
)

type store struct {
	db *sql.DB
}

type identity struct {
	privateKey ed25519.PrivateKey
	publicKey  ed25519.PublicKey
	did        string
}

func (s *store) load() (*identity, error) {
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
	return &identity{
		privateKey: ed25519.PrivateKey(privKey),
		publicKey:  ed25519.PublicKey(pubKey),
		did:        did,
	}, nil
}

func (s *store) save(id *identity) error {
	_, err := s.db.Exec(
		"INSERT INTO node_identity (id, private_key, public_key, did) VALUES ('self', ?, ?, ?)",
		[]byte(id.privateKey), []byte(id.publicKey), id.did,
	)
	if err != nil {
		return errors.Wrap(err, "failed to save node identity")
	}
	return nil
}
