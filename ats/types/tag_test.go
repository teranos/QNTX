package types

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// The tag a predicate names, and nothing for a predicate that names no tag.
func TestTaggedNamesTheTag(t *testing.T) {
	tag, ok := Tagged("tag:ci-runner")
	assert.True(t, ok)
	assert.Equal(t, "ci-runner", tag)

	// A tag under a tag is one tag, named by everything after the marker.
	tag, ok = Tagged("tag:incident:sev1")
	assert.True(t, ok)
	assert.Equal(t, "incident:sev1", tag)

	// Not a tag at all.
	_, ok = Tagged("noted")
	assert.False(t, ok)
	_, ok = Tagged("distill:noted")
	assert.False(t, ok)

	// Every tag rather than one. The write path refuses this before Tagged is
	// asked; answering false here is the same answer said twice.
	_, ok = Tagged("tag:")
	assert.False(t, ok)
	_, ok = Tagged("tag: ")
	assert.False(t, ok)
}

// Every tag the predicates name, in the order named and without repeats: one
// attestation carrying a tag twice attests the tag once.
func TestTagsNamedKeepsOrderAndDropsRepeats(t *testing.T) {
	named := TagsNamed([]string{"tag:incident", "noted", "tag:ci-runner", "tag:incident", "tag:"})

	assert.Equal(t, []string{"incident", "ci-runner"}, named)
	assert.Empty(t, TagsNamed([]string{"noted", "type"}))
	assert.Empty(t, TagsNamed(nil))
}

// What a tag says on the day it is born: its own name, and nothing about how it
// looks. The colour is attested, so it is somebody's to say.
func TestATagIsBornSayingItsOwnNameAndNothingElse(t *testing.T) {
	def := TagDef("ci-runner")

	assert.Equal(t, "ci-runner", def.Name)
	assert.Equal(t, "ci-runner", def.Label)
	assert.Empty(t, def.Color, "this build has no opinion about what a tag looks like")
	assert.Empty(t, def.RichStringFields)
	assert.Empty(t, def.ArrayFields)
	assert.False(t, def.Deprecated)
}

// A tag that already says something keeps saying it. Being born is the only
// time this build says anything about a tag.
func TestATagThatAlreadySaysSomethingIsLeftAlone(t *testing.T) {
	store := &MockAttestationStore{}

	chosen := TagDef("ci-runner")
	chosen.Color = "#e67e22"

	if err := EnsureTypesExist(store, saying(t, chosen), "tagging", TagDefs([]string{"ci-runner"})...); err != nil {
		t.Fatalf("EnsureTypesExist returned %v", err)
	}

	assert.Empty(t, store.attestations, "a colour somebody chose was attested over")
}

// A tag nothing has said anything about is attested, which is how it comes to
// exist at all.
func TestATagNothingHasSaidIsAttested(t *testing.T) {
	store := &MockAttestationStore{}

	if err := EnsureTypesExist(store, SaysNothing, "tagging", TagDefs([]string{"ci-runner", "incident"})...); err != nil {
		t.Fatalf("EnsureTypesExist returned %v", err)
	}

	assert.Len(t, store.attestations, 2)
	assert.Equal(t, []string{"ci-runner"}, store.attestations[0].Subjects)
	assert.Equal(t, []string{"type"}, store.attestations[0].Predicates)
	// Self-certifying: the tag is its own actor, the way any type is.
	assert.Equal(t, []string{"ci-runner"}, store.attestations[0].Actors)
}
