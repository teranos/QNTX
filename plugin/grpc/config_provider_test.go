package grpc

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	appcfg "github.com/teranos/QNTX/internal/config"
)

func TestNewConfigProvider_WithoutEndpoints(t *testing.T) {
	provider := NewConfigProvider(nil)
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

	provider := NewConfigProvider(endpoints)
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
		{"_auth_token", "test-token-123"},
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

	provider := NewConfigProvider(endpoints)
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
	t.Cleanup(func() { SetPluginRecords(nil) })

	config := NewConfigProvider(nil).GetPluginConfig("cleanAPI")
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
	t.Cleanup(func() { SetPluginRecords(nil) })

	config := NewConfigProvider(nil).GetPluginConfig("pyre")
	assert.Empty(t, config.GetKeys())
	assert.Equal(t, 0, config.GetInt("poll_interval"))
}
