package server

import (
	"strings"
	"testing"

	appcfg "github.com/teranos/QNTX/internal/config"
)

// A deployment on the park's public path. Everything the network can reach
// has to be answered for before the gate opens.
func publicAuth() appcfg.AuthConfig {
	return appcfg.AuthConfig{
		Enabled:        true,
		RPID:           "pond.example",
		RootIdentities: []string{"did:key:z6MkDuckDuckDuckDuckDuckDuckDuckDuckDuckDuck"},
		PublicOrigin:   "https://api.pond.example",
	}
}

// The redirect_uri a provider delivers an authorization code to is built from
// this, and unset it used to be read off a header the caller wrote.
func TestPublicBindRefusesEmptyPublicOrigin(t *testing.T) {
	auth := publicAuth()
	auth.PublicOrigin = ""
	err := refusePublicDeploy("0.0.0.0", auth)
	if err == nil {
		t.Fatal("a public bind naming no public origin started")
	}
	if !strings.Contains(err.Error(), "auth.public_origin") {
		t.Fatalf("the refusal does not name auth.public_origin: %v", err)
	}
}

func TestLoopbackAsksForNothing(t *testing.T) {
	for _, addr := range []string{"", "127.0.0.1", "::1", "localhost"} {
		if err := refusePublicDeploy(addr, appcfg.AuthConfig{}); err != nil {
			t.Fatalf("bind %q is loopback and answered %v", addr, err)
		}
	}
}

func TestPublicBindRefusesAuthDisabled(t *testing.T) {
	auth := publicAuth()
	auth.Enabled = false
	err := refusePublicDeploy("0.0.0.0", auth)
	if err == nil {
		t.Fatal("a public bind with auth off started")
	}
	if !strings.Contains(err.Error(), "auth.enabled") {
		t.Fatalf("the refusal does not name auth.enabled: %v", err)
	}
}

func TestPublicBindRefusesEmptyRPID(t *testing.T) {
	auth := publicAuth()
	auth.RPID = ""
	err := refusePublicDeploy("192.0.2.10", auth)
	if err == nil {
		t.Fatal("a public bind with no rp_id started")
	}
	if !strings.Contains(err.Error(), "auth.rp_id") {
		t.Fatalf("the refusal does not name auth.rp_id: %v", err)
	}
}

// A public deploy naming nobody has no identity for a passkey to speak for,
// and mayRegister's ungoverned path decides enrolment on the credential table
// instead of on who is asking.
func TestPublicBindRefusesEmptyRootIdentities(t *testing.T) {
	auth := publicAuth()
	auth.RootIdentities = nil
	err := refusePublicDeploy("0.0.0.0", auth)
	if err == nil {
		t.Fatal("a public bind naming no root identity started")
	}
	if !strings.Contains(err.Error(), "auth.root_identities") {
		t.Fatalf("the refusal does not name auth.root_identities: %v", err)
	}
}

func TestPublicBindStartsWhenEverythingIsNamed(t *testing.T) {
	if err := refusePublicDeploy("0.0.0.0", publicAuth()); err != nil {
		t.Fatalf("a fully configured public deploy was refused: %v", err)
	}
}

// The park's gate is reached over https while the kiosk behind it listens on
// loopback. The socket says plain http and the browser is on TLS.
func TestTLSIsReadFromTheOriginsNotTheSocket(t *testing.T) {
	tls, err := servedOverTLS([]string{"https://pond.example"})
	if err != nil {
		t.Fatal(err)
	}
	if !tls {
		t.Fatal("an https rp_origin did not count as TLS")
	}
	tls, err = servedOverTLS([]string{"http://localhost:8820", "http://127.0.0.1:8770"})
	if err != nil {
		t.Fatal(err)
	}
	if tls {
		t.Fatal("loopback http origins counted as TLS")
	}
	tls, err = servedOverTLS(nil)
	if err != nil {
		t.Fatal(err)
	}
	if tls {
		t.Fatal("an unset rp_origins counted as TLS")
	}
}

// A deployment serving both loses the cookie on its plain-http origin. Reading
// the list as http instead would drop Secure on the one that needs it.
func TestOneHTTPSOriginIsEnough(t *testing.T) {
	tls, err := servedOverTLS([]string{"http://localhost:8820", "https://pond.example"})
	if err != nil {
		t.Fatal(err)
	}
	if !tls {
		t.Fatal("a mixed rp_origins did not count as TLS")
	}
}

// An unparseable origin used to be skipped, and skipping the only https entry
// silently drops Secure from every auth cookie. Guessing either way is a lie,
// so a bad entry refuses instead.
func TestUnparseableOriginRefuses(t *testing.T) {
	if _, err := servedOverTLS([]string{"://tenniscourt"}); err == nil {
		t.Fatal("an unparseable origin was skipped instead of refused")
	}
	if _, err := servedOverTLS([]string{"://tenniscourt", "https://pond.example"}); err == nil {
		t.Fatal("an https origin excused the unparseable one before it")
	}
}

// The refusal names the address it read, so a deployment that is public by
// accident can see which address made it so.
func TestRefusalNamesTheBindAddress(t *testing.T) {
	auth := publicAuth()
	auth.RootIdentities = nil
	err := refusePublicDeploy("198.51.100.7", auth)
	if err == nil || !strings.Contains(err.Error(), "198.51.100.7") {
		t.Fatalf("the refusal does not name the bind address: %v", err)
	}
}
