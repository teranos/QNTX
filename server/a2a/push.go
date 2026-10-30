package a2a

import (
	"encoding/json"

	errors "github.com/teranos/sacred-error"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/dynamicpb"
)

// "Push Notifications: Asynchronous task updates delivered via server-initiated
// HTTP POST requests to a client-provided webhook URL, for long-running or
// disconnected scenarios." (§1)
//
// "Push notifications is something I want though, especially for approvals"

// Push is a TaskPushNotificationConfig as the node holds it: where a Task's
// updates are POSTed, and with what credentials (§4.3).
type Push struct {
	ID, TaskID string
	URL        string
	// Token is "A token unique for this task or session" (a2a.proto), kept
	// and given back as the client set it.
	Token string
	// Scheme and Credentials are its AuthenticationInfo, empty when it has
	// none: "The agent MUST include authentication credentials in the request
	// headers as specified in the PushNotificationConfig.authentication field"
	// (§4.3.3).
	Scheme, Credentials string
}

type pushRead struct {
	ID             string `json:"id"`
	TaskID         string `json:"taskId"`
	URL            string `json:"url"`
	Token          string `json:"token"`
	Authentication *struct {
		Scheme      string `json:"scheme"`
		Credentials string `json:"credentials"`
	} `json:"authentication"`
}

// ReadPush is the TaskPushNotificationConfig request is, as
// CreateTaskPushNotificationConfig takes it.
func ReadPush(request proto.Message) (Push, *Error) {
	raw, err := protojson.Marshal(request)
	if err != nil {
		return Push{}, invalid("the TaskPushNotificationConfig did not marshal to be read: " + err.Error())
	}
	return pushOf(raw)
}

// pushOf is a TaskPushNotificationConfig as lf.a2a.v1 JSON, or why the node
// does not take it.
func pushOf(raw json.RawMessage) (Push, *Error) {
	var read pushRead
	if err := json.Unmarshal(raw, &read); err != nil {
		return Push{}, invalid("the TaskPushNotificationConfig did not read: " + err.Error())
	}
	push := Push{ID: read.ID, TaskID: read.TaskID, URL: read.URL, Token: read.Token}
	if read.Authentication != nil {
		push.Scheme, push.Credentials = read.Authentication.Scheme, read.Authentication.Credentials
	}
	return push, nil
}

// Response is the config as lf.a2a.v1.TaskPushNotificationConfig, at tenant.
// Credentials are not given back: what a client handed the node to speak with
// is not answered, as a namespace's keys are not (ADR-051).
func (p Push) Response(tenant string) (proto.Message, error) {
	descriptor, err := message("lf.a2a.v1.TaskPushNotificationConfig")
	if err != nil {
		return nil, err
	}
	config := map[string]any{"id": p.ID, "taskId": p.TaskID, "url": p.URL}
	if tenant != "" {
		config["tenant"] = tenant
	}
	if p.Token != "" {
		config["token"] = p.Token
	}
	if p.Scheme != "" {
		config["authentication"] = map[string]any{"scheme": p.Scheme}
	}
	body, err := json.Marshal(config)
	if err != nil {
		return nil, errors.Wrapf(err, "push notification config %s of task %s did not marshal", p.ID, p.TaskID)
	}
	built := dynamicpb.NewMessage(descriptor)
	if err := protojson.Unmarshal(body, built); err != nil {
		return nil, errors.Wrapf(err, "push notification config %s of task %s is not an lf.a2a.v1.TaskPushNotificationConfig", p.ID, p.TaskID)
	}
	return built, nil
}

// PushesListed is configs as ListTaskPushNotificationConfigsResponse carries
// them, all of them on one page: pagination is a MAY (§3.1.9).
func PushesListed(configs []Push, tenant string) (proto.Message, error) {
	descriptor, err := message("lf.a2a.v1.ListTaskPushNotificationConfigsResponse")
	if err != nil {
		return nil, err
	}
	response := dynamicpb.NewMessage(descriptor)
	field, found := fieldNamed(descriptor, "configs")
	if !found {
		return nil, errors.New("lf.a2a.v1.ListTaskPushNotificationConfigsResponse has no configs")
	}
	listed := response.Mutable(field).List()
	for _, config := range configs {
		built, err := config.Response(tenant)
		if err != nil {
			return nil, err
		}
		listed.Append(protoreflect.ValueOfMessage(built.ProtoReflect()))
	}
	return response, nil
}

// PushNamed is the task and config a GetTaskPushNotificationConfigRequest or a
// DeleteTaskPushNotificationConfigRequest names.
func PushNamed(request proto.Message) (task, id string, err error) {
	held := request.ProtoReflect()
	for name, into := range map[protoreflect.Name]*string{"task_id": &task, "id": &id} {
		field, found := fieldNamed(held.Descriptor(), name)
		if !found || field.Kind() != protoreflect.StringKind {
			return "", "", errors.Newf("%s has no %s", held.Descriptor().FullName(), name)
		}
		*into = held.Get(field).String()
	}
	return task, id, nil
}

// PushesOf is the task a ListTaskPushNotificationConfigsRequest names.
func PushesOf(request proto.Message) (string, error) {
	held := request.ProtoReflect()
	field, found := fieldNamed(held.Descriptor(), "task_id")
	if !found || field.Kind() != protoreflect.StringKind {
		return "", errors.Newf("%s has no task_id", held.Descriptor().FullName())
	}
	return held.Get(field).String(), nil
}

// Notification is what is POSTed to a webhook when the task is updated: "The
// payload uses the same StreamResponse format as streaming operations"
// (§4.3.3), here the Task as it now stands.
func (t Task) Notification() ([]byte, error) {
	task, err := t.Response(HistoryLimit{})
	if err != nil {
		return nil, err
	}
	descriptor, err := message("lf.a2a.v1.StreamResponse")
	if err != nil {
		return nil, err
	}
	response := dynamicpb.NewMessage(descriptor)
	field, found := fieldNamed(descriptor, "task")
	if !found {
		return nil, errors.New("lf.a2a.v1.StreamResponse has no task")
	}
	response.Set(field, protoreflect.ValueOfMessage(task))
	body, err := protojson.Marshal(response)
	return body, errors.Wrapf(err, "the notification of task %s did not marshal", t.ID)
}

// ContentType is how a notification is POSTed (§4.3.3).
const ContentType = contentType

// PushConfigNotFound is a config a task does not have: "The operation MUST
// fail if the configuration does not exist or the client lacks access"
// (§3.1.8).
func PushConfigNotFound(task, id string) *Error {
	return notFound("task " + task + " has no push notification config " + id)
}
