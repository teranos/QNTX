package grpc

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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

func TestNewConfigProvider_NonEndpointKeysFallThrough(t *testing.T) {
	endpoints := &ServiceEndpoints{LLMAddress: "localhost:5555"}
	provider := NewConfigProvider(endpoints)
	config := provider.GetPluginConfig("testdomain")

	// Regular keys go through am config (returns zero values in test)
	assert.Equal(t, 0, config.GetInt("some_number"))
	assert.Equal(t, false, config.GetBool("some_flag"))
}
