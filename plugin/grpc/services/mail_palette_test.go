package services

import (
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// "but i do still want the dark themed qntx tokens css email template"
//
// A mail client reads no CSS variables, so the palette is written out as
// values. This holds each one to the token it is copied from, so the two
// cannot drift apart.
func TestTheDarkPaletteIsQNTXsTokens(t *testing.T) {
	css, err := os.ReadFile("../../../web/css/tokens.css")
	require.NoError(t, err)

	tokens := map[string]string{}
	for _, line := range strings.Split(string(css), "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "--") {
			continue
		}
		name, value, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		value, _, _ = strings.Cut(value, ";")
		tokens[name] = strings.TrimSpace(value)
	}

	for token, value := range map[string]string{
		"--bg-almost-black":            Dark.Background,
		"--bg-secondary":               Dark.Surface,
		"--bg-tertiary":                Dark.Raised,
		"--border-on-dark":             Dark.Border,
		"--text-on-dark":               Dark.Text,
		"--text-on-dark-secondary":     Dark.Secondary,
		"--text-on-dark-emphasis":      Dark.Emphasis,
		"--accent-on-dark":             Dark.Accent,
		"--element-status-success-bg":  Dark.AccentDim,
		"--element-status-error-text":  Dark.Error,
		"--font-mono":                  Dark.Mono,
	} {
		assert.Equal(t, tokens[token], value, token)
	}
}
