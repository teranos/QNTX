// Package sigil is where one thing QNTX does is defined (ADR-039).
//
// "a new thing is a new handler is a new mcp tool is a new api endpoint".
// "no handrolled tools".
//
// Nothing is served from here yet. This package says what a sigil and a signum
// are. Package server fills them in, because the functions that answer are
// methods on the server and cannot be named from here.
package sigil

import (
	"net/http"
	"strings"

	"github.com/teranos/errors"
)

// A Sigil is one thing the node does: one HTTP API endpoint and one MCP tool.
//
// "the tool is equivalent to 1 sigil each".
type Sigil struct {
	// Name is what the reach table and the MCP tool call it.
	Name string
	// Does is what it is for, in words. It is what a connector reads before it
	// calls, so it is written for somebody who has never seen the code.
	Does string
	// Method and Path are the HTTP API endpoint.
	Method string
	Path   string
	// Answer is the function that does it, filled in by package server.
	Answer http.HandlerFunc
}

// A Signum holds sigils: watchers is a signum, and listing, creating, reading,
// updating and deleting a watcher are its sigils.
type Signum struct {
	Name   string
	Sigils []Sigil
}

// methods is every method a sigil may answer. Anything else is a typo, and a
// typo that read would be an endpoint nobody can call.
var methods = map[string]bool{
	http.MethodGet:    true,
	http.MethodPost:   true,
	http.MethodPut:    true,
	http.MethodPatch:  true,
	http.MethodDelete: true,
}

// Check refuses a signum that does not say what it holds. A sigil is the one
// place its thing is defined, so one that leaves a part out has defined
// nothing, and two that share a name or an endpoint have defined one thing
// twice.
func (s Signum) Check() error {
	if s.Name == "" {
		return errors.New("a signum has no name")
	}
	if len(s.Sigils) == 0 {
		return errors.Newf("the signum %s holds no sigils", s.Name)
	}
	named := map[string]bool{}
	answered := map[string]string{}
	for _, sigil := range s.Sigils {
		if sigil.Name == "" {
			return errors.Newf("a sigil of %s has no name (%s %s)", s.Name, sigil.Method, sigil.Path)
		}
		if named[sigil.Name] {
			return errors.Newf("%s holds the sigil %s twice", s.Name, sigil.Name)
		}
		named[sigil.Name] = true

		if sigil.Does == "" {
			return errors.Newf("the sigil %s of %s does not say what it does", sigil.Name, s.Name)
		}
		if !methods[sigil.Method] {
			return errors.Newf("the sigil %s of %s names the method %q, which is not one", sigil.Name, s.Name, sigil.Method)
		}
		if !strings.HasPrefix(sigil.Path, "/") {
			return errors.Newf("the sigil %s of %s names the path %q, which does not start at /", sigil.Name, s.Name, sigil.Path)
		}
		if sigil.Answer == nil {
			return errors.Newf("the sigil %s of %s has nothing that answers %s %s", sigil.Name, s.Name, sigil.Method, sigil.Path)
		}

		endpoint := sigil.Method + " " + sigil.Path
		if first, twice := answered[endpoint]; twice {
			return errors.Newf("%s and %s of %s both answer %s", first, sigil.Name, s.Name, endpoint)
		}
		answered[endpoint] = sigil.Name
	}
	return nil
}
