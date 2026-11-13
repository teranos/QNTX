// 「[Fable 5.2] Package apns reaches a phone through Apple: the node starts, moves and ends」
// 「[Fable 5.2] a Live Activity on it, with the app closed (teranos/QNTX#1409).」
//
// 「[Fable 5.2] What is sent is Apple's own shape: one HTTP/2 POST to /3/device/<token>, the」
// 「[Fable 5.2] push type liveactivity, the topic the app's bundle id and」
// 「[Fable 5.2] .push-type.liveactivity after it, and a provider token: a JWT the key signs,」
// 「[Fable 5.2] ES256, that Apple takes for an hour and refuses when it is renewed more」
// 「[Fable 5.2] often than every twenty minutes.」
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

// 「[Fable 5.2] Production is where Apple takes what is said to an app from TestFlight or」
// 「[Fable 5.2] the App Store, and Development where it takes it for one built for」
// development. A token of the one is refused at the other.
const (
	Production  = "https://api.push.apple.com"
	Development = "https://api.sandbox.push.apple.com"
)

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

// 「[Fable 5.2] Sender reaches the Live Activities of one app.」
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

// 「[Fable 5.2] Refused is Apple's own answer to what it did not take.」
type Refused struct {
	Status int
	Reason string
	// Gone is whether the token is no longer one: the app was removed from
	// 「[Fable 5.2] the phone, or the activity is over. Nothing is sent to it again.」
	Gone bool
}

func (r *Refused) Error() string {
	return "Apple refused what the Live Activity was told: " + strconv.Itoa(r.Status) + " " + r.Reason
}

// 「[Fable 5.2] What a Live Activity is told: that it begins, that what it shows changed, or that it is over.」
const (
	ActivityStarts = "start"
	ActivityMoves  = "update"
	ActivityEnds   = "end"
)

// 「[Fable 5.2] Activity is one thing said to a Live Activity: what stands on the phone's」
// 「[Fable 5.2] island and lock screen, and in the menu bar of the person's Mac, for as long」
// 「[Fable 5.2] as it lasts. Apple's own shape for it is "Starting and updating Live」
// 「[Fable 5.2] Activities with ActivityKit push notifications".」
type Activity struct {
	// 「[Fable 5.2] Token is the phone's token for starting one when it starts, and the」
	// 「[Fable 5.2] activity's own afterwards.」
	Token string
	// 「[Fable 5.2] Event is ActivityStarts, ActivityMoves or ActivityEnds.」
	Event string
	// 「[Fable 5.2] Type names the app's ActivityAttributes type, and Attributes is what」
	// 「[Fable 5.2] never changes while it lasts. Both are said when it starts and only then.」
	Type       string
	Attributes any
	// 「[Fable 5.2] State is what it shows now, in the shape of the type's ContentState.」
	State any
	// 「[Fable 5.2] Title and Body are what the phone says as it begins: Apple starts none unannounced.」
	Title, Body string
	// 「[Fable 5.2] Stale is when what it shows is no longer so, were nothing said after this.」
	Stale time.Time
	// 「[Fable 5.2] Gone is when an ended one leaves the lock screen. Zero is Apple's own time, hours later.」
	Gone time.Time
	// 「[Fable 5.2] Expires is when Apple stops trying to hand it to a phone it cannot」
	// 「[Fable 5.2] reach. Zero is one try: a phone that comes back later is not shown」
	// 「[Fable 5.2] what is over by then.」
	Expires time.Time
}

// 「[Fable 5.2] atMost is the most Apple takes as what one thing said to a Live Activity carries, in bytes.」
const atMost = 4096

// 「[Fable 5.2] Show says one thing to a Live Activity, and answers Apple's id for it. What Apple refuses is a *Refused.」
func (s *Sender) Show(ctx context.Context, a Activity) (string, error) {
	switch {
	case a.Token == "":
		return "", errors.New("a Live Activity is reached by a token, and this has none")
	case a.Event != ActivityStarts && a.Event != ActivityMoves && a.Event != ActivityEnds:
		return "", errors.Newf("a Live Activity starts, moves or ends, and %q is none of them", a.Event)
	case a.State == nil:
		return "", errors.New("a Live Activity shows something, and this has no state: Apple decodes none from nothing")
	case a.Event == ActivityStarts && (a.Type == "" || a.Attributes == nil || a.Title == ""):
		return "", errors.New("a Live Activity that starts names its type, its attributes and what the phone says as it begins")
	}
	aps := map[string]any{"timestamp": s.Now().Unix(), "event": a.Event, "content-state": a.State}
	if a.Event == ActivityStarts {
		aps["attributes-type"], aps["attributes"] = a.Type, a.Attributes
		aps["alert"] = map[string]string{"title": a.Title, "body": a.Body}
		// 「[Fable 5.2] iOS 18 hands the app the started activity's own token only when asked: it is what moves and ends it.」
		aps["input-push-token"] = 1
	}
	if !a.Stale.IsZero() {
		aps["stale-date"] = a.Stale.Unix()
	}
	if a.Event == ActivityEnds && !a.Gone.IsZero() {
		aps["dismissal-date"] = a.Gone.Unix()
	}
	body, err := json.Marshal(map[string]any{"aps": aps})
	if err != nil {
		return "", errors.Wrap(err, "what the Live Activity is told did not marshal")
	}
	if len(body) > atMost {
		return "", errors.Newf("what the Live Activity is told is %d bytes, and Apple takes %d at most", len(body), atMost)
	}
	expires := "0"
	if !a.Expires.IsZero() {
		expires = strconv.FormatInt(a.Expires.Unix(), 10)
	}
	return s.post(ctx, a.Token, map[string]string{
		"apns-push-type": "liveactivity", "apns-topic": s.BundleID + ".push-type.liveactivity", "apns-expiration": expires,
	}, body)
}

// 「[Fable 5.2] post hands Apple one body for one token, and answers Apple's id for it. What Apple refuses is a *Refused.」
func (s *Sender) post(ctx context.Context, to string, headers map[string]string, body []byte) (string, error) {
	token, err := s.providerToken()
	if err != nil {
		return "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.At+"/3/device/"+to, bytes.NewReader(body))
	if err != nil {
		return "", errors.Wrap(err, "what is sent to Apple was not made into a request")
	}
	req.Header.Set("authorization", "bearer "+token)
	// Now: what it says is about something that happens in minutes.
	req.Header.Set("apns-priority", "10")
	req.Header.Set("content-type", "application/json")
	for name, value := range headers {
		req.Header.Set(name, value)
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
		// Unregistered is the app off the phone. BadDeviceToken is not: it is
		// also what a token of the other host is refused as, and that phone is still there.
		Gone: answer.StatusCode == http.StatusGone || why.Reason == "Unregistered",
	}
}
