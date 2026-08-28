// Package projectctx names the namespace attestations from this checkout land in.
package projectctx

import (
	"os"
	"path/filepath"
)

// Unknown is used when the working directory cannot be read. It is deliberately
// not "project:./.", which every checkout would otherwise share.
const Unknown = "project:unknown"

// frozen is computed once from the startup working directory, because os.Getwd
// can shift during shutdown and the namespace must not move with it.
var frozen = compute()

func compute() string {
	cwd, err := os.Getwd()
	if err != nil {
		return Unknown
	}
	return "project:" + filepath.Join(filepath.Base(filepath.Dir(cwd)), filepath.Base(cwd))
}

// Namespace returns the project namespace for this checkout.
func Namespace() string { return frozen }
