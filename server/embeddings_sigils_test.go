package server

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/teranos/QNTX/plugin/grpc/protocol"
	"github.com/teranos/QNTX/server/sigil"
)

// A count sent as text reaches the handler as a number, and a list as a list.
func TestACountReachesTheHandlerAsANumber(t *testing.T) {
	takes := []*protocol.Param{
		{Name: "min_cluster_size", Kind: sigil.Count, Says: "fewest members"},
		{Name: "model", Says: "the model"},
	}
	arrived := map[string]any{"min_cluster_size": "7", "model": "m", "source_ids": []any{"a"}}
	handed := countsAsNumbers(arrived, sigil.Sent{"min_cluster_size": "7", "model": "m"}, takes)

	body, err := json.Marshal(handed)
	require.NoError(t, err)
	var decoded struct {
		MinClusterSize int      `json:"min_cluster_size"`
		Model          string   `json:"model"`
		SourceIDs      []string `json:"source_ids"`
	}
	require.NoError(t, json.Unmarshal(body, &decoded))
	assert.Equal(t, 7, decoded.MinClusterSize)
	assert.Equal(t, "m", decoded.Model)
	assert.Equal(t, []string{"a"}, decoded.SourceIDs)
	assert.Equal(t, "7", arrived["min_cluster_size"], "what arrived is not changed")
}
