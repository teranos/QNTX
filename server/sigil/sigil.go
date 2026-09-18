// "a new thing is a new handler is a new mcp tool is a new api endpoint"

// "no handrolled tools"

// The shape of a sigil is protocol.Sigil and nothing else: proto is the single
// source of truth (ADR-006), and the same shape crosses to the browser and to
// a plugin.

// Package sigil holds what is not a shape: the function that answers, and what
// is asked of a sigil (ADR-039). Package server fills the functions in, because
// they are methods on the server and cannot be named from here.
package sigil

import (
	"context"
	"encoding/json"
	"net/http"
	"slices"
	"strconv"
	"strings"

	"github.com/teranos/QNTX/plugin/grpc/protocol"
	"github.com/teranos/errors"
)

// "the tool is equivalent to 1 sigil each"

// A Signum holds the sigils of one subject (protocol.Signum), and beside it
// the function that answers each, by the sigil's name. A signum a plugin hands
// the node arrives with no Answers; what answers for it is the plugin.
type Signum struct {
	*protocol.Signum
	Answers map[string]Answer
}

// Sent is what a caller sent, by the name of each param. Every surface reads
// what arrived into this, so the function that answers never learns whether it
// came in a query, a body or a tool's arguments.
type Sent map[string]string

// An Answer is the function that does what a sigil says. It is no surface's:
// it is handed who is asking, in the context, and what they sent, and gives
// back the answer or a refusal. It never sees a request or a response.
type Answer func(ctx context.Context, sent Sent) (any, *protocol.Refusal)

// What a param's value is (protocol.Param.Kind). Each surface says it in its
// own terms: a tool's schema calls a count an integer, a query carries digits.
const (
	// Text is anything said in one value: a name, a slug, a time in words.
	Text = ""
	// Count is a whole number, zero or more.
	Count = "count"
)

// The kind of no (protocol.Refusal.Why).
const (
	// Missing is a required param that was not sent, or sent empty.
	Missing = "missing"
	// NotOneOf is a param sent with a value it does not take.
	NotOneOf = "not one of"
	// Invalid is a param sent with a value that does not read: a limit that is
	// not a count, a since that is not a time.
	Invalid = "invalid"
	// NotFound is a param naming something the node does not hold.
	NotFound = "not found"
	// NotAllowed is something the caller may not do, whoever they are.
	NotAllowed = "not allowed"
	// Failed is the node's own fault: a store that did not answer. What went
	// wrong is in the log, and the caller is told only that it did.
	Failed = "failed"
)

// Status is the HTTP API's form of a refusal. What was wrong with what was
// sent is the caller's to fix.
func Status(r *protocol.Refusal) int {
	switch r.GetWhy() {
	case Missing, NotOneOf, Invalid:
		return http.StatusBadRequest
	case NotFound:
		return http.StatusNotFound
	case NotAllowed:
		return http.StatusForbidden
	}
	return http.StatusInternalServerError
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

// Check refuses a signum that does not say what it holds: a sigil that leaves
// a part out has defined nothing, two that share a name or an endpoint have
// defined one thing twice, and one nothing answers for is not offered.
func (s Signum) Check() error {
	if s.Signum == nil {
		return errors.New("a signum has no shape")
	}
	if s.GetName() == "" {
		return errors.New("a signum has no name")
	}
	if len(s.GetSigils()) == 0 {
		return errors.Newf("the signum %s holds no sigils", s.GetName())
	}
	named := map[string]bool{}
	bound := map[string]string{}
	for _, sigil := range s.GetSigils() {
		endpoint := sigil.GetHttp().GetMethod() + " " + sigil.GetHttp().GetPath()
		if sigil.GetName() == "" {
			return errors.Newf("a sigil of %s has no name (%s)", s.GetName(), endpoint)
		}
		if named[sigil.GetName()] {
			return errors.Newf("%s holds the sigil %s twice", s.GetName(), sigil.GetName())
		}
		named[sigil.GetName()] = true

		if sigil.GetDoes() == "" {
			return errors.Newf("the sigil %s of %s does not say what it does", sigil.GetName(), s.GetName())
		}
		if s.Answers[sigil.GetName()] == nil {
			return errors.Newf("the sigil %s of %s has nothing that answers", sigil.GetName(), s.GetName())
		}

		taken := map[string]*protocol.Param{}
		for _, param := range sigil.GetTakes() {
			if param.GetName() == "" {
				return errors.Newf("the sigil %s of %s takes a param with no name", sigil.GetName(), s.GetName())
			}
			if _, twice := taken[param.GetName()]; twice {
				return errors.Newf("the sigil %s of %s takes %s twice", sigil.GetName(), s.GetName(), param.GetName())
			}
			if param.GetSays() == "" {
				return errors.Newf("the sigil %s of %s takes %s and does not say what it is", sigil.GetName(), s.GetName(), param.GetName())
			}
			taken[param.GetName()] = param
		}

		given := map[string]bool{}
		for _, field := range sigil.GetGives() {
			if field.GetName() == "" {
				return errors.Newf("the sigil %s of %s gives a field with no name", sigil.GetName(), s.GetName())
			}
			if given[field.GetName()] {
				return errors.Newf("the sigil %s of %s gives %s twice", sigil.GetName(), s.GetName(), field.GetName())
			}
			if field.GetSays() == "" {
				return errors.Newf("the sigil %s of %s gives %s and does not say what it is", sigil.GetName(), s.GetName(), field.GetName())
			}
			given[field.GetName()] = true
		}

		// A sigil is one endpoint, so its endpoint is checked with it.
		if !methods[sigil.GetHttp().GetMethod()] {
			return errors.Newf("the sigil %s of %s is bound to the method %q, which is not one", sigil.GetName(), s.GetName(), sigil.GetHttp().GetMethod())
		}
		if !strings.HasPrefix(sigil.GetHttp().GetPath(), "/") {
			return errors.Newf("the sigil %s of %s is bound to the path %q, which does not start at /", sigil.GetName(), s.GetName(), sigil.GetHttp().GetPath())
		}
		// A path that names a segment is called by filling it, so what fills
		// it is something the sigil takes, and cannot be left out.
		for _, segment := range segmentsOf(sigil.GetHttp().GetPath()) {
			param, held := taken[segment]
			if !held {
				return errors.Newf("the sigil %s of %s is bound to %s and takes no %s", sigil.GetName(), s.GetName(), sigil.GetHttp().GetPath(), segment)
			}
			if !param.GetRequired() {
				return errors.Newf("the sigil %s of %s is bound to %s, so %s is required", sigil.GetName(), s.GetName(), sigil.GetHttp().GetPath(), segment)
			}
		}
		if first, twice := bound[endpoint]; twice {
			return errors.Newf("%s and %s of %s are both bound to %s", first, sigil.GetName(), s.GetName(), endpoint)
		}
		bound[endpoint] = sigil.GetName()
	}
	for name := range s.Answers {
		if !named[name] {
			return errors.Newf("%s has an answer for %s, which is not a sigil it holds", s.GetName(), name)
		}
	}
	return nil
}

// Read is what arrived, read as what the sigil takes: by the names it takes,
// each as the kind it is. A query's strings, a body's fields and a tool's
// arguments all come through here, so a count is a count whichever way it came.
func Read(sigil *protocol.Sigil, arrived map[string]any) (Sent, *protocol.Refusal) {
	sent := Sent{}
	for _, param := range sigil.GetTakes() {
		value, came := arrived[param.GetName()]
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
			return nil, &protocol.Refusal{Why: Invalid, Param: param.GetName(),
				Says: sigil.GetName() + " takes " + param.GetName() + " as " + kindInWords(param.GetKind()) + ", and what was sent is not."}
		}
		if param.GetKind() == Count && read != "" {
			if n, err := strconv.Atoi(read); err != nil || n < 0 {
				return nil, &protocol.Refusal{Why: Invalid, Param: param.GetName(),
					Says: sigil.GetName() + " takes " + param.GetName() + " as a count, and " + read + " is not one."}
			}
		}
		sent[param.GetName()] = read
	}
	return sent, nil
}

// kindInWords is a kind as a refusal says it.
func kindInWords(kind string) string {
	if kind == Count {
		return "a count"
	}
	return "text"
}

// Refuses reads what was sent against what the sigil takes, in the order the
// sigil names it, and refuses the first thing wrong. Nil is nothing wrong.
func Refuses(sigil *protocol.Sigil, sent Sent) *protocol.Refusal {
	for _, param := range sigil.GetTakes() {
		value := sent[param.GetName()]
		if value == "" {
			if !param.GetRequired() {
				continue
			}
			says := sigil.GetName() + " needs " + param.GetName() + ": " + param.GetSays()
			if len(param.GetOneOf()) > 0 {
				says += " One of " + strings.Join(param.GetOneOf(), ", ") + "."
			}
			return &protocol.Refusal{Why: Missing, Param: param.GetName(), Says: says}
		}
		if len(param.GetOneOf()) > 0 && !slices.Contains(param.GetOneOf(), value) {
			return &protocol.Refusal{Why: NotOneOf, Param: param.GetName(), Says: sigil.GetName() + " takes " + param.GetName() +
				" as one of " + strings.Join(param.GetOneOf(), ", ") + ", and " + value + " is not one."}
		}
	}
	return nil
}

// Holds holds an answer to what the sigil gives: an object carries exactly the
// fields given, and a list is held a row at a time. Gives is said beside code
// that writes the answer, a second place, so a test asks this of a real answer.
func Holds(sigil *protocol.Sigil, answer []byte) error {
	var rows []map[string]json.RawMessage
	if err := json.Unmarshal(answer, &rows); err != nil {
		var one map[string]json.RawMessage
		if err := json.Unmarshal(answer, &one); err != nil {
			return errors.Newf("%s gives fields, and the answer is not JSON", sigil.GetName())
		}
		rows = []map[string]json.RawMessage{one}
	}
	given := map[string]bool{}
	for _, field := range sigil.GetGives() {
		given[field.GetName()] = true
	}
	for _, row := range rows {
		for _, field := range sigil.GetGives() {
			if _, carried := row[field.GetName()]; !carried {
				return errors.Newf("%s gives %s, and the answer has none", sigil.GetName(), field.GetName())
			}
		}
		for name := range row {
			if !given[name] {
				return errors.Newf("the answer carries %s, which %s never said it gives", name, sigil.GetName())
			}
		}
	}
	return nil
}
