package server

import (
	"database/sql"

	"github.com/teranos/QNTX/ats/signing"
	"github.com/teranos/QNTX/ats/storage"
	appcfg "github.com/teranos/QNTX/internal/config"
	"github.com/teranos/QNTX/server/nodedid"
	"github.com/teranos/errors"
	"go.uber.org/zap"
)

type nodeDIDSubsystem struct{}

func (nodeDIDSubsystem) Name() string { return "node-did" }

// openNodeDID loads the node identity from whichever store the backend owns.
// A backend with its own store never falls through to the database one — that
// is how a parquet deployment avoids keeping its signing key in the scratch.
func openNodeDID(cfg *appcfg.Config, db *sql.DB, logger *zap.SugaredLogger) (*nodedid.Handler, error) {
	store, err := newIdentityStore(cfg)
	if err != nil {
		return nil, err
	}
	if store != nil {
		return nodedid.NewWithStore(store, logger)
	}
	return nodedid.New(db, logger)
}

func (nodeDIDSubsystem) Init(s *QNTXServer) error {
	nodeDIDHandler, err := openNodeDID(s.deps.cfg, s.db, s.logger)
	if err != nil {
		return errors.Wrap(err, "failed to initialize node DID")
	}
	s.nodeDID = nodeDIDHandler

	// Set global signer so all attestations are signed with the node's DID key
	storage.SetDefaultSigner(signing.NewSigner(nodeDIDHandler.PrivateKey, nodeDIDHandler.DID))

	return nil
}
