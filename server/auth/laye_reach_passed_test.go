package auth

import (
	"encoding/json"
	"testing"

	errors "github.com/teranos/sacred-error"
)

// "each step in the thread should only say their thing, nothing more"
// 「[Fable 5.2] What the peer refused leaves this node as the peer made it, and one line of」
// 「[Fable 5.2] this node's. The test every hop has: out is in, plus that line.」
func TestAPeersRefusalIsPassedOnWithThisNodesLineAlone(t *testing.T) {
	refused := errors.From("kapteyn").Make(errors.What{
		Severity: errors.Warn,
		Context:  errors.Context{Surface: "reach:grant"},
		Title:    "http:vault:list: a runtime line opens only a plugin's route the compiled table does not name to a level",
		Why:      "not allowed, path",
	})
	said, err := json.Marshal(refused)
	if err != nil {
		t.Fatal(err)
	}

	out, err := json.Marshal(errors.Pass(here.Receive("kapteyn POST /api/reach", string(said)), "carried the reach line google:1 approved to kapteyn"))
	if err != nil {
		t.Fatal(err)
	}

	var was, is map[string]any
	if err := json.Unmarshal(said, &was); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(out, &is); err != nil {
		t.Fatal(err)
	}
	trace, _ := is["trace"].([]any)
	if len(trace) != 1 || trace[0] != "carried the reach line google:1 approved to kapteyn" {
		t.Errorf("the trace is %v, want this node's one line", trace)
	}
	delete(is, "trace")
	for field, want := range was {
		if got, _ := json.Marshal(is[field]); string(got) != mustJSON(t, want) {
			t.Errorf("%s left as %s, came as %s", field, got, mustJSON(t, want))
		}
	}
	if len(is) != len(was) {
		t.Errorf("this node added %d fields of its own to the peer's Error", len(is)-len(was))
	}
}

// 「[Fable 5.2] An answer that is no Error is this node's finding, and the peer's answer is」
// 「[Fable 5.2] kept whole: nothing is made in the peer's name.」
func TestAnAnswerThatIsNoErrorIsThisNodesFinding(t *testing.T) {
	answered := `{"id":"ERR-1795867972986-86","error":"http:vault:list: a runtime line opens only a plugin's route the compiled table does not name to a level","timestamp":1795867972}`
	received := here.Receive("kapteyn POST /api/reach", answered)
	if received.Source() != "qntx" || received.Raw() != answered || received.Title() != "kapteyn POST /api/reach did not answer with an Error" {
		t.Errorf("received as %+v", received)
	}
}

func mustJSON(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}
