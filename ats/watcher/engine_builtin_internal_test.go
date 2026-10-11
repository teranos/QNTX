package watcher

import (
	"github.com/teranos/QNTX/ats/types"
	"testing"

	"github.com/teranos/QNTX/ats/storage"
)

// A built-in has no plugin to wait on. Asking whether one is loaded for it
// would park every fire in the queue for a plugin that does not exist.
func TestBuiltinActionRequiresNoPlugin(t *testing.T) {
	w := &storage.Watcher{Filter: types.AxFilter{TimeStart: types.Unbounded(), TimeEnd: types.Unbounded()},
		ActionType: storage.ActionTypeBuiltinExecute,
		ActionData: `{"handler_name":"ci.watch"}`,
	}
	if got := actionRequiresPlugin(w); got != "" {
		t.Errorf("a built-in action says it requires plugin %q", got)
	}
}
