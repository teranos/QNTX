package watcher

import (
	"testing"

	"github.com/teranos/QNTX/ats/storage"
)

// A built-in has no plugin to wait on. Asking whether one is loaded for it
// would park every fire in the queue for a plugin that does not exist.
func TestBuiltinActionRequiresNoPlugin(t *testing.T) {
	w := &storage.Watcher{
		ActionType: storage.ActionTypeBuiltinExecute,
		ActionData: `{"handler_name":"ci.watch"}`,
	}
	if got := actionRequiresPlugin(w); got != "" {
		t.Errorf("a built-in action says it requires plugin %q", got)
	}
}
