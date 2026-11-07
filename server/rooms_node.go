package server

import (
	"context"
	"io"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/teranos/QNTX/internal/sacred"
	"github.com/teranos/QNTX/plugin/grpc/protocol"
	"github.com/teranos/QNTX/server/sigil"
	errors "github.com/teranos/sacred-error"
)

// "i want all Node Agents to be in the Node Room"

// nodeRoomName is the room of this node: the provider holds it under this name,
// and the node says it is of this node (RoomOf.node), which the provider does
// not keep (rooms.proto).
const nodeRoomName = "Node Room"

// roomOfNode says the Node Room is this node's.
func (s *QNTXServer) roomOfNode(room *protocol.Room) {
	if room == nil || room.GetName() != nodeRoomName || s.nodeDID == nil {
		return
	}
	room.Of = &protocol.RoomOf{Of: &protocol.RoomOf_Node{Node: s.nodeDID.DID}}
}

// nodeAgentIn is the harness the Node Agent runs in under name, when am.toml
// names it there: a room's claude, agy or codex is the Node Agent in it.
func (s *QNTXServer) nodeAgentIn(name string) (*harness, bool) {
	if s.nodeAgent == nil {
		return nil, false
	}
	for _, h := range s.harnesses() {
		if h.name == name && h.named() {
			return h, true
		}
	}
	return nil, false
}

// nodeAgentRuns is the model and effort the Node Agent runs at in harness, as
// am.toml names them; a harness with no effort of its own says none.
func (s *QNTXServer) nodeAgentRuns(harness string) (model, effort string) {
	root := s.deps.cfg.Agent.Root
	switch harness {
	case agentLineClaude:
		return root.Model, root.Effort
	case agentLinePi:
		return root.Pi.Model, root.Pi.Thinking
	case agentLineCodex:
		return root.Codex.Model, ""
	case agentLineAgy:
		return root.Agy.Model, ""
	}
	return "", ""
}

// roomAgentAs fills in a room's agent that is the Node Agent: its DID, its
// harness, and the model and effort it runs at, so one read of the room says
// what each agent is. One the provider holds under another DID is not it.
func (s *QNTXServer) roomAgentAs(agent *protocol.RoomAgent) {
	if s.nodeAgent == nil || (agent.GetDid() != "" && agent.GetDid() != s.nodeAgent.did) {
		return
	}
	h, is := s.nodeAgentIn(agent.GetName())
	if !is {
		return
	}
	agent.Did = s.nodeAgent.did
	agent.Harness = h.name
	agent.Model, agent.Effort = s.nodeAgentRuns(h.name)
}

// saidText is the answer a Node Agent's turn gave, whichever harness ran it.
func saidText(said *protocol.NodeAgentSaid) string {
	switch {
	case said.GetClaude() != nil:
		return said.GetClaude().GetAnswer()
	case said.GetPi() != nil:
		return said.GetPi().GetAnswer()
	case said.GetCodex() != nil:
		return said.GetCodex().GetAnswer()
	case said.GetAgy() != nil:
		return said.GetAgy().GetAnswer()
	}
	return ""
}

// nodeAgentsInNodeRoom is each agent of room that is the Node Agent, when room
// is the Node Room: a namesake elsewhere, or a harness the room does not hold,
// is no Node Agent of it.
func (s *QNTXServer) nodeAgentsInNodeRoom(ctx context.Context, provider string, rooms protocol.RoomsServiceClient, room string) map[string]bool {
	got, err := rooms.GetRoom(ctx, &protocol.GetRoomRequest{Provider: provider, By: roomsBy(ctx), Room: room})
	if err != nil || got.GetRoom().GetName() != nodeRoomName {
		return nil
	}
	in := map[string]bool{}
	for _, agent := range got.GetAgents() {
		if _, is := s.nodeAgentIn(agent.GetName()); is {
			in[agent.GetName()] = true
		}
	}
	return in
}

// wakeNodeAgents runs a turn of each woken agent that is the Node Agent in the
// Node Room, as the waker asked it, and posts its answer into the room as that
// agent. A wake the provider declined runs nothing. A turn takes as long as it
// takes, so each runs on the node's own lifetime and the wake answers at once;
// the answer arrives in the room.
func (s *QNTXServer) wakeNodeAgents(ctx context.Context, provider string, rooms protocol.RoomsServiceClient, room, thread, says string, answer *protocol.WakeInRoomResponse) {
	caller := sigil.Caller(ctx)
	if caller == nil {
		return
	}
	inRoom := s.nodeAgentsInNodeRoom(ctx, provider, rooms, room)
	waker := "a person at ROOT"
	if by := roomsBy(ctx); by != nil && by.GetName() != "" {
		waker = by.GetName()
	}
	for _, woken := range answer.GetWoken() {
		if !inRoom[woken.GetAgent().GetName()] || woken.GetReach() == protocol.RoomReach_ROOM_REACH_DECLINED {
			continue
		}
		h, _ := s.nodeAgentIn(woken.GetAgent().GetName())
		woken.Reach = protocol.RoomReach_ROOM_REACH_ARRIVES
		name := h.name
		sacred.Go("rooms.wake."+name, func() {
			s.answerInRoom(caller, provider, rooms, room, thread, name, waker, says)
		})
	}
}

// roomFilesMost is the most a woken Node Agent's files carry into a room
// together, names and types counted: under the 4 MiB a provider's gRPC server
// takes by default (pyre's does). roomFilesMany is the most files.
const (
	roomFilesMost = 3 << 20
	roomFilesMany = 10
)

// answerFiles is what a turn wrote into dir, sent with its answer, up to
// roomFilesMost and roomFilesMany; what does not fit is named in the log. A file
// is read through dir, so a link out of it is never followed.
func (s *QNTXServer) answerFiles(dir string) []*protocol.RoomFile {
	root, err := os.OpenRoot(dir)
	if err != nil {
		return nil
	}
	defer root.Close()
	entries, _ := os.ReadDir(dir)
	var files []*protocol.RoomFile
	left := roomFilesMost
	for _, entry := range entries {
		file, err := s.answerFile(root, entry.Name(), left)
		if err != nil {
			s.logger.Warnw("A woken Node Agent's file was not sent into its room", "file", entry.Name(), "error", err)
			continue
		}
		if file == nil {
			continue
		}
		if len(files) == roomFilesMany {
			s.logger.Warnw("A woken Node Agent wrote more files than go into a room", "most", roomFilesMany)
			break
		}
		left -= len(file.Data) + len(file.Name) + len(file.Type)
		files = append(files, file)
	}
	return files
}

// answerFile is one regular file of root, when it fits in left; nil, nil when
// it is no regular file.
func (s *QNTXServer) answerFile(root *os.Root, name string, left int) (*protocol.RoomFile, error) {
	f, err := root.Open(name)
	if err != nil {
		return nil, errors.Wrapf(err, "%s did not open", name)
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return nil, errors.Wrapf(err, "%s did not stat", name)
	}
	if !info.Mode().IsRegular() {
		return nil, nil
	}
	kind := mime.TypeByExtension(filepath.Ext(name))
	room := left - len(name) - len(kind)
	if info.Size() > int64(room) {
		return nil, errors.Newf("%s is %d bytes, and %d are left", name, info.Size(), room)
	}
	data, err := io.ReadAll(io.LimitReader(f, int64(room)+1))
	if err != nil {
		return nil, errors.Wrapf(err, "%s was not read", name)
	}
	if len(data) > room {
		return nil, errors.Newf("%s grew past the %d bytes left", name, room)
	}
	if kind == "" {
		kind = http.DetectContentType(data)
	}
	return &protocol.RoomFile{Name: name, Type: kind, Data: data}, nil
}

// answerInRoom runs one Node Agent turn and posts what it answered, or why it
// did not, into the room as that agent, with the files it wrote for the room.
func (s *QNTXServer) answerInRoom(caller *http.Request, provider string, rooms protocol.RoomsServiceClient, room, thread, harness, waker, says string) {
	lifetime := s.lifetime()
	turn := "In the " + nodeRoomName + ", " + waker + " woke you (" + harness + ") with:\n\n" + says +
		"\n\nWhat you answer is posted into the room as " + harness + "."
	dir, err := os.MkdirTemp("", "room-answer-")
	if err == nil {
		defer os.RemoveAll(dir)
		turn += " A file you write into " + dir + " is posted with it."
	}
	said, refused := s.nodeAgentSay(sigil.WithCaller(lifetime, caller), sigil.Sent{"harness": harness, "says": turn})
	body := ""
	if refused != nil {
		// What refused stays in the node's log; the room is told only that it did.
		s.logger.Warnw("A woken Node Agent did not answer", "harness", harness, "room", room, "refused", refused.GetSays())
		body = harness + " was woken and did not answer"
	} else if answered, ok := said.(*protocol.NodeAgentSaid); ok {
		body = strings.TrimSpace(saidText(answered))
	}
	var files []*protocol.RoomFile
	if dir != "" && refused == nil {
		files = s.answerFiles(dir)
	}
	if body == "" && len(files) == 0 {
		body = harness + " was woken and answered nothing"
	}
	if _, err := rooms.PostToRoom(lifetime, &protocol.PostToRoomRequest{Provider: provider, By: &protocol.RoomAgent{Did: s.nodeAgent.did, Harness: harness, Name: harness}, Room: room, Body: body, Thread: thread, Files: files}); err != nil {
		s.logger.Errorw("A woken Node Agent's answer was not posted into its room", "harness", harness, "room", room, "error", err)
	}
}
