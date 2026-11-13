package server

import (
	"cmp"
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
	// "and it needs to be more clear if a session named itself, or if the User gave it that name]"
	given := sent["given"]
	if given != "" && !slices.Contains(nameGivers, given) {
		return nil, &protocol.Refusal{Why: sigil.Invalid, Param: "given",
			Says: strconv.Quote(given) + " is not who gives a name: " + strings.Join(nameGivers, ", ")}
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
	// "the token can be GONE"
	// A name a revoked token holds is held by nobody who can ask: it is let go
	// of here, in a line of its own, and taken by the agent asking.
	gone := slices.IndexFunc(names, func(kept agentName) bool { return kept.did != did && strings.EqualFold(kept.name, name) })
	if gone >= 0 && !s.revoked(names[gone].did) {
		return nil, &protocol.Refusal{Why: sigil.NotAllowed, Param: "name",
			Says: strconv.Quote(name) + " is taken: another agent holds it. Ask again with another name."}
	}
	// 「[Opus 5.5] The newest sequence is the greatest any line carries: the last line read may carry none.」
	var newest int64
	var newestStamp time.Time
	for _, line := range lines {
		newest = max(newest, line.sequence)
		if line.at.After(newestStamp) {
			newestStamp = line.at
		}
	}
	if gone >= 0 {
		let, err := s.keepNameLine(predicateAgentUnnamed, names[gone].did, did, names[gone].name, "", newest, newestStamp)
		if err != nil {
			return nil, &protocol.Refusal{Why: sigil.Failed, Says: err.Error()}
		}
		s.logger.Infow("a revoked token's name was let go of", "did", names[gone].did, "name", names[gone].name, "by", did)
		newest++
		if let.After(newestStamp) {
			newestStamp = let
		}
	}
	if _, err := s.keepNameLine(predicateAgentNamed, did, did, name, given, newest, newestStamp); err != nil {
		return nil, &protocol.Refusal{Why: sigil.Failed, Says: err.Error()}
	}
	if left != "" {
		s.logger.Infow("an agent renamed itself", "did", did, "was", left, "name", name)
	} else {
		s.logger.Infow("an agent named itself", "did", did, "name", name)
	}
	return &protocol.Named{Name: name, Did: did}, nil
}

// revoked is whether the agent whose DID this is was revoked: a token the node
// holds for it, switched off.
func (s *QNTXServer) revoked(did string) bool {
	return s.authHandler != nil && s.authHandler.SwitchedOff(did)
}

// itsOwnDID is the DID a name is held by: a token asking as itself, by the
// DID it has for as long as it is that token. An agent the node hosts asks so
// (ADR-048), and so does a token minted in the element.
//
// A session and a connector ask as a person, who is named as they arrive
// (ADR-031). A client's own token is issued anew under another DID each time
// (auth.TokenStrategy.GenerateAccessToken), so a name on it would not last.
//
// "I WANT TO MAKE EVERY SESSION BE ABLE TO NAME ITSELF"
// A key the person said yes to at the passkey (auth/device.go) is recorded as
// its own client. It signs as itself and its next signature is the same DID,
// so it is named by that DID.
//
// "I EXPECT EACH SESSION TO ALSO HAVE ITS OWN KEY AND NAME"
// Such a key is admitted as the person who said yes, with no Grant
// (auth.admittedAsThePerson): it is known by the DID it signs as being the
// client it was issued through. A connector's token is issued under another
// DID than its client's, and is refused as the person it asks as.
func itsOwnDID(admitted auth.Admission) (string, *protocol.Refusal) {
	if admitted.Grant == nil && admitted.TokenDID != "" && admitted.TokenDID == admitted.ClientDID {
		return admitted.TokenDID, nil
	}
	if admitted.Grant == nil {
		return "", &protocol.Refusal{Why: sigil.NotAllowed,
			Says: "i name names an agent by its own DID, and this asks as a person: a person is named as they arrive"}
	}
	if admitted.ClientDID != "" && admitted.ClientDID != admitted.Grant.DID {
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
	// "i want to ban numbers in names"
	// "a name is an Uppercase character followed by normal lowercase characters"
	// "and a name can be two names after each other as well"
	// "or three"
	// "so no special chars in names as well"
	// "a Name needs to be able to be phonetically spoken"
	// "Names need to be compatible with STT/TTS"
	words := strings.Split(name, " ")
	if len(words) > 3 {
		return "a name is one, two or three words", true
	}
	for _, word := range words {
		if !canBeSaid(word) {
			return "a name is one, two or three words, each a capital letter and then lowercase letters, " +
				"with a vowel to say it by: " + strconv.Quote(word) + " is not one", true
		}
	}
	return "", false
}

// canBeSaid is whether word is a capital letter and then lowercase letters, a to
// z, one of them a vowel.
func canBeSaid(word string) bool {
	if len(word) < 2 || word[0] < 'A' || word[0] > 'Z' {
		return false
	}
	voiced := strings.ContainsRune("AEIOUY", rune(word[0]))
	for _, letter := range word[1:] {
		if letter < 'a' || letter > 'z' {
			return false
		}
		voiced = voiced || strings.ContainsRune("aeiouy", letter)
	}
	return voiced
}

// nameGivers is who gives an agent its name, as the agent says it of its own
// name: the person, the agent itself, or the harness the session runs in.
var nameGivers = []string{"user", "itself", "harness"}

// nameGivenAttribute is the attribute a name line keeps its giver under.
const nameGivenAttribute = "given"

// predicateAgentUnnamed is the line a revoked token's name is let go of in:
// its subject is the DID that held the name, its actor the agent that asked
// for it.
const predicateAgentUnnamed = "agent:unnamed"

// agentName is one agent:named line: the DID, and the name it took; or one
// agent:unnamed line: the DID, and the name it was let go of.
type agentName struct {
	did  string
	name string
	// let is an agent:unnamed line: did holds name no longer.
	let bool
	at  time.Time
	// 「[Opus 5.5] sequence is the line's place among every name line; 0 is a line written before lines」
	// 「[Opus 5.5] carried one, before any that does.」
	sequence int64
	// 「[Opus 5.5] after is the newest stamp among the lines a sequenced line was written after: every line」
	// 「[Opus 5.5] with no sequence stamped at or before it was there when it was written. A sequenced line」
	// 「[Opus 5.5] that does not say is after the lines stamped at or before its own stamp.」
	after time.Time
}

// 「[Opus 5.5] nameAfterAttribute is the attribute a name line's after is kept under, in Unix milliseconds.」
const nameAfterAttribute = "after"

// 「[Opus 5.5] nameSequenceAttribute is the attribute a name line's sequence is kept under.」
const nameSequenceAttribute = "sequence"

// 「[Opus 5.5] sequenceOf is the sequence a name line carries, 0 for one that carries none.」
func sequenceOf(as *types.As) (int64, error) {
	return wholeAttribute(as, nameSequenceAttribute)
}

// 「[Opus 5.5] wholeAttribute is the whole number above zero a name line keeps under attribute, 0 for none.」
func wholeAttribute(as *types.As, attribute string) (int64, error) {
	held, carried := as.Attributes[attribute]
	if !carried {
		return 0, nil
	}
	switch sequence := held.(type) {
	case float64:
		if sequence == float64(int64(sequence)) && sequence > 0 {
			return int64(sequence), nil
		}
	case int:
		if sequence > 0 {
			return int64(sequence), nil
		}
	case int64:
		if sequence > 0 {
			return sequence, nil
		}
	}
	return 0, errors.Newf("the %s line %s keeps %s as %v (%T), not a whole number above zero", predicateAgentNamed, as.ID, attribute, held, held)
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
// were written: by the sequence each carries, which keepNameLine makes one
// more than every one before it; lines from before sequences come first, by
// when each is stamped.
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
	// One read per line kind: a filter naming several predicates reads lines
	// that carry them all.
	let, err := reading.GetAttestations(ats.AttestationFilter{TimeStart: types.Unbounded(), TimeEnd: types.Unbounded(),
		Predicates: []string{predicateAgentUnnamed},
		Limit:      ats.EveryRow,
	})
	if err != nil {
		return nil, errors.Wrapf(err, "failed to read the %s lines", predicateAgentUnnamed)
	}
	unnamed := make(map[string]bool, len(let))
	for _, as := range let {
		unnamed[as.ID] = true
	}
	var names []agentName
	for _, as := range append(found, let...) {
		name, _ := as.Attributes["name"].(string)
		if unnamed[as.ID] {
			if len(as.Subjects) != 1 || name == "" {
				s.logger.Warnw("an agent:unnamed line names no one DID and its name, and lets go of nothing", "id", as.ID)
				continue
			}
		} else if len(as.Subjects) != 1 || len(as.Actors) == 0 || as.Actors[0] != as.Subjects[0] || name == "" {
			s.logger.Warnw("an agent:named line is no agent naming itself, and is no name", "id", as.ID)
			continue
		}
		// 「[Opus 5.5] A line whose sequence does not read is no name, as one another actor wrote is not.」
		sequence, err := sequenceOf(as)
		if err != nil {
			s.logger.Warnw("an agent:named line carries a sequence that does not read, and is no name", "id", as.ID, "error", err)
			continue
		}
		line := agentName{did: as.Subjects[0], name: name, let: unnamed[as.ID], at: as.Timestamp, sequence: sequence, after: as.Timestamp}
		after, err := wholeAttribute(as, nameAfterAttribute)
		if err != nil {
			s.logger.Warnw("an agent:named line carries an after that does not read, and is no name", "id", as.ID, "error", err)
			continue
		}
		if after > 0 {
			line.after = time.UnixMilli(after)
		}
		names = append(names, line)
	}
	return inOrder(names), nil
}

// 「[Opus 5.5] inOrder is name lines in the order they were written: sequenced lines by their sequence,」
// 「[Opus 5.5] and a line with none (written before sequences, or by a build from before them run after)」
// 「[Opus 5.5] before the first sequenced line whose after is at or past its stamp: that line was written」
// 「[Opus 5.5] after it, however far ahead of the clock it was stamped.」
func inOrder(names []agentName) []agentName {
	var sequenced, unsequenced []agentName
	for _, line := range names {
		if line.sequence > 0 {
			sequenced = append(sequenced, line)
		} else {
			unsequenced = append(unsequenced, line)
		}
	}
	slices.SortStableFunc(sequenced, func(a, b agentName) int { return cmp.Compare(a.sequence, b.sequence) })
	slices.SortStableFunc(unsequenced, func(a, b agentName) int { return a.at.Compare(b.at) })
	ordered := make([]agentName, 0, len(names))
	for len(sequenced) > 0 && len(unsequenced) > 0 {
		if unsequenced[0].at.UnixMilli() > sequenced[0].after.UnixMilli() {
			ordered, sequenced = append(ordered, sequenced[0]), sequenced[1:]
		} else {
			ordered, unsequenced = append(ordered, unsequenced[0]), unsequenced[1:]
		}
	}
	return append(append(ordered, unsequenced...), sequenced...)
}

// standing is the name each agent holds after these lines, read in order.
func standing(names []agentName) []agentName {
	// One name is one agent's at a time and an agent holds one name. A line for
	// a name another agent held when it was written is no name, however it came
	// to be written; any other line is the agent's name from then on.
	var stands []agentName
	for _, line := range names {
		if line.let {
			stands = slices.DeleteFunc(stands, func(held agentName) bool {
				return held.did == line.did && strings.EqualFold(held.name, line.name)
			})
			continue
		}
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

// keepAgentName writes the line down, by the agent itself, stamped when it is
// written and carrying the sequence after newest, the newest name line's.
// Lines stamped alike read back in no order, and a rename and another agent
// taking the name it left, read back the other way round, would undo what
// was answered; a line stamped ahead of the clock would become the record's
// take-in mark (storage.TakeIn) and keep later rows out. Asked under naming,
// so on this node no line comes between.
// stampPrecision is how finely a line's timestamp is kept: to the millisecond.
const stampPrecision = time.Millisecond

// 「[Opus 5.5] namingNow is when a name line is written: the clock, which a test holds still.」
var namingNow = time.Now

// keepNameLine writes one line of either kind, about did by the agent asking,
// and answers when it is stamped.
func (s *QNTXServer) keepNameLine(predicate, did, by, name, given string, newest int64, newestStamp time.Time) (time.Time, error) {
	store, err := s.held.WriteWhatTheNodeKnowsOfItself()
	if err != nil {
		return time.Time{}, errors.Wrapf(err, "no store to keep the name of %s in", did)
	}
	id, err := identity.GenerateASUIDWithRetry("AS", did, predicate, auth.NamespaceSystem, store.AttestationExists)
	if err != nil {
		return time.Time{}, errors.Wrapf(err, "failed to name the %s line of %s", predicate, did)
	}
	now := namingNow()
	said := map[string]any{"name": name, nameSequenceAttribute: newest + 1, nameAfterAttribute: max(newestStamp.UnixMilli(), 1)}
	if given != "" {
		said[nameGivenAttribute] = given
	}
	if err := store.CreateAttestation(&types.As{
		ID: id, Subjects: []string{did}, Predicates: []string{predicate}, Contexts: []string{auth.NamespaceSystem},
		Actors: []string{by}, Timestamp: now, CreatedAt: now, Source: by,
		Attributes: said,
	}); err != nil {
		return time.Time{}, errors.Wrapf(err, "the %s line of %s was not kept", predicate, did)
	}
	measure.Count(measure.AttestationsWritten, 1)
	return now, nil
}
