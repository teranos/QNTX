package auth

import (
	"database/sql"
	"encoding/hex"

	"github.com/go-webauthn/webauthn/webauthn"
	"github.com/teranos/QNTX/errors"
	"go.uber.org/zap"
)

type credentialStore struct {
	db     *sql.DB
	logger *zap.SugaredLogger
}

func newCredentialStore(db *sql.DB, logger *zap.SugaredLogger) *credentialStore {
	return &credentialStore{db: db, logger: logger}
}

func (s *credentialStore) save(cred webauthn.Credential) error {
	id := hex.EncodeToString(cred.ID)
	_, err := s.db.Exec(
		`INSERT INTO webauthn_credentials (id, credential_id, public_key, attestation_type, aaguid, sign_count, backup_eligible, backup_state)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		id, cred.ID, cred.PublicKey, cred.AttestationType, cred.Authenticator.AAGUID, cred.Authenticator.SignCount,
		cred.Flags.BackupEligible, cred.Flags.BackupState,
	)
	if err != nil {
		return errors.Wrapf(err, "failed to save credential %s", id)
	}
	return nil
}

func (s *credentialStore) getAll() ([]webauthn.Credential, error) {
	rows, err := s.db.Query(
		`SELECT credential_id, public_key, attestation_type, aaguid, sign_count, backup_eligible, backup_state FROM webauthn_credentials`,
	)
	if err != nil {
		return nil, errors.Wrap(err, "failed to query webauthn credentials")
	}
	defer rows.Close()

	var creds []webauthn.Credential
	for rows.Next() {
		var (
			credID          []byte
			publicKey       []byte
			attestationType string
			aaguid          []byte
			signCount       uint32
			backupEligible  bool
			backupState     bool
		)
		if err := rows.Scan(&credID, &publicKey, &attestationType, &aaguid, &signCount, &backupEligible, &backupState); err != nil {
			return nil, errors.Wrap(err, "failed to scan webauthn credential row")
		}
		creds = append(creds, webauthn.Credential{
			ID:              credID,
			PublicKey:       publicKey,
			AttestationType: attestationType,
			Flags: webauthn.CredentialFlags{
				BackupEligible: backupEligible,
				BackupState:    backupState,
			},
			Authenticator: webauthn.Authenticator{
				AAGUID:    aaguid,
				SignCount: signCount,
			},
		})
	}
	return creds, rows.Err()
}

func (s *credentialStore) updateSignCount(credID []byte, newCount uint32) error {
	id := hex.EncodeToString(credID)
	_, err := s.db.Exec(
		`UPDATE webauthn_credentials SET sign_count = ? WHERE id = ?`,
		newCount, id,
	)
	if err != nil {
		return errors.Wrapf(err, "failed to update sign count for credential %s", id)
	}
	return nil
}

func (s *credentialStore) exists() (bool, error) {
	var count int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM webauthn_credentials`).Scan(&count)
	if err != nil {
		return false, errors.Wrap(err, "failed to count webauthn credentials")
	}
	return count > 0, nil
}
