package server

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"strings"

	"github.com/teranos/QNTX/server/auth"
	errors "github.com/teranos/sacred-error"
)

// "SESSION CANT REACH, IT NEEDS REACH LINE(S), IT GETS IN FRONT OF MY VIA AG OR LIVE ACTIVITY AS SOMETHING TO APPROVE,"
// "EASILY AND FAST"
// "AND WITHOUT CHANCE FOR USER ERROR"
// "CAN WE PLEASE STOP EXPECTING AN AGENT TO REQUEST IT OWN PERMISSIONS AND ALSO BE PERFECT?"
//
// A request refused for reach puts the reach it lacked into the approval
// pack of the person it stands for, at laye: the line, made from the
// refusal itself, this node, and who was refused. Approved there, the line
// is written here as that person.

// reachLackedPath is where laye takes the reach a refusal lacked (laye-front REACH_LACKED).
const reachLackedPath = "/laye/pack/reach"

// reachLacked is what laye is handed of one refusal.
type reachLacked struct {
	NodeDID string `json:"node_did"`
	// Node is what the node is called, its host's name, so the item says it and not its key.
	Node     string `json:"node"`
	Path     string `json:"path"`
	To       string `json:"to"`
	AskerDID string `json:"asker_did"`
	Asker    string `json:"asker"`
	Person   string `json:"person"`
}

// tellLayeOutOfReach hands laye the reach a refused request lacked. It never
// holds up the refusal, and what could not be handed is said in the log.
func (s *QNTXServer) tellLayeOutOfReach(admitted auth.Admission, route string) {
	if admitted.Identity == "" || route == "" {
		return
	}
	asker, label := admitted.TokenDID, admitted.TokenLabel
	if admitted.Grant != nil {
		asker, label = admitted.Grant.DID, admitted.Grant.Label
	}
	if asker == "" {
		asker = admitted.Identity
	}
	if label == "" {
		label = admitted.DisplayName
	}
	if s.authHandler == nil {
		return
	}
	own, err := s.authHandler.OwnDID()
	if err != nil {
		s.logger.Errorw("The reach a refused request lacked was not put before its person", "route", route, "error", err)
		return
	}
	host, err := os.Hostname()
	if err != nil {
		host = ""
	}
	lacked := reachLacked{NodeDID: own, Node: host, Path: route, To: admitted.LevelName(), AskerDID: asker, Asker: label, Person: admitted.Identity}
	go func() {
		if err := s.handLaye(context.Background(), reachLackedPath, lacked); err != nil {
			s.logger.Errorw("The reach a refused request lacked was not put before its person",
				"route", route, "level", lacked.To, "asker", asker, "error", err)
			return
		}
		s.logger.Infow("The reach a refused request lacked waits on its person at laye",
			"route", route, "level", lacked.To, "asker", asker, "person", admitted.Identity)
	}()
}

// handLaye posts what to laye's path, signed as this node.
func (s *QNTXServer) handLaye(ctx context.Context, path string, what any) error {
	if s.deps == nil || s.deps.cfg == nil || strings.TrimSpace(s.deps.cfg.Laye.Origin) == "" {
		return errors.New("am.toml names no [laye] origin")
	}
	if s.authHandler == nil {
		return errors.New("this node has no key to sign as")
	}
	origin := strings.TrimSuffix(strings.TrimSpace(s.deps.cfg.Laye.Origin), "/")
	signed, err := s.authHandler.SignedAsNode(origin)
	if err != nil {
		return err
	}
	body, err := json.Marshal(what)
	if err != nil {
		return errors.Wrap(err, "what laye is handed did not serialize")
	}
	within, done := context.WithTimeout(ctx, handedWithin)
	defer done()
	req, err := http.NewRequestWithContext(within, http.MethodPost, origin+path, bytes.NewReader(body))
	if err != nil {
		return errors.Wrap(err, "the request to laye was not made")
	}
	req.Header.Set("Authorization", signed)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return errors.Wrapf(err, "laye at %s did not answer", origin)
	}
	defer resp.Body.Close()
	said, err := io.ReadAll(io.LimitReader(resp.Body, 1<<14))
	if err != nil {
		return errors.Wrap(err, "laye's answer did not arrive")
	}
	if resp.StatusCode != http.StatusOK {
		return errors.Newf("laye answered %d: %s", resp.StatusCode, strings.TrimSpace(string(said)))
	}
	return nil
}
