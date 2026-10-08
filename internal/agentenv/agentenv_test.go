package agentenv

import (
	"slices"
	"testing"
)

// What the node holds beside what a process needs stays the node's.
func TestOnlyWhatAProcessNeedsIsCarried(t *testing.T) {
	t.Setenv("QNTX_NODE_SECRET", "the node's")
	t.Setenv("PATH", "/bin")
	carried := Carried()
	if !slices.Contains(carried, "PATH=/bin") {
		t.Errorf("PATH was not carried: %v", carried)
	}
	for _, kv := range carried {
		if kv == "QNTX_NODE_SECRET=the node's" {
			t.Errorf("the node's own variable was carried: %v", carried)
		}
	}
}
