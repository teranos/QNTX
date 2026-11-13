package apns

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"io"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	errors "github.com/teranos/sacred-error"
)

// aKey is an APNs key as Apple gives one: a P-256 key, PKCS#8, PEM.
func aKey(t *testing.T) (Key, *ecdsa.PublicKey) {
	t.Helper()
	made, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	der, err := x509.MarshalPKCS8PrivateKey(made)
	require.NoError(t, err)
	return Key{P8: pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der}), KeyID: "KEY1234567", TeamID: "TEAM123456"}, &made.PublicKey
}

// apple stands in for Apple: it keeps what it was sent and answers as told.
type apple struct {
	*httptest.Server
	asked  []*http.Request
	bodies []map[string]any
	status int
	says   string
}

func standIn(t *testing.T) *apple {
	t.Helper()
	a := &apple{status: http.StatusOK}
	a.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		var body map[string]any
		_ = json.Unmarshal(raw, &body)
		a.asked, a.bodies = append(a.asked, r), append(a.bodies, body)
		w.Header().Set("apns-id", "an-apns-id")
		w.WriteHeader(a.status)
		_, _ = io.WriteString(w, a.says)
	}))
	t.Cleanup(a.Close)
	return a
}

func aSender(t *testing.T) (*Sender, *apple, *ecdsa.PublicKey) {
	t.Helper()
	key, public := aKey(t)
	at := standIn(t)
	s, err := New("nl.example.app", key)
	require.NoError(t, err)
	s.At = at.URL
	return s, at, public
}

// aBatch is what waits for an agent, as it starts on a phone.
func aBatch(due time.Time) Activity {
	return Activity{
		Token: "aa11", Event: ActivityStarts,
		Type:       "WaitsAttributes",
		Attributes: map[string]any{"waits": "collected/did:key:z6Mkbirch", "title": "3 wait for Birch"},
		State:      map[string]any{"says": "sent in 15 min", "due": float64(due.Unix())},
		Title:      "3 wait for Birch", Body: "Alder: pull request one is open",
		Stale: due,
	}
}

// A Live Activity is started as Apple reads one: the push type and topic of
// Live Activities, the type and attributes it is made of, what it shows, and
// what the phone says as it begins.
func TestALiveActivityIsStartedAsAppleReadsOne(t *testing.T) {
	s, at, public := aSender(t)
	now := time.Date(2026, 11, 13, 13, 45, 0, 0, time.UTC)
	s.Now = func() time.Time { return now }
	due := now.Add(15 * time.Minute)

	id, err := s.Show(context.Background(), aBatch(due))
	require.NoError(t, err)
	assert.Equal(t, "an-apns-id", id)

	require.Len(t, at.asked, 1)
	asked := at.asked[0]
	assert.Equal(t, "/3/device/aa11", asked.URL.Path)
	assert.Equal(t, "liveactivity", asked.Header.Get("apns-push-type"))
	assert.Equal(t, "nl.example.app.push-type.liveactivity", asked.Header.Get("apns-topic"))
	assert.Equal(t, "10", asked.Header.Get("apns-priority"))

	assert.Len(t, at.bodies[0], 1, "something stands beside aps")
	aps := at.bodies[0]["aps"].(map[string]any)
	assert.Equal(t, map[string]any{
		"timestamp":        float64(now.Unix()),
		"event":            "start",
		"attributes-type":  "WaitsAttributes",
		"attributes":       map[string]any{"waits": "collected/did:key:z6Mkbirch", "title": "3 wait for Birch"},
		"content-state":    map[string]any{"says": "sent in 15 min", "due": float64(due.Unix())},
		"alert":            map[string]any{"title": "3 wait for Birch", "body": "Alder: pull request one is open"},
		"input-push-token": float64(1),
		"stale-date":       float64(due.Unix()),
	}, aps)

	// The provider token is the key's: ES256, its id, the team, signed now.
	bearer, found := strings.CutPrefix(asked.Header.Get("authorization"), "bearer ")
	require.True(t, found)
	parts := strings.Split(bearer, ".")
	require.Len(t, parts, 3)
	var header, claims map[string]any
	decode := func(part string, into any) {
		raw, err := base64.RawURLEncoding.DecodeString(part)
		require.NoError(t, err)
		require.NoError(t, json.Unmarshal(raw, into))
	}
	decode(parts[0], &header)
	decode(parts[1], &claims)
	assert.Equal(t, map[string]any{"alg": "ES256", "kid": "KEY1234567"}, header)
	assert.Equal(t, map[string]any{"iss": "TEAM123456", "iat": float64(now.Unix())}, claims)
	sig, err := base64.RawURLEncoding.DecodeString(parts[2])
	require.NoError(t, err)
	require.Len(t, sig, 64)
	digest := sha256.Sum256([]byte(parts[0] + "." + parts[1]))
	assert.True(t, ecdsa.Verify(public, digest[:], new(big.Int).SetBytes(sig[:32]), new(big.Int).SetBytes(sig[32:])), "the provider token is not signed by the key")
}

// One that moves or ends says what it shows now and nothing of what it is made
// of; one that ends says when it leaves the lock screen.
func TestALiveActivityMovesAndEndsByItsOwnToken(t *testing.T) {
	s, at, _ := aSender(t)
	now := time.Date(2026, 11, 13, 13, 50, 0, 0, time.UTC)
	s.Now = func() time.Time { return now }

	_, err := s.Show(context.Background(), Activity{Token: "bb22", Event: ActivityMoves, State: map[string]any{"says": "held until tomorrow"}})
	require.NoError(t, err)
	_, err = s.Show(context.Background(), Activity{Token: "bb22", Event: ActivityEnds, State: map[string]any{"says": "sent"}, Gone: now})
	require.NoError(t, err)

	require.Len(t, at.asked, 2)
	assert.Equal(t, "/3/device/bb22", at.asked[0].URL.Path)
	assert.Equal(t, map[string]any{"timestamp": float64(now.Unix()), "event": "update", "content-state": map[string]any{"says": "held until tomorrow"}}, at.bodies[0]["aps"])
	assert.Equal(t, map[string]any{"timestamp": float64(now.Unix()), "event": "end", "content-state": map[string]any{"says": "sent"}, "dismissal-date": float64(now.Unix())}, at.bodies[1]["aps"])
}

// Apple is asked for a new hour's admission no more often than it allows.
func TestTheProviderTokenIsSignedOnceUntilItIsOld(t *testing.T) {
	s, at, _ := aSender(t)
	now := time.Date(2026, 11, 13, 13, 0, 0, 0, time.UTC)
	s.Now = func() time.Time { return now }

	for range 3 {
		_, err := s.Show(context.Background(), aBatch(now))
		require.NoError(t, err)
	}
	now = now.Add(19 * time.Minute)
	_, err := s.Show(context.Background(), aBatch(now))
	require.NoError(t, err)
	first := at.asked[0].Header.Get("authorization")
	for _, asked := range at.asked {
		assert.Equal(t, first, asked.Header.Get("authorization"), "a provider token was signed anew inside twenty minutes")
	}
	now = now.Add(25 * time.Minute)
	_, err = s.Show(context.Background(), aBatch(now))
	require.NoError(t, err)
	assert.NotEqual(t, first, at.asked[4].Header.Get("authorization"), "a provider token older than its life was used")
}

// What Apple refuses is said in Apple's words, and a token that is gone is said to be.
func TestWhatAppleRefusesIsSaidAsAppleSaysIt(t *testing.T) {
	s, at, _ := aSender(t)
	starts := aBatch(time.Now())

	at.status, at.says = http.StatusForbidden, `{"reason":"InvalidProviderToken"}`
	_, err := s.Show(context.Background(), starts)
	var refused *Refused
	require.True(t, errors.As(err, &refused), "what Apple refused is not a Refused: %v", err)
	assert.Equal(t, "InvalidProviderToken", refused.Reason)
	assert.False(t, refused.Gone)

	at.status, at.says = http.StatusGone, `{"reason":"Unregistered","timestamp":1}`
	_, err = s.Show(context.Background(), starts)
	require.True(t, errors.As(err, &refused))
	assert.True(t, refused.Gone, "a token Apple says is unregistered is not said to be gone")

	// A token of Apple's other host is refused as a bad one, and that phone is still there.
	at.status, at.says = http.StatusBadRequest, `{"reason":"BadDeviceToken"}`
	_, err = s.Show(context.Background(), starts)
	require.True(t, errors.As(err, &refused))
	assert.False(t, refused.Gone, "a phone asked at the wrong host was taken for gone")
}

// What is no Live Activity is not sent, and a key that is no key makes no sender.
func TestWhatIsNoLiveActivityIsNotSent(t *testing.T) {
	s, at, _ := aSender(t)
	due := time.Now()
	noToken, noEvent, noState, noType, noTitle := aBatch(due), aBatch(due), aBatch(due), aBatch(due), aBatch(due)
	noToken.Token, noEvent.Event, noState.State, noType.Type, noTitle.Title = "", "begin", nil, "", ""
	for _, a := range []Activity{noToken, noEvent, noState, noType, noTitle} {
		_, err := s.Show(context.Background(), a)
		require.Error(t, err)
	}
	assert.Empty(t, at.asked, "what is no Live Activity was sent to Apple")

	key, _ := aKey(t)
	_, err := New("nl.example.app", Key{P8: []byte("not a key"), KeyID: "K", TeamID: "T"})
	require.Error(t, err)
	_, err = New("", key)
	require.Error(t, err)
}
