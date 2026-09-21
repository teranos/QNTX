package server

import (
	"net/http"

	"github.com/teranos/QNTX/plugin/grpc/protocol"
	"github.com/teranos/QNTX/server/sigil"
)

// Db is what the record cost, as a signum (ADR-039). Its path, its tool and
// its operation in the served document are all read from here; the function
// that answers is in db.go.
//
// The numbers are not on this box. internal/measure sends them through the
// Sentry client a DSN starts, and a gauge is a number in flight and not a
// table — nothing here keeps what it sent. So a screen wanting a week of them
// has to ask where they landed, and this is the node asking on its behalf,
// with a credential a caller never sees and never holds.

// drawable is every number this answers for.
//
// The credential it asks with can read the whole organisation. Naming the
// numbers here is what keeps a sigil for the node's own measurements from
// being a way to read everything else that ships to the same place — and the
// sigil refuses anything not on this list before the function is reached.
func drawable() []string {
	return []string{
		"qntx.store.requests",
		"qntx.store.files",
		"qntx.store.unsent",
		"qntx.store.sent.rows",
		"qntx.store.compacted",
		"qntx.store.compacted.files",
		"qntx.store.compacted.bytes",
		"qntx.store.taken_in.rows",
		"qntx.attestations.written",
		"qntx.query.took",
		"qntx.query.returned",
		"qntx.boot.subsystem.took",
	}
}

func (s *QNTXServer) dbSignum() sigil.Signum {
	return sigil.Signum{
		Signum: &protocol.Signum{
			Name: "db",
			Sigils: []*protocol.Sigil{
				{
					Name: "series",
					Does: "One of the node's own numbers over time, read back from where it was sent.",
					Takes: []*protocol.Param{
						{Name: "metric", Required: true, OneOf: drawable(),
							Says: "Which number, by the name internal/measure gives it."},
						{Name: "by",
							Says: "What the lines split on: store for the namespace, request for the kind of request. Naming none is one line for all of it."},
						{Name: "since",
							Says: "How far back: 24h, 7d, 14d. Naming none is 24h."},
						{Name: "every",
							Says: "How wide one bucket is: 1h, 1d. Naming none is 1h."},
					},
					Gives: []*protocol.Field{
						{Name: "metric", Says: "The number that was asked about."},
						{Name: "since", Says: "The window that was read."},
						{Name: "every", Says: "The bucket the readings are in."},
						{Name: "by", Says: "What the lines were split on, empty when nothing was."},
						{Name: "lines", Says: "One per value: what it is of, and a reading per bucket — when, what the numbers in it came to, and how many there were."},
					},
					Http: &protocol.Endpoint{Method: http.MethodGet, Path: "/api/db/series"},
				},
			},
		},
		Answers: map[string]sigil.Answer{"series": s.dbSeries},
	}
}
