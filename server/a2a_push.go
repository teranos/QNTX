package server

import (
	"context"
	"slices"
	"strings"
	"time"

	a2asdk "github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2asrv/push"
	"github.com/google/uuid"
	"github.com/teranos/QNTX/ats"
	"github.com/teranos/QNTX/ats/identity"
	"github.com/teranos/QNTX/ats/types"
	"github.com/teranos/QNTX/internal/sacred"
	errors "github.com/teranos/sacred-error"
)

// A Task's push notifications (§3.1.7–§3.1.10, §4.3).
//
// "Push notifications is something I want though, especially for approvals"
//
// A config is a PUSH line beside the Task's lines, in its task and its agent:
// newest per config holding, and a deleted config a line that says so. "The
// configuration MUST persist until task completion or explicit deletion"
// (§3.1.7), so it is written down as the Task is, and outlives a restart. Its
// credentials are sealed as a namespace's keys are (ADR-051): the node speaks
// with them to the webhook, and answers them to no one.
//
// Each event a Task reaches is POSTed to each of its webhooks by a2a-go's
// push.HTTPPushSender.

const pushLineSubject = "PUSH"

// A PUSH line sets its config, or says it was deleted.
const (
	pushSet     = "set"
	pushDeleted = "deleted"
)

// pushSealed is what credentials are sealed for, beside a namespace's keys.
const pushSealed = "push"

// pushContext is what a config's lines are in.
func pushContext(id string) string { return "push:" + id }

// pushSender is a2a-go's: it POSTs each event to each webhook, and dials no
// private address (§13.2). The test binary sends to httptest on loopback.
var pushSender push.Sender = push.NewHTTPPushSender(nil)

// pushesOnLines is the SDK's push.ConfigStore over the PUSH lines of the agent
// each call reaches. The SDK reads its configs to send with, credentials and
// all; what a client handed the node to speak with is not answered, as a
// namespace's keys are not (ADR-051), so a2aGate.After leaves them out.
type pushesOnLines struct{ s *QNTXServer }

var _ push.ConfigStore = pushesOnLines{}

func (p pushesOnLines) Save(ctx context.Context, task a2asdk.TaskID, config *a2asdk.PushConfig) (*a2asdk.PushConfig, error) {
	call, err := a2aCallFrom(ctx)
	if err != nil {
		return nil, err
	}
	saved := *config
	saved.TaskID = task
	// "Created configuration with assigned ID" (§3.1.7): the client's, when it
	// names one, and setting it again sets it anew.
	if saved.ID == "" {
		saved.ID = uuid.NewString()
	}
	if err := p.s.setPush(call.agent.taskLines, &saved, spokenBy(call.request)); err != nil {
		return nil, err
	}
	return atTenant(&saved, call.tenant), nil
}

func (p pushesOnLines) Get(ctx context.Context, task a2asdk.TaskID, id string) (*a2asdk.PushConfig, error) {
	call, err := a2aCallFrom(ctx)
	if err != nil {
		return nil, err
	}
	pushes, err := p.s.pushesOf(call.agent.taskLines, string(task))
	if err != nil {
		return nil, err
	}
	at := slices.IndexFunc(pushes, func(push *a2asdk.PushConfig) bool { return push.ID == id })
	if at < 0 {
		// "The operation MUST fail if the configuration does not exist or the
		// client lacks access" (§3.1.8).
		return nil, a2aRefusal(a2asdk.ErrTaskNotFound, "task "+string(task)+" has no push notification config "+id)
	}
	return atTenant(pushes[at], call.tenant), nil
}

// List is "all active push notification configurations for the specified
// task" (§3.1.9).
func (p pushesOnLines) List(ctx context.Context, task a2asdk.TaskID) ([]*a2asdk.PushConfig, error) {
	call, err := a2aCallFrom(ctx)
	if err != nil {
		return nil, err
	}
	pushes, err := p.s.pushesOf(call.agent.taskLines, string(task))
	if err != nil {
		return nil, err
	}
	answered := make([]*a2asdk.PushConfig, 0, len(pushes))
	for _, push := range pushes {
		answered = append(answered, atTenant(push, call.tenant))
	}
	return answered, nil
}

// Delete is "No further notifications will be sent to the configured webhook
// after deletion. This operation MUST be idempotent" (§3.1.10), so a config
// already gone is deleted again with nothing written.
func (p pushesOnLines) Delete(ctx context.Context, task a2asdk.TaskID, id string) error {
	call, err := a2aCallFrom(ctx)
	if err != nil {
		return err
	}
	pushes, err := p.s.pushesOf(call.agent.taskLines, string(task))
	if err != nil {
		return err
	}
	if !slices.ContainsFunc(pushes, func(push *a2asdk.PushConfig) bool { return push.ID == id }) {
		return nil
	}
	return p.s.writePushLine(call.agent.taskLines, &a2asdk.PushConfig{ID: id, TaskID: task}, pushDeleted, spokenBy(call.request), map[string]any{})
}

func (p pushesOnLines) DeleteAll(ctx context.Context, task a2asdk.TaskID) error {
	call, err := a2aCallFrom(ctx)
	if err != nil {
		return err
	}
	pushes, err := p.s.pushesOf(call.agent.taskLines, string(task))
	if err != nil {
		return err
	}
	for _, push := range pushes {
		if err := p.Delete(ctx, task, push.ID); err != nil {
			return err
		}
	}
	return nil
}

// atTenant is the config at the tenant the request named.
func atTenant(config *a2asdk.PushConfig, tenant string) *a2asdk.PushConfig {
	at := *config
	at.Tenant = tenant
	return &at
}

// withoutCredentials is the config as an operation answers it: its
// credentials left out.
func withoutCredentials(config *a2asdk.PushConfig) *a2asdk.PushConfig {
	answered := *config
	if config.Auth != nil {
		answered.Auth = &a2asdk.PushAuthInfo{Scheme: config.Auth.Scheme}
	}
	return &answered
}

// setPush writes the config down for its Task, its credentials sealed.
func (s *QNTXServer) setPush(lines taskLines, push *a2asdk.PushConfig, by string) error {
	attributes := map[string]any{"url": push.URL}
	if push.Token != "" {
		attributes["token"] = push.Token
	}
	if push.Auth != nil && push.Auth.Scheme != "" {
		attributes["scheme"] = push.Auth.Scheme
	}
	if push.Auth != nil && push.Auth.Credentials != "" {
		sealed, err := s.sealFor(pushSealed, lines.namespace, push.Auth.Credentials)
		if err != nil {
			return errors.Wrapf(err, "the credentials of push notification config %s of task %s were not sealed", push.ID, push.TaskID)
		}
		attributes["credentials"] = sealed
	}
	return s.writePushLine(lines, push, pushSet, by, attributes)
}

// writePushLine writes that the config was set or deleted, as by.
func (s *QNTXServer) writePushLine(lines taskLines, push *a2asdk.PushConfig, said, by string, attributes map[string]any) error {
	store, err := lines.write()
	if err != nil {
		return errors.Wrapf(err, "no store to write push notification config %s of task %s in, in %s", push.ID, push.TaskID, lines.namespace)
	}
	line, err := identity.GenerateASUIDWithRetry("AS", pushLineSubject, said, push.ID, store.AttestationExists)
	if err != nil {
		return errors.Wrapf(err, "failed to name the line of push notification config %s of task %s", push.ID, push.TaskID)
	}
	at := time.Now()
	return errors.Wrapf(store.CreateAttestation(&types.As{
		ID: line, Subjects: []string{pushLineSubject}, Predicates: []string{said},
		Contexts: []string{taskContext(string(push.TaskID)), agentContext(lines.did), pushContext(push.ID)},
		Actors:   []string{by}, Timestamp: at, CreatedAt: at, Source: "a2a", Attributes: attributes,
	}), "the %s line of push notification config %s of task %s was not written", said, push.ID, push.TaskID)
}

// pushesOf is every config the task has, oldest set first, credentials
// unsealed for the webhook.
func (s *QNTXServer) pushesOf(lines taskLines, task string) ([]*a2asdk.PushConfig, error) {
	reading, err := s.held.Read(lines.namespace)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to read the push notification configs of task %s", task)
	}
	written, err := reading.GetAttestations(ats.AttestationFilter{TimeStart: types.Unbounded(), TimeEnd: types.Unbounded(),
		Subjects: []string{pushLineSubject}, Contexts: []string{taskContext(task)}, Limit: ats.EveryRow})
	if err != nil {
		return nil, errors.Wrapf(err, "failed to read the push notification configs of task %s", task)
	}
	written = slices.DeleteFunc(written, func(line *types.As) bool { return s.agentOfLine(line) != lines.did })
	slices.SortFunc(written, func(a, b *types.As) int { return a.Timestamp.Compare(b.Timestamp) })
	newest, order := map[string]*types.As{}, []string{}
	for _, line := range written {
		id, found := pushIn(line)
		if !found {
			return nil, errors.Newf("%s line %s of task %s names no config", pushLineSubject, line.ID, task)
		}
		if _, seen := newest[id]; !seen {
			order = append(order, id)
		}
		newest[id] = line
	}
	pushes := []*a2asdk.PushConfig{}
	for _, id := range order {
		line := newest[id]
		if len(line.Predicates) != 1 {
			return nil, errors.Newf("%s line %s of task %s says %d things, and a line says one", pushLineSubject, line.ID, task, len(line.Predicates))
		}
		if line.Predicates[0] == pushDeleted {
			continue
		}
		push := &a2asdk.PushConfig{ID: id, TaskID: a2asdk.TaskID(task)}
		if push.URL, err = oneText(line.Attributes, "url", pushLineSubject, line.ID, task); err != nil {
			return nil, err
		}
		push.Token, _ = line.Attributes["token"].(string)
		scheme, _ := line.Attributes["scheme"].(string)
		credentials := ""
		if sealed, holds := line.Attributes["credentials"].(string); holds {
			if credentials, err = s.unsealFor(pushSealed, lines.namespace, sealed); err != nil {
				return nil, errors.Wrapf(err, "the credentials of push notification config %s of task %s did not open", id, task)
			}
		}
		if scheme != "" || credentials != "" {
			push.Auth = &a2asdk.PushAuthInfo{Scheme: scheme, Credentials: credentials}
		}
		pushes = append(pushes, push)
	}
	return pushes, nil
}

// pushIn is the config a PUSH line is of.
func pushIn(line *types.As) (string, bool) {
	for _, context := range line.Contexts {
		if id, isPush := strings.CutPrefix(context, "push:"); isPush {
			return id, true
		}
	}
	return "", false
}

// notifySettled tells each webhook of a Task the node settled with no
// execution left to tell them: a restart, or a turn read on from its stream.
func (s *QNTXServer) notifySettled(lines taskLines, task *a2asdk.Task) {
	pushes, err := s.pushesOf(lines, string(task.ID))
	if err != nil {
		s.logger.Errorw("a task was settled and its push notifications were not read", "agent", lines.did, "task", task.ID, "error", err)
		return
	}
	// Sent after endTask lets go of taskEnds: a slow webhook holds no other task.
	sacred.Go("the push notifications of a settled task", func() {
		for _, config := range pushes {
			// A config deleted while an earlier webhook took its time is sent nothing more (§3.1.10).
			held, err := s.pushesOf(lines, string(task.ID))
			if err != nil {
				s.logger.Errorw("a task was settled and its push notification was not delivered: its configs were not read again", "agent", lines.did, "task", task.ID, "config", config.ID, "error", err)
				continue
			}
			// Sent as the config now stands: one replaced under its id goes where it now says.
			at := slices.IndexFunc(held, func(push *a2asdk.PushConfig) bool { return push.ID == config.ID })
			if at < 0 {
				continue
			}
			if err := pushSender.SendPush(s.ctx, held[at], task); err != nil {
				s.logger.Errorw("a task was settled and its push notification was not delivered", "agent", lines.did, "task", task.ID, "config", config.ID, "error", err)
			}
		}
	})
}
