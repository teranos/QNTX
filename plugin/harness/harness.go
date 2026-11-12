// Package harness is what core and a harness plugin say to each other over
// A2A (ADR-059): "Harness is a plugin that speaks grpc A2A".
//
// A turn is one A2A Message, the user's: what is said as its text, and how
// the harness runs it as one record, the harness's own shape. What the
// harness prints as it goes is a working status with that record in its
// message (A2A §3.7, "Status Messages"), and how the turn ended is the
// Task completed: what it answered as its artifact's text, and what the
// harness says of the run as that artifact's record.
//
// A record crosses as its JSON, byte for byte, in a raw part: a data part is
// a protobuf Struct on gRPC, whose every number is a float64, and a number a
// tool was called with is the number the harness printed.
package harness

import (
	"encoding/json"

	a2asdk "github.com/a2aproject/a2a-go/v2/a2a"
	errors "github.com/teranos/sacred-error"
)

// AnswerArtifactID is the artifact a turn's answer is.
const AnswerArtifactID = "answer"

// Said is a turn as the Message core sends: says, and run, how the harness
// runs it.
func Said(says string, run any) (*a2asdk.Message, error) {
	record, err := asRecord(run)
	if err != nil {
		return nil, errors.Wrap(err, "how the turn is run is no JSON")
	}
	return a2asdk.NewMessage(a2asdk.MessageRoleUser, a2asdk.NewTextPart(says), record), nil
}

// SaidIn is what message says, and how it is run read into run.
func SaidIn(message *a2asdk.Message, run any) (string, error) {
	if message == nil {
		return "", errors.New("a turn is a message, and none was sent")
	}
	says, ran := "", false
	for _, part := range message.Parts {
		if text, isText := part.Content.(a2asdk.Text); isText {
			says = string(text)
			continue
		}
		carried, err := recordIn(part, run)
		if err != nil {
			return "", errors.Wrap(err, "how the turn is run did not read")
		}
		ran = ran || carried
	}
	if !ran {
		return "", errors.New("a turn says how it is run in a record, and this message carries none")
	}
	return says, nil
}

// Printed is one record the harness printed while the turn runs.
func Printed(info a2asdk.TaskInfoProvider, record any) (*a2asdk.TaskStatusUpdateEvent, error) {
	carried, err := asRecord(record)
	if err != nil {
		return nil, errors.Wrap(err, "what the harness printed is no JSON")
	}
	message := a2asdk.NewMessage(a2asdk.MessageRoleAgent, carried)
	return a2asdk.NewStatusUpdateEvent(info, a2asdk.TaskStateWorking, message), nil
}

// PrintedIn reads the record event carries into record, and reports whether it
// carries one.
func PrintedIn(event a2asdk.Event, record any) (bool, error) {
	update, isUpdate := event.(*a2asdk.TaskStatusUpdateEvent)
	if !isUpdate || update.Status.State != a2asdk.TaskStateWorking || update.Status.Message == nil {
		return false, nil
	}
	for _, part := range update.Status.Message.Parts {
		if carried, err := recordIn(part, record); carried || err != nil {
			return carried, errors.Wrap(err, "what the harness printed did not read")
		}
	}
	return false, nil
}

// Answered is the Task completed: text, what the turn answered, and answer,
// what the harness says of the run.
func Answered(submitted *a2asdk.Task, text string, answer any) (*a2asdk.Task, error) {
	record, err := asRecord(answer)
	if err != nil {
		return nil, errors.Wrap(err, "what the harness says of the run is no JSON")
	}
	completed := *submitted
	completed.Status = a2asdk.TaskStatus{State: a2asdk.TaskStateCompleted}
	completed.Artifacts = []*a2asdk.Artifact{{
		ID: AnswerArtifactID, Name: AnswerArtifactID,
		Parts: a2asdk.ContentParts{a2asdk.NewTextPart(text), record},
	}}
	return &completed, nil
}

// AnswerIn is what the Task completed answered, and what the harness says of
// the run read into answer. A Task that failed is an error in the harness's
// words.
func AnswerIn(task *a2asdk.Task, answer any) (string, error) {
	if task == nil {
		return "", errors.New("the harness ended the turn with no Task")
	}
	if task.Status.State != a2asdk.TaskStateCompleted {
		why := string(task.Status.State)
		if m := task.Status.Message; m != nil {
			for _, part := range m.Parts {
				if text, isText := part.Content.(a2asdk.Text); isText {
					why = string(text)
				}
			}
		}
		return "", errors.New(why)
	}
	for _, artifact := range task.Artifacts {
		if artifact.ID != AnswerArtifactID {
			continue
		}
		text, read := "", false
		for _, part := range artifact.Parts {
			if said, isText := part.Content.(a2asdk.Text); isText {
				text = string(said)
				continue
			}
			carried, err := recordIn(part, answer)
			if err != nil {
				return "", errors.Wrap(err, "what the harness says of the run did not read")
			}
			read = read || carried
		}
		if !read {
			return "", errors.New("the harness's answer says nothing of the run")
		}
		return text, nil
	}
	return "", errors.New("the harness completed the turn with no answer")
}

// recordType is the media type a record crosses as.
const recordType = "application/json"

// asRecord is v as the part a record crosses in: its JSON, as marshalled.
func asRecord(v any) (*a2asdk.Part, error) {
	raw, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	part := a2asdk.NewRawPart(raw)
	part.MediaType = recordType
	return part, nil
}

// recordIn reads the record part carries into v, and reports whether it
// carries one.
func recordIn(part *a2asdk.Part, v any) (bool, error) {
	raw, isRaw := part.Content.(a2asdk.Raw)
	if !isRaw || part.MediaType != recordType {
		return false, nil
	}
	return true, json.Unmarshal(raw, v)
}
