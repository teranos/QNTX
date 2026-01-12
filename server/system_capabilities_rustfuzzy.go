//go:build cgo && rustfuzzy

package server

import (
	"github.com/teranos/QNTX/ats/ax/fuzzy-ax/fuzzyax"
)

// fuzzyBackendVersion returns the fuzzy-ax library version
func fuzzyBackendVersion() string {
	return fuzzyax.Version()
}
