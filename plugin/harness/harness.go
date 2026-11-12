// Package harness is what core and a harness plugin say to each other over
// A2A (ADR-059): "Harness is a plugin that speaks grpc A2A".
//
// A turn is one A2A Message, the user's: what is said as its text, and how
// the harness runs it as one data part, the harness's own shape. What the
// harness prints as it goes is a working status with that record as its
// message's data (A2A §3.7, "Status Messages"), and how the turn ended is the
// Task completed: what it answered as its artifact's text, and what the
// harness says of the run as that artifact's data.
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
	data, err := asData(run)
	if err != nil {
		return nil, errors.Wrap(err, "how the turn is run is no JSON")
	}
	return a2asdk.NewMessage(a2asdk.MessageRoleUser, a2asdk.NewTextPart(says), a2asdk.NewDataPart(data)), nil
}

// SaidIn is what message says, and how it is run read into run.
func SaidIn(message *a2asdk.Message, run any) (string, error) {
	if message == nil {
		return "", errors.New("a turn is a message, and none was sent")
	}
	says, ran := "", false
	for _, part := range message.Parts {
		switch content := part.Content.(type) {
		case a2asdk.Text:
			says = string(content)
		case a2asdk.Data:
			if err := fromData(content, run); err != nil {
				return "", errors.Wrap(err, "how the turn is run did not read")
			}
			ran = true
		}
	}
	if !ran {
		return "", errors.New("a turn says how it is run in a data part, and this message carries none")
	}
	return says, nil
}

// Printed is one record the harness printed while the turn runs.
func Printed(info a2asdk.TaskInfoProvider, record any) (*a2asdk.TaskStatusUpdateEvent, error) {
	data, err := asData(record)
	if err != nil {
		return nil, errors.Wrap(err, "what the harness printed is no JSON")
	}
	message := a2asdk.NewMessage(a2asdk.MessageRoleAgent, a2asdk.NewDataPart(data))
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
		if data, isData := part.Content.(a2asdk.Data); isData {
			return true, errors.Wrap(fromData(data, record), "what the harness printed did not read")
		}
	}
	return false, nil
}

// Answered is the Task completed: text, what the turn answered, and answer,
// what the harness says of the run.
func Answered(submitted *a2asdk.Task, text string, answer any) (*a2asdk.Task, error) {
	data, err := asData(answer)
	if err != nil {
		return nil, errors.Wrap(err, "what the harness says of the run is no JSON")
	}
	completed := *submitted
	completed.Status = a2asdk.TaskStatus{State: a2asdk.TaskStateCompleted}
	completed.Artifacts = []*a2asdk.Artifact{{
		ID: AnswerArtifactID, Name: AnswerArtifactID,
		Parts: a2asdk.ContentParts{a2asdk.NewTextPart(text), a2asdk.NewDataPart(data)},
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
			switch content := part.Content.(type) {
			case a2asdk.Text:
				text = string(content)
			case a2asdk.Data:
				if err := fromData(content, answer); err != nil {
					return "", errors.Wrap(err, "what the harness says of the run did not read")
				}
				read = true
			}
		}
		if !read {
			return "", errors.New("the harness's answer says nothing of the run")
		}
		return text, nil
	}
	return "", errors.New("the harness completed the turn with no answer")
}

// asData is v as the JSON value a data part carries.
func asData(v any) (any, error) {
	raw, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	var data any
	if err := json.Unmarshal(raw, &data); err != nil {
		return nil, err
	}
	return data, nil
}

// fromData reads a data part's JSON value into v.
func fromData(data a2asdk.Data, v any) error {
	raw, err := json.Marshal(data.Value)
	if err != nil {
		return err
	}
	return json.Unmarshal(raw, v)
}
