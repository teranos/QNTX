//go:build !cgo

package storage

import (
	"github.com/teranos/QNTX/ats"
	errors "github.com/teranos/sacred-error"
	"go.uber.org/zap"
)

// NewStore is unavailable without CGO — requires Rust SQLite backend.
func NewStore(dbPath string, logger *zap.SugaredLogger) (ats.AttestationStore, error) {
	return nil, errors.Newf("attestation store unavailable: CGO required (path: %s)", dbPath)
}
