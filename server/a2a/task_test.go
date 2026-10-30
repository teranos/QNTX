package a2a

import (
	"testing"

	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/types/dynamicpb"
)

// sentWith is a SendMessageRequest of one text part, configured as configured.
func sentWith(t *testing.T, configured string) Sent {
	t.Helper()
	descriptor, err := message("lf.a2a.v1.SendMessageRequest")
	if err != nil {
		t.Fatal(err)
	}
	request := dynamicpb.NewMessage(descriptor)
	body := `{"message": {"messageId": "m1", "role": "ROLE_USER", "parts": [{"text": "hi"}]}, "configuration": ` + configured + `}`
	if err := protojson.Unmarshal([]byte(body), request); err != nil {
		t.Fatal(err)
	}
	read, err := ReadSent(request)
	if err != nil {
		t.Fatal(err)
	}
	return read
}

// "A list of media types the client is prepared to accept for response parts"
// (a2a.proto): the agent gives text/plain alone, so a client that takes none
// back is refused before anything is said, and one that names none takes it.
func TestAClientIsGivenTextOnlyWhenItTakesText(t *testing.T) {
	for configured, takes := range map[string]bool{
		`{}`:                          true,
		`{"acceptedOutputModes": []}`: true,
		`{"acceptedOutputModes": ["text/plain"]}`:                     true,
		`{"acceptedOutputModes": ["application/json", "TEXT/PLAIN"]}`: true,
		`{"acceptedOutputModes": ["text/plain; charset=utf-8"]}`:      true,
		`{"acceptedOutputModes": ["application/json"]}`:               false,
		`{"acceptedOutputModes": ["text/markdown", "image/png"]}`:     false,
	} {
		says, refused := sentWith(t, configured).Said()
		if takes && (refused != nil || says != "hi") {
			t.Errorf("%s was refused: %v", configured, refused)
		}
		if !takes && (refused == nil || refused.Name != "ContentTypeNotSupportedError") {
			t.Errorf("%s was not refused as ContentTypeNotSupportedError: %v", configured, refused)
		}
	}
}
