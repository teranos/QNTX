package server

import (
	"context"
	"net/http"

	"github.com/teranos/QNTX/internal/version"
	"github.com/teranos/QNTX/plugin/grpc/protocol"
	"github.com/teranos/QNTX/server/sigil"
	"github.com/teranos/QNTX/server/syscap"
)

// Am is the node about itself (ADR-039): which build runs, what it can do, and
// what one item on the status line is doing. The status line itself answers
// text for a terminal as well as JSON, so it stays a route.

func (s *QNTXServer) amSignum() sigil.Signum {
	return sigil.Signum{
		Signum: &protocol.Signum{
			Name: "am",
			Sigils: []*protocol.Sigil{
				{
					Name: "version",
					Does: "Which build is running, in full: the whole commit, not the seven characters the connect frame sends.",
					Gives: []*protocol.Field{
						{Name: "version", Says: "The version tag."},
						{Name: "commit_hash", Says: "The whole commit."},
						{Name: "build_time", Says: "When it was built."},
						{Name: "go_version", Says: "The Go it was built with."},
						{Name: "platform", Says: "The OS and architecture."},
					},
					Http: &protocol.Endpoint{Method: http.MethodGet, Path: "/am/version"},
				},
				{
					Name: "syscap",
					Does: "What this build can do: the storage backend and the parser it was built against. What the connect frame pushes, asked for.",
					Gives: []*protocol.Field{
						{Name: "type", Says: "system_capabilities."},
						{Name: "store", Says: "The store this node was started with."},
						{Name: "storage_backend", Says: "rust or go: which storage implementation is active."},
						{Name: "storage_optimized", Says: "Whether it is the Rust SQLite one."},
						{Name: "storage_version", Says: "The ats-sqlite library's version."},
						{Name: "parser_backend", Says: "wasm or go: which parser implementation is active."},
						{Name: "parser_optimized", Says: "Whether it is ats through WASM."},
						{Name: "parser_version", Says: "The ats version, through WASM."},
						{Name: "parser_size", Says: "The WASM module's size."},
					},
					Http: &protocol.Endpoint{Method: http.MethodGet, Path: "/am/syscap"},
				},
				{
					Name:  "item",
					Does:  "What one item on the status line is doing, in full: news by its id, a failing handler or watcher with its exact error, or a plugin's health.",
					Takes: []*protocol.Param{{Name: "name", Required: true, Says: "The item, by the name the status line gives it."}},
					Gives: []*protocol.Field{{Name: "name", Says: "The item, and whatever it carries in full."}},
					Http:  &protocol.Endpoint{Method: http.MethodGet, Path: "/am/statusline/{name}"},
				},
			},
		},
		Answers: map[string]sigil.Answer{
			"version": func(context.Context, sigil.Sent) (any, *protocol.Refusal) { return version.Get(), nil },
			"syscap":  func(context.Context, sigil.Sent) (any, *protocol.Refusal) { return syscap.Get(s.store), nil },
			"item": func(ctx context.Context, sent sigil.Sent) (any, *protocol.Refusal) {
				return s.statusLineHandler.item(ctx, sent["name"])
			},
		},
	}
}
