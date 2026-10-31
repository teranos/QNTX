package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/teranos/QNTX/internal/claudecode"
	"github.com/teranos/QNTX/plugin/grpc/protocol"
	errors "github.com/teranos/sacred-error"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

// The session while it runs: the turn's stream, tailed, each line as the
// harness printed it (protocol.SessionEvent). The transcript (transcripts.go)
// is the same session after the fact, and this writes no row of it.

// "i still want the transcript reader"
// "and i want something else to do the live session"

// HandleNodeAgentLive answers GET /api/agent/live?harness=claude&mark=N as
// server-sent events: every line of the turn going from mark on, as it lands.
func (s *QNTXServer) HandleNodeAgentLive(w http.ResponseWriter, r *http.Request) {
	if s.nodeAgent == nil {
		writeError(w, http.StatusNotFound, s.thereIsNoNodeAgent().Says)
		return
	}
	s.handleLive(w, r, s.nodeAgent, "the Node Agent")
}

// HandleElementLive is the same for the element's agent, at
// GET /api/elements/{element}/live (ADR-054): "AGENT PER ELEMENT, EACH ELEMENT
// IS ITS OWN CONVERSATION".
func (s *QNTXServer) HandleElementLive(w http.ResponseWriter, r *http.Request) {
	element := r.PathValue("element")
	if element == "" {
		writeError(w, http.StatusBadRequest, "no element is named")
		return
	}
	agent, refused := s.holdElementAgent(element)
	if refused != nil {
		writeError(w, http.StatusNotFound, refused.Says)
		return
	}
	s.handleLive(w, r, agent, agent.called)
}

// handleLive tails the turn agent is in, as the end event says: whether the
// turn still prints, and the mark to ask from next.
func (s *QNTXServer) handleLive(w http.ResponseWriter, r *http.Request, agent *rootAgent, called string) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	harness := r.URL.Query().Get("harness")
	if harness != agentLineClaude {
		// Pi prints to a pipe the node reads (internal/pi/run.go), and Codex
		// has no stream here: nothing is on disk to tail.
		writeError(w, http.StatusBadRequest, "live tails the stream a turn prints to, and only Claude Code prints to one: harness is claude, and "+strconv.Quote(harness)+" was sent")
		return
	}
	mark := int64(0)
	if from := r.URL.Query().Get("mark"); from != "" {
		var err error
		if mark, err = strconv.ParseInt(from, 10, 64); err != nil || mark < 0 {
			writeError(w, http.StatusBadRequest, "mark is where in the stream to read from, a whole number, and "+strconv.Quote(from)+" is not")
			return
		}
	}
	going := agent.in(s.claudeHarness()).answering.Load()
	if going == nil || going.stream == "" {
		writeError(w, http.StatusNotFound, called+" is not in a turn in Claude Code, so there is no stream to tail")
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "streaming not supported")
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	flusher.Flush()

	ctx, stop := context.WithCancel(r.Context())
	defer stop()
	end, live, err := claudecode.Tail(ctx, going.stream, mark, func(line []byte, end int64) {
		for _, event := range sessionEventsOf(going.session, harness, line, end, time.Now()) {
			if err := writeSSEMessage(w, "", event); err != nil {
				s.logger.Warnw("the live session ended early", "session", going.session, "stream", going.stream, "error", err)
				stop()
				return
			}
		}
		flusher.Flush()
	})
	if err != nil {
		s.logger.Errorw("the live session did not read on", "session", going.session, "stream", going.stream, "error", err)
	}
	if err := writeSSEMessage(w, "end", &protocol.SessionEvents{Live: live, Mark: end}); err != nil {
		s.logger.Warnw("the live session's end was not written", "session", going.session, "error", err)
	}
	flusher.Flush()
}

// writeSSEMessage writes one message as an event, named when name is not empty,
// in the form every sigil answers in (signa.go: proto names, zero as zero).
func writeSSEMessage(w http.ResponseWriter, name string, message proto.Message) error {
	data, err := protojson.MarshalOptions{UseProtoNames: true, EmitUnpopulated: true}.Marshal(message)
	if err != nil {
		return errors.Wrap(err, "the event did not encode")
	}
	if name != "" {
		if _, err := fmt.Fprintf(w, "event: %s\n", name); err != nil {
			return errors.Wrapf(err, "the event %s was not written", name)
		}
	}
	if written, err := fmt.Fprintf(w, "data: %s\n\n", data); err != nil {
		return errors.Wrapf(err, "the event was not written: %d of %d bytes went out", written, len(data)+len("data: \n\n"))
	}
	return nil
}

// sessionEventsOf is what one line of the stream says happened, each in the
// words every harness has one of, and the line itself whole as raw. A line
// that is no message at all is one "other" event carrying it.
func sessionEventsOf(session, harness string, line []byte, mark int64, readAt time.Time) []*protocol.SessionEvent {
	var m claudecode.Message
	if err := json.Unmarshal(line, &m); err != nil {
		return []*protocol.SessionEvent{{Session: session, Harness: harness, At: readAt.UTC().Format(time.RFC3339Nano), Kind: "other", Raw: string(line), Mark: mark}}
	}
	at := readAt.UTC().Format(time.RFC3339Nano)
	if stamped, err := time.Parse(time.RFC3339, m.Timestamp); err == nil {
		at = stamped.UTC().Format(time.RFC3339Nano)
	}
	one := func(kind, text string) *protocol.SessionEvent {
		return &protocol.SessionEvent{Session: session, Harness: harness, At: at, Kind: kind, Text: text, Raw: string(line), Mark: mark}
	}
	switch m.Type {
	case "system":
		if m.Subtype == "init" {
			return []*protocol.SessionEvent{one("started", m.Model)}
		}
	case "assistant", "user":
		var events []*protocol.SessionEvent
		for _, block := range m.Blocks() {
			switch block.Type {
			case "text":
				events = append(events, one("text", block.Text))
			case "thinking":
				events = append(events, one("thinking", block.Thinking))
			case "tool_use":
				event := one("tool_use", block.Name)
				event.Tool, event.ToolUseId, event.Input = block.Name, block.ID, string(block.Input)
				events = append(events, event)
			case "tool_result":
				event := one("tool_result", block.Said())
				event.ToolUseId = block.ToolUseID
				events = append(events, event)
			}
		}
		if len(events) > 0 {
			return events
		}
	case "stream_event":
		if delta := textDeltaOf(m.Event); delta != "" {
			return []*protocol.SessionEvent{one("delta", delta)}
		}
	case "result":
		if m.IsError {
			return []*protocol.SessionEvent{one("failed", m.Subtype+": "+m.Result)}
		}
		return []*protocol.SessionEvent{one("ended", m.Result)}
	}
	return []*protocol.SessionEvent{one("other", "")}
}

// textDeltaOf is the text a content_block_delta carries, or empty when the
// event is no text delta.
func textDeltaOf(event json.RawMessage) string {
	var e struct {
		Type  string `json:"type"`
		Delta struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"delta"`
	}
	if json.Unmarshal(event, &e) != nil || e.Type != "content_block_delta" || e.Delta.Type != "text_delta" {
		return ""
	}
	return e.Delta.Text
}
