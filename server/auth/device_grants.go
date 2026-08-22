package auth

import (
	"database/sql"
	"net/http"
	"time"

	"github.com/teranos/errors"
	"go.uber.org/zap"
)

// A device admitted by a device (ADR-032). The QR is scanned once and the
// passkey is the whole of every login after that, so what bounds the delegation
// is this record rather than the ceremony that made it.

// VERIFY: no grant has ever been written by a real scan. See connect.go for
// what testing it costs — a nuked ROOT User and a re-initialized deployment.

// deviceGrant is what a scanned QR left behind: which account this device
// speaks for, what it may do, and until when.
type deviceGrant struct {
	OwnerDID   string
	AdmittedAs string
	Level      Level
	GrantedBy  string
	ExpiresAt  time.Time
}

// Live reports whether this grant still admits anything.
func (g deviceGrant) Live() bool {
	return time.Now().Before(g.ExpiresAt)
}

type deviceGrants struct {
	db     *sql.DB
	logger *zap.SugaredLogger
}

func newDeviceGrants(db *sql.DB, logger *zap.SugaredLogger) *deviceGrants {
	return &deviceGrants{db: db, logger: logger}
}

// record writes a grant, replacing any the same device already had. Scanning a
// second QR is a renewal, not a second device.
func (s *deviceGrants) record(g deviceGrant) error {
	_, err := s.db.Exec(
		`INSERT INTO device_grants (owner_did, admitted_as, level, granted_by, created_at, expires_at)
		 VALUES (?, ?, ?, ?, ?, ?)
		 ON CONFLICT(owner_did) DO UPDATE SET
		     admitted_as = excluded.admitted_as,
		     level = excluded.level,
		     granted_by = excluded.granted_by,
		     created_at = excluded.created_at,
		     expires_at = excluded.expires_at`,
		g.OwnerDID, g.AdmittedAs, string(g.Level), g.GrantedBy,
		time.Now().UTC().UnixMilli(), g.ExpiresAt.UTC().UnixMilli(),
	)
	if err != nil {
		return errors.Wrapf(err, "failed to record the grant admitting %s as %s", g.OwnerDID, g.AdmittedAs)
	}
	return nil
}

// of returns the grant covering this device. False is a device nobody granted,
// which is not a failure to read — most devices enrolled the ordinary way.
func (s *deviceGrants) of(ownerDID string) (deviceGrant, bool, error) {
	var (
		g       deviceGrant
		level   string
		expires int64
	)
	err := s.db.QueryRow(
		`SELECT owner_did, admitted_as, level, granted_by, expires_at FROM device_grants WHERE owner_did = ?`,
		ownerDID,
	).Scan(&g.OwnerDID, &g.AdmittedAs, &level, &g.GrantedBy, &expires)
	if err == sql.ErrNoRows {
		return deviceGrant{}, false, nil
	}
	if err != nil {
		return deviceGrant{}, false, errors.Wrapf(err, "failed to read the grant for device %s", ownerDID)
	}

	g.Level = Level(level)
	g.ExpiresAt = time.UnixMilli(expires).UTC()
	return g, true, nil
}

// anyLive reports whether this node holds a grant that has not run out. It is
// what lets a login ceremony begin for a device that has no laye admission.
func (s *deviceGrants) anyLive() (bool, error) {
	var count int
	err := s.db.QueryRow(
		`SELECT COUNT(*) FROM device_grants WHERE expires_at > ?`, time.Now().UTC().UnixMilli(),
	).Scan(&count)
	if err != nil {
		return false, errors.Wrap(err, "failed to count the live device grants")
	}
	return count > 0, nil
}

// mayAssert reports whether a passkey ceremony may begin at all. A laye
// signature is one way; a device this node granted is the other, which is what
// makes the passkey the whole of a login for a connected device.
func (h *Handler) mayAssert(r *http.Request) bool {
	if _, ok := h.pendingLogins.peek(heldPending(r)); ok {
		return true
	}
	if h.grants == nil {
		return false
	}

	live, err := h.grants.anyLive()
	if err != nil {
		h.logger.Errorw("could not read the device grants, so no device stands on one", "error", err)
		return false
	}
	return live
}

// granted reports whether a live grant covers this credential. Beginning a
// ceremony asks whether any device was granted; finishing one asks about this
// device, and this is the question that decides a login.
func (h *Handler) granted(credID []byte) bool {
	if h.grants == nil {
		return false
	}

	ownerDID, err := h.creds.ownerOf(credID)
	if err != nil || ownerDID == "" {
		h.logger.Warnw("could not read the owner of a credential asserting a grant",
			"error", err, "owner_did", quoteIdentity(ownerDID))
		return false
	}

	grant, found, err := h.grants.of(ownerDID)
	if err != nil {
		h.logger.Errorw("could not read the grant for a device", "owner_did", ownerDID, "error", err)
		return false
	}
	if !found {
		return false
	}
	if !grant.Live() {
		h.logger.Infow("device grant has run out", "owner_did", ownerDID, "expired_at", grant.ExpiresAt)
		return false
	}
	return true
}

// forget drops a grant. A device being forgotten takes its delegation with it,
// or the next scan-free login would still be admitted by a row nobody can see.
func (s *deviceGrants) forget(ownerDID string) error {
	_, err := s.db.Exec(`DELETE FROM device_grants WHERE owner_did = ?`, ownerDID)
	if err != nil {
		return errors.Wrapf(err, "failed to delete the grant for device %s", ownerDID)
	}
	return nil
}
