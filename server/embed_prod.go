//go:build prod

package server

import "embed"

//go:embed dist
var webFiles embed.FS

//go:embed docs_embedded
var proseFilesEmbedded embed.FS
