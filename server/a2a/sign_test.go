package a2a

import (
	"crypto/ed25519"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/reflect/protoreflect"
)

// RFC 8785 §3.2.4's own example: members sorted, numbers as ECMAScript writes
// them, strings escaped as JSON.stringify escapes them.
func TestCanonicalIsRFC8785(t *testing.T) {
	in := `{
  "numbers": [333333333.33333329, 1E30, 4.50, 2e-3, 0.000000000000000000000000001],
  "string": "\u20ac$\u000F\u000aA'\u0042\u0022\u005c\\\"\/",
  "literals": [null, true, false]
}`
	out, err := Canonical([]byte(in))
	require.NoError(t, err)
	assert.Equal(t, `{"literals":[null,true,false],"numbers":[333333333.3333333,1e+30,4.5,0.002,1e-27],"string":"€$\u000f\nA'B\"\\\\\"/"}`, string(out))
}

// RFC 8785 §3.2.3: names are sorted by their UTF-16 code units, so a
// character outside the BMP sorts by its surrogates.
func TestCanonicalSortsByUTF16CodeUnits(t *testing.T) {
	out, err := Canonical([]byte(`{"\u20ac":1,"\r":2,"\ufb33":3,"1":4,"\ud83d\ude00":5,"\u0080":6,"\u00f6":7}`))
	require.NoError(t, err)
	assert.Equal(t, "{\"\\r\":2,\"1\":4,\"\u0080\":6,\"ö\":7,\"€\":1,\"😀\":5,\"\ufb33\":3}", string(out))
}

// Numbers as Number.prototype.toString writes them: plain from 1e-6 up to
// 1e21, exponent notation past either, and zero without its sign.
func TestCanonicalNumbersAreECMAScripts(t *testing.T) {
	for in, want := range map[string]string{
		"0": "0", "-0": "0", "1": "1", "-1": "-1", "4.50": "4.5",
		"1e20": "100000000000000000000", "1e21": "1e+21", "123e20": "1.23e+22",
		"0.000001": "0.000001", "1e-7": "1e-7", "-1.5e-7": "-1.5e-7",
		"9007199254740992": "9007199254740992", "5e-324": "5e-324",
		"1.7976931348623157e308": "1.7976931348623157e+308",
	} {
		out, err := Canonical([]byte(in))
		require.NoError(t, err, in)
		assert.Equal(t, want, string(out), in)
	}
}

func TestCanonicalRefusesWhatIsNotOneJSONValue(t *testing.T) {
	for _, in := range []string{`{`, `1 2`, `1e400`} {
		_, err := Canonical([]byte(in))
		assert.Error(t, err, in)
	}
}

// signingKey is a key the way a node holds one, and the key id it goes by.
func signingKey() (ed25519.PrivateKey, string) {
	return ed25519.NewKeyFromSeed(make([]byte, ed25519.SeedSize)), "did:key:z6Mk#z6Mk"
}

func aCard(t *testing.T) protoreflect.Message {
	t.Helper()
	card, err := Card{Name: "QNTX", Description: "A node.", Version: "1.0.0", URL: "https://node.example/a2a",
		Skills:     []Skill{{ID: "staands", Name: "staands", Description: "Stands.", Tags: []string{"analytics"}}},
		Extensions: []Extension{{URI: "https://example.org/ext", Description: "x", Params: map[string]any{"count": 3, "ratio": 0.5}}},
	}.Message()
	require.NoError(t, err)
	return card
}

// A signed card verifies against the key that signed it, under its key id;
// one byte of the card changed, it does not; and signing again leaves one
// signature, not two (§8.4).
func TestASignedCardVerifiesAndATamperedOneDoesNot(t *testing.T) {
	key, kid := signingKey()
	card := aCard(t)
	require.NoError(t, Sign(card, key, kid))
	require.NoError(t, Verify(card, key.Public().(ed25519.PublicKey), kid))

	require.NoError(t, Sign(card, key, kid))
	assert.Equal(t, 1, card.Get(card.Descriptor().Fields().ByName("signatures")).List().Len(), "a card signed twice carries two signatures")

	other := ed25519.NewKeyFromSeed(append(make([]byte, ed25519.SeedSize-1), 1))
	assert.Error(t, Verify(card, other.Public().(ed25519.PublicKey), kid), "a card verified against a key that did not sign it")
	assert.Error(t, Verify(card, key.Public().(ed25519.PublicKey), "did:key:other#other"), "a card verified under a key id it was not signed under")

	card.Set(card.Descriptor().Fields().ByName("name"), protoreflect.ValueOfString("Not QNTX"))
	assert.Error(t, Verify(card, key.Public().(ed25519.PublicKey), kid), "a card whose name changed still verifies")
}
