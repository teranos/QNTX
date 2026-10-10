package commands

import (
	"bytes"
	"strings"
	"testing"

	errors "github.com/teranos/sacred-error"
)

// minting stands in for the node: it keeps what was asked and answers a token.
type minting struct {
	asked [][2]string
	err   error
}

func (m *minting) mint(owner, repo string) (string, string, error) {
	m.asked = append(m.asked, [2]string{owner, repo})
	return "x-access-token", "ghs_minted", m.err
}

// git asks its helper for a credential by what it is about to reach, and is
// handed what the node mints for that one repository (ADR-048).
func TestGitIsHandedTheCredentialTheNodeMints(t *testing.T) {
	node := &minting{}
	var out bytes.Buffer
	asked := "protocol=https\nhost=github.com\npath=teranos/QNTX.git\n\n"
	if err := gitCredential("get", strings.NewReader(asked), &out, node.mint); err != nil {
		t.Fatalf("gitCredential: %v", err)
	}
	if len(node.asked) != 1 || node.asked[0] != [2]string{"teranos", "QNTX"} {
		t.Errorf("the node was asked for %v", node.asked)
	}
	if out.String() != "username=x-access-token\npassword=ghs_minted\n" {
		t.Errorf("git was handed %q", out.String())
	}
}

// A host the node mints nothing for is left to whatever else git has.
func TestGitIsHandedNothingForAnotherHost(t *testing.T) {
	node := &minting{}
	var out bytes.Buffer
	asked := "protocol=https\nhost=example.org\npath=some/thing.git\n\n"
	if err := gitCredential("get", strings.NewReader(asked), &out, node.mint); err != nil {
		t.Fatalf("gitCredential: %v", err)
	}
	if len(node.asked) != 0 || out.Len() != 0 {
		t.Errorf("the node was asked %v and git was handed %q", node.asked, out.String())
	}
}

// Nothing is kept, so there is nothing to store and nothing to erase.
func TestGitStoresAndErasesNothing(t *testing.T) {
	for _, operation := range []string{"store", "erase"} {
		node := &minting{}
		var out bytes.Buffer
		asked := "protocol=https\nhost=github.com\npath=teranos/QNTX.git\nusername=x-access-token\npassword=ghs_minted\n\n"
		if err := gitCredential(operation, strings.NewReader(asked), &out, node.mint); err != nil {
			t.Fatalf("%s: %v", operation, err)
		}
		if len(node.asked) != 0 || out.Len() != 0 {
			t.Errorf("%s asked the node %v and wrote %q", operation, node.asked, out.String())
		}
	}
}

// git that does not say which repository cannot be minted for, and is told why.
func TestGitThatNamesNoRepositoryIsToldWhy(t *testing.T) {
	node := &minting{}
	var out bytes.Buffer
	err := gitCredential("get", strings.NewReader("protocol=https\nhost=github.com\n\n"), &out, node.mint)
	if err == nil || !strings.Contains(err.Error(), "useHttpPath") {
		t.Errorf("a credential asked for with no path was answered: %v", err)
	}
}

// What the node refuses is said to whoever ran git, in the node's words.
func TestWhatTheNodeRefusesReachesWhoeverRanGit(t *testing.T) {
	node := &minting{err: errors.New("no installation of the App was found for teranos/QNTX")}
	var out bytes.Buffer
	err := gitCredential("get", strings.NewReader("protocol=https\nhost=github.com\npath=teranos/QNTX.git\n\n"), &out, node.mint)
	if err == nil || !strings.Contains(err.Error(), "no installation") {
		t.Errorf("the refusal did not come through: %v", err)
	}
	if out.Len() != 0 {
		t.Errorf("git was handed %q with a refusal", out.String())
	}
}
