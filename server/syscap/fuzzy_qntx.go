//go:build qntxwasm

package syscap

import "github.com/teranos/QNTX/ats/wasm"

// fuzzyBackendVersion returns the qntx-core version when WASM fuzzy is available.
func fuzzyBackendVersion() string {
	engine, err := wasm.GetEngine()
	if err != nil {
		return ""
	}
	version, err := engine.CallNoArgs("qntx_core_version")
	if err != nil {
		return ""
	}
	return version
}
