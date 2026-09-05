package auth

// Apple: a signing key the operator holds, spent for one sub.

import (
	"context"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"math/big"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/golang-jwt/jwt/v5"
	"github.com/teranos/errors"
)

// appleIdentityPrefix qualifies the sub in auth.root_identities, for the
// reason googleIdentityPrefix does: Apple's sub is an opaque string that says
// nothing about where it came from.
const appleIdentityPrefix = "apple:"

// appleAuthHost is where the person consents. Not a thing anyone picks, so the
// ceremony never asks for it.
const appleAuthHost = "appleid.apple.com"

// appleIssuer is what the identity token's iss must say and what the client
// secret's aud must name. Both are Apple's rule.
const appleIssuer = "https://appleid.apple.com"

// appleSecretTTL is how long a client secret this node mints is good for.
// Apple allows six months; one exchange needs seconds. A secret that lives
// minutes is not worth keeping, so none is kept.
const appleSecretTTL = 5 * time.Minute

// maxAppleNameBytes bounds the name Apple relays from the person's own typing.
// A name is a line, not a page.
const maxAppleNameBytes = 200

// The two endpoints the exchange talks to. Vars for the same reason
// providerDialControl is one: the test binary points them at httptest.
var (
	appleTokenURL = "https://appleid.apple.com/auth/token"
	appleKeysURL  = "https://appleid.apple.com/auth/keys"
)

// appleProvider binds a configured client into a provider, the way
// googleProvider does. Apple asks for one more thing than Google at the
// return: name and email are scopes, and asking for either means Apple
// answers by POST rather than redirect, which the callback receives.
func appleProvider(client OperatorClient) provider {
	return provider{
		ID:          "apple",
		Label:       "Apple",
		Kind:        kindRedirect,
		HostDefault: appleAuthHost,
		authorize: func(_ context.Context, host, redirectURI string) (string, providerState, error) {
			// The identity token comes back carrying this, which is what
			// makes it about this ceremony and not one obtained anywhere.
			nonce, err := randomTicket()
			if err != nil {
				return "", providerState{}, errors.Wrap(err, "failed to mint a nonce for the Apple ceremony")
			}
			authorize := "https://" + host + "/auth/authorize" +
				"?client_id=" + urlEncode(client.ID) +
				"&redirect_uri=" + urlEncode(redirectURI) +
				"&response_type=code" +
				// Apple's rule: any scope at all means form_post.
				"&response_mode=form_post" +
				"&scope=" + urlEncode("name email") +
				"&nonce=" + urlEncode(nonce)
			return authorize, providerState{
				Host:         host,
				ClientID:     client.ID,
				ClientSecret: client.Secret,
				TeamID:       client.TeamID,
				KeyID:        client.KeyID,
				Nonce:        nonce,
			}, nil
		},
		exchange: appleExchange,
	}
}

// appleClientSecret is the secret Apple's token endpoint wants: not a string
// the operator was given, but a JWT this node signs with the key they were
// given, naming the team, the key and the client.
func appleClientSecret(st providerState) (string, error) {
	key, err := jwt.ParseECPrivateKeyFromPEM([]byte(st.ClientSecret))
	if err != nil {
		// The key is the secret; the failure names it by id and never by value.
		return "", errors.Wrapf(err, "the Apple key %s for team %s is not an EC private key", st.KeyID, st.TeamID)
	}
	now := time.Now()
	// A map rather than RegisteredClaims: Apple documents aud as one string,
	// and RegisteredClaims writes a one-element list.
	token := jwt.NewWithClaims(jwt.SigningMethodES256, jwt.MapClaims{
		"iss": st.TeamID,
		"sub": st.ClientID,
		"aud": appleIssuer,
		"iat": now.Unix(),
		"exp": now.Add(appleSecretTTL).Unix(),
	})
	token.Header["kid"] = st.KeyID
	secret, err := token.SignedString(key)
	if err != nil {
		return "", errors.Wrapf(err, "failed to sign the Apple client secret with key %s", st.KeyID)
	}
	return secret, nil
}

// appleIdentity is what the identity token says, read only after the token
// has been verified.
type appleIdentity struct {
	jwt.RegisteredClaims
	Email string `json:"email"`
	Nonce string `json:"nonce"`
}

func appleExchange(ctx context.Context, st providerState, code, redirectURI string) (account, error) {
	secret, err := appleClientSecret(st)
	if err != nil {
		return account{}, err
	}
	form := strings.NewReader("grant_type=authorization_code" +
		"&client_id=" + urlEncode(st.ClientID) +
		"&client_secret=" + urlEncode(secret) +
		"&redirect_uri=" + urlEncode(redirectURI) +
		"&code=" + urlEncode(code))
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, appleTokenURL, form)
	if err != nil {
		return account{}, errors.Wrapf(err, "failed to build the token exchange against %s", appleTokenURL)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	var token struct {
		IDToken string `json:"id_token"`
	}
	if err := getJSON(req, "token exchange", &token); err != nil {
		return account{}, err
	}
	if token.IDToken == "" {
		return account{}, errors.Newf("%s exchanged the code for no identity token", appleTokenURL)
	}

	// Apple has no userinfo endpoint; the identity token is the whole answer,
	// so it is verified rather than believed: Apple's signature by kid, the
	// issuer, this client as the audience, the expiry, and this ceremony's
	// nonce.
	var who appleIdentity
	if _, err := jwt.ParseWithClaims(token.IDToken, &who, func(unverified *jwt.Token) (any, error) {
		kid, named := unverified.Header["kid"].(string)
		if !named {
			return nil, errors.Newf("the identity token from %s names no key id in its header", appleTokenURL)
		}
		return appleKey(ctx, kid)
	},
		jwt.WithValidMethods([]string{jwt.SigningMethodRS256.Alg()}),
		jwt.WithIssuer(appleIssuer),
		jwt.WithAudience(st.ClientID),
		jwt.WithExpirationRequired(),
	); err != nil {
		return account{}, errors.Wrapf(err, "the identity token from %s did not verify", appleTokenURL)
	}
	if who.Nonce != st.Nonce {
		return account{}, errors.Newf("the identity token from %s carries a nonce for a different ceremony", appleTokenURL)
	}
	// sub is the only thing Apple promises stays the same. The email may be a
	// relay address the person can turn off, so it is the handle and never the
	// identity.
	if who.Subject == "" {
		return account{}, errors.Newf("the identity token from %s carries no sub", appleTokenURL)
	}
	return account{
		CanonicalID: appleIdentityPrefix + who.Subject,
		Handle:      who.Email,
	}, nil
}

// appleKey is the public key Apple publishes under one kid. Fetched at every
// exchange: a login is rare, and Apple rotates keys on its own schedule.
func appleKey(ctx context.Context, kid string) (*rsa.PublicKey, error) {
	if kid == "" {
		return nil, errors.New("the identity token names no key id")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, appleKeysURL, nil)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to build the keys request against %s", appleKeysURL)
	}
	var published struct {
		Keys []struct {
			Kty string `json:"kty"`
			Kid string `json:"kid"`
			N   string `json:"n"`
			E   string `json:"e"`
		} `json:"keys"`
	}
	if err := getJSON(req, "Apple's keys", &published); err != nil {
		return nil, err
	}
	for _, key := range published.Keys {
		if key.Kid != kid {
			continue
		}
		if key.Kty != "RSA" {
			return nil, errors.Newf("Apple's key %s is %s, not RSA", kid, key.Kty)
		}
		n, err := base64.RawURLEncoding.DecodeString(key.N)
		if err != nil {
			return nil, errors.Wrapf(err, "Apple's key %s has an unreadable modulus", kid)
		}
		e, err := base64.RawURLEncoding.DecodeString(key.E)
		if err != nil {
			return nil, errors.Wrapf(err, "Apple's key %s has an unreadable exponent", kid)
		}
		return &rsa.PublicKey{N: new(big.Int).SetBytes(n), E: int(new(big.Int).SetBytes(e).Int64())}, nil
	}
	return nil, errors.Newf("%s publishes no key %s", appleKeysURL, kid)
}

// appleName reads the name out of the user JSON Apple posts on the first
// authorization and never again. It is the person's own typing, relayed by
// the browser, so it is read for the two fields it has and cut to a line.
func appleName(user string) string {
	if user == "" {
		return ""
	}
	var posted struct {
		Name struct {
			First string `json:"firstName"`
			Last  string `json:"lastName"`
		} `json:"name"`
	}
	if err := json.Unmarshal([]byte(user), &posted); err != nil {
		return ""
	}
	name := strings.Join(strings.Fields(posted.Name.First+" "+posted.Name.Last), " ")
	if len(name) > maxAppleNameBytes {
		cut := maxAppleNameBytes
		for cut > 0 && !utf8.RuneStart(name[cut]) {
			cut--
		}
		name = name[:cut]
	}
	return name
}
