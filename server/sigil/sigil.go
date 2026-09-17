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

// A Sigil is one thing the node does. The HTTP API and MCP are bindings of it:
// one endpoint and one tool, which do the same thing.
//
// "the tool is equivalent to 1 sigil each"
type Sigil struct {
	// Name is what the reach table and the MCP tool call it.
	Name string
	// Does is what it is for, in words. It is what a connector reads before it
	// calls, so it is written for somebody who has never seen the code.
	Does string
	// Answer is the function that does it, filled in by package server.
	Answer http.HandlerFunc
	// HTTP is its binding to the HTTP API. It is said here so a sigil reads in
	// one place, and it is not what the sigil is.
	HTTP Endpoint
}

// An Endpoint is a sigil's form in the HTTP API: the method and the whole path
// it answers on.
type Endpoint struct {
	Method string
	Path   string
}

func (e Endpoint) String() string { return e.Method + " " + e.Path }

// A Signum holds sigils: watchers is a signum, and listing, creating, reading,
// updating and deleting a watcher are its sigils.
type Signum struct {
	Name   string
	Sigils []Sigil
}

// methods is every method an endpoint may name. Anything else is a typo, and a
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
	bound := map[Endpoint]string{}
	for _, sigil := range s.Sigils {
		if sigil.Name == "" {
			return errors.Newf("a sigil of %s has no name (%s)", s.Name, sigil.HTTP)
		}
		if named[sigil.Name] {
			return errors.Newf("%s holds the sigil %s twice", s.Name, sigil.Name)
		}
		named[sigil.Name] = true

		if sigil.Does == "" {
			return errors.Newf("the sigil %s of %s does not say what it does", sigil.Name, s.Name)
		}
		if sigil.Answer == nil {
			return errors.Newf("the sigil %s of %s has nothing that answers", sigil.Name, s.Name)
		}

		// A sigil is one endpoint, so its binding is checked with it.
		if !methods[sigil.HTTP.Method] {
			return errors.Newf("the sigil %s of %s is bound to the method %q, which is not one", sigil.Name, s.Name, sigil.HTTP.Method)
		}
		if !strings.HasPrefix(sigil.HTTP.Path, "/") {
			return errors.Newf("the sigil %s of %s is bound to the path %q, which does not start at /", sigil.Name, s.Name, sigil.HTTP.Path)
		}
		if first, twice := bound[sigil.HTTP]; twice {
			return errors.Newf("%s and %s of %s are both bound to %s", first, sigil.Name, s.Name, sigil.HTTP)
		}
		bound[sigil.HTTP] = sigil.Name
	}
	return nil
}
