package storage

import (
	"os"
	"strings"
	"time"

	"github.com/teranos/errors"
)

// FileMark is a Mark kept in a file beside the operational db: one RFC3339
// instant, which a person can read, and delete to make the next open take in
// the whole record again.
type FileMark struct {
	Path string
}

func (m FileMark) Read() (time.Time, bool, error) {
	body, err := os.ReadFile(m.Path)
	if err != nil {
		if os.IsNotExist(err) {
			return time.Time{}, false, nil
		}
		return time.Time{}, false, errors.Wrapf(err, "failed to read the take-in mark at %s", m.Path)
	}
	at, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(string(body)))
	if err != nil {
		return time.Time{}, false, errors.Wrapf(err, "the take-in mark at %s is not an RFC3339 instant", m.Path)
	}
	return at, true, nil
}

func (m FileMark) Write(at time.Time) error {
	body := at.UTC().Format(time.RFC3339Nano) + "\n"
	if err := os.WriteFile(m.Path, []byte(body), 0o640); err != nil {
		return errors.Wrapf(err, "failed to write the take-in mark at %s", m.Path)
	}
	return nil
}
