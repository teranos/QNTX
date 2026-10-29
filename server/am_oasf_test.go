package server

import (
	"encoding/json"
	"regexp"
	"testing"
	"time"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/teranos/QNTX/server/auth"
	"github.com/teranos/QNTX/server/parity"
	"github.com/teranos/QNTX/server/sigil"
)

// am oasf is the node as an OASF Record: the skills its card's signa name,
// with the ids OASF derives, the card am node gives and the tools /mcp lists
// as its modules, and what OASF requires that the node does not have.
func TestAmOASFIsTheNodeAsARecord(t *testing.T) {
	srv, _ := pluginServingServer(t, "fake")
	signum := srv.amSignum()
	asked := askedAs(auth.LevelRoot)
	ctx := sigil.WithCaller(asked.Context(), asked)
	answer, refused := signum.Answers["oasf"](ctx, nil)
	require.Nil(t, refused)
	holds(t, signum, "oasf", answer)

	body, err := json.Marshal(answer)
	require.NoError(t, err)
	var got struct {
		Record struct {
			SchemaVersion string `json:"schema_version"`
			Skills        []struct {
				Name string `json:"name"`
				ID   uint32 `json:"id"`
			} `json:"skills"`
			Modules []struct {
				Name string          `json:"name"`
				ID   uint32          `json:"id"`
				Data json.RawMessage `json:"data"`
			} `json:"modules"`
		} `json:"record"`
		Missing []string `json:"missing"`
	}
	require.NoError(t, json.Unmarshal(body, &got))
	assert.Equal(t, "1.1.0", got.Record.SchemaVersion)

	skills, refused := parity.OASFSkills()
	require.Nil(t, refused)
	require.NotEmpty(t, got.Record.Skills, "ROOT's card carries signa that name OASF skills")
	for _, skill := range got.Record.Skills {
		assert.Equal(t, skills[skill.Name], skill.ID, "%s", skill.Name)
	}

	require.Len(t, got.Record.Modules, 2)
	assert.Equal(t, "integration/a2a", got.Record.Modules[0].Name)
	assert.Equal(t, uint32(203), got.Record.Modules[0].ID)
	var a2aData struct {
		CardData          json.RawMessage `json:"card_data"`
		CardSchemaVersion string          `json:"card_schema_version"`
	}
	require.NoError(t, json.Unmarshal(got.Record.Modules[0].Data, &a2aData))
	assert.Equal(t, "1.0", a2aData.CardSchemaVersion)
	node, refused := signum.Answers["node"](ctx, nil)
	require.Nil(t, refused)
	assert.JSONEq(t, string(node.(amNode).Card), string(a2aData.CardData), "the card is the one am node gives")

	assert.Equal(t, "integration/mcp", got.Record.Modules[1].Name)
	assert.Equal(t, uint32(202), got.Record.Modules[1].ID)
	var mcpData struct {
		Tools []struct {
			Name string `json:"name"`
		} `json:"tools"`
		Connections []struct {
			Type string `json:"type"`
			URL  string `json:"url"`
		} `json:"connections"`
	}
	require.NoError(t, json.Unmarshal(got.Record.Modules[1].Data, &mcpData))
	require.Len(t, mcpData.Connections, 1)
	assert.Equal(t, "streamable-http", mcpData.Connections[0].Type)
	assert.Equal(t, "https://node.example/mcp", mcpData.Connections[0].URL)
	var named, listed []string
	for _, tool := range mcpData.Tools {
		named = append(named, tool.Name)
	}
	for _, tool := range toolsOfferedFor(t, srv, asked) {
		listed = append(listed, tool.Name)
	}
	assert.Equal(t, listed, named, "the tools are the ones /mcp lists to the same caller")

	assert.Contains(t, got.Missing, "authors")
	assert.Contains(t, got.Missing, "created_at")
	assert.NotContains(t, got.Missing, "skills")
	assert.NotContains(t, got.Missing, "schema_version")

	// Held to the schema OASF's server gives a Record, it fails only for what
	// it says it leaves empty: with those filled, it is a Record.
	raw, refused := parity.OASFRecordSchema()
	require.Nil(t, refused)
	var schema jsonschema.Schema
	require.NoError(t, json.Unmarshal(flatDefs(t, raw), &schema))
	resolved, err := schema.Resolve(nil)
	require.NoError(t, err)
	var record map[string]any
	require.NoError(t, json.Unmarshal(answer.(amOASF).Record, &record))
	filled := time.Now().UTC().Format(time.RFC3339)
	for _, name := range got.Missing {
		require.NotContains(t, record, name, "%s is said missing and is there", name)
		if schema.Properties[name].Type == "array" {
			record[name] = []any{filled}
		} else {
			record[name] = filled
		}
	}
	assert.NoError(t, resolved.Validate(record), "the record is an OASF Record, but for what it says it leaves empty")
}

// flatDefs is a schema whose $defs group their schemas one level down,
// "#/$defs/skills/x", with each brought up to "#/$defs/skills~1x": the same
// schema, addressed so that jsonschema-go, which follows a pointer only
// through schemas, resolves it.
func flatDefs(t *testing.T, raw []byte) []byte {
	t.Helper()
	var schema map[string]any
	require.NoError(t, json.Unmarshal(raw, &schema))
	grouped, _ := schema["$defs"].(map[string]any)
	flat := map[string]any{}
	for group, defs := range grouped {
		for name, def := range defs.(map[string]any) {
			flat[group+"/"+name] = def
		}
	}
	schema["$defs"] = flat
	out, err := json.Marshal(schema)
	require.NoError(t, err)
	return []byte(regexp.MustCompile(`"#/\$defs/([a-z_]+)/`).ReplaceAllString(string(out), `"#/$$defs/${1}~1`))
}
