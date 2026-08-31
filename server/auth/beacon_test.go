package auth

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A beacon's scope is its entire meaning: one predicate, written, nothing
// read, in one namespace that is not system. Anything wider is refused at the
// mint, because the credential is public the moment it is printed on a page.
func TestABeaconMintIsExactlyOneWrittenPredicate(t *testing.T) {
	h, _ := grantHandler(t)

	for _, body := range []string{
		`{"label":"door","level":"BEACON","scope":{"write":["card:scanned","card:noted"]}}`,
		`{"label":"door","level":"BEACON","scope":{"write":["card:scanned"],"read":["card:printed"]}}`,
		`{"label":"door","level":"BEACON","scope":{"write":["*"]}}`,
		`{"label":"door","level":"BEACON","scope":{"write":[]}}`,
		`{"label":"door","level":"BEACON","namespaces":["system"],"scope":{"write":["card:scanned"]}}`,
	} {
		rec := httptest.NewRecorder()
		mint(h, rec, mintRequest(body, ""))
		assert.Equal(t, http.StatusBadRequest, rec.Code, body)
	}
}

// The mint hands back the path the beacon answers on, raw value inside —
// shown once here, public forever where it is put.
func TestABeaconMintNamesItsPath(t *testing.T) {
	h, store := grantHandler(t)
	rec := httptest.NewRecorder()

	mint(h, rec, mintRequest(`{"label":"door","level":"BEACON","scope":{"write":["card:scanned"]}}`, ""))
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	var resp struct {
		Token      string `json:"token"`
		BeaconPath string `json:"beacon_path"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.Equal(t, BeaconPathPrefix+resp.Token+BeaconPathSuffix, resp.BeaconPath)

	grant, live := store.Lookup(sha256Hex(resp.Token))
	require.True(t, live)
	assert.Equal(t, LevelBeacon, grant.Level)
	assert.Equal(t, []string{"card:scanned"}, grant.ScopeWrite)
}

// A beacon is public by type: anyone holding the page holds the string, so it
// never authenticates a request, however live its record is.
func TestABeaconNeverAuthenticatesARequest(t *testing.T) {
	h, store := grantHandler(t)
	rec := httptest.NewRecorder()
	mint(h, rec, mintRequest(`{"label":"door","level":"BEACON","scope":{"write":["card:scanned"]}}`, ""))
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	var resp struct {
		Token string `json:"token"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	_, live := store.Lookup(sha256Hex(resp.Token))
	require.True(t, live, "the beacon is live in the store")

	req := httptest.NewRequest(http.MethodGet, "/api/attestations", nil)
	req.Header.Set("Authorization", "Bearer "+resp.Token)
	assert.Nil(t, h.presented(req).Bearer, "a beacon authenticated at the bearer path")
}

// The subject of an arrival speaks the beacon's own vocabulary: the caller
// names the individual, never the kind, and never anything but an identifier.
func TestABeaconSubjectSpeaksThePredicatesVocabulary(t *testing.T) {
	assert.Equal(t, "card:TIMDEV000001", BeaconSubject("card:scanned", "TIMDEV000001"))
	assert.Equal(t, "arrived:x-1_2.3", BeaconSubject("arrived", "x-1_2.3"))

	for _, local := range []string{"", "a b", "a/b", "a:b", strings.Repeat("x", 200)} {
		assert.Empty(t, BeaconSubject("card:scanned", local), local)
	}
}

// What survives of a stranger's query string is capped, and losing the tail
// is not losing the arrival.
func TestBeaconAttributesAreCapped(t *testing.T) {
	params := map[string][]string{
		"subject": {"TIMDEV000001"},
		"schema":  {"1"},
		"long":    {strings.Repeat("x", 200)},
	}
	for i := range 20 {
		params["k"+string(rune('a'+i))] = []string{"v"}
	}

	got := BeaconAttributes(params)
	assert.Equal(t, "1", got["schema"])
	assert.NotContains(t, got, "subject")
	assert.NotContains(t, got, "long")
	assert.LessOrEqual(t, len(got), 8)
}
