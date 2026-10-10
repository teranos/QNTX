package grpc

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	appcfg "github.com/teranos/QNTX/internal/config"
	"github.com/teranos/errors"
	"go.uber.org/zap"
)

func TestNewConfigProvider_WithoutEndpoints(t *testing.T) {
	provider := NewConfigProvider(nil, nil, zap.NewNop().Sugar())
	require.NotNil(t, provider)

	config := provider.GetPluginConfig("testdomain")
	require.NotNil(t, config)

	// Without endpoints, service keys return empty
	assert.Equal(t, "", config.GetString("_llm_endpoint"))
}

func TestNewConfigProvider_InjectsEndpoints(t *testing.T) {
	endpoints := &ServiceEndpoints{
		ATSStoreAddress:     "localhost:9001",
		QueueAddress:        "localhost:9002",
		ScheduleAddress:     "localhost:9003",
		FileServiceAddress:  "localhost:9004",
		LLMAddress:          "localhost:9005",
		EmbeddingAddress:    "localhost:9006",
		VectorSearchAddress: "localhost:9007",
		GroundAddress:       "localhost:9008",
		SearchAddress:       "localhost:9009",
		MailAddress:         "localhost:9010",
		AuthToken:           "test-token-123",
	}

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
		{"_llm_endpoint", "localhost:9005"},
		{"_embedding_endpoint", "localhost:9006"},
		{"_vector_search_endpoint", "localhost:9007"},
		{"_ground_endpoint", "localhost:9008"},
		{"_search_endpoint", "localhost:9009"},
		{"_mail_endpoint", "localhost:9010"},
	}

	for _, tc := range cases {
		t.Run(tc.key, func(t *testing.T) {
			assert.Equal(t, tc.want, config.GetString(tc.key))
		})
	}
}

func TestNewConfigProvider_GetAlsoInjectsEndpoints(t *testing.T) {
	endpoints := &ServiceEndpoints{
		LLMAddress: "localhost:5555",
	}

	provider := NewConfigProvider(endpoints, nil, zap.NewNop().Sugar())
	config := provider.GetPluginConfig("x")

	assert.Equal(t, "localhost:5555", config.Get("_llm_endpoint"))
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

	config := NewConfigProvider(nil, nil, zap.NewNop().Sugar()).GetPluginConfig("cleanAPI")
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

	config := NewConfigProvider(nil, nil, zap.NewNop().Sugar()).GetPluginConfig("pyre")
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

	provider := NewConfigProvider(&ServiceEndpoints{AuthToken: "shared"}, minted, zap.NewNop().Sugar())
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

	config := NewConfigProvider(&ServiceEndpoints{AuthToken: "shared"}, minted, zap.NewNop().Sugar()).GetPluginConfig("datapunt")
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

	config := NewConfigProvider(&ServiceEndpoints{AuthToken: "shared"}, refused, zap.NewNop().Sugar()).GetPluginConfig("cleanAPI")
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

	config := NewConfigProvider(&ServiceEndpoints{AuthToken: "shared"}, refused, zap.NewNop().Sugar()).GetPluginConfig("cleanAPI")
	assert.Equal(t, "", config.GetString("_auth_token"))
	assert.Equal(t, []string{""}, asked, "an empty namespace was not handed to the node")
	require.Error(t, config.(interface{ Err() error }).Err(), "an empty namespace started the plugin standing nowhere")
}
