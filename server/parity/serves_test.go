package parity

import (
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// a2a.serves.json is what the a2a-go go.mod pins ships a server for: an SDK
// that serves something else is make says to run.
func TestA2AServesIsWhatTheSDKServes(t *testing.T) {
	module, err := exec.Command("go", "list", "-m", "-f", "{{.Dir}}", "github.com/a2aproject/a2a-go/v2").Output()
	require.NoError(t, err)
	read, err := A2AGoServes(os.DirFS(strings.TrimSpace(string(module))))
	require.NoError(t, err)
	require.NotEmpty(t, read)
	written, err := A2AServes()
	require.NoError(t, err)
	assert.Equal(t, read, written, "a2a-go ships servers that a2a.serves.json does not say: run make says")
}
