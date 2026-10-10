package parity

import (
	"os"
	"strings"

	"github.com/teranos/errors"
)

// "We should pin our storage backends in Nix and deal with it through parity
// like everything else in the system."
//
// A storage engine is pinned the way a reference is: a directory named for the
// engine, its version and the commit it was taken at, with SOURCE saying where
// it came from. The directory name is the one place the version is written.
// No schema sits beside SOURCE, so the embed above never carries these and the
// parity sigil holds no signum to them.

// Pin is what an engine's pin names: its version, and the commit it was taken
// at, by the 7 characters git abbreviates a commit to.
type Pin struct {
	Version string
	Rev     string
}

// PinOf is the one pin under dir for the engine named.
func PinOf(dir, engine string) (Pin, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return Pin{}, errors.Wrapf(err, "failed to read the pins under %s", dir)
	}
	var found []Pin
	var names []string
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		name, rest, ok := strings.Cut(entry.Name(), "_")
		if !ok || name != engine {
			continue
		}
		version, rev, ok := strings.Cut(rest, "_")
		if !ok || len(rev) != 7 || strings.ContainsFunc(rev, notHex) {
			return Pin{}, errors.Newf("%s names no commit: a pin is %s_<version>_<7 hex of the commit>", entry.Name(), engine)
		}
		source := dir + "/" + entry.Name() + "/SOURCE"
		info, err := os.Stat(source)
		if err != nil {
			return Pin{}, errors.Wrapf(err, "%s under %s has no SOURCE", entry.Name(), dir)
		}
		if !info.Mode().IsRegular() {
			return Pin{}, errors.Newf("%s is not a file", source)
		}
		found = append(found, Pin{Version: version, Rev: rev})
		names = append(names, entry.Name())
	}
	if len(found) != 1 {
		return Pin{}, errors.Newf("%s is pinned %d times under %s, and is pinned once: [%s]", engine, len(found), dir, strings.Join(names, ", "))
	}
	return found[0], nil
}

func notHex(r rune) bool {
	return !strings.ContainsRune("0123456789abcdef", r)
}
