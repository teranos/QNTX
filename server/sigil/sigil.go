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
	"slices"
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
	// Takes is what goes in. A sigil that takes nothing names none.
	Takes []Param
	// Answer is the function that does it, filled in by package server.
	Answer http.HandlerFunc
	// HTTP is its binding to the HTTP API. It is said here so a sigil reads in
	// one place, and it is not what the sigil is.
	HTTP Endpoint
}

// A Param is one thing a sigil takes. It says nothing about how it travels:
// that is each binding's to decide. In the HTTP API a param fills the path
// segment of its name, and otherwise rides the query of a GET or a DELETE and
// the JSON body of anything else. To MCP it is a tool argument.
type Param struct {
	// Name is what the caller writes.
	Name string
	// Says is what it is, in words.
	Says string
	// Required is a param the sigil refuses without.
	Required bool
	// OneOf is every value it takes, when it takes only some.
	OneOf []string
}

// A Refusal is a sigil saying no, in its own terms. It names the param the
// caller has to change, so a refusal is something a caller can act on. Each
// binding gives it its form: the HTTP API a status, MCP a tool error.
type Refusal struct {
	Why   Why
	Param string
	Says  string
}

// Why is the kind of no.
type Why string

const (
	// Missing is a required param that was not sent, or sent empty.
	Missing Why = "missing"
	// NotOneOf is a param sent with a value it does not take.
	NotOneOf Why = "not one of"
)

// Status is the HTTP API's form of a refusal. What was wrong with what was
// sent is the caller's to fix.
func (r Refusal) Status() int {
	switch r.Why {
	case Missing, NotOneOf:
		return http.StatusBadRequest
	}
	return http.StatusInternalServerError
}

// Refuses reads what was sent against what the sigil takes, in the order the
// sigil names it, and refuses the first thing wrong. Nil is nothing wrong.
func (s Sigil) Refuses(sent map[string]string) *Refusal {
	for _, param := range s.Takes {
		value := sent[param.Name]
		if value == "" {
			if !param.Required {
				continue
			}
			says := s.Name + " needs " + param.Name + ": " + param.Says
			if len(param.OneOf) > 0 {
				says += " One of " + strings.Join(param.OneOf, ", ") + "."
			}
			return &Refusal{Why: Missing, Param: param.Name, Says: says}
		}
		if len(param.OneOf) > 0 && !slices.Contains(param.OneOf, value) {
			return &Refusal{Why: NotOneOf, Param: param.Name, Says: s.Name + " takes " + param.Name +
				" as one of " + strings.Join(param.OneOf, ", ") + ", and " + value + " is not one."}
		}
	}
	return nil
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

// segmentsOf is every segment a path names in braces: /api/watchers/{id}
// names id.
func segmentsOf(path string) []string {
	var named []string
	for _, part := range strings.Split(path, "/") {
		if strings.HasPrefix(part, "{") && strings.HasSuffix(part, "}") {
			named = append(named, part[1:len(part)-1])
		}
	}
	return named
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

		taken := map[string]Param{}
		for _, param := range sigil.Takes {
			if param.Name == "" {
				return errors.Newf("the sigil %s of %s takes a param with no name", sigil.Name, s.Name)
			}
			if _, twice := taken[param.Name]; twice {
				return errors.Newf("the sigil %s of %s takes %s twice", sigil.Name, s.Name, param.Name)
			}
			if param.Says == "" {
				return errors.Newf("the sigil %s of %s takes %s and does not say what it is", sigil.Name, s.Name, param.Name)
			}
			taken[param.Name] = param
		}

		// A sigil is one endpoint, so its binding is checked with it.
		if !methods[sigil.HTTP.Method] {
			return errors.Newf("the sigil %s of %s is bound to the method %q, which is not one", sigil.Name, s.Name, sigil.HTTP.Method)
		}
		if !strings.HasPrefix(sigil.HTTP.Path, "/") {
			return errors.Newf("the sigil %s of %s is bound to the path %q, which does not start at /", sigil.Name, s.Name, sigil.HTTP.Path)
		}
		// A path that names a segment is called by filling it, so what fills
		// it is something the sigil takes, and cannot be left out.
		for _, segment := range segmentsOf(sigil.HTTP.Path) {
			param, held := taken[segment]
			if !held {
				return errors.Newf("the sigil %s of %s is bound to %s and takes no %s", sigil.Name, s.Name, sigil.HTTP.Path, segment)
			}
			if !param.Required {
				return errors.Newf("the sigil %s of %s is bound to %s, so %s is required", sigil.Name, s.Name, sigil.HTTP.Path, segment)
			}
		}
		if first, twice := bound[sigil.HTTP]; twice {
			return errors.Newf("%s and %s of %s are both bound to %s", first, sigil.Name, s.Name, sigil.HTTP)
		}
		bound[sigil.HTTP] = sigil.Name
	}
	return nil
}
