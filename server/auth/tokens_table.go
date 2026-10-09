package auth

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"sort"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/teranos/QNTX/internal/sqlclose"
	"github.com/teranos/errors"
)

// An access token lives in the operational db (ADR-037). Before this the gate
// read a HashMap the open had filled from S3, and recording a use rewrote the
// token's object — one PUT per authenticated request, which measured as three
// quarters of everything this node asked its location for.

// TokenTable is the TokenStore over the operational db, with the record behind
// it: on parquet the one object per token under system/access_tokens/, which
// is where a token is rebuilt from after host loss and nowhere a request
// reads from.
//
// The table is held in memory as well, rows as they are written, and last-used
// is kept there until Flush writes it: the gate reads a token and records its
// use on every request, and the node is the only writer of this table, so a
// request never needs the operational db behind it.
type TokenTable struct {
	db     *sql.DB
	record TokenRecordStore

	writing sync.Mutex // One write at a time, so the table and memory agree on the last.
	mu      sync.RWMutex
	rows    map[string]tokenRow // hash → the row, as the table holds it
	used    map[string]int64    // hash → when it was last presented, not yet in the table
}

// tokenRow is one row of access_tokens.
type tokenRow struct {
	id        string
	record    []byte
	createdAt int64
}

// TookIn is what opening the table found: how many tokens it already held,
// how many the record held that it lacked and took in, and how many it held
// that the record disagreed with and were written back.
type TookIn struct {
	Held        int
	TakenIn     int
	WrittenBack int
}

// OpenTokenTable reads the record once. A token the record holds and the table
// lacks is taken in; a token the table holds is the truth, written back when
// the record disagrees. A nil record is a deployment that keeps none.
func OpenTokenTable(db *sql.DB, record TokenRecordStore) (*TokenTable, TookIn, error) {
	t := &TokenTable{db: db, record: record, used: map[string]int64{}}
	var done TookIn
	if err := t.load(); err != nil {
		return nil, done, err
	}
	if record == nil {
		return t, done, nil
	}
	recorded, err := record.Records()
	if err != nil {
		return nil, done, errors.Wrap(err, "the access token record did not answer, so the table cannot be reconciled")
	}
	held, err := t.records()
	if err != nil {
		return nil, done, err
	}
	done.Held = len(held)
	local := map[string]TokenRecord{}
	for _, one := range held {
		local[one.Hash] = one
	}
	for _, theirs := range recorded {
		mine, have := local[theirs.Hash]
		if !have {
			if err := t.write(theirs); err != nil {
				return nil, done, err
			}
			done.TakenIn++
			continue
		}
		if sameToken(mine, theirs) {
			continue
		}
		if err := record.PutRecord(mine); err != nil {
			return nil, done, errors.Wrapf(err, "access token %s in the table was not written back to the record", mine.ID)
		}
		done.WrittenBack++
	}
	// A token whose record write failed when it was issued is in the table
	// alone, and the record lacking it disagrees as much as one that differs.
	theirs := map[string]bool{}
	for _, one := range recorded {
		theirs[one.Hash] = true
	}
	for _, mine := range held {
		if theirs[mine.Hash] {
			continue
		}
		if err := record.PutRecord(mine); err != nil {
			return nil, done, errors.Wrapf(err, "access token %s in the table, which the record lacks, was not written to it", mine.ID)
		}
		done.WrittenBack++
	}
	return t, done, nil
}

// Lookup resolves a presented hash. The table answers; the record is not
// reached. A token this node does not hold is not live, so a hash nobody
// minted fails closed.
func (t *TokenTable) Lookup(hash string) (Grant, bool) {
	held, found, err := t.byHash(hash)
	if err != nil || !found || !held.Usable(time.Now().UTC().UnixMilli()) {
		return Grant{}, false
	}
	return held.Grant(), true
}

// LookupSpent answers for a revoked or expired token too, because a refresh
// token presented twice has to be told from one nobody ever issued.
func (t *TokenTable) LookupSpent(hash string) (Grant, bool, bool) {
	held, found, err := t.byHash(hash)
	if err != nil || !found {
		return Grant{}, false, false
	}
	return held.Grant(), held.Usable(time.Now().UTC().UnixMilli()), true
}

// Create issues a token. The raw value is returned once and never stored —
// only the hash it reduces to reaches the table or the record.
func (t *TokenTable) Create(spec NewToken) (string, string, error) {
	raw, did, err := MintToken()
	if err != nil {
		return "", "", err
	}
	id, err := t.Issue(IssuedToken{
		Hash:                sha256Hex(raw),
		DID:                 did,
		Label:               spec.Label,
		MintedBy:            spec.MintedBy,
		MintedByUser:        spec.MintedByUser,
		MintedByDisplayName: spec.MintedByDisplayName,
		Level:               spec.Level,
		Namespaces:          spec.Namespaces,
		ExpiresAt:           spec.ExpiresAt,
	})
	if err != nil {
		return "", "", err
	}
	if spec.ReturnAddress != "" {
		if err := t.change(id, func(held *TokenRecord) { held.ReturnAddress = spec.ReturnAddress }); err != nil {
			return "", "", err
		}
	}
	return raw, id, nil
}

// Issue writes down a token the flow already minted. Table first, then the
// record: a record that would not take the write is said rather than
// swallowed, and the next open writes it back.
func (t *TokenTable) Issue(spec IssuedToken) (string, error) {
	held := TokenRecord{
		ID:                  uuid.NewString(),
		Hash:                spec.Hash,
		Label:               spec.Label,
		DID:                 spec.DID,
		MintedBy:            spec.MintedBy,
		MintedByUser:        spec.MintedByUser,
		MintedByDisplayName: spec.MintedByDisplayName,
		Level:               string(spec.Level),
		Namespaces:          spec.Namespaces,
		ClientDID:           spec.ClientDID,
		RequestID:           spec.RequestID,
		// The lines say what a token may touch (ADR-034). The two lists stay on
		// the record so what was written before still reads, and carry nothing.
		ScopeRead:  []string{},
		ScopeWrite: []string{},
		CreatedAt:  time.Now().UTC().UnixMilli(),
	}
	if spec.ExpiresAt == nil {
		return "", errors.Newf("token %q says nothing of when it ends; one that does not end says never", spec.Label)
	}
	ms := spec.ExpiresAt.UTC().UnixMilli()
	held.ExpiresAt = &ms
	if err := t.put(held); err != nil {
		return "", err
	}
	return held.ID, nil
}

// List returns every token without its hash — what GET /auth/tokens answers
// with. Revoked and expired ones included, so a revoked token is visibly
// revoked rather than absent.
func (t *TokenTable) List() ([]TokenInfo, error) {
	held, err := t.records()
	if err != nil {
		return nil, err
	}
	out := make([]TokenInfo, 0, len(held))
	for _, one := range held {
		out = append(out, one.Info())
	}
	return out, nil
}

// Revoke marks a token revoked. Revoking twice keeps the first timestamp — the
// moment it stopped working is when it stopped working.
func (t *TokenTable) Revoke(id string) error {
	now := time.Now().UTC().UnixMilli()
	return t.change(id, func(held *TokenRecord) {
		if held.RevokedAt == nil {
			held.RevokedAt = &now
		}
	})
}

// Enable lifts a revocation. It does not extend an expiry.
func (t *TokenTable) Enable(id string) error {
	return t.change(id, func(held *TokenRecord) { held.RevokedAt = nil })
}

// SetNamespaces changes where a token acts, in the table and then the record.
func (t *TokenTable) SetNamespaces(id string, namespaces []string) error {
	return t.change(id, func(held *TokenRecord) { held.Namespaces = namespaces })
}

// Touch records that this token was presented now.
//
// The table and nothing else. Last-used is a watch — it is what revoking a
// token and seeing whether anything still presents it is read from — and a
// watch is not a record. Losing the host loses it, which is the right thing to
// lose for the one write that runs on every authenticated request.
//
// Memory, until Flush: a request records its use without writing, and what is
// lost with the process is at most one Flush of last-used.
func (t *TokenTable) Touch(hash string) error {
	now := time.Now().UTC().UnixMilli()
	t.mu.Lock()
	defer t.mu.Unlock()
	if _, held := t.rows[hash]; held {
		t.used[hash] = now
	}
	return nil
}

// Flush writes every last-used Touch has kept since the one before. A token
// whose write fails keeps its last-used for the next.
func (t *TokenTable) Flush() error {
	t.mu.RLock()
	hashes := make([]string, 0, len(t.used))
	for hash := range t.used {
		hashes = append(hashes, hash)
	}
	t.mu.RUnlock()

	var first error
	failed := 0
	for _, hash := range hashes {
		held, found, err := t.byHash(hash)
		if err == nil && found {
			err = t.write(held)
		}
		if err != nil {
			failed++
			if first == nil {
				first = err
			}
		}
	}
	if first != nil {
		return errors.Wrapf(first, "%d of %d access tokens' last-used were not written", failed, len(hashes))
	}
	return nil
}

// change reads a token by id, applies what the caller wants changed, and
// writes the table and then the record. A token nobody holds is not an error:
// the caller asked for a state that is already the case.
func (t *TokenTable) change(id string, apply func(*TokenRecord)) error {
	held, err := t.records()
	if err != nil {
		return err
	}
	for _, one := range held {
		if one.ID != id {
			continue
		}
		apply(&one)
		return t.put(one)
	}
	return nil
}

// put writes the table, then the record. Both are handed the token whole: the
// record takes a list only as a list.
func (t *TokenTable) put(held TokenRecord) error {
	held = wholeToken(held)
	if err := t.write(held); err != nil {
		return err
	}
	if t.record == nil {
		return nil
	}
	if err := t.record.PutRecord(held); err != nil {
		return errors.Wrapf(err, "access token %s is in the operational db but its record was not written", held.ID)
	}
	return nil
}

func (t *TokenTable) write(held TokenRecord) error {
	if held.Hash == "" {
		return errors.New("an access token with no hash cannot be written")
	}
	body, err := json.Marshal(wholeToken(held))
	if err != nil {
		return errors.Wrapf(err, "failed to serialize access token %s", held.ID)
	}
	t.writing.Lock()
	defer t.writing.Unlock()
	_, err = t.db.Exec(
		`INSERT INTO access_tokens (hash, id, record) VALUES (?, ?, ?)
		 ON CONFLICT(hash) DO UPDATE SET id = excluded.id, record = excluded.record`,
		held.Hash, held.ID, string(body))
	if err != nil {
		return errors.Wrapf(err, "failed to write access token %s to the operational db", held.ID)
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	row := t.rows[held.Hash] // created_at is the table's, and a write leaves it
	row.id, row.record = held.ID, body
	t.rows[held.Hash] = row
	if written := held.LastUsedAt; written != nil && t.used[held.Hash] <= *written {
		delete(t.used, held.Hash)
	}
	return nil
}

// load reads the table into memory, once, when it opens.
func (t *TokenTable) load() (err error) {
	rows, err := t.db.Query(`SELECT hash, id, record, created_at FROM access_tokens`)
	if err != nil {
		return errors.Wrap(err, "failed to read the access_tokens table")
	}
	defer func() { err = sqlclose.With(err, rows.Close(), "rows for access_tokens") }()

	held := map[string]tokenRow{}
	for rows.Next() {
		var hash, id, body string
		var createdAt int64
		if err := rows.Scan(&hash, &id, &body, &createdAt); err != nil {
			return errors.Wrap(err, "failed to scan an access token row")
		}
		held[hash] = tokenRow{id: id, record: []byte(body), createdAt: createdAt}
	}
	if err := rows.Err(); err != nil {
		return errors.Wrap(err, "the access_tokens table stopped answering")
	}
	t.mu.Lock()
	t.rows = held
	t.mu.Unlock()
	return nil
}

// byHash is the one read a gated request makes.
func (t *TokenTable) byHash(hash string) (TokenRecord, bool, error) {
	t.mu.RLock()
	defer t.mu.RUnlock()
	row, found := t.rows[hash]
	if !found {
		return TokenRecord{}, false, nil
	}
	held, err := t.token(hash, row)
	return held, err == nil, err
}

// records is every token in the table, oldest first so runs are comparable.
func (t *TokenTable) records() ([]TokenRecord, error) {
	t.mu.RLock()
	defer t.mu.RUnlock()
	hashes := make([]string, 0, len(t.rows))
	for hash := range t.rows {
		hashes = append(hashes, hash)
	}
	sort.Slice(hashes, func(i, j int) bool {
		a, b := t.rows[hashes[i]], t.rows[hashes[j]]
		if a.createdAt != b.createdAt {
			return a.createdAt < b.createdAt
		}
		return a.id < b.id
	})
	held := make([]TokenRecord, 0, len(hashes))
	for _, hash := range hashes {
		one, err := t.token(hash, t.rows[hash])
		if err != nil {
			return nil, err
		}
		held = append(held, one)
	}
	return held, nil
}

// token is a row as a token, with the last-used Touch has kept and not yet
// written. The caller holds mu.
func (t *TokenTable) token(hash string, row tokenRow) (TokenRecord, error) {
	var held TokenRecord
	if err := json.Unmarshal(row.record, &held); err != nil {
		return TokenRecord{}, errors.Wrapf(err, "access token %s in the access_tokens table is not a token", row.id)
	}
	if used, kept := t.used[hash]; kept && (held.LastUsedAt == nil || *held.LastUsedAt < used) {
		held.LastUsedAt = &used
	}
	return wholeToken(held), nil
}

// wholeToken is a token with every list present. A nil slice marshals as null
// and an empty one as [], and the two are the same token.
func wholeToken(t TokenRecord) TokenRecord {
	if t.Namespaces == nil {
		t.Namespaces = []string{}
	}
	if t.ScopeRead == nil {
		t.ScopeRead = []string{}
	}
	if t.ScopeWrite == nil {
		t.ScopeWrite = []string{}
	}
	return t
}

// sameToken is whether the table and the record hold one token.
//
// Last-used is left out on purpose. Touch writes it to the table alone, so
// every token that has ever been presented differs from its object by that
// field — and a comparison that counted it would write every used token back
// at every open, spending on S3 exactly what keeping last-used local saved.
func sameToken(a, b TokenRecord) bool {
	a.LastUsedAt, b.LastUsedAt = nil, nil
	ab, err := json.Marshal(wholeToken(a))
	if err != nil {
		return false
	}
	bb, err := json.Marshal(wholeToken(b))
	if err != nil {
		return false
	}
	return bytes.Equal(ab, bb)
}
