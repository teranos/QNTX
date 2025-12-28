//go:build !prod

package server

import "embed"

// Stub embeds for development/testing - no actual files needed
var webFiles embed.FS
var proseFilesEmbedded embed.FS
