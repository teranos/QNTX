package parity

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// a2a.says.json is what a2a-go's generated Go says, as the module go.mod
// pins says it: an SDK that says something else is make says to run.
func TestA2ASaysIsWhatTheSDKSays(t *testing.T) {
	module, err := exec.Command("go", "list", "-m", "-f", "{{.Dir}}", "github.com/a2aproject/a2a-go/v2").Output()
	require.NoError(t, err)
	generated := filepath.Join(strings.TrimSpace(string(module)), "a2apb", "v1")
	pb, err := os.ReadFile(filepath.Join(generated, "a2av1.pb.go"))
	require.NoError(t, err)
	grpc, err := os.ReadFile(filepath.Join(generated, "a2av1_grpc.pb.go"))
	require.NoError(t, err)
	resolver, err := os.ReadFile(filepath.Join(strings.TrimSpace(string(module)), "a2aclient", "agentcard", "resolver.go"))
	require.NoError(t, err)
	read, err := A2AGoSays(pb, grpc, resolver)
	require.NoError(t, err)
	written := sdkSaid()
	for name, said := range read {
		if written[name] != said {
			t.Errorf("a2a-go says %q of %s, and a2a.says.json has %q: run make says", said, name, written[name])
		}
	}
	for name := range written {
		if _, held := read[name]; !held {
			t.Errorf("a2a.says.json has %s, which a2a-go says nothing of: run make says", name)
		}
	}
	for name, said := range read {
		if strings.Contains(said, "protolint:") || strings.Contains(said, "Next ID:") {
			t.Errorf("%s says %q, a line the .proto keeps for its tools", name, said)
		}
	}
}
