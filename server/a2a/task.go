package a2a

import (
	"encoding/json"
	"strconv"
	"strings"
	"time"

	errors "github.com/teranos/sacred-error"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/dynamicpb"
)

// A Task is "The fundamental unit of work managed by A2A, identified by a
// unique ID. Tasks are stateful and progress through a defined lifecycle."
// "It has a current status and when results are created for the task they are
// stored in the artifact. If there are multiple turns for a task, these are
// stored in history." (a2a.proto Task)

// The states a Task here reaches, as TaskState names them.
const (
	Submitted = "TASK_STATE_SUBMITTED"
	Working   = "TASK_STATE_WORKING"
	Completed = "TASK_STATE_COMPLETED"
	Failed    = "TASK_STATE_FAILED"
)

// Terminal is whether a Task in state has ended, and takes no further message
// (§3.1.1).
func Terminal(state string) bool {
	return state == Completed || state == Failed || state == "TASK_STATE_CANCELED" || state == "TASK_STATE_REJECTED"
}

// Text is the media type of what a text part carries.
const Text = "text/plain"

// Sent is a SendMessageRequest as the node reads it.
type Sent struct {
	// Message is the message sent, whole, as the request carried it.
	Message protoreflect.Message
	// TaskID and ContextID are what the message names, when it names them.
	TaskID, ContextID string
	// Role is the message's Role by name.
	Role string
	// ReturnImmediately and History are SendMessageConfiguration's.
	ReturnImmediately bool
	History           HistoryLimit
	// Push is whether the request configured a push notification.
	Push bool

	parts []sentPart
}

type sentPart struct {
	Text      *string `json:"text"`
	MediaType string  `json:"mediaType"`
}

// ReadSent is the SendMessageRequest request is.
func ReadSent(request proto.Message) (Sent, error) {
	raw, err := protojson.Marshal(request)
	if err != nil {
		return Sent{}, errors.Wrap(err, "the SendMessageRequest did not marshal to be read")
	}
	var read struct {
		Message struct {
			TaskID    string     `json:"taskId"`
			ContextID string     `json:"contextId"`
			Role      string     `json:"role"`
			Parts     []sentPart `json:"parts"`
		} `json:"message"`
		Configuration struct {
			ReturnImmediately bool            `json:"returnImmediately"`
			HistoryLength     *int32          `json:"historyLength"`
			Push              json.RawMessage `json:"taskPushNotificationConfig"`
		} `json:"configuration"`
	}
	if err := json.Unmarshal(raw, &read); err != nil {
		return Sent{}, errors.Wrap(err, "the SendMessageRequest did not read")
	}
	held := request.ProtoReflect()
	field, found := fieldNamed(held.Descriptor(), "message")
	if !found {
		return Sent{}, errors.Newf("%s has no message", held.Descriptor().FullName())
	}
	m := read.Message
	return Sent{
		Message: held.Get(field).Message(), TaskID: m.TaskID, ContextID: m.ContextID, Role: m.Role,
		ReturnImmediately: read.Configuration.ReturnImmediately, History: limitOf(read.Configuration.HistoryLength),
		Push: len(read.Configuration.Push) > 0, parts: m.Parts,
	}, nil
}

// Said is what the message's text parts say, one after another. A part that is
// not text, or text in another media type than Text, is refused: the agent
// takes text and nothing else (ContentTypeNotSupportedError).
func (s Sent) Said() (string, *Error) {
	texts := make([]string, 0, len(s.parts))
	for i, part := range s.parts {
		if part.Text == nil {
			return "", &Error{Name: "ContentTypeNotSupportedError", Message: "part " + strconv.Itoa(i) + " carries no text, and the agent takes " + Text + " alone"}
		}
		if part.MediaType != "" && part.MediaType != Text {
			return "", &Error{Name: "ContentTypeNotSupportedError", Message: "part " + strconv.Itoa(i) + " is " + part.MediaType + ", and the agent takes " + Text + " alone"}
		}
		texts = append(texts, *part.Text)
	}
	return strings.Join(texts, "\n"), nil
}

// InTask is the message sent as the history of task holds it: naming the task
// and the context it is in (§3.4.1, §3.4.2), as lf.a2a.v1.Message JSON.
func (s Sent) InTask(task, context string) (json.RawMessage, error) {
	m := proto.Clone(s.Message.Interface()).ProtoReflect()
	for name, value := range map[protoreflect.Name]string{"task_id": task, "context_id": context} {
		if value == "" {
			continue
		}
		if err := setText(m, name, value); err != nil {
			return nil, err
		}
	}
	body, err := protojson.Marshal(m.Interface())
	return body, errors.Wrapf(err, "the message of task %s did not marshal", task)
}

// Task is one Task as the node holds it.
type Task struct {
	ID, ContextID string
	// State is its TaskState by name, and At when it was reached.
	State string
	At    time.Time
	// History is the messages of its turns, as lf.a2a.v1.Message JSON.
	History []json.RawMessage
	// Answer is what the agent gave, when it completed; Why is why it failed,
	// when it failed.
	Answer, Why string
}

// HistoryLimit is a request's history_length: how many of a task's most
// recent messages it is given. Unlimited is "the client does not impose any
// limit", and a Length of zero "a request to not include any messages"
// (a2a.proto).
type HistoryLimit struct {
	Limited bool
	Length  int32
}

func limitOf(length *int32) HistoryLimit {
	if length == nil {
		return HistoryLimit{}
	}
	return HistoryLimit{Limited: true, Length: *length}
}

// Response is the task as lf.a2a.v1.Task, with as much of its history as limit
// gives.
func (t Task) Response(limit HistoryLimit) (protoreflect.Message, error) {
	descriptor, err := message("lf.a2a.v1.Task")
	if err != nil {
		return nil, err
	}
	history := t.History
	if limit.Limited {
		history = history[max(len(history)-max(int(limit.Length), 0), 0):]
	}
	status := map[string]any{"state": t.State, "timestamp": t.At.UTC().Format(time.RFC3339Nano)}
	if t.State == Failed {
		// "A message associated with the status": the agent's, saying why.
		status["message"] = map[string]any{
			"messageId": t.ID + "-failed", "contextId": t.ContextID, "taskId": t.ID, "role": "ROLE_AGENT",
			"parts": []map[string]any{{"text": t.Why, "mediaType": Text}},
		}
	}
	task := map[string]any{"id": t.ID, "contextId": t.ContextID, "status": status, "history": history}
	if t.State == Completed {
		// "Results SHOULD BE returned using Artifacts associated with a Task" (§3.7).
		task["artifacts"] = []map[string]any{{
			"artifactId": "answer", "name": "answer",
			"parts": []map[string]any{{"text": t.Answer, "mediaType": Text}},
		}}
	}
	body, err := json.Marshal(task)
	if err != nil {
		return nil, errors.Wrapf(err, "task %s did not marshal", t.ID)
	}
	built := dynamicpb.NewMessage(descriptor)
	if err := protojson.Unmarshal(body, built); err != nil {
		return nil, errors.Wrapf(err, "task %s is not an lf.a2a.v1.Task", t.ID)
	}
	return built, nil
}

// Sending is the task as SendMessageResponse carries it.
func (t Task) Sending(limit HistoryLimit) (proto.Message, error) {
	task, err := t.Response(limit)
	if err != nil {
		return nil, err
	}
	descriptor, err := message("lf.a2a.v1.SendMessageResponse")
	if err != nil {
		return nil, err
	}
	response := dynamicpb.NewMessage(descriptor)
	field, found := fieldNamed(descriptor, "task")
	if !found {
		return nil, errors.New("lf.a2a.v1.SendMessageResponse has no task")
	}
	response.Set(field, protoreflect.ValueOfMessage(task))
	return response, nil
}

// Asked is a GetTaskRequest as the node reads it: the task's id, and how much
// of its history.
func Asked(request proto.Message) (id string, limit HistoryLimit, err error) {
	held := request.ProtoReflect()
	named, found := fieldNamed(held.Descriptor(), "id")
	if !found || named.Kind() != protoreflect.StringKind {
		return "", HistoryLimit{}, errors.Newf("%s has no id", held.Descriptor().FullName())
	}
	length, found := fieldNamed(held.Descriptor(), "history_length")
	if !found || length.Kind() != protoreflect.Int32Kind {
		return "", HistoryLimit{}, errors.Newf("%s has no history_length", held.Descriptor().FullName())
	}
	limit = HistoryLimit{Limited: held.Has(length), Length: int32(held.Get(length).Int())}
	return held.Get(named).String(), limit, nil
}

// TaskNotFound is a task id that does not correspond to a task the caller may
// know of (§3.4.2, §3.3.2).
func TaskNotFound(id string) *Error {
	return &Error{Name: "TaskNotFoundError", Message: "no task " + id}
}

// Invalid is a request the node does not take, saying why.
func Invalid(message string) *Error { return invalid(message) }
