package auth

import (
	"database/sql"
	"encoding/hex"
	"github.com/teranos/QNTX/internal/sqlclose"

	"github.com/go-webauthn/webauthn/webauthn"
	"github.com/teranos/errors"
	"go.uber.org/zap"
)

type credentialStore struct {
	db     *sql.DB
	logger *zap.SugaredLogger
}

func newCredentialStore(db *sql.DB, logger *zap.SugaredLogger) *credentialStore {
	return &credentialStore{db: db, logger: logger}
}

// save enrols a credential at the node's own door, which is the one onto
// default. Reach for saveAt when the door is known.
func (s *credentialStore) save(cred webauthn.Credential, ownerDID, admittedAs string) error {
	return s.saveAt(cred, ownerDID, admittedAs, NamespaceDefault)
}

// saveAt enrols a credential at one door. The door is where the key was made,
// and a key made at one door is refused by every browser at any other.
func (s *credentialStore) saveAt(cred webauthn.Credential, ownerDID, admittedAs, door string) error {
	id := hex.EncodeToString(cred.ID)
	_, err := s.db.Exec(
		`INSERT INTO webauthn_credentials (id, credential_id, public_key, attestation_type, aaguid, sign_count, backup_eligible, backup_state, owner_did, admitted_as, door)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		id, cred.ID, cred.PublicKey, cred.AttestationType, cred.Authenticator.AAGUID, cred.Authenticator.SignCount,
		cred.Flags.BackupEligible, cred.Flags.BackupState, ownerDID, admittedAs, door,
	)
	if err != nil {
		return errors.Wrapf(err, "failed to save credential %s for %s at door %q (admitted as %q)", id, ownerDID, door, admittedAs)
	}
	return nil
}

// doorOf returns the door a credential was made at, or empty when the node
// holds no such key. Empty is an answer, not a read failure.
func (s *credentialStore) doorOf(credID []byte) (string, error) {
	id := hex.EncodeToString(credID)
	var door string
	err := s.db.QueryRow(
		`SELECT door FROM webauthn_credentials WHERE id = ?`, id,
	).Scan(&door)
	if err == sql.ErrNoRows {
		return "", nil
	}
	if err != nil {
		return "", errors.Wrapf(err, "failed to read the door credential %s was made at", id)
	}
	return door, nil
}

// admittedAs returns the identity whose session enrolled this credential — the
// account a fingerprint stands in for. Empty means the credential was enrolled
// without one, so it can speak for no account.
func (s *credentialStore) admittedAs(credID []byte) (string, error) {
	id := hex.EncodeToString(credID)
	var identity string
	err := s.db.QueryRow(
		`SELECT admitted_as FROM webauthn_credentials WHERE id = ?`, id,
	).Scan(&identity)
	if err == sql.ErrNoRows {
		return "", nil
	}
	if err != nil {
		return "", errors.Wrapf(err, "failed to read the admitting identity of credential %s", id)
	}
	return identity, nil
}

// owner returns the DID this deployment's credentials belong to, or empty when
// none has been established. Registration admits one owner today, so the first
// non-empty answer is the answer.
func (s *credentialStore) owner() (string, error) {
	var owner string
	err := s.db.QueryRow(
		`SELECT owner_did FROM webauthn_credentials WHERE owner_did != '' ORDER BY created_at LIMIT 1`,
	).Scan(&owner)
	if err == sql.ErrNoRows {
		return "", nil
	}
	if err != nil {
		return "", errors.Wrap(err, "failed to read the registered owner DID")
	}
	return owner, nil
}

// ownerOf returns who holds this credential, or empty when it is unregistered.
// Empty is an answer, not a read failure — an unknown key has no owner.
func (s *credentialStore) ownerOf(credID []byte) (string, error) {
	id := hex.EncodeToString(credID)
	var owner string
	err := s.db.QueryRow(
		`SELECT owner_did FROM webauthn_credentials WHERE id = ?`, id,
	).Scan(&owner)
	if err == sql.ErrNoRows {
		return "", nil
	}
	if err != nil {
		return "", errors.Wrapf(err, "failed to read the owner of credential %s", id)
	}
	return owner, nil
}

// forget deletes a credential. A device nobody can assert is a row that admits
// nobody, so the row goes rather than being marked.
func (s *credentialStore) forget(credID []byte) error {
	id := hex.EncodeToString(credID)
	result, err := s.db.Exec(`DELETE FROM webauthn_credentials WHERE id = ?`, id)
	if err != nil {
		return errors.Wrapf(err, "failed to delete credential %s", id)
	}
	dropped, err := result.RowsAffected()
	if err != nil {
		return errors.Wrapf(err, "failed to read how many rows credential %s deleted", id)
	}
	if dropped == 0 {
		return errors.Newf("credential %s was not there to delete", id)
	}
	return nil
}

// credentialColumns is what a webauthn.Credential is built from. The two
// readers below differ only in what they ask for, so the shape is named once.
const credentialColumns = `credential_id, public_key, attestation_type, aaguid, sign_count, backup_eligible, backup_state`

// doorCredentials returns the keys made at one door. A ceremony runs against
// one relying party, so it is offered the keys that relying party made and no
// others — the rest are keys the browser would refuse, and offering them says
// an account exists somewhere the caller was not asking about.
func (s *credentialStore) doorCredentials(door string) (_ []webauthn.Credential, err error) {
	rows, err := s.db.Query(`SELECT `+credentialColumns+` FROM webauthn_credentials WHERE door = ?`, door)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to query the credentials made at door %q", door)
	}
	defer func() { err = sqlclose.With(err, rows.Close(), "rows for doorCredentials") }()

	return scanCredentials(rows)
}

// doorCredentialsFor returns the keys enrolled at one door under any of these
// routes: every way auth.root_identities reaches one User (ADR-031). A passkey
// is the second half of an admission, so a login is offered that person's
// devices and nobody else's. Offering every key at the door let a browser
// holding several assert one enrolled by somebody else's ceremony, and be
// refused for it.
//
// "I am still a root identity. And I am the owner. It's not like there is another
// owner. I'm the only one."
func (s *credentialStore) doorCredentialsFor(door string, routes []string) (_ []webauthn.Credential, err error) {
	if len(routes) == 0 {
		return nil, nil
	}
	rows, err := s.db.Query(`SELECT `+credentialColumns+` FROM webauthn_credentials WHERE door = ? AND admitted_as IN (`+placeholders(len(routes))+`)`,
		append([]any{door}, asAny(routes)...)...)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to query the credentials %v made at door %q", routes, door)
	}
	defer func() { err = sqlclose.With(err, rows.Close(), "rows for doorCredentialsFor") }()

	return scanCredentials(rows)
}

// existsForAny reports whether any of these routes has a device: whether the
// person they reach has stood on one anywhere.
func (s *credentialStore) existsForAny(routes []string) (bool, error) {
	if len(routes) == 0 {
		return false, nil
	}
	var count int
	err := s.db.QueryRow(
		`SELECT COUNT(*) FROM webauthn_credentials WHERE admitted_as IN (`+placeholders(len(routes))+`)`, asAny(routes)...,
	).Scan(&count)
	if err != nil {
		return false, errors.Wrapf(err, "failed to count webauthn credentials for %v", routes)
	}
	return count > 0, nil
}

// placeholders is n question marks for an IN clause.
func placeholders(n int) string {
	marks := make([]byte, 0, 2*n)
	for i := 0; i < n; i++ {
		if i > 0 {
			marks = append(marks, ',')
		}
		marks = append(marks, '?')
	}
	return string(marks)
}

func asAny(routes []string) []any {
	args := make([]any, len(routes))
	for i, r := range routes {
		args[i] = r
	}
	return args
}

func scanCredentials(rows *sql.Rows) ([]webauthn.Credential, error) {
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

// existsFor reports whether this identity already has a device. It decides
// whether a login is asked to enrol one or to use the one it has.
func (s *credentialStore) existsFor(identity string) (bool, error) {
	var count int
	err := s.db.QueryRow(
		`SELECT COUNT(*) FROM webauthn_credentials WHERE admitted_as = ?`, identity,
	).Scan(&count)
	if err != nil {
		return false, errors.Wrapf(err, "failed to count webauthn credentials for %s", identity)
	}
	return count > 0, nil
}
