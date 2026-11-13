// Package apns sends a notification to a phone through Apple: the node says
// something to a person whose app is closed (teranos/QNTX#1409).
//
// What is sent is Apple's own shape: one HTTP/2 POST per notification to
// /3/device/<token>, the push type alert, the topic the app's bundle id, and a
// provider token: a JWT the key signs, ES256, that Apple takes for an hour and
// refuses when it is renewed more often than every twenty minutes.
package apns

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"io"
	"net/http"
	"strconv"
	"sync"
	"time"

	errors "github.com/teranos/sacred-error"
)

// Production is where Apple takes notifications for an app from TestFlight or the App Store.
const Production = "https://api.push.apple.com"

// tokenLives is how long one provider token is used before another is signed:
// under Apple's hour, over its twenty minutes.
const tokenLives = 40 * time.Minute

// Key is the APNs auth key of an Apple Developer team: the .p8 as Apple gave
// it, the id Apple gave the key, and the team's.
type Key struct {
	P8     []byte
	KeyID  string
	TeamID string
}

// Notification is one thing said to a phone.
type Notification struct {
	// Token is the device's, hex as the app read it.
	Token string
	// Title and Body are what the phone shows.
	Title, Body string
	// Opens is what the app is handed when the person taps it: where to go.
	Opens map[string]string
	// Replaces names what this is about. A later one with the same name takes
	// the place of the earlier on the phone, and does not stand beside it.
	Replaces string
	// Expires is when Apple stops trying to deliver it. Zero is one try.
	Expires time.Time
}

// Sender sends notifications for one app.
type Sender struct {
	// BundleID is the app's.
	BundleID string
	// At is Apple's host, Production unless a test stands in for it.
	At     string
	Client *http.Client
	Now    func() time.Time

	key    Key
	signer *ecdsa.PrivateKey

	mu       sync.Mutex
	token    string
	signedAt time.Time
}

// New is a sender for the app whose bundle id this is, signing with key.
func New(bundleID string, key Key) (*Sender, error) {
	if bundleID == "" || key.KeyID == "" || key.TeamID == "" {
		return nil, errors.New("an APNs sender needs the app's bundle id, the key's id and the team's id")
	}
	block, _ := pem.Decode(key.P8)
	if block == nil {
		return nil, errors.Newf("APNs key %s is not PEM, as a .p8 from Apple is", key.KeyID)
	}
	parsed, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, errors.Wrapf(err, "APNs key %s did not parse", key.KeyID)
	}
	signer, ok := parsed.(*ecdsa.PrivateKey)
	if !ok {
		return nil, errors.Newf("APNs key %s is not an EC key, and Apple signs with ES256", key.KeyID)
	}
	return &Sender{BundleID: bundleID, At: Production, Client: &http.Client{Timeout: 20 * time.Second}, Now: time.Now, key: key, signer: signer}, nil
}

func b64(raw []byte) string { return base64.RawURLEncoding.EncodeToString(raw) }

// providerToken is the JWT Apple admits the sender by, signed anew when the one held is old.
func (s *Sender) providerToken() (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.Now()
	if s.token != "" && now.Sub(s.signedAt) < tokenLives {
		return s.token, nil
	}
	header, err := json.Marshal(map[string]string{"alg": "ES256", "kid": s.key.KeyID})
	if err != nil {
		return "", errors.Wrap(err, "the provider token's header did not marshal")
	}
	claims, err := json.Marshal(map[string]any{"iss": s.key.TeamID, "iat": now.Unix()})
	if err != nil {
		return "", errors.Wrap(err, "the provider token's claims did not marshal")
	}
	signing := b64(header) + "." + b64(claims)
	digest := sha256.Sum256([]byte(signing))
	r, sig, err := ecdsa.Sign(rand.Reader, s.signer, digest[:])
	if err != nil {
		return "", errors.Wrapf(err, "APNs key %s did not sign", s.key.KeyID)
	}
	// JWS wants r and s side by side, each the curve's width, not ASN.1.
	width := (s.signer.Curve.Params().BitSize + 7) / 8
	joined := make([]byte, 2*width)
	r.FillBytes(joined[:width])
	sig.FillBytes(joined[width:])
	s.token, s.signedAt = signing+"."+b64(joined), now
	return s.token, nil
}

// Refused is Apple's own answer to a notification it did not take.
type Refused struct {
	Status int
	Reason string
	// Gone is whether the token is no longer one: the app was removed from
	// the phone. Nothing is sent to it again.
	Gone bool
}

func (r *Refused) Error() string {
	return "Apple refused the notification: " + strconv.Itoa(r.Status) + " " + r.Reason
}

// Send sends one notification, and answers Apple's id for it. What Apple refuses is a *Refused.
func (s *Sender) Send(ctx context.Context, n Notification) (string, error) {
	switch {
	case n.Token == "":
		return "", errors.New("a notification goes to a token, and this has none")
	case n.Title == "":
		return "", errors.New("a notification says something, and this has no title")
	}
	payload := map[string]any{"aps": map[string]any{"alert": map[string]string{"title": n.Title, "body": n.Body}, "sound": "default"}}
	for key, value := range n.Opens {
		if key != "aps" {
			payload[key] = value
		}
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return "", errors.Wrap(err, "the notification did not marshal")
	}
	token, err := s.providerToken()
	if err != nil {
		return "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.At+"/3/device/"+n.Token, bytes.NewReader(body))
	if err != nil {
		return "", errors.Wrap(err, "the notification was not made into a request")
	}
	req.Header.Set("authorization", "bearer "+token)
	req.Header.Set("apns-push-type", "alert")
	req.Header.Set("apns-topic", s.BundleID)
	// Now: what it says is about something that happens in minutes.
	req.Header.Set("apns-priority", "10")
	req.Header.Set("content-type", "application/json")
	if n.Replaces != "" {
		req.Header.Set("apns-collapse-id", n.Replaces)
	}
	if !n.Expires.IsZero() {
		req.Header.Set("apns-expiration", strconv.FormatInt(n.Expires.Unix(), 10))
	}
	answer, err := s.Client.Do(req)
	if err != nil {
		return "", errors.Wrapf(err, "Apple at %s was not reached", s.At)
	}
	defer func() { _ = answer.Body.Close() }()
	said, _ := io.ReadAll(io.LimitReader(answer.Body, 4096))
	if answer.StatusCode == http.StatusOK {
		return answer.Header.Get("apns-id"), nil
	}
	var why struct {
		Reason string `json:"reason"`
	}
	_ = json.Unmarshal(said, &why)
	if why.Reason == "" {
		why.Reason = string(said)
	}
	return "", &Refused{
		Status: answer.StatusCode, Reason: why.Reason,
		Gone: answer.StatusCode == http.StatusGone || why.Reason == "BadDeviceToken" || why.Reason == "Unregistered",
	}
}
