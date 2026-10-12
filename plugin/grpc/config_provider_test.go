package grpc

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	appcfg "github.com/teranos/QNTX/internal/config"
	"github.com/teranos/QNTX/plugin/grpc/protocol"
	errors "github.com/teranos/sacred-error"
	"go.uber.org/zap"
)

// noServices is a node whose plugin services did not start.
var noServices = UnservedEndpoints(errors.New("this test serves no plugin services"))

// A plugin asking for an endpoint on a node whose plugin services did not
// start is told why, and is not handed an empty address as if one were served.
func TestNewConfigProvider_WithoutEndpoints(t *testing.T) {
	SetPluginRecords(heldRecords{"testdomain": {Name: "testdomain", Config: map[string]string{}}})
	t.Cleanup(func() { SetPluginRecords(recordsNotHanded{}) })

	config := NewConfigProvider(noServices, nil, zap.NewNop().Sugar()).GetPluginConfig("testdomain")
	require.NotNil(t, config)

	assert.Equal(t, "", config.GetString("_ats_store_endpoint"))
	err := config.(interface{ Err() error }).Err()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "this test serves no plugin services")
	assert.Contains(t, err.Error(), "_ats_store_endpoint")

	_, err = config.(endpointsHanded).ServiceEndpoints()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "this test serves no plugin services")
}

func TestNewConfigProvider_InjectsEndpoints(t *testing.T) {
	endpoints := ServedEndpoints(ServiceEndpoints{
		ATSStore:    ServingAt("localhost:9001"),
		Queue:       ServingAt("localhost:9002"),
		Schedule:    ServingAt("localhost:9003"),
		FileService: ServingAt("localhost:9004"),
		LLM:         ServingAt("localhost:9005"),
		AuthToken:   "test-token-123",
	})

	provider := NewConfigProvider(endpoints, nil, zap.NewNop().Sugar())
	config := provider.GetPluginConfig("anydomain")

	cases := []struct {
		key  string
		want string
	}{
		{"_ats_store_endpoint", "localhost:9001"},
		{"_queue_endpoint", "localhost:9002"},
		{"_schedule_endpoint", "localhost:9003"},
		{"_file_service_endpoint", "localhost:9004"},
	}

	for _, tc := range cases {
		t.Run(tc.key, func(t *testing.T) {
			assert.Equal(t, tc.want, config.GetString(tc.key))
		})
	}

	served, err := config.(endpointsHanded).ServiceEndpoints()
	require.NoError(t, err)
	llm, err := served.LLM.Address()
	require.NoError(t, err)
	assert.Equal(t, "localhost:9005", llm)
}

// A service the node runs without is handed to a plugin as why, in place of
// an address; one the node named nothing for is refused, not handed as empty.
func TestAServiceTheNodeRunsWithoutIsHandedAsWhy(t *testing.T) {
	why := errors.New("the mail service did not start: port taken")
	req := &protocol.InitializeRequest{}
	SetPluginRecords(heldRecords{"x": {Name: "x", Config: map[string]string{}}})
	t.Cleanup(func() { SetPluginRecords(recordsNotHanded{}) })

	every := ServiceEndpoints{
		LLM: ServingAt("localhost:1"), Embedding: ServingAt("localhost:2"), VectorSearch: ServingAt("localhost:3"),
		Search: ServingAt("localhost:5"), Fetch: ServingAt("localhost:6"),
		Mail: NotRunning(why),
	}
	config := NewConfigProvider(ServedEndpoints(every), nil, zap.NewNop().Sugar()).GetPluginConfig("x")
	require.NoError(t, handOptionalEndpoints(req, config))
	assert.Equal(t, "localhost:1", req.GetLlmEndpoint().GetAddress())
	assert.Equal(t, why.Error(), req.GetMailEndpoint().GetNotRunning())

	_, err := EndpointFrom("mail", req.GetMailEndpoint()).Address()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "port taken")

	_, err = EndpointFrom("ground", nil).Address()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "named no ground endpoint")

	unnamed := ServiceEndpoints{LLM: ServingAt("localhost:1")}
	config = NewConfigProvider(ServedEndpoints(unnamed), nil, zap.NewNop().Sugar()).GetPluginConfig("x")
	err = handOptionalEndpoints(&protocol.InitializeRequest{}, config)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "names no address and no reason")
}

// A plugin is handed the config the plugin element saved in its record, a
// plugin named with a capital included.
func TestAPluginIsHandedTheConfigItsRecordHolds(t *testing.T) {
	SetPluginRecords(heldRecords{"cleanAPI": {Name: "cleanAPI", Config: map[string]string{
		"token":         "ssm:///q/box/token",
		"poll_interval": "300",
		"verbose":       "true",
	}}})
	t.Cleanup(func() { SetPluginRecords(recordsNotHanded{}) })

	config := NewConfigProvider(noServices, nil, zap.NewNop().Sugar()).GetPluginConfig("cleanAPI")
	assert.ElementsMatch(t, []string{"token", "poll_interval", "verbose"}, config.GetKeys())
	assert.Equal(t, "ssm:///q/box/token", config.Get("token"))
	assert.Equal(t, 300, config.GetInt("poll_interval"))
	assert.True(t, config.GetBool("verbose"))
}

// The plugin's record is the whole of its config.
func TestAPluginsConfigIsItsRecord(t *testing.T) {
	appcfg.Set("pyre.poll_interval", "300")
	t.Cleanup(appcfg.Reset)
	SetPluginRecords(heldRecords{"pyre": {Name: "pyre", Config: map[string]string{}}})
	t.Cleanup(func() { SetPluginRecords(recordsNotHanded{}) })

	config := NewConfigProvider(noServices, nil, zap.NewNop().Sugar()).GetPluginConfig("pyre")
	assert.Empty(t, config.GetKeys())
	assert.Equal(t, 0, config.GetInt("poll_interval"))
}

// A plugin whose record names a namespace is handed a token of its own for it,
// minted by the node (ADR-046).
func TestAPluginStandingInANamespaceIsHandedItsOwnToken(t *testing.T) {
	SetPluginRecords(heldRecords{
		"cleanAPI": {Name: "cleanAPI", Config: map[string]string{PluginNamespaceKey: "Clean"}},
	})
	t.Cleanup(func() { SetPluginRecords(recordsNotHanded{}) })
	minted := func(plugin, namespace string) (string, error) {
		return plugin + "@" + namespace, nil
	}

	provider := NewConfigProvider(ServedEndpoints(ServiceEndpoints{AuthToken: "shared"}), minted, zap.NewNop().Sugar())
	assert.Equal(t, "cleanAPI@Clean", provider.GetPluginConfig("cleanAPI").GetString("_auth_token"))
}

// "nil is nil"

// A record naming no namespace stands nowhere, and is handed no token: not the
// shared one on the served store. It still starts, and acts only where a
// caller acts, through the call's own token.
func TestAPluginNamingNoNamespaceIsHandedNoToken(t *testing.T) {
	SetPluginRecords(heldRecords{"datapunt": {Name: "datapunt", Config: map[string]string{}}})
	t.Cleanup(func() { SetPluginRecords(recordsNotHanded{}) })
	minted := func(plugin, namespace string) (string, error) {
		return plugin + "@" + namespace, nil
	}

	config := NewConfigProvider(ServedEndpoints(ServiceEndpoints{AuthToken: "shared"}), minted, zap.NewNop().Sugar()).GetPluginConfig("datapunt")
	assert.Equal(t, "", config.GetString("_auth_token"), "a plugin naming no namespace was handed a token")
	assert.NoError(t, config.(interface{ Err() error }).Err(), "a plugin naming no namespace was refused its start")
}

// A namespace the node cannot mint a token for is a plugin not handed its
// config, said with the reason, never a plugin started on an empty token.
func TestATokenTheNodeCannotMintFailsTheInitialize(t *testing.T) {
	SetPluginRecords(heldRecords{"cleanAPI": {Name: "cleanAPI", Config: map[string]string{PluginNamespaceKey: "pond"}}})
	t.Cleanup(func() { SetPluginRecords(recordsNotHanded{}) })
	refused := func(plugin, namespace string) (string, error) {
		return "", errors.Newf("namespace %s is not served", namespace)
	}

	config := NewConfigProvider(ServedEndpoints(ServiceEndpoints{AuthToken: "shared"}), refused, zap.NewNop().Sugar()).GetPluginConfig("cleanAPI")
	assert.Equal(t, "", config.GetString("_auth_token"))
	err := config.(interface{ Err() error }).Err()
	require.Error(t, err, "a token the node could not mint was not said")
	assert.Contains(t, err.Error(), "pond")
}

// A record whose namespace key is there but names nothing is the node's to
// refuse, as any namespace it does not serve, not a plugin standing nowhere.
func TestAnEmptyNamespaceIsTheNodesToRefuse(t *testing.T) {
	SetPluginRecords(heldRecords{"cleanAPI": {Name: "cleanAPI", Config: map[string]string{PluginNamespaceKey: ""}}})
	t.Cleanup(func() { SetPluginRecords(nil) })
	var asked []string
	refused := func(plugin, namespace string) (string, error) {
		asked = append(asked, namespace)
		return "", errors.Newf("namespace %q is not served", namespace)
	}

	config := NewConfigProvider(ServedEndpoints(ServiceEndpoints{AuthToken: "shared"}), refused, zap.NewNop().Sugar()).GetPluginConfig("cleanAPI")
	assert.Equal(t, "", config.GetString("_auth_token"))
	assert.Equal(t, []string{""}, asked, "an empty namespace was not handed to the node")
	require.Error(t, config.(interface{ Err() error }).Err(), "an empty namespace started the plugin standing nowhere")
}
