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
// it came from. sqlite is sqlite_3.46.0_96c92ab, duckdb is duckdb_1.4.3_d1dc88f.
// No schema sits beside SOURCE, so the embed above never carries these and the
// parity sigil holds no signum to them.

// Pinned is the version of the engine named, from the one directory under dir
// pinned for it.
func Pinned(dir, engine string) (string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", errors.Wrapf(err, "failed to read the pins under %s", dir)
	}
	var found []string
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		name, rest, ok := strings.Cut(entry.Name(), "_")
		if !ok || name != engine {
			continue
		}
		version, _, ok := strings.Cut(rest, "_")
		if !ok {
			return "", errors.Newf("%s names no commit: a pin is %s_<version>_<rev>", entry.Name(), engine)
		}
		if _, err := os.Stat(dir + "/" + entry.Name() + "/SOURCE"); err != nil {
			return "", errors.Wrapf(err, "%s under %s has no SOURCE", entry.Name(), dir)
		}
		found = append(found, version)
	}
	switch len(found) {
	case 0:
		return "", errors.Newf("%s is not pinned under %s", engine, dir)
	case 1:
		return found[0], nil
	default:
		return "", errors.Newf("%s is pinned more than once under %s: %s", engine, dir, strings.Join(found, ", "))
	}
}
