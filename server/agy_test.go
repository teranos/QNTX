package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/teranos/QNTX/internal/acp/acptest"
	"github.com/teranos/QNTX/internal/agy"
	appcfg "github.com/teranos/QNTX/internal/config"
	"github.com/teranos/QNTX/plugin/grpc/protocol"
	"github.com/teranos/QNTX/server/sigil"
)

// "wiring it into node and namespace config as an actual harness"

// "correct it for libACP client use for agy"
// What a stand-in for `agy acp` tells of a turn that ran one command and
// answered, as ACP session updates.
var agyAnswered = acptest.Script{Session: "conv-7", Updates: []string{
	`{"sessionUpdate":"user_message_chunk","content":{"type":"text","text":"how long has the box been up?"}}`,
	`{"sessionUpdate":"tool_call","toolCallId":"t1","title":"uptime","kind":"execute","status":"pending","rawInput":{"command":"uptime"}}`,
	`{"sessionUpdate":"tool_call_update","toolCallId":"t1","status":"completed","rawOutput":"up 3 days"}`,
	`{"sessionUpdate":"agent_message_chunk","content":{"type":"text","text":"Up 3 days."}}`,
	`{"sessionUpdate":"usage_update","used":14,"size":1000000}`,
}}

var flashInAgy = appcfg.AgyConfig{Model: "gemini-3.8-flash"}

// runningAgyToo is the Node Agent of runningTheNodeAgent, with agy named and a
// stand-in for it held.
func runningAgyToo(t *testing.T) (s *QNTXServer, ran string) {
	t.Helper()
	s, _, _ = runningNamespaceAgents(t)
	s.deps.cfg.Agent.Root.Agy = flashInAgy
	require.Nil(t, s.keepGoogleLine("google:root-sub", googleKept{project: theProject, pool: thePool, provider: thePool + "/providers/node", account: theAccount}))
	binary, ran := acptest.StandIn(t, agyAnswered)
	fetched := make(chan struct{})
	close(fetched)
	s.holdHarness("agy", &harnessHeld{name: "agy", fetched: fetched, path: binary})
	return s, ran
}

func sayingToAgy(s *QNTXServer, sent sigil.Sent) (*protocol.AgySaid, *protocol.Refusal) {
	asked := httptest.NewRequest(http.MethodPost, "/api/agent/say", nil)
	answer, refused := s.harnessSay(sigil.WithCaller(context.Background(), asked), s.agyHarness(), sent)
	if refused != nil {
		return nil, refused
	}
	return answer.(*protocol.AgySaid), nil
}

// A node not connected to Google Cloud says nothing to agy, and says where to connect it.
func TestAgyIsSaidNothingUntilGoogleIsConnected(t *testing.T) {
	s, _, _ := runningNamespaceAgents(t)
	s.deps.cfg.Agent.Root.Agy = flashInAgy
	binary, ran := acptest.StandIn(t, agyAnswered)
	fetched := make(chan struct{})
	close(fetched)
	s.holdHarness("agy", &harnessHeld{name: "agy", fetched: fetched, path: binary})
	_, refused := sayingToAgy(s, sigil.Sent{"says": "hello"})
	require.NotNil(t, refused)
	assert.Contains(t, refused.GetSays(), "Node Connectors")
	_, err := os.Stat(filepath.Join(ran, "0"))
	assert.True(t, os.IsNotExist(err), "agy did not run")
}

// A node whose am.toml names no agy has none to speak to, and says where.
func TestANodeThatNamesNoAgyHasNoneToSpeakTo(t *testing.T) {
	s, _ := runningTheNodeAgent(t, opusLow)
	_, refused := sayingToAgy(s, sigil.Sent{"says": "hello"})
	require.NotNil(t, refused)
	assert.Equal(t, sigil.NotFound, refused.GetWhy())
	assert.Contains(t, refused.GetSays(), "[agent.root.agy]")
}

// agy says who answers in it and how it is run.
func TestAgySaysWhoItIs(t *testing.T) {
	s, _ := runningAgyToo(t)
	is, refused := s.harnessAm(s.agyHarness())
	require.Nil(t, refused)
	am := is.(*protocol.AgyAm)
	assert.Equal(t, s.nodeAgent.did, am.GetDid())
	assert.Equal(t, "gemini-3.8-flash", am.GetModel())
	assert.Equal(t, agy.PinnedVersion, am.GetAgyVersion())
	holds(t, s.nodeAgentSignum(), "am", &protocol.NodeAgentAm{Agy: am})
	require.NoError(t, s.nodeAgentSignum().Check())

	harness, refused := s.harnessNamed(sigil.Sent{"harness": "agy"})
	require.Nil(t, refused)
	assert.Equal(t, "agy", harness.name)
}
