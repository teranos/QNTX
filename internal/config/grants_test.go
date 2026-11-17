package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// "a QNTX change that declares grants in am.toml"

// 「[Opus 5.5] auth.grants reads as the recommendation wrote it: a label, a did:key and」
// 「[Opus 5.5] the namespaces it acts in, one table each.」
func TestAuthGrantsReadAsWritten(t *testing.T) {
	cfg, err := LoadFromFile(writeConfig(t, `
[auth]
grants = [
  { label = "guest1", did = "did:key:z6MkhaXgBZDvotDkL5257faiztiGiC2QtKLGpbnnEGta2doK", namespaces = ["fleetingmem"] },
  { label = "guest2", did = "did:key:z6MkjchhfUsD6mmvni8mCdXHw216Xrm9bQe2mBH1P5RDjVJG", namespaces = ["fleetingmem", "garden"] },
]
`))
	require.NoError(t, err)
	assert.Equal(t, []GrantConfig{
		{Label: "guest1", DID: "did:key:z6MkhaXgBZDvotDkL5257faiztiGiC2QtKLGpbnnEGta2doK", Namespaces: []string{"fleetingmem"}},
		{Label: "guest2", DID: "did:key:z6MkjchhfUsD6mmvni8mCdXHw216Xrm9bQe2mBH1P5RDjVJG", Namespaces: []string{"fleetingmem", "garden"}},
	}, cfg.Auth.Grants)
}

// 「[Opus 5.5] An am.toml that lists no grants holds none.」
func TestNoAuthGrantsIsNone(t *testing.T) {
	cfg, err := LoadFromFile(writeConfig(t, `
[auth]
enabled = true
`))
	require.NoError(t, err)
	assert.Empty(t, cfg.Auth.Grants)
}
