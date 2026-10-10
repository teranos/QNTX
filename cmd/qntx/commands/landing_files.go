package commands

import (
	"os"
	"path/filepath"
	"time"

	"github.com/teranos/QNTX/internal/slug"
	"github.com/teranos/errors"
)

// sendInterval is how often a landing file sends what it holds to the record,
// and so the most a lost host loses. A crashed process loses nothing: the
// rows wait in the landing file for the next send.
//
// "let's go for 6h"
const sendInterval = 6 * time.Hour

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
	kept := []string{path, path + "-wal", path + "-shm", path + ".taken-in", path + ".sent", path + ".sent.next"}
	for _, file := range append(kept, flights...) {
		if err := os.Remove(file); err != nil && !os.IsNotExist(err) {
			return errors.Wrapf(err, "failed to remove %s", file)
		}
	}
	return nil
}
