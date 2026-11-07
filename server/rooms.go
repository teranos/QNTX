package server

import (
	"context"
	"net/http"
	"slices"
	"strconv"
	"strings"

	grpcplugin "github.com/teranos/QNTX/plugin/grpc"
	"github.com/teranos/QNTX/plugin/grpc/protocol"
	"github.com/teranos/QNTX/server/auth"
	"github.com/teranos/QNTX/server/sigil"
)

// "the point of rooms is to implement the switch.ai way of doing rooms and to win a hackathon"
// "rooms will be plugin provided"
// "is the most elegant way of exposing Room, perhaps via the MCP say ?"
// "so my ChatGPT can also reach into a Room if wanted"

// roomsPath is where the node answers for the rooms its plugins hold (ADR-057).
const roomsPath = "/api/rooms"

func (s *QNTXServer) roomsSignum() sigil.Signum {
	provider := &protocol.Param{Name: "provider", Says: "The plugin that holds the room, by its name. Not sent, the one plugin that holds rooms."}
	room := &protocol.Param{Name: "room", Required: true, Says: "The room, as its provider names it."}
	thread := &protocol.Param{Name: "thread", Says: "The thread inside the room to answer in. Empty is the room itself."}
	return sigil.Signum{
		Signum: &protocol.Signum{
			Name:        "rooms",
			Description: "The rooms the node's plugins hold (ADR-057): what is said in each, and who is woken there.",
			Tags:        []string{"rooms", "agents"},
			Oasf:        []string{"ai_ml_engineering/agent_orchestration/multi_agent_coordination"},
			Sigils: []*protocol.Sigil{
				{
					Name:   "list",
					Does:   "Every room a plugin holds.",
					Takes:  []*protocol.Param{provider},
					Answer: "protocol.ListRoomsResponse",
					Http:   &protocol.Endpoint{Method: http.MethodGet, Path: roomsPath},
				},
				{
					Name:   "get",
					Does:   "One room: what it is, what it is connected to, and the agents in it.",
					Takes:  []*protocol.Param{room, provider},
					Answer: "protocol.GetRoomResponse",
					Http:   &protocol.Endpoint{Method: http.MethodGet, Path: roomsPath + "/{room}"},
				},
				{
					Name:   "read",
					Does:   "What was said in a room, oldest first.",
					Takes:  []*protocol.Param{room, provider, {Name: "limit", Kind: sigil.Count, Says: "The most to answer."}},
					Answer: "protocol.ReadRoomResponse",
					Http:   &protocol.Endpoint{Method: http.MethodGet, Path: roomsPath + "/{room}/read"},
				},
				{
					Name:   "post",
					Does:   "Says something in a room, as whoever asks: an agent by its token, a person as no agent.",
					Takes:  []*protocol.Param{room, provider, {Name: "body", Required: true, Says: "What is said."}, thread},
					Answer: "protocol.PostToRoomResponse",
					Http:   &protocol.Endpoint{Method: http.MethodPost, Path: roomsPath + "/{room}/post"},
				},
				{
					Name:   "wake",
					Does:   "Wakes agents in a room with what is said to them, and answers whether each wake arrives.",
					Takes:  []*protocol.Param{room, provider, {Name: "agents", Required: true, Says: "The agents to wake, by the names the room holds them under, comma-separated."}, {Name: "says", Says: "What they are woken with."}, thread},
					Answer: "protocol.WakeInRoomResponse",
					Http:   &protocol.Endpoint{Method: http.MethodPost, Path: roomsPath + "/{room}/wake"},
				},
			},
		},
		Answers: map[string]sigil.Answer{
			"list": s.roomsList, "get": s.roomsGet, "read": s.roomsRead, "post": s.roomsPost, "wake": s.roomsWake,
		},
	}
}

// roomsOf is the plugin that holds rooms under name, or the one plugin that
// holds rooms when name is empty.
func (s *QNTXServer) roomsOf(name string) (string, protocol.RoomsServiceClient, *protocol.Refusal) {
	pm, err := s.getPluginManager()
	if err != nil {
		return "", nil, &protocol.Refusal{Why: sigil.Failed, Says: "no plugin holds rooms on this node: " + err.Error()}
	}
	var holders []string
	clients := map[string]protocol.RoomsServiceClient{}
	for _, p := range pm.GetAllPlugins() {
		proxy, ok := p.(*grpcplugin.ExternalDomainProxy)
		if !ok || !proxy.IsRoomsProvider() {
			continue
		}
		holder := p.Metadata().Name
		holders = append(holders, holder)
		clients[holder] = proxy.RoomsServiceClient()
	}
	slices.Sort(holders)
	if len(holders) == 0 {
		return "", nil, &protocol.Refusal{Why: sigil.NotFound, Says: "no plugin holds rooms on this node: a plugin holds them by declaring rooms_provider"}
	}
	if name == "" {
		if len(holders) > 1 {
			return "", nil, &protocol.Refusal{Why: sigil.Invalid, Param: "provider", Says: "several plugins hold rooms: " + strings.Join(holders, ", ") + "; name one"}
		}
		name = holders[0]
	}
	client, held := clients[name]
	if !held {
		return "", nil, &protocol.Refusal{Why: sigil.NotFound, Param: "provider", Says: name + " holds no rooms; " + strings.Join(holders, ", ") + " does"}
	}
	return name, client, nil
}

// roomsBy is who asks, as a room holds them: an agent by its token, and a
// person, or a connector a person said yes to, as no agent.
func roomsBy(ctx context.Context) *protocol.RoomAgent {
	admitted, known := auth.AdmissionFrom(ctx)
	if !known || admitted.Grant == nil {
		return nil
	}
	return &protocol.RoomAgent{Did: admitted.Grant.DID, Name: admitted.Grant.Label}
}

func roomsFailed(err error) *protocol.Refusal {
	return &protocol.Refusal{Why: sigil.Failed, Says: err.Error()}
}

func (s *QNTXServer) roomsList(ctx context.Context, sent sigil.Sent) (any, *protocol.Refusal) {
	provider, rooms, refused := s.roomsOf(sent["provider"])
	if refused != nil {
		return nil, refused
	}
	answer, err := rooms.ListRooms(ctx, &protocol.ListRoomsRequest{Provider: provider, By: roomsBy(ctx)})
	if err != nil {
		return nil, roomsFailed(err)
	}
	return answer, nil
}

func (s *QNTXServer) roomsGet(ctx context.Context, sent sigil.Sent) (any, *protocol.Refusal) {
	provider, rooms, refused := s.roomsOf(sent["provider"])
	if refused != nil {
		return nil, refused
	}
	answer, err := rooms.GetRoom(ctx, &protocol.GetRoomRequest{Provider: provider, By: roomsBy(ctx), Room: sent["room"]})
	if err != nil {
		return nil, roomsFailed(err)
	}
	return answer, nil
}

func (s *QNTXServer) roomsRead(ctx context.Context, sent sigil.Sent) (any, *protocol.Refusal) {
	provider, rooms, refused := s.roomsOf(sent["provider"])
	if refused != nil {
		return nil, refused
	}
	limit := 0
	if sent["limit"] != "" {
		var err error
		if limit, err = strconv.Atoi(sent["limit"]); err != nil {
			return nil, &protocol.Refusal{Why: sigil.Invalid, Param: "limit", Says: "limit is a whole number, and " + sent["limit"] + " is not"}
		}
	}
	answer, err := rooms.ReadRoom(ctx, &protocol.ReadRoomRequest{Provider: provider, By: roomsBy(ctx), Room: sent["room"], Limit: int32(limit)})
	if err != nil {
		return nil, roomsFailed(err)
	}
	return answer, nil
}

func (s *QNTXServer) roomsPost(ctx context.Context, sent sigil.Sent) (any, *protocol.Refusal) {
	provider, rooms, refused := s.roomsOf(sent["provider"])
	if refused != nil {
		return nil, refused
	}
	answer, err := rooms.PostToRoom(ctx, &protocol.PostToRoomRequest{Provider: provider, By: roomsBy(ctx), Room: sent["room"], Body: sent["body"], Thread: sent["thread"]})
	if err != nil {
		return nil, roomsFailed(err)
	}
	return answer, nil
}

func (s *QNTXServer) roomsWake(ctx context.Context, sent sigil.Sent) (any, *protocol.Refusal) {
	provider, rooms, refused := s.roomsOf(sent["provider"])
	if refused != nil {
		return nil, refused
	}
	var agents []*protocol.RoomAgent
	for _, name := range strings.Split(sent["agents"], ",") {
		if name = strings.TrimSpace(name); name != "" {
			agents = append(agents, &protocol.RoomAgent{Name: name})
		}
	}
	if len(agents) == 0 {
		return nil, &protocol.Refusal{Why: sigil.Invalid, Param: "agents", Says: "name at least one agent to wake"}
	}
	answer, err := rooms.WakeInRoom(ctx, &protocol.WakeInRoomRequest{Provider: provider, By: roomsBy(ctx), Room: sent["room"], Agents: agents, Says: sent["says"], Thread: sent["thread"]})
	if err != nil {
		return nil, roomsFailed(err)
	}
	return answer, nil
}
