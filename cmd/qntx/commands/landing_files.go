package commands

import (
	"os"
	"path/filepath"

	"github.com/teranos/QNTX/internal/slug"
	"github.com/teranos/errors"
)

// landingPath is where a namespace's landing file is: beside the operational
// db, under namespaces/, named by the slug (ADR-037).
func landingPath(dbPath, name string) string {
	return filepath.Join(filepath.Dir(dbPath), "namespaces", slug.Of(name)+".db")
}

// removeLanding removes a namespace's landing file and everything kept beside
// it. A file that is not there is already what this asks for.
func removeLanding(dbPath, name string) error {
	path := landingPath(dbPath, name)
	flights, err := filepath.Glob(path + ".flight.*")
	if err != nil {
		return errors.Wrapf(err, "failed to list the flight records beside %s", path)
	}
	for _, file := range append([]string{path, path + "-wal", path + "-shm", path + ".taken-in"}, flights...) {
		if err := os.Remove(file); err != nil && !os.IsNotExist(err) {
			return errors.Wrapf(err, "failed to remove %s", file)
		}
	}
	return nil
}
