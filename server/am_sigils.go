package server

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/teranos/QNTX/internal/version"
	"github.com/teranos/QNTX/plugin/grpc/protocol"
	"github.com/teranos/QNTX/server/a2a"
	"github.com/teranos/QNTX/server/sigil"
	"github.com/teranos/QNTX/server/syscap"
	errors "github.com/teranos/sacred-error"
	"google.golang.org/protobuf/encoding/protojson"
)

// Am is the node about itself (ADR-039): which build runs, what it can do, and
// what one item on the status line is doing. The status line itself answers
// text for a terminal as well as JSON, so it stays a route.

// amFollowsAgentCard is which of the node's own facts fill A2A's AgentCard,
// and nothing it does not have.
func amFollowsAgentCard() *protocol.Follows {
	return &protocol.Follows{Reference: "a2a", Columns: []*protocol.Corresponds{
		{Field: "protocol.Node.name", Column: "AgentCard.name"},
		{Field: "protocol.Node.description", Column: "AgentCard.description"},
		{Field: "protocol.VersionInfo.version", Column: "AgentCard.version"},
		{Field: "protocol.Node.signa", Column: "AgentCard.skills"},
	}}
}

// amSignumName is am: being, the node, and the Agent Card that describes it.
const amSignumName = "am"

func (s *QNTXServer) amSignum() sigil.Signum {
	return sigil.Signum{
		Signum: &protocol.Signum{
			Name:    amSignumName,
			Follows: []*protocol.Follows{amFollowsAgentCard()},
			Sigils: []*protocol.Sigil{
				{
					Name:   "version",
					Does:   "Which build is running, in full: the whole commit, not the seven characters the connect frame sends.",
					Answer: "protocol.VersionInfo",
					Http:   &protocol.Endpoint{Method: http.MethodGet, Path: "/am/version"},
				},
				{
					// "am node same thing": am node answers the Agent Card the
					// asker would be given.
					Name: "node",
					Does: "What the node says of itself, as the A2A agent card the asker would be given, read through the pinned spec, and what it leaves empty that the spec requires.",
					Gives: []*protocol.Field{
						{Name: "card", Says: "The card, as the pinned A2A spec shapes it.", Message: a2a.AgentCard},
						{Name: "missing", Says: "Every field the card leaves empty that the spec requires, by its path."},
					},
					Http: &protocol.Endpoint{Method: http.MethodGet, Path: "/am/node"},
				},
				{
					Name:   "syscap",
					Does:   "What this build can do: the storage backend and the parser it was built against. What the connect frame pushes, asked for.",
					Answer: "protocol.SystemCapabilitiesMessage",
					Http:   &protocol.Endpoint{Method: http.MethodGet, Path: "/am/syscap"},
				},
				{
					Name:  "item",
					Does:  "What one item on the status line is doing, in full: news by its id, a failing handler or watcher with its exact error, or a plugin's health.",
					Takes: []*protocol.Param{{Name: "name", Required: true, Says: "The item, by the name the status line gives it."}},
					Gives: []*protocol.Field{{Name: "name", Says: "The item, and whatever it carries in full."}},
					Http:  &protocol.Endpoint{Method: http.MethodGet, Path: "/am/statusline/{name}"},
				},
				{
					Name: "ground",
					Does: "What this node does for Ground, for whoever asks: the pushes and dispatched runs it waits on in their place, what it concluded, and how many conclusions it left on the status line since it began.",
					Gives: []*protocol.Field{
						{Name: "started", Says: "When this process began answering, as RFC 3339. What it left before then went with the process that left it."},
						{Name: "left", Says: "How many conclusions it left on the status line for the caller since then."},
						{Name: "watches", Says: "The standing watchers that reach ci.watch: each one's id, its name, and the predicates Ground attests that set it off."},
						{Name: "news", Says: "What ci.watch said for the caller, newest first: each wait and each conclusion, when it was left, until when the status line carries it, and what the item holds in full."},
						{Name: "failed", Says: "What ci.watch could not do in the last day, newest first: when, the exact error, and which run of it."},
						{Name: "ug", Says: "What the node sees of ug: when the caller's tmux bar last asked for the status line, how often since this process began and by the minute over the last hour; each session whose status line posted a usage reading in the last day, with its first, its latest and its readings by the hour; and each window ug read, with what it read over that day."},
					},
					Http: &protocol.Endpoint{Method: http.MethodGet, Path: "/am/ground"},
				},
			},
		},
		Answers: map[string]sigil.Answer{
			"version": func(context.Context, sigil.Sent) (any, *protocol.Refusal) { return versionInfo(version.Get()), nil },
			"node":    s.amNode,
			"syscap": func(context.Context, sigil.Sent) (any, *protocol.Refusal) {
				return capabilities(syscap.Get(s.store)), nil
			},
			"ground": s.amGround,
			"item": func(ctx context.Context, sent sigil.Sent) (any, *protocol.Refusal) {
				return s.statusLineHandler.item(ctx, sent["name"])
			},
		},
	}
}

// amNode is what am node answers: the card, and what it lacks.
type amNode struct {
	Card    json.RawMessage `json:"card"`
	Missing []string        `json:"missing"`
}

func (s *QNTXServer) amNode(ctx context.Context, _ sigil.Sent) (any, *protocol.Refusal) {
	caller := sigil.Caller(ctx)
	if caller == nil {
		return nil, &protocol.Refusal{Why: sigil.Failed, Says: "the card names where its caller reached the node, and this asking carried no request"}
	}
	card, err := s.a2aCard(caller).Message()
	if err != nil {
		return nil, &protocol.Refusal{Why: sigil.Failed, Says: err.Error()}
	}
	body, err := protojson.Marshal(card.Interface())
	if err != nil {
		return nil, &protocol.Refusal{Why: sigil.Failed, Says: errors.Wrap(err, "the card did not marshal").Error()}
	}
	missing := a2a.Missing(card)
	if missing == nil {
		missing = []string{}
	}
	return amNode{Card: body, Missing: missing}, nil
}

// versionInfo is the build as am version answers it.
func versionInfo(v version.Info) *protocol.VersionInfo {
	return &protocol.VersionInfo{CommitHash: v.CommitHash, BuildTime: v.BuildTime, Version: v.Version, GoVersion: v.GoVersion, Platform: v.Platform}
}

// capabilities is what this build can do, as am syscap answers it.
func capabilities(m syscap.Message) *protocol.SystemCapabilitiesMessage {
	return &protocol.SystemCapabilitiesMessage{
		Type: m.Type, Store: m.Store,
		StorageBackend: m.StorageBackend, StorageOptimized: m.StorageOptimized, StorageVersion: m.StorageVersion,
		ParserBackend: m.ParserBackend, ParserOptimized: m.ParserOptimized, ParserVersion: m.ParserVersion, ParserSize: m.ParserSize,
	}
}
