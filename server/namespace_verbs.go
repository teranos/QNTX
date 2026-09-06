package server

// We drain before delete, and a namespace needs to be empty when deleted.
//
// "WE SUPERSEDE. WE SAY, HERE IS ANOTHER PIECE OF DATA, THAT IS NOW MORE
// LOAD-BEARING THAN BEFORE. AND THE DATA ACTUALLY NEVER LEAVES."
//
// Draining is that sentence made into a verb. Every attestation in the source
// is written into the target, carrying where it came from; the source's own
// bytes stay exactly where they are, and a newer record says the namespace is
// out of service. Deleting is the second verb and it removes nothing either —
// it refuses anything that is not empty, and writes the record that says the
// name is spent.

import (
	"encoding/json"
	"io"
	"maps"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/teranos/QNTX/ats"
	"github.com/teranos/QNTX/ats/identity"
	"github.com/teranos/QNTX/ats/storage"
	"github.com/teranos/QNTX/ats/types"
	appcfg "github.com/teranos/QNTX/internal/config"
	"github.com/teranos/QNTX/internal/slug"
	"github.com/teranos/QNTX/server/auth"
	"github.com/teranos/errors"
)

// What happens to a namespace itself, rather than to anything inside one. Both
// go into system: a namespace cannot hold the record of its own end.
const (
	// PredicateNamespaceDrained is the source, with the target and the count.
	PredicateNamespaceDrained = "namespace:drained"
	// PredicateNamespaceDeleted is the name. Nothing after this opens it.
	PredicateNamespaceDeleted = "namespace:deleted"
)

// What a drained copy carries so the supersession is visible on the copy
// itself: which namespace it came out of, and which attestation it was made
// from. The second is also what makes a second drain copy nothing.
const (
	attrDrainedFrom  = "drained_from"
	attrDrainedASUID = "drained_asuid"
)

// The two paths these verbs answer on.
const (
	namespacePathPrefix = "/api/namespaces/"
	drainPathSuffix     = "/drain"
)

// drainNamespaceRequest names where the attestations go. Nothing else: the
// source is the path, and who is asking is the admission.
type drainNamespaceRequest struct {
	Into string `json:"into"`
}

// drainedNamespaceResponse is what a drain did, counted.
//
// Copied and Carried are separate because draining twice is allowed and copies
// nothing the second time — a caller that saw only a total could not tell a
// drain that ran from one that had already run.
type drainedNamespaceResponse struct {
	Namespace string `json:"namespace"`
	Into      string `json:"into"`
	// Copied is what this drain wrote into the target.
	Copied int `json:"copied"`
	// Carried is every attestation now in the target that came from here,
	// this drain's and any earlier one's. It is what the source's definition
	// records, and what deleting holds the prefix against.
	Carried int `json:"carried"`
	// Remains is the prefix at the storage location. The bytes do not move.
	Remains  string `json:"remains"`
	Attested bool   `json:"attested"`
	// Why the record did not go into system, when it did not. The drain still
	// happened; this says the node could not write down that it did.
	NotAttested string `json:"not_attested,omitempty"`
}

// deletedNamespaceResponse says the name is spent and where the bytes are.
type deletedNamespaceResponse struct {
	Namespace string `json:"namespace"`
	DeletedAt string `json:"deleted_at"`
	DeletedBy string `json:"deleted_by"`
	// Remains is the prefix this node did not remove and will not. Remote
	// prefixes are removed by the object store, not from here, so the operator
	// is told exactly what is still out there and where to go for it.
	Remains  string `json:"remains"`
	Attested bool   `json:"attested"`
	// Why the record did not go into system, when it did not.
	NotAttested string `json:"not_attested,omitempty"`
}

// namespaceNotEmptyResponse is the refusal, as data. Said is the same thing in
// one sentence, for whoever is reading it rather than parsing it.
type namespaceNotEmptyResponse struct {
	Namespace string        `json:"namespace"`
	Said      string        `json:"said"`
	Fills     namespaceFill `json:"fills"`
}

// namespaceFill is everything that still holds a namespace open.
//
// Every field here is an answer somebody went and got. Empty is checked and
// never assumed: a lookup that would not answer is an error rather than a zero,
// because deleting on the strength of a question nobody answered is deleting
// something that was still in use.
type namespaceFill struct {
	// Attestations written into it and not carried across by a drain.
	Attestations int `json:"attestations"`
	// Kinds under the prefix beyond ns.toml and the attestations a drain
	// carried — watchers, schedules, anything a later feature writes there.
	Kinds []string `json:"kinds"`
	// Doors in the current am.toml that open onto it, by the key am.toml
	// gives them.
	Doors []string `json:"doors"`
	// Users registered at it, by User id.
	Users []string `json:"users"`
	// Sessions naming it. A session has no id anybody outside it can act on.
	Sessions int `json:"sessions"`
	// Tokens naming it that have not been revoked, by token id.
	Tokens []string `json:"tokens"`
}

// Empty reports whether nothing at all still names this namespace.
func (f namespaceFill) Empty() bool {
	return f.Attestations == 0 && len(f.Kinds) == 0 && len(f.Doors) == 0 &&
		len(f.Users) == 0 && f.Sessions == 0 && len(f.Tokens) == 0
}

// Said is the refusal in one sentence: every thing that still holds, by name
// and count. The caller empties them by the verbs that already exist — revoke
// the token, take the door line out of am.toml, forget the credential — and
// asks again, so each one is named rather than summarised.
func (f namespaceFill) Said(namespace string) string {
	var holds []string
	if f.Attestations > 0 {
		holds = append(holds, strconv.Itoa(f.Attestations)+
			" attestations were not drained")
	}
	if len(f.Kinds) > 0 {
		holds = append(holds, "it holds "+strings.Join(f.Kinds, ", "))
	}
	if len(f.Doors) > 0 {
		holds = append(holds, plural(len(f.Doors), "door", "doors")+
			" open onto it: "+strings.Join(f.Doors, ", "))
	}
	if len(f.Users) > 0 {
		holds = append(holds, plural(len(f.Users), "User is", "Users are")+
			" registered at it: "+strings.Join(f.Users, ", "))
	}
	if f.Sessions > 0 {
		holds = append(holds, plural(f.Sessions, "live session names it",
			"live sessions name it"))
	}
	if len(f.Tokens) > 0 {
		holds = append(holds, plural(len(f.Tokens), "token names it", "tokens name it")+
			": "+strings.Join(f.Tokens, ", "))
	}
	if len(holds) == 0 {
		return namespace + " is empty"
	}
	return namespace + " is not empty: " + strings.Join(holds, "; ")
}

// plural is a count and the words that go with it, so a refusal reads as a
// sentence rather than as a field dump.
func plural(count int, one, many string) string {
	if count == 1 {
		return "1 " + one
	}
	return strconv.Itoa(count) + " " + many
}

// HandleNamespaceDrain writes every attestation in a namespace into another
// one: POST /api/namespaces/{name}/drain, with a body naming the target.
//
// Nothing is moved and nothing is removed. The copies carry the source's name,
// so the supersession is visible on each one, and the source's ns.toml is
// rewritten to say where it went — after which reads and writes to it are
// refused naming the target, so nobody writes into a namespace that has been
// emptied.
//
// Idempotent. A copy carries the id it was made from, so a second drain sees
// what the first one wrote and copies nothing.
//
// SUPER per ADR-027, and session-only: what this does outlives the request.
func (s *QNTXServer) HandleNamespaceDrain(w http.ResponseWriter, r *http.Request) {
	namespaces, asked, ok := s.namespaceVerb(w, r)
	if !ok {
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	name := strings.Trim(strings.TrimSuffix(
		strings.TrimPrefix(r.URL.Path, namespacePathPrefix), drainPathSuffix), "/")
	if name == "" {
		http.Error(w, "the path must name a namespace: /api/namespaces/{name}/drain",
			http.StatusBadRequest)
		return
	}

	var req drainNamespaceRequest
	if err := json.NewDecoder(io.LimitReader(r.Body, maxNamespaceBodyBytes)).Decode(&req); err != nil {
		writeRichError(w, s.logger, errors.Wrapf(err, "failed to decode the request to drain %s", name),
			http.StatusBadRequest)
		return
	}
	if req.Into == "" {
		http.Error(w, "into is required: a drain names the namespace the attestations go to",
			http.StatusBadRequest)
		return
	}

	// Neither was created, so neither is drained. The store refuses them too,
	// where the writing happens; this one makes the answer a refusal naming the
	// namespace rather than a failure arriving from underneath.
	if auth.Permanent(name) {
		http.Error(w, "the "+name+" namespace cannot be drained; it was never created (ADR-026)",
			http.StatusForbidden)
		return
	}

	known, err := namespaces.List()
	if err != nil {
		writeRichError(w, s.logger, errors.Wrapf(err, "cannot tell what %s is", name),
			http.StatusInternalServerError)
		return
	}
	source, err := namespaceNamed(known, name)
	if err != nil {
		writeRichError(w, s.logger, err, http.StatusNotFound)
		return
	}
	target, err := namespaceNamed(known, req.Into)
	if err != nil {
		writeRichError(w, s.logger,
			errors.Wrapf(err, "%s cannot be drained into %s", source, req.Into),
			http.StatusBadRequest)
		return
	}
	// A namespace drained into itself would copy every attestation beside
	// itself and then refuse reads of both the copies and the originals.
	if slug.Of(source) == slug.Of(target) {
		http.Error(w, source+" cannot be drained into itself", http.StatusBadRequest)
		return
	}

	held := definitionOf(known, source)
	if held == nil {
		http.Error(w, "the namespace "+source+" has no ns.toml, so there is nothing to record a "+
			"drain on; it predates the file that defines a namespace (ADR-026)",
			http.StatusConflict)
		return
	}
	if held.Deleted() {
		writeRichError(w, s.logger,
			errNamespaceDeleted{name: source, when: held.DeletedAt, by: held.DeletedBy},
			http.StatusConflict)
		return
	}
	// Draining somewhere that is itself out of service would put the
	// attestations somewhere nothing can read them.
	if into := definitionOf(known, target); into != nil && (into.Deleted() || into.Drained()) {
		http.Error(w, source+" cannot be drained into "+target+", which is itself out of service",
			http.StatusBadRequest)
		return
	}
	// Draining twice is allowed and copies nothing the second time, but only
	// back into the namespace it went into the first time.
	if held.Drained() && slug.Of(held.DrainedInto) != slug.Of(target) {
		writeRichError(w, s.logger, errNamespaceDrained{name: source, into: held.DrainedInto},
			http.StatusConflict)
		return
	}

	copied, carried, err := s.drainInto(source, target)
	if err != nil {
		writeRichError(w, s.logger, err, http.StatusInternalServerError)
		return
	}

	// The newer record, and the one that counts from here on. Written after the
	// copying: a namespace recorded as drained before its attestations were
	// across would refuse the reads the drain itself still needs.
	drained := *held
	drained.Enabled = false
	drained.DrainedInto = target
	drained.DrainedAt = time.Now().UTC().Format(time.RFC3339)
	drained.DrainedCount = carried
	if err := namespaces.Amend(source, drained); err != nil {
		writeRichError(w, s.logger, errors.Wrapf(err,
			"%d attestations were written into %s and %s was not recorded as drained",
			copied, target, source), http.StatusInternalServerError)
		return
	}
	// The handle comes off, which is what makes the refusal reach requests: an
	// open store answers before anything asks what state the namespace is in.
	// Closing flushes what is still buffered on the way out.
	if err := s.closeIn(source); err != nil {
		writeRichError(w, s.logger, errors.Wrapf(err,
			"%s was recorded as drained into %s and is still open", source, target),
			http.StatusInternalServerError)
		return
	}

	s.logger.Infow("namespace drained", "namespace", source, "into", target,
		"copied", copied, "carried", carried, "by", asked)

	said := drainedNamespaceResponse{
		Namespace: source,
		Into:      target,
		Copied:    copied,
		Carried:   carried,
		Remains:   s.namespacePrefix(source),
		Attested:  true,
	}
	if err := s.attestNamespace(PredicateNamespaceDrained, source, map[string]any{
		"into":    target,
		"copied":  copied,
		"carried": carried,
		"by":      asked,
	}); err != nil {
		said.Attested = false
		said.NotAttested = err.Error()
		s.logger.Errorw("a namespace was drained and the record was refused",
			"namespace", source, "into", target, "error", err)
	}
	if err := writeJSON(w, http.StatusOK, said); err != nil {
		s.logger.Errorw("failed to write the drained namespace", "error", err, "namespace", source)
	}
}

// HandleNamespace deletes one namespace: DELETE /api/namespaces/{name}.
//
// A namespace needs to be empty when deleted, and empty is checked rather than
// assumed: no attestations that a drain did not carry across, nothing else
// under the prefix, no door onto it, no User registered at it, no live session
// naming it, no unrevoked token naming it. The refusal lists every one of those
// that still holds, so the caller can empty them and ask again.
//
// Nothing is removed at the storage location. The ns.toml is rewritten to say
// deleted, which is what keeps the name from being taken again over the old
// bytes, and the answer says what prefix is still there.
//
// SUPER per ADR-027, and session-only.
func (s *QNTXServer) HandleNamespace(w http.ResponseWriter, r *http.Request) {
	namespaces, asked, ok := s.namespaceVerb(w, r)
	if !ok {
		return
	}
	if r.Method != http.MethodDelete {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	name := strings.Trim(strings.TrimPrefix(r.URL.Path, namespacePathPrefix), "/")
	if name == "" {
		http.Error(w, "the path must name a namespace: /api/namespaces/{name}",
			http.StatusBadRequest)
		return
	}
	if auth.Permanent(name) {
		http.Error(w, "the "+name+" namespace cannot be deleted; it was never created (ADR-026)",
			http.StatusForbidden)
		return
	}

	known, err := namespaces.List()
	if err != nil {
		writeRichError(w, s.logger, errors.Wrapf(err, "cannot tell what %s is", name),
			http.StatusInternalServerError)
		return
	}
	found, err := namespaceNamed(known, name)
	if err != nil {
		writeRichError(w, s.logger, err, http.StatusNotFound)
		return
	}
	held := definitionOf(known, found)
	if held == nil {
		http.Error(w, "the namespace "+found+" has no ns.toml, so there is nothing to record a "+
			"deletion on; it predates the file that defines a namespace (ADR-026)",
			http.StatusConflict)
		return
	}
	// Already gone. The record says when and by whom, which is the answer to
	// asking again rather than a second deletion of the same name.
	if held.Deleted() {
		if err := writeJSON(w, http.StatusOK, deletedNamespaceResponse{
			Namespace: found,
			DeletedAt: held.DeletedAt,
			DeletedBy: held.DeletedBy,
			Remains:   s.namespacePrefix(found),
			Attested:  true,
		}); err != nil {
			s.logger.Errorw("failed to write the deleted namespace", "error", err, "namespace", found)
		}
		return
	}

	// Read here rather than taken from what this node booted with: a door added
	// to am.toml since is a door onto this namespace all the same.
	cfg, err := appcfg.Load()
	if err != nil {
		writeRichError(w, s.logger, errors.Wrapf(err,
			"cannot read am.toml, so cannot tell whether a door opens onto %s", found),
			http.StatusInternalServerError)
		return
	}
	fills, err := s.whatFillsIt(cfg, known, found)
	if err != nil {
		writeRichError(w, s.logger, errors.Wrapf(err,
			"cannot tell whether %s is empty, so it is not deleted", found),
			http.StatusInternalServerError)
		return
	}
	if !fills.Empty() {
		if err := writeJSON(w, http.StatusConflict, namespaceNotEmptyResponse{
			Namespace: found,
			Said:      fills.Said(found),
			Fills:     fills,
		}); err != nil {
			s.logger.Errorw("failed to write what fills a namespace", "error", err, "namespace", found)
		}
		return
	}

	// The handle comes off before the record is written: a store left open
	// keeps flushing, and a flush writes the prefix back.
	if err := s.closeIn(found); err != nil {
		writeRichError(w, s.logger, errors.Wrapf(err,
			"%s is still open, so it is not deleted", found), http.StatusInternalServerError)
		return
	}

	deleted := *held
	deleted.Enabled = false
	deleted.DeletedAt = time.Now().UTC().Format(time.RFC3339)
	deleted.DeletedBy = asked
	if err := namespaces.Amend(found, deleted); err != nil {
		writeRichError(w, s.logger, errors.Wrapf(err,
			"%s was closed and was not recorded as deleted", found), http.StatusInternalServerError)
		return
	}

	s.logger.Infow("namespace deleted", "namespace", found, "by", asked,
		"remains", s.namespacePrefix(found))

	said := deletedNamespaceResponse{
		Namespace: found,
		DeletedAt: deleted.DeletedAt,
		DeletedBy: deleted.DeletedBy,
		Remains:   s.namespacePrefix(found),
		Attested:  true,
	}
	if err := s.attestNamespace(PredicateNamespaceDeleted, found, map[string]any{
		"by":      asked,
		"remains": said.Remains,
	}); err != nil {
		said.Attested = false
		said.NotAttested = err.Error()
		s.logger.Errorw("a namespace was deleted and the record was refused",
			"namespace", found, "error", err)
	}
	if err := writeJSON(w, http.StatusOK, said); err != nil {
		s.logger.Errorw("failed to write the deleted namespace", "error", err, "namespace", found)
	}
}

// drainInto writes every attestation in the source into the target.
//
// Copied is what this run wrote; carried is everything in the target that came
// from the source, this run's and every earlier run's. Carried is what the
// source's definition records, because it is what deleting counts against.
func (s *QNTXServer) drainInto(source, target string) (copied, carried int, err error) {
	// openedIn and not storeIn: a source that has already been drained refuses
	// requests, and this is the verb rather than a request.
	from, err := s.openedIn(source)
	if err != nil {
		return 0, 0, errors.Wrapf(err, "failed to open %s to drain it", source)
	}
	to, err := s.openedIn(target)
	if err != nil {
		return 0, 0, errors.Wrapf(err, "failed to open %s to drain %s into it", target, source)
	}

	held, err := from.GetAttestations(ats.AttestationFilter{})
	if err != nil {
		return 0, 0, errors.Wrapf(err, "failed to read what %s holds", source)
	}
	already, err := alreadyDrained(to, source)
	if err != nil {
		return 0, 0, errors.Wrapf(err, "failed to read what %s already carries from %s", target, source)
	}

	for _, as := range held {
		if already[as.ID] {
			continue
		}
		made, err := drainedCopy(as, source, to)
		if err != nil {
			return copied, copied + len(already), errors.Wrapf(err,
				"failed to make the copy of %s for %s", as.ID, target)
		}
		if err := to.CreateAttestation(made); err != nil {
			return copied, copied + len(already), errors.Wrapf(err,
				"failed to write %s into %s as %s", as.ID, target, made.ID)
		}
		copied++
	}
	return copied, copied + len(already), nil
}

// alreadyDrained is every source attestation the target already holds a copy
// of, by the id the copy was made from.
//
// This is what makes draining twice copy nothing. A vanity ASUID is minted per
// subject, predicate and context and is not a hash of what it names, so the
// same attestation copied twice gets two ids — the id it came from has to ride
// on the copy for anything to be able to tell.
func alreadyDrained(to ats.AttestationStore, source string) (map[string]bool, error) {
	held, err := to.GetAttestations(ats.AttestationFilter{})
	if err != nil {
		return nil, errors.Wrap(err, "failed to read the target")
	}

	carried := map[string]bool{}
	for _, as := range held {
		if as.Attributes == nil {
			continue
		}
		from, named := as.Attributes[attrDrainedFrom].(string)
		if !named || from != source {
			continue
		}
		was, said := as.Attributes[attrDrainedASUID].(string)
		if said {
			carried[was] = true
		}
	}
	return carried, nil
}

// drainedCopy is one attestation as it lands in the target: the same subjects,
// predicates, actors, timestamp and attributes, with where it came from added
// and a new id minted in the target.
//
// The signature does not come across. It is over the original's canonical JSON,
// and this is a different attestation — a different id, and two attributes the
// original never carried. Carrying the old signature would be carrying one that
// cannot verify; the copy is signed by the node that made it.
func drainedCopy(as *types.As, source string, into ats.AttestationStore) (*types.As, error) {
	attrs := make(map[string]any, len(as.Attributes)+2)
	maps.Copy(attrs, as.Attributes)
	attrs[attrDrainedFrom] = source
	attrs[attrDrainedASUID] = as.ID

	id, err := identity.GenerateASUIDWithRetry("AS",
		firstNamed(as.Subjects), firstNamed(as.Predicates), firstNamed(as.Contexts),
		into.AttestationExists)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to mint an id for the copy of %s", as.ID)
	}

	return &types.As{
		ID:         id,
		Subjects:   slices.Clone(as.Subjects),
		Predicates: slices.Clone(as.Predicates),
		Contexts:   slices.Clone(as.Contexts),
		Actors:     slices.Clone(as.Actors),
		// When it was said. A drain does not change when something happened.
		Timestamp:  as.Timestamp,
		Source:     as.Source,
		Attributes: attrs,
		// When this copy was made, which is a different fact and is this one.
		CreatedAt: time.Now(),
	}, nil
}

// firstNamed is what an id is minted against. An attestation always names at
// least one of each, and "_" is what the rest of the tree uses for none.
func firstNamed(named []string) string {
	if len(named) == 0 {
		return "_"
	}
	return named[0]
}

// whatFillsIt asks every thing that could still hold this namespace open.
//
// Six questions, each put to whatever would know: the store for what it holds,
// the location's listing for what else is under the prefix, am.toml for the
// doors, and the auth handler for the Users, sessions and tokens. Anything that
// will not answer comes back as an error, because an unanswered question is not
// an empty one.
func (s *QNTXServer) whatFillsIt(cfg *appcfg.Config, known []storage.Namespace, name string) (namespaceFill, error) {
	fill := namespaceFill{}

	held := storage.Namespace{}
	for _, one := range known {
		if one.Name == name {
			held = one
		}
	}

	// What is under the prefix beyond the attestations. ns.toml is not a kind,
	// so what is left here is watchers, schedules, or anything else written
	// there — none of which a drain carries, and none of which is empty.
	for _, kind := range held.Kinds {
		if kind != "attestations" {
			fill.Kinds = append(fill.Kinds, kind)
		}
	}

	// The attestations, counted rather than believed. A drain records how many
	// it carried across; anything the store holds beyond that number is an
	// attestation no drain took anywhere.
	store, err := s.openedIn(name)
	if err != nil {
		return namespaceFill{}, errors.Wrapf(err, "cannot open %s to count what it holds", name)
	}
	inside, err := store.GetAttestations(ats.AttestationFilter{})
	if err != nil {
		return namespaceFill{}, errors.Wrapf(err, "cannot count what %s holds", name)
	}
	carried := 0
	if held.Definition != nil {
		carried = held.Definition.DrainedCount
	}
	if notDrained := len(inside) - carried; notDrained > 0 {
		fill.Attestations = notDrained
	}

	// The doors am.toml names right now. A door added since this node started
	// is still a door onto this namespace, and deleting under it would leave a
	// door opening onto nothing.
	fill.Doors = doorsOnto(cfg, name)

	// The Users, sessions and tokens. A node with no auth handler holds none of
	// the three, which is a deployment that has no way to name a namespace from
	// outside it at all.
	if s.authHandler != nil {
		holding, err := s.authHandler.NamesNamespace(name)
		if err != nil {
			return namespaceFill{}, errors.Wrapf(err, "cannot tell what still names %s", name)
		}
		fill.Users = holding.Users
		fill.Sessions = holding.Sessions
		fill.Tokens = holding.Tokens
	}

	return fill, nil
}

// doorsOnto is every door in a config that opens onto a namespace, by the key
// am.toml gives it. The key is a slug and a namespace keeps the name it was
// created with, so the two meet at the slug.
func doorsOnto(cfg *appcfg.Config, namespace string) []string {
	if cfg == nil {
		return nil
	}
	reachedBy := slug.Of(namespace)
	var onto []string
	for door := range cfg.Auth.Door {
		if slug.Of(door) == reachedBy {
			onto = append(onto, door)
		}
	}
	slices.Sort(onto)
	return onto
}

// definitionOf is what one namespace's ns.toml says, out of a listing. Nil is
// a namespace nobody defined — one written before the file existed.
func definitionOf(known []storage.Namespace, name string) *storage.NamespaceDefinition {
	for _, held := range known {
		if held.Name == name {
			return held.Definition
		}
	}
	return nil
}

// namespacePrefix is where a namespace's bytes are, so an answer can say what
// is still there. Empty configured location gives the name alone, which is all
// this node knows in that case.
func (s *QNTXServer) namespacePrefix(name string) string {
	if s.deps == nil || s.deps.cfg == nil {
		return name
	}
	location := strings.TrimSuffix(s.deps.cfg.Storage.Parquet.Location, "/")
	if location == "" {
		return name
	}
	return location + "/" + name
}

// attestNamespace records what happened to a namespace, into system.
//
// A namespace cannot hold the record of its own end, and system is the node
// talking about itself. Unlike an admission this is returned rather than only
// logged: the caller is standing there, and a verb that happened without being
// written down is worth telling them about while they can still act on it.
func (s *QNTXServer) attestNamespace(predicate, namespace string, attrs map[string]any) error {
	store := s.systemAttestor()
	if store == nil {
		return errors.Newf("this node keeps no attestation store, so %s of %s was not recorded",
			predicate, namespace)
	}

	id, err := identity.GenerateASUID("AS", namespace, predicate, auth.NamespaceSystem)
	if err != nil {
		return errors.Wrapf(err, "failed to mint an id for the %s record of %s", predicate, namespace)
	}
	actor := "did:key:unknown"
	if s.nodeDID != nil {
		actor = s.nodeDID.DID
	}
	now := time.Now()
	if err := store.CreateAttestation(&types.As{
		ID:         id,
		Subjects:   []string{namespace},
		Predicates: []string{predicate},
		Contexts:   []string{auth.NamespaceSystem},
		Actors:     []string{actor},
		Timestamp:  now,
		Source:     actor,
		Attributes: attrs,
		CreatedAt:  now,
	}); err != nil {
		return errors.Wrapf(err, "the store refused the %s record of %s", predicate, namespace)
	}
	return nil
}

// namespaceVerb answers the three questions drain and delete share: does this
// backend keep namespaces, was this request admitted, and did it come in on a
// session rather than a token.
//
// Session-only the way minting is (ADR-025). What these two do outlives the
// request that asked for it, and a token that could delete the namespace it
// acts in is a credential that can remove the ground it stands on.
func (s *QNTXServer) namespaceVerb(w http.ResponseWriter, r *http.Request) (storage.Namespaces, string, bool) {
	namespaces, ok := s.superNamespaces(w, r)
	if !ok {
		return nil, "", false
	}
	admitted, ok := auth.AdmissionFrom(r.Context())
	if !ok {
		http.Error(w, "refused", http.StatusForbidden)
		return nil, "", false
	}
	if admitted.Grant != nil {
		http.Error(w, "draining and deleting a namespace are done from a session, not from a token",
			http.StatusForbidden)
		return nil, "", false
	}
	if admitted.Identity == "" {
		writeRichError(w, s.logger,
			errors.New("this request carries no identity, so there is nobody to record the change against"),
			http.StatusInternalServerError)
		return nil, "", false
	}
	return namespaces, admitted.Identity, true
}
