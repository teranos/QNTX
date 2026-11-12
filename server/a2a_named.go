package server

import (
	"context"
	"slices"
	"strings"
	"time"

	a2asdk "github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2asrv"
	"github.com/teranos/QNTX/server/namespaces"
	"github.com/teranos/QNTX/server/statusline"
	errors "github.com/teranos/sacred-error"
)

// "AGENTS CAN TALK TO EACH OTHER BY THEIR NAME"
// "AND GROUND DELIVERS IT VIA UG AND STATUSLINE"
// "A SESSION THATS SENDING TO ANOTHER SESSION BY NAME IS TOLD ABOUT THE MESSAGE BEING RECEIVED OR NOT, AND WE DONT INVENT HERE, WE TAKE THE A2A CONVENTIONS, WE TAKE THE SDK SERIOUSLY"
//
// A name an agent holds (i name) is a tenant: SendMessage at it is a Task of
// that agent. The node runs no turn for it. What was said is left on the
// status row as news for the agent, where ug sees it and writes it into
// ground's store (ADR-059). The Task is the delivery: completed when a ground
// took it from the row, failed when the row let go of it untaken.

// saidToANameContext is what the first line of a Task said to a named agent
// is in beside its task: no turn of the node stands behind it, so a node that
// stopped does not fail it for one (settleTasksNoTurnLeft).
const saidToANameContext = "said:to-a-name"

// toldWithin is how long what was said to a name stays on the status row for
// a ground to take: the row's own hold.
var toldWithin = statusline.NewsHold

// agentNamedAt is the agent served at tenant by the name it holds. A
// namespace by that name is the namespace's: a name is a tenant only where no
// namespace is.
func (s *QNTXServer) agentNamedAt(tenant string) (agentName, bool, error) {
	if s.held == nil || strings.TrimSpace(tenant) == "" {
		return agentName{}, false, nil
	}
	var notServed namespaces.NotServed
	if _, err := s.optedIntoAgent(tenant); !errors.As(err, &notServed) {
		return agentName{}, false, nil
	}
	names, err := s.agentNames()
	if err != nil {
		return agentName{}, false, errors.Wrapf(err, "whether an agent holds the name %s was not read", tenant)
	}
	at := slices.IndexFunc(names, func(kept agentName) bool { return strings.EqualFold(kept.name, tenant) })
	if at < 0 {
		return agentName{}, false, nil
	}
	return names[at], true, nil
}

// holdsAName is whether an agent is served at tenant by its name.
func (s *QNTXServer) holdsAName(tenant string) bool {
	_, held, err := s.agentNamedAt(tenant)
	return err == nil && held
}

// namedTaskAgent is the agent that holds a name, its Tasks in system, where
// its name is kept.
func (s *QNTXServer) namedTaskAgent(named agentName) taskAgent {
	return taskAgent{taskLines: s.systemTasks(named.did), called: named.name, named: true}
}

// saidToAName is the Task of what was said to a named agent: submitted, left
// on the status row, and then received or not.
func (e turnExecutor) saidToAName(ctx context.Context, ec *a2asrv.ExecutorContext, call a2aCall, yield func(a2asdk.Event, error) bool) {
	ta, id := call.agent, ec.TaskID
	says, err := saidIn(ec.Message)
	if err != nil {
		yield(nil, err)
		return
	}
	sent := *ec.Message
	sent.TaskID, sent.ContextID = id, ec.ContextID
	submitted := a2asdk.NewSubmittedTask(ec, &sent)
	if ec.StoredTask == nil && !yield(submitted, nil) {
		return
	}
	if e.s.news == nil {
		yield(a2asdk.NewStatusUpdateEvent(ec, a2asdk.TaskStateFailed, failedWhy(id, ec.ContextID, "this node keeps no status row to leave it on")), nil)
		return
	}
	taken := make(chan struct{}, 1)
	e.s.leaveForAName(ta, string(id), spokenBy(call.request), says, func() {
		select {
		case taken <- struct{}{}:
		default:
		}
	})
	defer e.s.news.Drop(string(id))
	hold := time.NewTimer(toldWithin)
	defer hold.Stop()
	select {
	case <-ctx.Done():
	case <-taken:
		completed := *submitted
		now := time.Now()
		completed.Status = a2asdk.TaskStatus{State: a2asdk.TaskStateCompleted, Timestamp: &now, Message: &a2asdk.Message{
			ID: string(id) + "-taken", ContextID: ec.ContextID, TaskID: id, Role: a2asdk.MessageRoleAgent,
			Parts: a2asdk.ContentParts{textPart("ground took it from the status row for " + ta.called)},
		}}
		yield(&completed, nil)
	case <-hold.C:
		yield(a2asdk.NewStatusUpdateEvent(ec, a2asdk.TaskStateFailed, failedWhy(id, ec.ContextID,
			"no ground took it from the status row within "+toldWithin.String()+": "+ta.called+" was not told")), nil)
	}
}

// leaveForAName leaves what by said to the named agent on the status row, as
// news for the person its key speaks for: the row their ug polls.
func (s *QNTXServer) leaveForAName(ta taskAgent, id, by, says string, taken func()) {
	from := by
	if names, err := s.agentNames(); err == nil {
		if at := slices.IndexFunc(names, func(kept agentName) bool { return kept.did == by }); at >= 0 {
			from = names[at].name
		}
	}
	addressee := ta.did
	if s.authHandler != nil {
		if who, speaksFor := s.authHandler.MintedBy(ta.did); speaksFor {
			addressee = who
		}
	}
	s.news.Leave(statusline.News{
		ID:      id,
		For:     addressee,
		Item:    statusline.StatusItem{Name: from + " to " + ta.called, Note: masked(says), Symbol: statusline.SymbolWell},
		Detail:  map[string]any{"to": ta.called, "did": ta.did, "from": from, "task": id},
		UntilMs: time.Now().Add(toldWithin).UnixMilli(),
		Taken:   taken,
	})
	s.logger.Infow("what was said to a named agent is on the status row", "to", ta.called, "did", ta.did, "from", from, "task", id, "for", addressee)
}
