package server

import (
	"context"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/teranos/QNTX/ats"
	"github.com/teranos/QNTX/ats/identity"
	"github.com/teranos/QNTX/ats/types"
	"github.com/teranos/QNTX/internal/measure"
	"github.com/teranos/QNTX/plugin/grpc/protocol"
	"github.com/teranos/QNTX/server/auth"
	"github.com/teranos/QNTX/server/sigil"
	errors "github.com/teranos/sacred-error"
)

// "assign yourself a name using signa:i sigil:name if available otherwise retry"
//
// "why cant i say room agent, and its one thing"

// An agent has its own DID and attests as itself (ADR-048), and the name is
// held by that DID: `<did> is 'agent:named' of system by <did>`, the name in
// the line's attributes, as a person's display_name is in identity:named's
// (ADR-031). A person's name is the User record's and is not unique; an
// agent's is the newest of these lines it could hold, and is one agent's on
// the node at a time. Neither reads the other, and root is nobody's to take in
// either.

const iNamePath = "/i/name"

// predicateAgentNamed is the line an agent's name is: its subject and its
// actor are the agent's DID.
const predicateAgentNamed = "agent:named"

// "ask the Qodo agent to name itself Qodoa"
//
// "ask the ground agent to rename itself to Slate"

// iName assigns the agent asking the name it asked for. The name it already
// holds is answered again. Another name is taken in its place, and the one it
// leaves is free for another agent. A name another agent holds is refused as
// taken. A person's display_name is settled once (ADR-031); an agent's name is
// the newest it could hold.
func (s *QNTXServer) iName(ctx context.Context, sent sigil.Sent) (any, *protocol.Refusal) {
	admitted, refusal := s.iAdmitted(ctx)
	if refusal != nil {
		return nil, refusal
	}
	did, refusal := itsOwnDID(admitted)
	if refusal != nil {
		return nil, refusal
	}
	name := strings.TrimSpace(sent["name"])
	if says, refused := noName(name); refused {
		return nil, &protocol.Refusal{Why: sigil.Invalid, Param: "name", Says: says}
	}
	if s.held == nil {
		return nil, &protocol.Refusal{Why: sigil.Failed, Says: "this node holds no store to keep a name in"}
	}

	s.naming.Lock()
	defer s.naming.Unlock()

	lines, err := s.agentLines()
	if err != nil {
		return nil, &protocol.Refusal{Why: sigil.Failed, Says: err.Error()}
	}
	names := standing(lines)
	left := ""
	if at := slices.IndexFunc(names, func(kept agentName) bool { return kept.did == did }); at >= 0 {
		left = names[at].name
		if strings.EqualFold(left, name) {
			return &protocol.Named{Name: left, Did: did}, nil
		}
	}
	if slices.ContainsFunc(names, func(kept agentName) bool { return kept.did != did && strings.EqualFold(kept.name, name) }) {
		return nil, &protocol.Refusal{Why: sigil.NotAllowed, Param: "name",
			Says: strconv.Quote(name) + " is taken: another agent holds it. Ask again with another name."}
	}
	var newest time.Time
	if len(lines) > 0 {
		newest = lines[len(lines)-1].at
	}
	if err := s.keepAgentName(did, name, newest); err != nil {
		return nil, &protocol.Refusal{Why: sigil.Failed, Says: err.Error()}
	}
	if left != "" {
		s.logger.Infow("an agent renamed itself", "did", did, "was", left, "name", name)
	} else {
		s.logger.Infow("an agent named itself", "did", did, "name", name)
	}
	return &protocol.Named{Name: name, Did: did}, nil
}

// itsOwnDID is the DID a name is held by: a token asking as itself, by the
// DID it has for as long as it is that token. An agent the node hosts asks so
// (ADR-048), and so does a token minted in the element (ADR-025).
//
// A session and a connector ask as a person, who is named as they arrive
// (ADR-031). A client's own token is issued anew under another DID each time
// (auth.TokenStrategy.GenerateAccessToken), so a name on it would not last.
func itsOwnDID(admitted auth.Admission) (string, *protocol.Refusal) {
	if admitted.Grant == nil {
		return "", &protocol.Refusal{Why: sigil.NotAllowed,
			Says: "i name names an agent by its own DID, and this asks as a person: a person is named as they arrive"}
	}
	if admitted.ClientDID != "" {
		return "", &protocol.Refusal{Why: sigil.NotAllowed,
			Says: "this token was issued through the client " + admitted.ClientDID + ", which is issued its next one under another DID: a name on it would not last"}
	}
	if admitted.Grant.DID == "" {
		return "", &protocol.Refusal{Why: sigil.Failed, Says: "this token carries no DID for a name to be held by"}
	}
	return admitted.Grant.DID, nil
}

// noName says why this is no name, and whether it is refused at all: what a
// display_name is held to (auth's refuseName and nameUser).
func noName(name string) (string, bool) {
	switch {
	case name == "" || strings.ContainsFunc(name, unicode.IsControl):
		return "a name is one line of text", true
	case len(name) > auth.MaxName:
		return "the name is longer than " + strconv.Itoa(auth.MaxName) + " characters", true
	// "regardless of root_identity setting it or not, root is never an available display name except for the one root identity user, they dont need to set their display name as root"
	case strings.EqualFold(name, auth.RootName):
		return auth.RootName + " is not available", true
	}
	return "", false
}

// agentName is one agent:named line: the DID, and the name it took.
type agentName struct {
	did  string
	name string
	at   time.Time
}

// agentNames is the name each agent holds now, read from every line in the
// order they were written, so it is what i name answered as each was written,
// on any node and after any restart. An agent's name is the newest line it
// wrote about itself for a name nobody else held at that moment; the name it
// held before is free from then on. A line some other actor wrote about a DID
// is not the agent naming itself, and is no name.
func (s *QNTXServer) agentNames() ([]agentName, error) {
	lines, err := s.agentLines()
	if err != nil {
		return nil, err
	}
	return standing(lines), nil
}

// agentLines is every line an agent wrote naming itself, in the order they
// were written: by when each is stamped, which keepAgentName makes later than
// every one before it.
func (s *QNTXServer) agentLines() ([]agentName, error) {
	reading, err := s.held.Read(auth.NamespaceSystem)
	if err != nil {
		return nil, errors.Wrapf(err, "the names agents hold are kept in %s, which is not open", auth.NamespaceSystem)
	}
	found, err := reading.GetAttestations(ats.AttestationFilter{TimeStart: types.Unbounded(), TimeEnd: types.Unbounded(),
		Predicates: []string{predicateAgentNamed},
		Limit:      ats.EveryRow,
	})
	if err != nil {
		return nil, errors.Wrapf(err, "failed to read the %s lines", predicateAgentNamed)
	}
	var names []agentName
	for _, as := range found {
		name, _ := as.Attributes["name"].(string)
		if len(as.Subjects) != 1 || len(as.Actors) == 0 || as.Actors[0] != as.Subjects[0] || name == "" {
			s.logger.Warnw("an agent:named line is no agent naming itself, and is no name", "id", as.ID)
			continue
		}
		names = append(names, agentName{did: as.Subjects[0], name: name, at: as.Timestamp})
	}
	slices.SortStableFunc(names, func(a, b agentName) int { return a.at.Compare(b.at) })
	return names, nil
}

// standing is the name each agent holds after these lines, read in order.
func standing(names []agentName) []agentName {
	// One name is one agent's at a time and an agent holds one name. A line for
	// a name another agent held when it was written is no name, however it came
	// to be written; any other line is the agent's name from then on.
	var stands []agentName
	for _, line := range names {
		if slices.ContainsFunc(stands, func(held agentName) bool {
			return held.did != line.did && strings.EqualFold(held.name, line.name)
		}) {
			continue
		}
		stands = slices.DeleteFunc(stands, func(held agentName) bool { return held.did == line.did })
		stands = append(stands, line)
	}
	return stands
}

// stampPrecision is how finely a line's timestamp is kept: to the millisecond.
const stampPrecision = time.Millisecond

// keepAgentName writes the line down, by the agent itself, stamped later than
// newest, the newest name line already written, at the precision it is kept.
// Lines stamped alike read back in no order, and a rename and another agent
// taking the name it left, read back the other way round, would undo what
// was answered. Asked under naming, so on this node no line comes between.
func (s *QNTXServer) keepAgentName(did, name string, newest time.Time) error {
	store, err := s.held.WriteWhatTheNodeKnowsOfItself()
	if err != nil {
		return errors.Wrapf(err, "no store to keep the name of %s in", did)
	}
	id, err := identity.GenerateASUIDWithRetry("AS", did, predicateAgentNamed, auth.NamespaceSystem, store.AttestationExists)
	if err != nil {
		return errors.Wrapf(err, "failed to name the %s line of %s", predicateAgentNamed, did)
	}
	now := time.Now()
	stamped := now.Truncate(stampPrecision)
	if newest = newest.Truncate(stampPrecision); !stamped.After(newest) {
		stamped = newest.Add(stampPrecision)
	}
	if err := store.CreateAttestation(&types.As{
		ID: id, Subjects: []string{did}, Predicates: []string{predicateAgentNamed}, Contexts: []string{auth.NamespaceSystem},
		Actors: []string{did}, Timestamp: stamped, CreatedAt: now, Source: did,
		Attributes: map[string]any{"name": name},
	}); err != nil {
		return errors.Wrapf(err, "the name of %s was not kept", did)
	}
	measure.Count(measure.AttestationsWritten, 1)
	return nil
}
