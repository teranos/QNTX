package qntxcode

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestPluginMetadata verifies the code plugin returns correct metadata
func TestPluginMetadata(t *testing.T) {
	plugin := NewPlugin()
	meta := plugin.Metadata()

	assert.Equal(t, "code", meta.Name, "Plugin name must be 'code'")
	assert.Equal(t, "0.1.0", meta.Version, "Plugin version")
	assert.Equal(t, ">= 0.1.0", meta.QNTXVersion, "Required QNTX version")
	assert.Equal(t, "Software development domain (git, GitHub, gopls, code editor)", meta.Description)
	assert.Equal(t, "QNTX Team", meta.Author)
	assert.Equal(t, "MIT", meta.License)
}

// TestPluginMetadata_NotWebscraper explicitly verifies we're not returning webscraper metadata
func TestPluginMetadata_NotWebscraper(t *testing.T) {
	plugin := NewPlugin()
	meta := plugin.Metadata()

	// Regression test: Ensure we're not accidentally returning webscraper metadata
	assert.NotEqual(t, "webscraper", meta.Name, "Code plugin must not return 'webscraper' as name")
	assert.NotEqual(t, "0.2.0", meta.Version, "Code plugin must not return webscraper's version")
	assert.NotContains(t, meta.Description, "Web scraping", "Code plugin must not have webscraper description")
}
