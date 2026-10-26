package types

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// "v is tag:k of subject": the key is what follows the marker.
func TestTheKeyIsWhatFollowsTheMarker(t *testing.T) {
	key, ok := TagKey("tag:Name")
	assert.True(t, ok)
	assert.Equal(t, "Name", key)

	_, ok = TagKey("tag:")
	assert.False(t, ok, "tag: alone names every key, not one")
	_, ok = TagKey("tag: ")
	assert.False(t, ok, "a key named by a space is a key nobody named")
	_, ok = TagKey("Name")
	assert.False(t, ok)
}

func TestKeysAreNamedOnceInTheOrderGiven(t *testing.T) {
	assert.Equal(t, []string{"Name", "Owner"},
		TagKeys([]string{"tag:Name", "noted", "tag:Owner", "tag:Name"}))
	assert.Empty(t, TagKeys([]string{"noted", "type"}))
}

// "a tag is a type", and the key is the tag: it is born saying its own name
// and nothing about how it looks.
func TestAKeyIsBornSayingItsOwnName(t *testing.T) {
	def := TagDef("Name")
	assert.Equal(t, "Name", def.Name)
	assert.Equal(t, "Name", def.Label)
	assert.Empty(t, def.Color)
}

// MyRootAgent is tag:Name of [agent did] by [user that tagged]
func TestAThingsNameIsTheValueOfItsNameTag(t *testing.T) {
	found := []*As{{
		Subjects:   []string{"MyRootAgent"},
		Predicates: []string{"tag:Name"},
		Contexts:   []string{"did:key:agent"},
		Actors:     []string{"US-TIM-7K4M3B9X"},
		Timestamp:  time.Unix(100, 0),
	}}

	assert.Equal(t, map[string]string{"Name": "MyRootAgent"}, TagsOf(found, "did:key:agent"))
	assert.Empty(t, TagsOf(found, "did:key:other"), "a tag holds for the thing it is of and no other")
}

// One value per key per thing, and a second one is "Replace".
func TestANewerValueForAKeyReplacesTheOlder(t *testing.T) {
	older := &As{Subjects: []string{"Old"}, Predicates: []string{"tag:Name"},
		Contexts: []string{"did:key:agent"}, Timestamp: time.Unix(100, 0)}
	newer := &As{Subjects: []string{"MyRootAgent"}, Predicates: []string{"tag:Name"},
		Contexts: []string{"did:key:agent"}, Timestamp: time.Unix(200, 0)}

	// Whichever order the store answers in, the newer holds.
	assert.Equal(t, "MyRootAgent", TagsOf([]*As{older, newer}, "did:key:agent")["Name"])
	assert.Equal(t, "MyRootAgent", TagsOf([]*As{newer, older}, "did:key:agent")["Name"])
}

// Keys are freely defined, and each holds its own value.
func TestEachKeyHoldsItsOwnValue(t *testing.T) {
	found := []*As{
		{Subjects: []string{"MyRootAgent"}, Predicates: []string{"tag:Name"},
			Contexts: []string{"did:key:agent"}, Timestamp: time.Unix(100, 0)},
		{Subjects: []string{"garden"}, Predicates: []string{"tag:Home"},
			Contexts: []string{"did:key:agent"}, Timestamp: time.Unix(50, 0)},
	}

	assert.Equal(t, map[string]string{"Name": "MyRootAgent", "Home": "garden"},
		TagsOf(found, "did:key:agent"))
}
