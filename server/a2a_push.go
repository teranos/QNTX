package server

import (
	"bytes"
	"context"
	"io"
	"net"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/google/uuid"
	"github.com/teranos/QNTX/ats"
	"github.com/teranos/QNTX/ats/identity"
	"github.com/teranos/QNTX/ats/types"
	"github.com/teranos/QNTX/internal/sacred"
	"github.com/teranos/QNTX/server/a2a"
	"github.com/teranos/QNTX/server/auth"
	errors "github.com/teranos/sacred-error"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/emptypb"
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
// Each line a Task reaches is POSTed to each of its webhooks as the Task then
// stands (§4.3.3).

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

// The push notification config operations (§3.1.7–§3.1.10).
const (
	createPush = "CreateTaskPushNotificationConfig"
	getPush    = "GetTaskPushNotificationConfig"
	listPushes = "ListTaskPushNotificationConfigs"
	deletePush = "DeleteTaskPushNotificationConfig"
)

// webhookAddress is whether the node POSTs a notification to ip: what the
// internet routes to, as a provider's host is (auth.IsPublicIP). "Agents
// SHOULD validate webhook URLs to prevent SSRF (Server-Side Request Forgery)
// attacks" (§13.2). The test binary clears it to reach httptest on loopback.
var webhookAddress = auth.IsPublicIP

// webhookTimeout is how long one POST to a webhook is given: "Agents SHOULD
// include a reasonable timeout for webhook requests (recommended: 10-30
// seconds)" (§4.3.3).
const webhookTimeout = 15 * time.Second

// webhookAttempts is how many times a notification is POSTed before it is
// given up, webhookBackoff waited before the second and doubled after: "Agents
// MUST attempt delivery at least once for each configured webhook", "MAY
// implement retry logic with exponential backoff" (§4.3.3).
const webhookAttempts = 3

var webhookBackoff = time.Second

// webhookRefused is why the node does not POST to the config's URL, nil when it
// does: "Reject private IP ranges ... Reject localhost and link-local
// addresses" (§13.2). A name is asked again where it is dialed, since DNS can
// answer differently there.
func webhookRefused(push a2a.Push) *a2a.Error {
	u, err := url.Parse(push.URL)
	if err != nil {
		return a2a.Invalid("url " + push.URL + " is no URL: " + err.Error())
	}
	if u.Scheme != "https" && u.Scheme != "http" {
		return a2a.Invalid("url " + push.URL + " is no http or https URL to POST to")
	}
	host := strings.ToLower(u.Hostname())
	if host == "" {
		return a2a.Invalid("url " + push.URL + " names no host")
	}
	if host == "localhost" || strings.HasSuffix(host, ".localhost") {
		return a2a.Invalid("url " + push.URL + " is localhost, which the node POSTs no notification to")
	}
	if ip := net.ParseIP(host); ip != nil && !webhookAddress(ip) {
		return a2a.Invalid("url " + push.URL + " is " + ip.String() + ", which is no public address the node POSTs a notification to")
	}
	return nil
}

// createTaskPush is CreateTaskPushNotificationConfig (§3.1.7): a webhook for a
// Task of the agent, there from now until the config is deleted.
func (s *QNTXServer) createTaskPush(r *http.Request, ta taskAgent, request proto.Message) (proto.Message, *a2a.Error) {
	push, refused := a2a.ReadPush(request)
	if refused != nil {
		return nil, refused
	}
	if refused := webhookRefused(push); refused != nil {
		return nil, refused
	}
	if refused := s.taskHeld(ta.taskLines, push.TaskID); refused != nil {
		return nil, refused
	}
	// "Created configuration with assigned ID" (§3.1.7): the client's, when it
	// names one, and setting it again sets it anew.
	if push.ID == "" {
		push.ID = uuid.NewString()
	}
	if err := s.setPush(ta.taskLines, push, spokenBy(r)); err != nil {
		return nil, &a2a.Error{Name: "InvalidAgentResponseError", Message: err.Error()}
	}
	return pushAnswered(push, request)
}

// getTaskPush is GetTaskPushNotificationConfig (§3.1.8).
func (s *QNTXServer) getTaskPush(ta taskAgent, request proto.Message) (proto.Message, *a2a.Error) {
	task, id, err := a2a.PushNamed(request)
	if err != nil {
		return nil, a2a.Invalid(err.Error())
	}
	if refused := s.taskHeld(ta.taskLines, task); refused != nil {
		return nil, refused
	}
	pushes, err := s.pushesOf(ta.taskLines, task)
	if err != nil {
		return nil, &a2a.Error{Name: "InvalidAgentResponseError", Message: err.Error()}
	}
	at := slices.IndexFunc(pushes, func(push a2a.Push) bool { return push.ID == id })
	if at < 0 {
		return nil, a2a.PushConfigNotFound(task, id)
	}
	return pushAnswered(pushes[at], request)
}

// listTaskPushes is ListTaskPushNotificationConfigs (§3.1.9): "all active push
// notification configurations for the specified task".
func (s *QNTXServer) listTaskPushes(ta taskAgent, request proto.Message) (proto.Message, *a2a.Error) {
	task, err := a2a.PushesOf(request)
	if err != nil {
		return nil, a2a.Invalid(err.Error())
	}
	if refused := s.taskHeld(ta.taskLines, task); refused != nil {
		return nil, refused
	}
	pushes, err := s.pushesOf(ta.taskLines, task)
	if err != nil {
		return nil, &a2a.Error{Name: "InvalidAgentResponseError", Message: err.Error()}
	}
	tenant, _ := a2a.TenantOf(request)
	listed, err := a2a.PushesListed(pushes, tenant)
	if err != nil {
		return nil, &a2a.Error{Name: "InvalidAgentResponseError", Message: err.Error()}
	}
	return listed, nil
}

// deleteTaskPush is DeleteTaskPushNotificationConfig (§3.1.10): "No further
// notifications will be sent to the configured webhook after deletion. This
// operation MUST be idempotent", so a config already gone is deleted again
// with nothing written.
func (s *QNTXServer) deleteTaskPush(r *http.Request, ta taskAgent, request proto.Message) (proto.Message, *a2a.Error) {
	task, id, err := a2a.PushNamed(request)
	if err != nil {
		return nil, a2a.Invalid(err.Error())
	}
	if refused := s.taskHeld(ta.taskLines, task); refused != nil {
		return nil, refused
	}
	pushes, err := s.pushesOf(ta.taskLines, task)
	if err != nil {
		return nil, &a2a.Error{Name: "InvalidAgentResponseError", Message: err.Error()}
	}
	if slices.ContainsFunc(pushes, func(push a2a.Push) bool { return push.ID == id }) {
		if err := s.writePushLine(ta.taskLines, a2a.Push{ID: id, TaskID: task}, pushDeleted, spokenBy(r), map[string]any{}); err != nil {
			return nil, &a2a.Error{Name: "InvalidAgentResponseError", Message: err.Error()}
		}
	}
	return &emptypb.Empty{}, nil
}

// taskHeld is TaskNotFoundError when the agent has no task id, nil when it has.
func (s *QNTXServer) taskHeld(lines taskLines, id string) *a2a.Error {
	_, held, err := s.taskOf(lines, id)
	if err != nil {
		return &a2a.Error{Name: "InvalidAgentResponseError", Message: err.Error()}
	}
	if !held {
		return a2a.TaskNotFound(id)
	}
	return nil
}

// pushAnswered is the config as an operation answers it, at the tenant the
// request named.
func pushAnswered(push a2a.Push, request proto.Message) (proto.Message, *a2a.Error) {
	tenant, _ := a2a.TenantOf(request)
	answered, err := push.Response(tenant)
	if err != nil {
		return nil, &a2a.Error{Name: "InvalidAgentResponseError", Message: err.Error()}
	}
	return answered, nil
}

// setPush writes the config down for its Task, its credentials sealed.
func (s *QNTXServer) setPush(lines taskLines, push a2a.Push, by string) error {
	attributes := map[string]any{"url": push.URL}
	if push.Token != "" {
		attributes["token"] = push.Token
	}
	if push.Scheme != "" {
		attributes["scheme"] = push.Scheme
	}
	if push.Credentials != "" {
		sealed, err := s.sealFor(pushSealed, lines.namespace, push.Credentials)
		if err != nil {
			return errors.Wrapf(err, "the credentials of push notification config %s of task %s were not sealed", push.ID, push.TaskID)
		}
		attributes["credentials"] = sealed
	}
	return s.writePushLine(lines, push, pushSet, by, attributes)
}

// writePushLine writes that the config was set or deleted, as by.
func (s *QNTXServer) writePushLine(lines taskLines, push a2a.Push, said, by string, attributes map[string]any) error {
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
		Contexts: []string{taskContext(push.TaskID), agentContext(lines.did), pushContext(push.ID)},
		Actors:   []string{by}, Timestamp: at, CreatedAt: at, Source: "a2a", Attributes: attributes,
	}), "the %s line of push notification config %s of task %s was not written", said, push.ID, push.TaskID)
}

// pushesOf is every config the task has, oldest set first, credentials
// unsealed for the webhook.
func (s *QNTXServer) pushesOf(lines taskLines, task string) ([]a2a.Push, error) {
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
	pushes := []a2a.Push{}
	for _, id := range order {
		line := newest[id]
		if len(line.Predicates) != 1 {
			return nil, errors.Newf("%s line %s of task %s says %d things, and a line says one", pushLineSubject, line.ID, task, len(line.Predicates))
		}
		if line.Predicates[0] == pushDeleted {
			continue
		}
		push := a2a.Push{ID: id, TaskID: task}
		if push.URL, err = oneText(line.Attributes, "url", pushLineSubject, line.ID, task); err != nil {
			return nil, err
		}
		push.Token, _ = line.Attributes["token"].(string)
		push.Scheme, _ = line.Attributes["scheme"].(string)
		if sealed, holds := line.Attributes["credentials"].(string); holds {
			if push.Credentials, err = s.unsealFor(pushSealed, lines.namespace, sealed); err != nil {
				return nil, errors.Wrapf(err, "the credentials of push notification config %s of task %s did not open", id, task)
			}
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

// notify POSTs the task as it now stands to each of its webhooks. A task's notifications go one after
// another, each as the task stands when it goes, so a webhook is never told an
// older state after a newer one.
func (s *QNTXServer) notify(lines taskLines, task string) {
	held, _ := s.pushing.LoadOrStore(task, &sync.Mutex{})
	one := held.(*sync.Mutex)
	sacred.Go("the push notifications of a task", func() {
		one.Lock()
		defer one.Unlock()
		ended, err := s.notifyNow(lines, task)
		if err != nil {
			s.logger.Errorw("a task was updated and its push notifications were not sent", "agent", lines.did, "task", task, "error", err)
		}
		// A task that ended reaches no state after it (endTask).
		if ended {
			s.pushing.Delete(task)
		}
	})
}

// notifyNow is notify's POSTs, one webhook after another, and whether the task
// has ended.
func (s *QNTXServer) notifyNow(lines taskLines, task string) (bool, error) {
	held, found, err := s.taskOf(lines, task)
	if err != nil {
		return false, err
	}
	if !found {
		return false, errors.Newf("task %s was updated and has no lines", task)
	}
	ended := a2a.Terminal(held.State)
	pushes, err := s.pushesOf(lines, task)
	if err != nil {
		return ended, err
	}
	if len(pushes) == 0 {
		return ended, nil
	}
	body, err := held.Notification()
	if err != nil {
		return ended, err
	}
	for _, push := range pushes {
		if err := s.deliver(push, body); err != nil {
			// One webhook not answering is no reason the next is not told.
			s.logger.Errorw("a push notification was not delivered", "agent", lines.did, "task", task, "config", push.ID, "url", push.URL, "state", held.State, "error", err)
		}
	}
	return ended, nil
}

// deliver POSTs body to the webhook, again after a backoff that doubles while
// it does not answer 2xx: "Clients MUST respond with HTTP 2xx status codes to
// acknowledge successful receipt" (§4.3.3).
func (s *QNTXServer) deliver(push a2a.Push, body []byte) error {
	wait := webhookBackoff
	var last error
	for attempt := 1; attempt <= webhookAttempts; attempt++ {
		if attempt > 1 {
			select {
			case <-time.After(wait):
			case <-s.ctx.Done():
				return errors.Wrapf(last, "the node stopped before push notification config %s of task %s was delivered", push.ID, push.TaskID)
			}
			wait *= 2
		}
		if last = s.post(push, body); last == nil {
			return nil
		}
	}
	return errors.Wrapf(last, "%s did not take the notification of task %s in %d attempts", push.URL, push.TaskID, webhookAttempts)
}

// post is one POST of body to the webhook, with the credentials the config
// carries: "Authorization: {authentication_scheme} {credentials}" (§4.3.3).
func (s *QNTXServer) post(push a2a.Push, body []byte) error {
	ctx, cancel := context.WithTimeout(s.ctx, webhookTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, push.URL, bytes.NewReader(body))
	if err != nil {
		return errors.Wrapf(err, "no request to POST the notification of task %s to %s", push.TaskID, push.URL)
	}
	req.Header.Set("Content-Type", a2a.ContentType)
	if push.Scheme != "" {
		req.Header.Set("Authorization", strings.TrimSpace(push.Scheme+" "+push.Credentials))
	}
	resp, err := webhookClient().Do(req)
	if err != nil {
		return errors.Wrapf(err, "the notification of task %s was not POSTed to %s", push.TaskID, push.URL)
	}
	defer func() { _ = resp.Body.Close() }()
	said, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return errors.Newf("%s answered the notification of task %s with HTTP %d: %s", push.URL, push.TaskID, resp.StatusCode, strings.TrimSpace(string(said)))
	}
	return nil
}

// webhookClient POSTs to a webhook and nowhere else: the address it dials is
// asked again, since DNS can answer differently there than when the config was
// set, and a redirect is not followed somewhere the URL did not name.
func webhookClient() *http.Client {
	dialer := &net.Dialer{Timeout: webhookTimeout, Control: func(_, address string, _ syscall.RawConn) error {
		host, _, err := net.SplitHostPort(address)
		if err != nil {
			return errors.Wrapf(err, "webhook address %q is not host:port", address)
		}
		ip := net.ParseIP(host)
		if ip == nil {
			return errors.Newf("webhook address %q did not resolve to an IP", address)
		}
		if !webhookAddress(ip) {
			return errors.Newf("the webhook resolves to %s, which is no public address the node POSTs a notification to", ip)
		}
		return nil
	}}
	return &http.Client{
		Timeout:       webhookTimeout,
		Transport:     &http.Transport{DialContext: dialer.DialContext},
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
}
