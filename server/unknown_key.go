package server

import (
	"context"
	"os"
)

// "CAN WE PLEASE STOP EXPECTING AN AGENT TO REQUEST IT OWN PERMISSIONS AND ALSO BE PERFECT?"
// "yeah, it jus tfeels like this should happen for sure"
//
// A key that signed as itself for this node, that the node holds nothing
// for, is put in the approval pack of each of the node's ROOT, at laye: admit
// it. Approved there, the primary carries the person to this node's
// /auth/admit.

// keyUnknownPath is where laye takes a key to admit (laye-front KEY_UNKNOWN).
const keyUnknownPath = "/laye/pack/admit"

// keyUnknown is what laye is handed of one key.
type keyUnknown struct {
	NodeDID string `json:"node_did"`
	// Node is what the node is called, its host's name, so the item says it and not its key.
	Node   string `json:"node"`
	DID    string `json:"did"`
	Label  string `json:"label"`
	Person string `json:"person"`
}

// tellLayeUnknownKey hands laye a key this node holds nothing for, once for
// each ROOT. It never holds up the refusal, and what could not be handed is
// said in the log.
func (s *QNTXServer) tellLayeUnknownKey(did, label string) {
	if s.authHandler == nil {
		return
	}
	own, err := s.authHandler.OwnDID()
	if err != nil {
		s.logger.Errorw("A key this node holds nothing for was not put before its ROOT", "did", did, "error", err)
		return
	}
	host, err := os.Hostname()
	if err != nil {
		host = ""
	}
	for _, person := range s.authHandler.Roots() {
		unknown := keyUnknown{NodeDID: own, Node: host, DID: did, Label: label, Person: person}
		go func() {
			if err := s.handLaye(context.Background(), keyUnknownPath, unknown); err != nil {
				s.logger.Errorw("A key this node holds nothing for was not put before its ROOT",
					"did", did, "label", label, "person", person, "error", err)
				return
			}
			s.logger.Infow("A key this node holds nothing for waits on its ROOT at laye",
				"did", did, "label", label, "person", person)
		}()
	}
}
