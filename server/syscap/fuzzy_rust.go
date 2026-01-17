//go:build cgo && rustfuzzy

package syscap

import (
	"github.com/teranos/QNTX/ats/ax/fuzzy-ax/fuzzyax"
)

// fuzzyBackendVersion returns the fuzzy-ax library version
func fuzzyBackendVersion() string {
	return fuzzyax.Version()
}
