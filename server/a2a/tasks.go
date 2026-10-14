package a2a

import (
	"cmp"
	"encoding/base64"
	"encoding/json"
	"slices"
	"strconv"
	"strings"
	"time"

	errors "github.com/teranos/sacred-error"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/dynamicpb"
)

// Tasks as the operations read and answer them (§3.1.1 to §3.1.5), whoever
// keeps them. What a Task is made of is the keeper's; how it is said is here.

// State is a TaskState (§4.1.3), as JSON writes it.
type State string

const (
	Submitted State = "TASK_STATE_SUBMITTED"
	Working   State = "TASK_STATE_WORKING"
	Completed State = "TASK_STATE_COMPLETED"
	Failed    State = "TASK_STATE_FAILED"
)

// Terminal is a state a Task does not leave: "Messages sent to Tasks that are
// in a terminal state ... cannot accept further messages" (§3.1.1).
func (s State) Terminal() bool {
	return slices.Contains([]State{Completed, Failed, "TASK_STATE_CANCELED", "TASK_STATE_REJECTED"}, s)
}

// Task is one Task as its keeper holds it: its status, the context it is in,
// and every Message of it in the order they were said, as lf.a2a.v1.Message
// JSON.
type Task struct {
	ID        string
	ContextID string
	State     State
	// At is when the status was recorded, which ListTasks orders by (§3.1.4).
	At      time.Time
	History []map[string]any
}

// UserSaid is a Message from the client, its role and parts as A2A writes
// them, and nothing the client did not send.
func UserSaid(id, text string) map[string]any {
	return map[string]any{"messageId": id, "role": "ROLE_USER", "parts": []map[string]any{{"text": text}}}
}

// AgentSaid is a Message from the agent: what it answered, as text.
func AgentSaid(id, text string) map[string]any {
	return map[string]any{"messageId": id, "role": "ROLE_AGENT", "parts": []map[string]any{{"text": text}}}
}

// History is how much of a Task's history a caller asked for (§3.2.4): unset
// is all of it, 0 is none, and n is the n most recent.
type History struct {
	limited bool
	most    int
}

// historyOf reads historyLength off a request's JSON: present or not, since
// 0 is a request for none and absent is a request for all.
func historyOf(fields map[string]json.RawMessage, named string) (History, error) {
	raw, set := fields[named]
	if !set {
		return History{}, nil
	}
	var most int
	if err := json.Unmarshal(raw, &most); err != nil {
		return History{}, invalid(named + " is not a number: " + err.Error())
	}
	if most < 0 {
		return History{}, invalid(named + " is " + strconv.Itoa(most) + ", and a history has no fewer than no messages")
	}
	return History{limited: true, most: most}, nil
}

// of is the messages a History keeps: the most recent, up to its limit.
func (h History) of(history []map[string]any) []map[string]any {
	if !h.limited || h.most >= len(history) {
		return history
	}
	return history[len(history)-h.most:]
}

// fieldsOf is a request message as its JSON fields, read as protojson writes
// them: camelCase, and an optional field present when it was set, 0 or not.
func fieldsOf(request proto.Message) (map[string]json.RawMessage, error) {
	body, err := protojson.Marshal(request)
	if err != nil {
		return nil, invalid("the request did not marshal: " + err.Error())
	}
	fields := map[string]json.RawMessage{}
	if err := json.Unmarshal(body, &fields); err != nil {
		return nil, invalid("the request is not a JSON object: " + err.Error())
	}
	return fields, nil
}

// textOf is one string field of a request, and whether it was sent.
func textOf(fields map[string]json.RawMessage, named string) (string, bool, error) {
	raw, set := fields[named]
	if !set {
		return "", false, nil
	}
	var text string
	if err := json.Unmarshal(raw, &text); err != nil {
		return "", false, invalid(named + " is not text: " + err.Error())
	}
	return text, true, nil
}

// Send is a SendMessageRequest as the operation reads it (§3.2.1).
type Send struct {
	// Message is the client's Message whole, as it sent it.
	Message   map[string]any
	MessageID string
	// Text is every text part, in the order sent, a blank line between two.
	Text string
	// ContextID and TaskID are what the client named, when it named them.
	ContextID, TaskID       string
	NamesContext, NamesTask bool
	// ReturnImmediately is §3.2.2's non-blocking mode; blocking is the default.
	ReturnImmediately bool
	History           History
}

// ReadSend reads a SendMessageRequest. Its parts are text: the node takes no
// file and no data (ContentTypeNotSupportedError), and a Message sent to the
// agent is the user's (Role, §4.1.5).
func ReadSend(request proto.Message) (Send, error) {
	fields, err := fieldsOf(request)
	if err != nil {
		return Send{}, err
	}
	var sent struct {
		Message struct {
			MessageID string `json:"messageId"`
			ContextID string `json:"contextId"`
			TaskID    string `json:"taskId"`
			Role      string `json:"role"`
			Parts     []map[string]json.RawMessage
		} `json:"message"`
		Configuration map[string]json.RawMessage `json:"configuration"`
	}
	body, err := protojson.Marshal(request)
	if err != nil {
		return Send{}, invalid("the request did not marshal: " + err.Error())
	}
	if err := json.Unmarshal(body, &sent); err != nil {
		return Send{}, invalid("the request is not a SendMessageRequest: " + err.Error())
	}
	whole := map[string]any{}
	if err := json.Unmarshal(fields["message"], &whole); err != nil {
		return Send{}, invalid("the message is not a Message: " + err.Error())
	}
	if sent.Message.Role != "ROLE_USER" {
		return Send{}, invalid("the message's role is " + sent.Message.Role + ", and what is sent to an agent is the user's: ROLE_USER")
	}
	var texts []string
	for i, part := range sent.Message.Parts {
		at := "message.parts[" + strconv.Itoa(i) + "]"
		raw, isText := part["text"]
		if !isText {
			return Send{}, &Error{Name: "ContentTypeNotSupportedError", Message: at + " is not text, and text is what the agent takes"}
		}
		if media, named := part["mediaType"]; named {
			var said string
			if err := json.Unmarshal(media, &said); err != nil || said != "text/plain" {
				return Send{}, &Error{Name: "ContentTypeNotSupportedError", Message: at + " is " + string(media) + ", and text/plain is what the agent takes"}
			}
		}
		var text string
		if err := json.Unmarshal(raw, &text); err != nil {
			return Send{}, invalid(at + ".text is not text: " + err.Error())
		}
		texts = append(texts, text)
	}
	send := Send{
		Message: whole, MessageID: sent.Message.MessageID, Text: strings.Join(texts, "\n\n"),
		ContextID: sent.Message.ContextID, TaskID: sent.Message.TaskID,
	}
	if _, named := whole["contextId"]; named {
		send.NamesContext = true
	}
	if _, named := whole["taskId"]; named {
		send.NamesTask = true
	}
	if raw, set := sent.Configuration["returnImmediately"]; set {
		if err := json.Unmarshal(raw, &send.ReturnImmediately); err != nil {
			return Send{}, invalid("configuration.returnImmediately is not true or false: " + err.Error())
		}
	}
	if send.History, err = historyOf(sent.Configuration, "historyLength"); err != nil {
		return Send{}, err
	}
	return send, nil
}

// Get is a GetTaskRequest (§3.1.3).
type Get struct {
	ID      string
	History History
}

// ReadGet reads a GetTaskRequest.
func ReadGet(request proto.Message) (Get, error) {
	fields, err := fieldsOf(request)
	if err != nil {
		return Get{}, err
	}
	id, err := idOf(fields)
	if err != nil {
		return Get{}, err
	}
	history, err := historyOf(fields, "historyLength")
	if err != nil {
		return Get{}, err
	}
	return Get{ID: id, History: history}, nil
}

// idOf is the task a request names: its id, which the spec requires.
func idOf(fields map[string]json.RawMessage) (string, error) {
	id, named, err := textOf(fields, "id")
	if err != nil {
		return "", err
	}
	if !named {
		return "", invalid("id is required: the task asked about")
	}
	return id, nil
}

// ReadCancel reads a CancelTaskRequest's task (§3.1.5).
func ReadCancel(request proto.Message) (string, error) {
	fields, err := fieldsOf(request)
	if err != nil {
		return "", err
	}
	return idOf(fields)
}

// List is a ListTasksRequest (§3.1.4).
type List struct {
	ContextID, Status  string
	ByContext, ByState bool
	// After is statusTimestampAfter: a Task whose status is this recent or
	// more.
	After   time.Time
	ByAfter bool
	Size    int
	// Token is where the page starts, when a previous page gave one.
	Token    string
	ByToken  bool
	History  History
	Artifact bool
}

// The page sizes ListTasksRequest allows: "If unspecified, at most 50 tasks
// will be returned. The minimum value is 1. The maximum value is 100."
const (
	unspecifiedPage = 50
	smallestPage    = 1
	largestPage     = 100
)

// ReadList reads a ListTasksRequest. A page size outside 1 to 100 is refused,
// not moved into it.
func ReadList(request proto.Message) (List, error) {
	fields, err := fieldsOf(request)
	if err != nil {
		return List{}, err
	}
	list := List{Size: unspecifiedPage}
	if list.ContextID, list.ByContext, err = textOf(fields, "contextId"); err != nil {
		return List{}, err
	}
	if list.Status, list.ByState, err = textOf(fields, "status"); err != nil {
		return List{}, err
	}
	if list.Token, list.ByToken, err = textOf(fields, "pageToken"); err != nil {
		return List{}, err
	}
	if raw, set := fields["pageSize"]; set {
		if err := json.Unmarshal(raw, &list.Size); err != nil {
			return List{}, invalid("pageSize is not a number: " + err.Error())
		}
		if list.Size < smallestPage || list.Size > largestPage {
			return List{}, invalid("pageSize is " + strconv.Itoa(list.Size) + ", and a page is 1 to 100 tasks")
		}
	}
	after, byAfter, err := textOf(fields, "statusTimestampAfter")
	if err != nil {
		return List{}, err
	}
	if byAfter {
		at, err := time.Parse(time.RFC3339Nano, after)
		if err != nil {
			return List{}, invalid("statusTimestampAfter is not an ISO 8601 time: " + err.Error())
		}
		list.After, list.ByAfter = at, true
	}
	if list.History, err = historyOf(fields, "historyLength"); err != nil {
		return List{}, err
	}
	if raw, set := fields["includeArtifacts"]; set {
		if err := json.Unmarshal(raw, &list.Artifact); err != nil {
			return List{}, invalid("includeArtifacts is not true or false: " + err.Error())
		}
	}
	return list, nil
}

// json is the Task as lf.a2a.v1.Task JSON, with as much history as asked for.
// Every message carries the Task's id and context, and the status carries
// what the agent said last once the Task ended (TaskStatus.message).
func (t Task) json(h History, artifacts bool) map[string]any {
	history := []map[string]any{}
	for _, said := range h.of(t.History) {
		message := map[string]any{"taskId": t.ID, "contextId": t.ContextID}
		for name, value := range said {
			message[name] = value
		}
		history = append(history, message)
	}
	status := map[string]any{"state": string(t.State), "timestamp": t.At.UTC().Format(time.RFC3339Nano)}
	if last, said := t.saidLast(); said && t.State.Terminal() {
		message := map[string]any{"taskId": t.ID, "contextId": t.ContextID}
		for name, value := range last {
			message[name] = value
		}
		status["message"] = message
	}
	task := map[string]any{"id": t.ID, "contextId": t.ContextID, "status": status, "history": history}
	// The agent's answer is the Message it said: no Task here has an artifact.
	if artifacts {
		task["artifacts"] = []any{}
	}
	return task
}

// saidLast is the agent's Message when it said the last one of the Task. A
// Task with no Message said none.
func (t Task) saidLast() (map[string]any, bool) {
	for _, last := range slices.Backward(t.History) {
		return last, last["role"] == "ROLE_AGENT"
	}
	return map[string]any{}, false
}

// Message is the Task as lf.a2a.v1.Task.
func (t Task) Message(h History) (proto.Message, error) {
	return built("lf.a2a.v1.Task", t.json(h, false))
}

// Sent is SendMessage's answer: the Task the message made (SendMessageResponse).
func (t Task) Sent(h History) (proto.Message, error) {
	return built("lf.a2a.v1.SendMessageResponse", map[string]any{"task": t.json(h, false)})
}

// Listed is ListTasks' answer: the Tasks the request names, newest status
// first, one page of them. The page token is where the page ended: the status
// time and id of the last Task on it, which the next page starts after.
func Listed(tasks []Task, asked List) (proto.Message, error) {
	kept := slices.DeleteFunc(slices.Clone(tasks), func(t Task) bool {
		return (asked.ByContext && t.ContextID != asked.ContextID) ||
			(asked.ByState && string(t.State) != asked.Status) ||
			(asked.ByAfter && t.At.Before(asked.After))
	})
	slices.SortFunc(kept, func(a, b Task) int {
		return cmp.Or(b.At.Compare(a.At), strings.Compare(b.ID, a.ID))
	})
	from := 0
	if asked.ByToken {
		at, id, err := readToken(asked.Token)
		if err != nil {
			return nil, err
		}
		from = len(kept)
		for i, t := range kept {
			if t.At.Before(at) || (t.At.Equal(at) && t.ID < id) {
				from = i
				break
			}
		}
	}
	page := kept[from:]
	next := ""
	if len(page) > asked.Size {
		page = page[:asked.Size]
		last := page[len(page)-1]
		next = base64.RawURLEncoding.EncodeToString([]byte(last.At.UTC().Format(time.RFC3339Nano) + " " + last.ID))
	}
	listed := []map[string]any{}
	for _, t := range page {
		listed = append(listed, t.json(asked.History, asked.Artifact))
	}
	message, err := built("lf.a2a.v1.ListTasksResponse", map[string]any{
		"tasks": listed, "nextPageToken": next, "pageSize": asked.Size, "totalSize": len(kept),
	})
	if err != nil {
		return nil, &Error{Name: "InvalidAgentResponseError", Message: "the tasks did not read as a ListTasksResponse: " + err.Error()}
	}
	return message, nil
}

// readToken is a page token as Listed writes it.
func readToken(token string) (time.Time, string, error) {
	raw, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil {
		return time.Time{}, "", invalid("pageToken is not one this node gave: " + err.Error())
	}
	at, id, found := strings.Cut(string(raw), " ")
	if !found {
		return time.Time{}, "", invalid("pageToken is not one this node gave")
	}
	when, err := time.Parse(time.RFC3339Nano, at)
	if err != nil {
		return time.Time{}, "", invalid("pageToken is not one this node gave: " + err.Error())
	}
	return when, id, nil
}

// built is JSON read as the named message of the pinned spec, so it cannot
// carry what the proto does not declare.
func built(name string, fields map[string]any) (proto.Message, error) {
	descriptor, err := message(protoreflect.FullName(name))
	if err != nil {
		return nil, err
	}
	body, err := json.Marshal(fields)
	if err != nil {
		return nil, errors.Wrapf(err, "the %s did not marshal", name)
	}
	m := dynamicpb.NewMessage(descriptor)
	if err := protojson.Unmarshal(body, m); err != nil {
		return nil, errors.Wrapf(err, "what was built is not an %s", name)
	}
	return m, nil
}

// Invalid is a request the operation cannot take as it was sent.
func Invalid(message string) *Error { return invalid(message) }

// TaskNotFound is a Task that does not exist, or that the caller may not know
// of (§3.3.2).
func TaskNotFound(id string) *Error {
	return &Error{Name: "TaskNotFoundError", Message: "no task " + id}
}

// TaskNotCancelable is a Task the node will not cancel (§3.1.5).
func TaskNotCancelable(message string) *Error {
	return &Error{Name: "TaskNotCancelableError", Message: message}
}

// Error is the A2A error it is, wherever it was wrapped.
func (e *Error) Error() string { return e.Message }

// Refused is err as the binding writes it: the A2A error inside it, or the
// node's failing.
func Refused(err error) *Error {
	var refused *Error
	if errors.As(err, &refused) {
		return refused
	}
	return &Error{Message: err.Error()}
}

// UnsupportedBecause is UnsupportedOperationError, saying why.
func UnsupportedBecause(message string) *Error {
	return &Error{Name: "UnsupportedOperationError", Message: message}
}
