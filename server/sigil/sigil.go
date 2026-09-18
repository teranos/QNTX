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
	"context"
	"encoding/json"
	"net/http"
	"slices"
	"strconv"
	"strings"

	"github.com/teranos/errors"
)

// A Sigil is one thing the node does. The HTTP API and MCP are surfaces of it:
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
	// Gives is what comes out, by field. A sigil whose answer is not JSON, a
	// file or a stream, names none.
	Gives []Field
	// Answer is the function that does it, filled in by package server.
	Answer Answer
	// HTTP is where it answers on the HTTP API. It is said here so a sigil
	// reads in one place, and it is not what the sigil is.
	HTTP Endpoint
}

// Sent is what a caller sent, by the name of each param. Every surface reads
// what arrived into this, so the function that answers never learns whether it
// came in a query, a body or a tool's arguments.
type Sent map[string]string

// An Answer is the function that does what a sigil says. It is no surface's:
// it is handed who is asking, in the context, and what they sent, and gives
// back the answer or a refusal. It never sees a request or a response.
type Answer func(ctx context.Context, sent Sent) (any, *Refusal)

// A Param is one thing a sigil takes. It says nothing about how it travels:
// that is each surface's to decide. In the HTTP API a param fills the path
// segment of its name, and otherwise rides the query of a GET or a DELETE and
// the JSON body of anything else. To MCP it is a tool argument.
type Param struct {
	// Name is what the caller writes.
	Name string
	// Says is what it is, in words.
	Says string
	// Kind is what its value is. Text when it names none.
	Kind Kind
	// Required is a param the sigil refuses without.
	Required bool
	// OneOf is every value it takes, when it takes only some.
	OneOf []string
}

// Kind is what a param's value is. Each surface says it in its own terms: a
// tool's schema calls a count an integer, a query carries it as digits.
type Kind string

const (
	// Text is anything said in one value: a name, a slug, a time in words.
	Text Kind = ""
	// Count is a whole number, zero or more.
	Count Kind = "count"
)

// Read is what arrived, read as what the sigil takes: by the names it takes,
// each as the kind it is. A query's strings, a body's fields and a tool's
// arguments all come through here, so a count is a count whichever way it
// came, and what the sigil does not take never reaches the function that
// answers.
func (s Sigil) Read(arrived map[string]any) (Sent, *Refusal) {
	sent := Sent{}
	for _, param := range s.Takes {
		value, came := arrived[param.Name]
		if !came || value == nil {
			continue
		}
		var read string
		switch v := value.(type) {
		case string:
			read = v
		case bool:
			read = strconv.FormatBool(v)
		case float64:
			read = strconv.FormatFloat(v, 'f', -1, 64)
		case int:
			read = strconv.Itoa(v)
		default:
			return nil, &Refusal{Why: Invalid, Param: param.Name,
				Says: s.Name + " takes " + param.Name + " as " + param.Kind.inWords() + ", and what was sent is not."}
		}
		if param.Kind == Count && read != "" {
			if n, err := strconv.Atoi(read); err != nil || n < 0 {
				return nil, &Refusal{Why: Invalid, Param: param.Name,
					Says: s.Name + " takes " + param.Name + " as a count, and " + read + " is not one."}
			}
		}
		sent[param.Name] = read
	}
	return sent, nil
}

// inWords is a kind as a refusal says it.
func (k Kind) inWords() string {
	if k == Count {
		return "a count"
	}
	return "text"
}

// A Field is one thing a sigil's answer carries, at the top of the answer.
// What sits inside a field is said in its words rather than spelled out: the
// point is that a caller knows what it will get, not a schema.
type Field struct {
	Name string
	Says string
}

// Holds holds an answer to what the sigil gives. Gives is said beside code
// that writes the answer, which makes it a second place, and a second place
// nothing checks goes stale. A test asks this of a real answer.
//
// An object carries exactly the fields given. A list is held a row at a time:
// what a list gives is what each row carries.
func (s Sigil) Holds(answer []byte) error {
	var rows []map[string]json.RawMessage
	if err := json.Unmarshal(answer, &rows); err != nil {
		var one map[string]json.RawMessage
		if err := json.Unmarshal(answer, &one); err != nil {
			return errors.Newf("%s gives fields, and the answer is not JSON", s.Name)
		}
		rows = []map[string]json.RawMessage{one}
	}
	given := map[string]bool{}
	for _, field := range s.Gives {
		given[field.Name] = true
	}
	for _, row := range rows {
		for _, field := range s.Gives {
			if _, carried := row[field.Name]; !carried {
				return errors.Newf("%s gives %s, and the answer has none", s.Name, field.Name)
			}
		}
		for name := range row {
			if !given[name] {
				return errors.Newf("the answer carries %s, which %s never said it gives", name, s.Name)
			}
		}
	}
	return nil
}

// A Refusal is a sigil saying no, in its own terms. It names the param the
// caller has to change, so a refusal is something a caller can act on. Each
// surface gives it its form: the HTTP API a status, MCP a tool error.
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
	// Invalid is a param sent with a value that does not read: a limit that is
	// not a count, a since that is not a time.
	Invalid Why = "invalid"
	// NotFound is a param naming something the node does not hold.
	NotFound Why = "not found"
	// NotAllowed is something the caller may not do, whoever they are.
	NotAllowed Why = "not allowed"
	// Failed is the node's own fault: a store that did not answer. What went
	// wrong is in the log, and the caller is told only that it did.
	Failed Why = "failed"
)

// Status is the HTTP API's form of a refusal. What was wrong with what was
// sent is the caller's to fix.
func (r Refusal) Status() int {
	switch r.Why {
	case Missing, NotOneOf, Invalid:
		return http.StatusBadRequest
	case NotFound:
		return http.StatusNotFound
	case NotAllowed:
		return http.StatusForbidden
	}
	return http.StatusInternalServerError
}

// Refuses reads what was sent against what the sigil takes, in the order the
// sigil names it, and refuses the first thing wrong. Nil is nothing wrong.
func (s Sigil) Refuses(sent Sent) *Refusal {
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

		given := map[string]bool{}
		for _, field := range sigil.Gives {
			if field.Name == "" {
				return errors.Newf("the sigil %s of %s gives a field with no name", sigil.Name, s.Name)
			}
			if given[field.Name] {
				return errors.Newf("the sigil %s of %s gives %s twice", sigil.Name, s.Name, field.Name)
			}
			if field.Says == "" {
				return errors.Newf("the sigil %s of %s gives %s and does not say what it is", sigil.Name, s.Name, field.Name)
			}
			given[field.Name] = true
		}

		// A sigil is one endpoint, so its endpoint is checked with it.
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
