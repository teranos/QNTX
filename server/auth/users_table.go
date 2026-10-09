package auth

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"sort"
	"sync"

	"github.com/teranos/QNTX/internal/sqlclose"
	"github.com/teranos/errors"
)

// A User lives in the operational db (ADR-037). The gate reads this table;
// nothing on a request path reads S3 for a User.

// "we need to keep users in mem"
// "why not just implement the pending operational db work"

// UserTable is the UserStore over the operational db, with the record behind
// it: on parquet the one object per User under system/users/, which is where
// a User is rebuilt from after host loss and nowhere the node reads from.
//
// The table is held in memory as well, rows as they are written: the gate asks
// for Users on every request, and the node is the only writer of this table,
// so a read never needs the operational db behind it.
type UserTable struct {
	db     *sql.DB
	record UserStore

	writing sync.Mutex // One write at a time, so the table and memory agree on the last.
	mu      sync.RWMutex
	rows    map[string][]byte // id → the row's record, as the table holds it
}

// Reconciled is what opening the table found: how many Users the record
// held that the table lacked and took in, and how many the table held that
// differed from the record and were written back.
type Reconciled struct {
	Held        int
	TakenIn     int
	WrittenBack int
}

// OpenUserTable reads the record once. A User the record holds and the table
// lacks is taken in; a User the table holds is the truth, and is written back
// when the record differs. No timestamp says which is newer, so the record
// itself is the watermark. A nil record is a deployment that keeps none.
func OpenUserTable(db *sql.DB, record UserStore) (*UserTable, Reconciled, error) {
	t := &UserTable{db: db, record: record}
	var done Reconciled
	if err := t.load(); err != nil {
		return nil, done, err
	}
	if record == nil {
		return t, done, nil
	}
	recorded, err := record.List()
	if err != nil {
		return nil, done, errors.Wrap(err, "the User record did not answer, so the table cannot be reconciled")
	}
	held, err := t.List()
	if err != nil {
		return nil, done, err
	}
	done.Held = len(held)
	local := map[string]User{}
	for _, u := range held {
		local[u.ID] = u
	}
	for _, theirs := range recorded {
		mine, have := local[theirs.ID]
		if !have {
			if err := t.write(theirs); err != nil {
				return nil, done, err
			}
			done.TakenIn++
			continue
		}
		if sameUser(mine, theirs) {
			continue
		}
		if err := record.Put(mine); err != nil {
			return nil, done, errors.Wrapf(err, "User %s in the table was not written back to the record", mine.ID)
		}
		done.WrittenBack++
	}
	return t, done, nil
}

// List returns every User in the table, by id.
func (t *UserTable) List() ([]User, error) {
	t.mu.RLock()
	defer t.mu.RUnlock()
	ids := make([]string, 0, len(t.rows))
	for id := range t.rows {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	users := make([]User, 0, len(ids))
	for _, id := range ids {
		var u User
		if err := json.Unmarshal(t.rows[id], &u); err != nil {
			return nil, errors.Wrapf(err, "User %s in the users table is not a User", id)
		}
		users = append(users, whole(u))
	}
	return users, nil
}

// load reads the table into memory, once, when it opens.
func (t *UserTable) load() (err error) {
	rows, err := t.db.Query(`SELECT id, record FROM users`)
	if err != nil {
		return errors.Wrap(err, "failed to read the users table")
	}
	defer func() { err = sqlclose.With(err, rows.Close(), "rows for users") }()

	held := map[string][]byte{}
	for rows.Next() {
		var id, body string
		if err := rows.Scan(&id, &body); err != nil {
			return errors.Wrap(err, "failed to scan a User row")
		}
		held[id] = []byte(body)
	}
	if err := rows.Err(); err != nil {
		return errors.Wrap(err, "the users table stopped answering")
	}
	t.mu.Lock()
	t.rows = held
	t.mu.Unlock()
	return nil
}

// ByRoute resolves an auth.root_identities entry to the User it reaches.
func (t *UserTable) ByRoute(route string) (User, bool, error) {
	held, err := t.List()
	if err != nil {
		return User{}, false, err
	}
	u, found := reachedBy(held, route)
	return u, found, nil
}

// reachedBy is the User a route reaches. One account registered at several
// public doors is a User at each; a User above that, ROOT or one ROOT made, is
// the person, and is the answer when there is one.
func reachedBy(held []User, route string) (User, bool) {
	var public *User
	for i := range held {
		if !held[i].Reaches(route) {
			continue
		}
		if held[i].Level != LevelPublicRegistration {
			return held[i], true
		}
		if public == nil {
			public = &held[i]
		}
	}
	if public == nil {
		return User{}, false
	}
	return *public, true
}

// Put writes the table first, then the record. A record that would not take
// the write is said, not swallowed: the User is in the table and the next
// open writes it back.
func (t *UserTable) Put(u User) error {
	if err := t.write(u); err != nil {
		return err
	}
	if t.record == nil {
		return nil
	}
	if err := t.record.Put(u); err != nil {
		return errors.Wrapf(err, "User %s is in the operational db but its record was not written", u.ID)
	}
	return nil
}

func (t *UserTable) write(u User) error {
	if u.ID == "" {
		return errors.New("a User with no id cannot be written")
	}
	body, err := json.Marshal(whole(u))
	if err != nil {
		return errors.Wrapf(err, "failed to serialize User %s", u.ID)
	}
	t.writing.Lock()
	defer t.writing.Unlock()
	_, err = t.db.Exec(
		`INSERT INTO users (id, record) VALUES (?, ?) ON CONFLICT(id) DO UPDATE SET record = excluded.record`,
		u.ID, string(body))
	if err != nil {
		return errors.Wrapf(err, "failed to write User %s to the operational db", u.ID)
	}
	t.mu.Lock()
	t.rows[u.ID] = body
	t.mu.Unlock()
	return nil
}

// whole is a User with every list present. A nil slice marshals as null and
// an empty one as [], and the two are the same User.
func whole(u User) User {
	if u.EmailAddresses == nil {
		u.EmailAddresses = []string{}
	}
	if u.PhoneNumbers == nil {
		u.PhoneNumbers = []string{}
	}
	if u.Keys == nil {
		u.Keys = []UserKey{}
	}
	if u.Accounts == nil {
		u.Accounts = []UserAccount{}
	}
	return u
}

// sameUser is whether two records are one User, byte for byte once whole.
func sameUser(a, b User) bool {
	ab, err := json.Marshal(whole(a))
	if err != nil {
		return false
	}
	bb, err := json.Marshal(whole(b))
	if err != nil {
		return false
	}
	return bytes.Equal(ab, bb)
}
