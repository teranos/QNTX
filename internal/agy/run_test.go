//go:build libacp

package agy

import (
	"context"
	"strings"
	"testing"

	"github.com/teranos/QNTX/internal/acp"
	"github.com/teranos/QNTX/internal/acp/acptest"
)

// "correct it for libACP client use for agy"
// agy 1.4.12 speaks ACP as `agy acp`: signed in with the agent's Gemini key, the model set as the
// session's model option, and the prompt said whole.
func TestRunSpeaksACPInItsOwnEnvironment(t *testing.T) {
	t.Setenv("GEMINI_API_KEY", "node-secret")
	t.Setenv("GOOGLE_API_KEY", "node-secret")
	t.Setenv("AGY_ADC_AUTH", "true")
	t.Setenv("QNTX_TOKEN", "node-secret")
	binary, ran := acptest.StandIn(t, acptest.Script{Session: "conv-1", Updates: []string{
		`{"sessionUpdate":"agent_message_chunk","content":{"type":"text","text":"do"}}`,
		`{"sessionUpdate":"agent_message_chunk","content":{"type":"text","text":"ne"}}`,
	}})
	home := t.TempDir()
	for run, session := range []string{"", "conv-1"} {
		s := Said{Binary: binary, Home: home, Session: session, Says: "/model\nhello", Model: "gemini-3.8-flash", APIKey: "agent-key"}
		var kinds []acp.UpdateKind
		answer, err := s.Run(context.Background(), func(u acp.Update) { kinds = append(kinds, u.Kind) })
		if err != nil {
			t.Fatal(err)
		}
		if answer.Text != "done" || answer.Session != "conv-1" || len(kinds) != 3 || kinds[0] != acp.Started {
			t.Fatalf("answer=%+v updates=%v", answer, kinds)
		}
		n := string(rune('0' + run))
		args := strings.Join(acptest.Read(t, ran, n, "args"), " ")
		for _, want := range []string{"acp", "--sandbox", "--disable-telemetry", "--log-file " + home + "/" + turnLog} {
			if !strings.Contains(args, want) {
				t.Fatalf("args lack %q: %s", want, args)
			}
		}
		if strings.Contains(args, "hello") {
			t.Fatalf("the prompt is on the command line: %s", args)
		}
		env := acptest.Read(t, ran, n, "env")
		for _, want := range []string{"HOME=" + home, "GEMINI_API_KEY=agent-key", "AGY_CLI_DISABLE_AUTO_UPDATE=true"} {
			if !contains(env, want) {
				t.Fatalf("env lacks %s", want)
			}
		}
		for _, node := range []string{"GOOGLE_API_KEY=", "AGY_ADC_AUTH=", "QNTX_TOKEN="} {
			for _, line := range env {
				if strings.HasPrefix(line, node) {
					t.Fatalf("the node's %s reached agy", node)
				}
			}
		}
		input := strings.Join(acptest.Read(t, ran, n, "input"), "\n")
		for _, want := range []string{
			`"method":"authenticate","params":{"methodId":"gemini-api-key"}`,
			`"configId":"model","value":"gemini-3.8-flash"`,
			`"text":"/model\nhello"`,
		} {
			if !strings.Contains(input, want) {
				t.Fatalf("agy was not told %s:\n%s", want, input)
			}
		}
		if resumed := strings.Contains(input, `"method":"session/resume","params":{"sessionId":"conv-1"`); resumed != (session != "") {
			t.Fatalf("run %d resumed=%v:\n%s", run, resumed, input)
		}
	}
}

// agy ends a turn it could not run as end_turn, with a warning for its message; the error itself is in
// its log. The turn fails, with the log's words.
func TestRunRefusesATerminatedTurnAsSuccess(t *testing.T) {
	binary, _ := acptest.StandIn(t, acptest.Script{
		Updates: []string{`{"sessionUpdate":"agent_message_chunk","content":{"type":"text","text":"\n⚠ Agent execution terminated due to error.\n"}}`},
		Log: "I1102 03:00:34.884032       1 options.go:483] Starting ACP server\n" +
			"E1102 03:00:35.363145     170 errorreport.go:224] agent executor error: generating and executing: Error 400, Message: API key not valid. Please pass a valid API key.\n",
	})
	_, err := (Said{Binary: binary, Home: t.TempDir(), Says: "hi", Model: "m", APIKey: "k"}).Run(context.Background(), nil)
	if err == nil || !strings.Contains(err.Error(), "API key not valid") || !strings.Contains(err.Error(), "terminated") {
		t.Fatalf("err = %v", err)
	}
}

// A stop other than end_turn is no answer.
func TestRunRefusesAnotherStop(t *testing.T) {
	binary, _ := acptest.StandIn(t, acptest.Script{Stop: "refusal"})
	_, err := (Said{Binary: binary, Home: t.TempDir(), Says: "hi", Model: "m", APIKey: "k"}).Run(context.Background(), nil)
	if err == nil || !strings.Contains(err.Error(), "refusal") {
		t.Fatalf("err = %v", err)
	}
}

func contains(lines []string, want string) bool {
	for _, l := range lines {
		if l == want {
			return true
		}
	}
	return false
}
