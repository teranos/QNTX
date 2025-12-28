//go:build testing

package server

import "embed"

// Stub embeds for testing - no actual files needed
var webFiles embed.FS
var proseFilesEmbedded embed.FS
