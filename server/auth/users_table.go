package auth

import (
	"bytes"
	"database/sql"
	"encoding/json"

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
type UserTable struct {
	db     *sql.DB
	record UserStore
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

// List returns every User in the table.
func (t *UserTable) List() (_ []User, err error) {
	rows, err := t.db.Query(`SELECT record FROM users ORDER BY id`)
	if err != nil {
		return nil, errors.Wrap(err, "failed to read the users table")
	}
	defer func() { err = sqlclose.With(err, rows.Close(), "rows for users") }()

	users := []User{}
	for rows.Next() {
		var body string
		if err := rows.Scan(&body); err != nil {
			return nil, errors.Wrap(err, "failed to scan a User row")
		}
		var u User
		if err := json.Unmarshal([]byte(body), &u); err != nil {
			return nil, errors.Wrap(err, "a User row in the operational db is not a User")
		}
		users = append(users, whole(u))
	}
	if err := rows.Err(); err != nil {
		return nil, errors.Wrap(err, "the users table stopped answering")
	}
	return users, nil
}

// ByRoute resolves an auth.root_identities entry to the User it reaches.
func (t *UserTable) ByRoute(route string) (User, bool, error) {
	held, err := t.List()
	if err != nil {
		return User{}, false, err
	}
	for _, u := range held {
		if u.Reaches(route) {
			return u, true, nil
		}
	}
	return User{}, false, nil
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
	_, err = t.db.Exec(
		`INSERT INTO users (id, record) VALUES (?, ?) ON CONFLICT(id) DO UPDATE SET record = excluded.record`,
		u.ID, string(body))
	if err != nil {
		return errors.Wrapf(err, "failed to write User %s to the operational db", u.ID)
	}
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
