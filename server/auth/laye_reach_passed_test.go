package auth

import (
	"encoding/json"
	"testing"

	errors "github.com/teranos/sacred-error"
)

// "each step in the thread should only say their thing, nothing more"
// 「[Opus 5.5] What the peer refused leaves this node as it came, and one line of this node's.」
func TestAPeersRefusalIsPassedOnWithThisNodesLineAlone(t *testing.T) {
	refused := errors.Error{ID: "ERR-1", Severity: errors.Warn, Context: errors.Context{Surface: "reach:grant"}, Title: "http:vault:list: a runtime line opens only a plugin's route the compiled table does not name to a level", Why: "not allowed, path", At: "2026-11-28T12:00:00Z"}
	said, err := json.Marshal(refused)
	if err != nil {
		t.Fatal(err)
	}
	out := passedOn(said, 403, "kapteyn", "carried the reach line google:1 approved to kapteyn")

	got, isError := errors.Decode(string(out)).(*errors.Error)
	if !isError {
		t.Fatalf("what left is no Error: %s", out)
	}
	want := errors.Pass(refused, "carried the reach line google:1 approved to kapteyn")
	if got.Title != want.Title || got.Why != want.Why || got.ID != want.ID || got.Context != want.Context || len(got.Trace) != 1 || got.Trace[0] != want.Trace[0] {
		t.Errorf("passed on as %+v, want %+v", got, want)
	}
}

// 「[Opus 5.5] An answer that is no Error becomes one here, once, kept whole, with the same line.」
func TestAnAnswerThatIsNoErrorIsKeptWhole(t *testing.T) {
	out := passedOn([]byte("<html>Bad gateway</html>"), 502, "kapteyn", "carried it to kapteyn")
	got, isError := errors.Decode(string(out)).(*errors.Error)
	if !isError || got.Raw != "<html>Bad gateway</html>" || len(got.Trace) != 1 {
		t.Errorf("not kept whole: %s", out)
	}
}

// 「[Opus 5.5] A node answering in QNTX's older shape: its sentence and id, verbatim, and the line.」
func TestAnOlderAnswerKeepsItsWordsAndID(t *testing.T) {
	said := []byte(`{"id":"ERR-1795867972986-86","error":"http:vault:list: a runtime line opens only a plugin's route the compiled table does not name to a level","timestamp":1795867972}`)
	out := passedOn(said, 403, "kapteyn", "carried the reach line google:1 approved to kapteyn")
	got, isError := errors.Decode(string(out)).(*errors.Error)
	if !isError || got.ID != "ERR-1795867972986-86" || got.Title != "http:vault:list: a runtime line opens only a plugin's route the compiled table does not name to a level" || len(got.Trace) != 1 || got.Raw != string(said) {
		t.Errorf("not kept as said: %s", out)
	}
}
