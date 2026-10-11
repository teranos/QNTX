package types

import (
	"slices"
	"testing"
)

// A plugin in another module cannot import internal/named, so it names its
// actors and version here, and nothing it hands is read as someone.
func TestAPluginOutsideNamesItsActorsAndVersion(t *testing.T) {
	by, err := ActorsOf("inbox", "did:key:z6asker")
	if err != nil {
		t.Fatalf("ActorsOf = %v", err)
	}
	if got := by.Actors.All(); !slices.Equal(got, []string{"inbox", "did:key:z6asker"}) {
		t.Errorf("ActorsOf named %v", got)
	}
	if _, err := ActorsOf(); err == nil {
		t.Error("ActorsOf naming nobody was not refused")
	}
	if _, err := ActorsOf("inbox", ""); err == nil {
		t.Error("ActorsOf naming an empty actor was not refused")
	}

	v, err := VersionOf("0.5.2")
	if err != nil || v.Version.String() != "0.5.2" {
		t.Errorf("VersionOf = %v, %v", v.Version.String(), err)
	}
	if _, err := VersionOf(""); err == nil {
		t.Error("VersionOf naming no version was not refused")
	}
}
