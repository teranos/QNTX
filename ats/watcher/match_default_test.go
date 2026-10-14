//go:build !qntxwasm

package watcher

import (
	"strings"
	"testing"

	"github.com/teranos/QNTX/ats/storage"
	"github.com/teranos/QNTX/ats/types"
)

// Without Rust there is no matcher, and no second one in Go to answer instead.
func TestABuildWithoutRustMatchesNoWatcherAndSaysSo(t *testing.T) {
	w := &storage.Watcher{ID: "w", Enabled: true, Filter: types.AxFilter{TimeStart: types.Unbounded(), TimeEnd: types.Unbounded()}}
	matched, _, err := batchMatchStructural(&types.As{ID: "AS-1"}, []*storage.Watcher{w}, nil)
	if err == nil || !strings.Contains(err.Error(), "qntxwasm") {
		t.Fatalf("matched %v, err %v: want a refusal naming qntxwasm", matched, err)
	}
}
