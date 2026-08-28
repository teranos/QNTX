package db

import (
	"database/sql"

	"github.com/teranos/errors"
)

// Undone rolls a transaction back and folds a rollback that itself failed into
// whatever the function was already going to return. Deferred against a named
// error return it covers both exits. A committed transaction answers ErrTxDone.
func Undone(err error, tx *sql.Tx) error {
	rbErr := tx.Rollback()
	if rbErr == nil || errors.Is(rbErr, sql.ErrTxDone) {
		return err
	}
	// A failed rollback leaves the transaction open and holding its locks, its
	// writes neither applied nor undone.
	if err == nil {
		return errors.Wrap(rbErr, "left an open transaction: the rollback failed")
	}
	// The original stays in the chain so errors.Is and errors.As still work.
	return errors.Wrapf(err, "left an open transaction, because the rollback also failed (%v)", rbErr)
}
