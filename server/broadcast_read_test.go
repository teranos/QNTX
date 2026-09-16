package server

import (
	"testing"

	"github.com/teranos/QNTX/ats/types"
	"github.com/teranos/QNTX/server/auth"
	"go.uber.org/zap"
)

// A watcher match is pushed because a watcher fired, not because the reader
// asked. There is no query behind it that the read gate already refused, so a
// watcher somebody else made would hand a connection the whole attestation —
// predicates and attributes — that it could not have queried for.
//
// Being in the namespace says a socket may hear that something happened.
// Whether it may be handed the thing is the read gate's, and it is asked here.

func broadcastServer() *QNTXServer {
	return &QNTXServer{logger: zap.NewNop().Sugar(), held: servingStub(stubStore{})}
}

// connected is a client admitted as `as`, in the served namespace.
func connected(srv *QNTXServer, id string, as auth.Admission) *Client {
	return &Client{
		server:   srv,
		sendMsg:  make(chan interface{}, 4),
		id:       id,
		in:       auth.NamespaceDefault,
		admitted: as,
		gated:    true,
	}
}

func row(predicate string, actors ...string) *types.As {
	return &types.As{
		ID:         "AS-ROW-" + predicate,
		Subjects:   []string{"thing"},
		Predicates: []string{predicate},
		Actors:     actors,
	}
}

func TestAnAttestationReachesOnlyClientsThatMayReadIt(t *testing.T) {
	srv := broadcastServer()

	// A role whose READ line names one word, and the row carries another.
	narrow := auth.Saying(
		auth.Holding(auth.Admitted(auth.LevelPublicRegistration, auth.NamespaceDefault), "reader"),
		auth.Words{Read: []string{"observed"}, All: true},
	)
	// Above the ladder: narrowed by nothing.
	wide := auth.Admitted(auth.LevelSuper, auth.NamespaceDefault)

	held := connected(srv, "narrow", narrow)
	all := connected(srv, "wide", wide)
	srv.clients = map[*Client]bool{held: true, all: true}

	srv.sendMessageToClients("a match", "", auth.NamespaceDefault, row("secret"))

	if got := queued(held); len(got) != 0 {
		t.Errorf("a client was handed an attestation its READ lines do not name: %v", got)
	}
	if len(queued(all)) != 1 {
		t.Error("a client that may read everything was not handed the match")
	}
}

func TestAnAttestationReachesAClientWhosePredicateItIs(t *testing.T) {
	srv := broadcastServer()

	reader := auth.Saying(
		auth.Holding(auth.Admitted(auth.LevelPublicRegistration, auth.NamespaceDefault), "reader"),
		auth.Words{Read: []string{"observed"}, All: true},
	)
	held := connected(srv, "reader", reader)
	srv.clients = map[*Client]bool{held: true}

	srv.sendMessageToClients("a match", "", auth.NamespaceDefault, row("observed"))

	if len(queued(held)) != 1 {
		t.Error("a client was withheld an attestation its READ lines name")
	}
}

// "DEFAULT DENY" is per predicate and not per row: one word the reader may not
// have is enough, however many it may.
func TestOnePredicateTheReaderMayNotHaveWithholdsTheRow(t *testing.T) {
	srv := broadcastServer()

	reader := auth.Saying(
		auth.Holding(auth.Admitted(auth.LevelPublicRegistration, auth.NamespaceDefault), "reader"),
		auth.Words{Read: []string{"observed"}, All: true},
	)
	held := connected(srv, "reader", reader)
	srv.clients = map[*Client]bool{held: true}

	both := row("observed")
	both.Predicates = append(both.Predicates, "secret")

	srv.sendMessageToClients("a match", "", auth.NamespaceDefault, both)

	if got := queued(held); len(got) != 0 {
		t.Errorf("a row carrying a word the reader may not have was handed over: %v", got)
	}
}

// Below the ladder a reader sees their own rows unless a READ line said `all`.
// A push must not be the way around that.
func TestAnOwnOnlyReaderIsHandedOnlyItsOwnRows(t *testing.T) {
	srv := broadcastServer()

	// No `all`, so OwnOnly: what this actor wrote and nothing else.
	own := auth.Saying(
		auth.Holding(auth.Admitted(auth.LevelPublicRegistration, auth.NamespaceDefault), "reader"),
		auth.Words{Read: []string{"observed"}},
	)
	own.Identity = "did:key:mine"

	held := connected(srv, "own", own)
	srv.clients = map[*Client]bool{held: true}

	srv.sendMessageToClients("theirs", "", auth.NamespaceDefault, row("observed", "did:key:theirs"))
	if got := queued(held); len(got) != 0 {
		t.Errorf("an own-only reader was handed somebody else's row: %v", got)
	}

	srv.sendMessageToClients("mine", "", auth.NamespaceDefault, row("observed", "did:key:mine"))
	if len(queued(held)) != 1 {
		t.Error("an own-only reader was withheld its own row")
	}
}

// A message carrying no attestation has nothing to withhold: the daemon's
// load, a plugin's health, the queue's depth.
func TestAMessageCarryingNoAttestationIsNotGated(t *testing.T) {
	srv := broadcastServer()

	narrow := auth.Saying(
		auth.Holding(auth.Admitted(auth.LevelPublicRegistration, auth.NamespaceDefault), "reader"),
		auth.Words{Read: nil},
	)
	held := connected(srv, "narrow", narrow)
	srv.clients = map[*Client]bool{held: true}

	srv.sendMessageToClients("the daemon stopped", "", auth.NamespaceDefault, nil)

	if len(queued(held)) != 1 {
		t.Error("a message about the node was withheld from a reader it is not about")
	}
}

// A node running without auth has one caller, and nobody to keep anything from.
func TestAnUngatedClientIsHandedEverything(t *testing.T) {
	srv := broadcastServer()

	held := &Client{server: srv, sendMsg: make(chan interface{}, 4), id: "ungated", in: auth.NamespaceDefault}
	srv.clients = map[*Client]bool{held: true}

	srv.sendMessageToClients("a match", "", auth.NamespaceDefault, row("anything"))

	if len(queued(held)) != 1 {
		t.Error("a node without auth withheld a row from its one caller")
	}
}
