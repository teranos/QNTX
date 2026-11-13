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
	"strconv"
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

func aBatch() Notification {
	return Notification{
		Token: "aa11", Title: "3 wait for Birch", Body: "Alder: pull request one is open",
		Opens: map[string]string{"collected": "did:key:z6Mkbirch"}, Replaces: "collected-birch",
	}
}

// A notification is sent as Apple reads one: an alert, to the app's own topic,
// saying what it says, and carrying where a tap goes.
func TestANotificationIsSentAsAppleReadsOne(t *testing.T) {
	s, at, public := aSender(t)
	now := time.Date(2026, 11, 13, 13, 45, 0, 0, time.UTC)
	s.Now = func() time.Time { return now }
	sends := aBatch()
	sends.Expires = now.Add(15 * time.Minute)

	id, err := s.Send(context.Background(), sends)
	require.NoError(t, err)
	assert.Equal(t, "an-apns-id", id)

	require.Len(t, at.asked, 1)
	asked := at.asked[0]
	assert.Equal(t, "/3/device/aa11", asked.URL.Path)
	assert.Equal(t, "alert", asked.Header.Get("apns-push-type"))
	assert.Equal(t, "nl.example.app", asked.Header.Get("apns-topic"))
	assert.Equal(t, "10", asked.Header.Get("apns-priority"))
	assert.Equal(t, "collected-birch", asked.Header.Get("apns-collapse-id"))
	assert.Equal(t, strconv.FormatInt(sends.Expires.Unix(), 10), asked.Header.Get("apns-expiration"))

	aps := at.bodies[0]["aps"].(map[string]any)
	assert.Equal(t, map[string]any{"title": "3 wait for Birch", "body": "Alder: pull request one is open"}, aps["alert"])
	assert.Equal(t, "did:key:z6Mkbirch", at.bodies[0]["collected"], "where a tap goes is not carried beside aps")

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

// What a tap carries cannot stand in for what Apple reads as the notification itself.
func TestWhatATapCarriesDoesNotReplaceTheNotification(t *testing.T) {
	s, at, _ := aSender(t)
	sends := aBatch()
	sends.Opens = map[string]string{"aps": "something else", "collected": "did:key:z6Mkbirch"}
	_, err := s.Send(context.Background(), sends)
	require.NoError(t, err)
	aps, isOne := at.bodies[0]["aps"].(map[string]any)
	require.True(t, isOne, "what a tap carries replaced aps")
	assert.Contains(t, aps, "alert")
}

// Apple is asked for a new hour's admission no more often than it allows.
func TestTheProviderTokenIsSignedOnceUntilItIsOld(t *testing.T) {
	s, at, _ := aSender(t)
	now := time.Date(2026, 11, 13, 13, 0, 0, 0, time.UTC)
	s.Now = func() time.Time { return now }

	for range 3 {
		_, err := s.Send(context.Background(), aBatch())
		require.NoError(t, err)
	}
	now = now.Add(19 * time.Minute)
	_, err := s.Send(context.Background(), aBatch())
	require.NoError(t, err)
	first := at.asked[0].Header.Get("authorization")
	for _, asked := range at.asked {
		assert.Equal(t, first, asked.Header.Get("authorization"), "a provider token was signed anew inside twenty minutes")
	}
	now = now.Add(25 * time.Minute)
	_, err = s.Send(context.Background(), aBatch())
	require.NoError(t, err)
	assert.NotEqual(t, first, at.asked[4].Header.Get("authorization"), "a provider token older than its life was used")
}

// What Apple refuses is said in Apple's words, and a token that is gone is said to be.
func TestWhatAppleRefusesIsSaidAsAppleSaysIt(t *testing.T) {
	s, at, _ := aSender(t)

	at.status, at.says = http.StatusForbidden, `{"reason":"InvalidProviderToken"}`
	_, err := s.Send(context.Background(), aBatch())
	var refused *Refused
	require.True(t, errors.As(err, &refused), "what Apple refused is not a Refused: %v", err)
	assert.Equal(t, "InvalidProviderToken", refused.Reason)
	assert.False(t, refused.Gone)

	at.status, at.says = http.StatusGone, `{"reason":"Unregistered","timestamp":1}`
	_, err = s.Send(context.Background(), aBatch())
	require.True(t, errors.As(err, &refused))
	assert.True(t, refused.Gone, "a token Apple says is unregistered is not said to be gone")
}

// What is no notification is not sent, and a key that is no key makes no sender.
func TestWhatIsNoNotificationIsNotSent(t *testing.T) {
	s, at, _ := aSender(t)
	noToken, noTitle := aBatch(), aBatch()
	noToken.Token, noTitle.Title = "", ""
	for _, n := range []Notification{noToken, noTitle} {
		_, err := s.Send(context.Background(), n)
		require.Error(t, err)
	}
	assert.Empty(t, at.asked, "what is no notification was sent to Apple")

	key, _ := aKey(t)
	_, err := New("nl.example.app", Key{P8: []byte("not a key"), KeyID: "K", TeamID: "T"})
	require.Error(t, err)
	_, err = New("", key)
	require.Error(t, err)
}
