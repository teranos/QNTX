package server

import (
	"github.com/teranos/QNTX/ats/signing"
	"github.com/teranos/QNTX/ats/storage"
	"github.com/teranos/errors"
	"github.com/teranos/QNTX/server/nodedid"
)

type nodeDIDSubsystem struct{}

func (nodeDIDSubsystem) Name() string { return "node-did" }

func (nodeDIDSubsystem) Init(s *QNTXServer) error {
	nodeDIDHandler, err := nodedid.New(s.db, s.logger)
	if err != nil {
		return errors.Wrap(err, "failed to initialize node DID")
	}
	s.nodeDID = nodeDIDHandler

	// Set global signer so all attestations are signed with the node's DID key
	storage.SetDefaultSigner(signing.NewSigner(nodeDIDHandler.PrivateKey, nodeDIDHandler.DID))

	return nil
}
