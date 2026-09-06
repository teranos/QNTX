// Package sacred is the one typed value an error crosses every layer as.
//
// "ERRORS ARE SACRED, WE NEVER DROP, SUPRESS, TRUNCATE, OR ADD LIES TO THEM"
//
// The shape is crates/laye-error, field for field. What Rust writes at the
// FFI is decoded here into a value with the same fields, so nothing between
// the store and the person reading the answer has to carry a sentence.
package sacred

import (
	"encoding/json"
	"strings"

	"github.com/teranos/errors"
)

// Severity is a closed set. A label outside it fails to decode, which is the
// axiom enforced: an unknown severity is a contract violation, not Info.
type Severity string

const (
	Info  Severity = "info"
	Warn  Severity = "warn"
	Err   Severity = "error"
	Panic Severity = "panic"
)

// UnmarshalJSON refuses a label outside the set.
func (s *Severity) UnmarshalJSON(b []byte) error {
	var label string
	if err := json.Unmarshal(b, &label); err != nil {
		return errors.Wrap(err, "severity is not a string")
	}
	switch Severity(label) {
	case Info, Warn, Err, Panic:
		*s = Severity(label)
		return nil
	}
	return errors.Newf("severity %q is not one of info, warn, error, panic", label)
}

// Anchor is where on a surface the failure was triggered, in pixels.
type Anchor struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
}

// Context is where the failure happened.
type Context struct {
	Surface string  `json:"surface"`
	Region  string  `json:"region,omitempty"`
	Anchor  *Anchor `json:"anchor,omitempty"`
}

// Error is the value. Every field is what the writer put there; none is
// derived, shortened, or filled in on the way.
type Error struct {
	ID             string   `json:"id"`
	Severity       Severity `json:"severity"`
	Context        Context  `json:"context"`
	Title          string   `json:"title"`
	Why            string   `json:"why"`
	Trace          []string `json:"trace,omitempty"`
	Raw            string   `json:"raw,omitempty"`
	At             string   `json:"at"`
	Source         string   `json:"source,omitempty"`
	FFICall        string   `json:"ffi_call,omitempty"`
	Location       string   `json:"location,omitempty"`
	JSStack        string   `json:"js_stack,omitempty"`
	RawStderr      string   `json:"raw_stderr,omitempty"`
	RequiresReload bool     `json:"requires_reload,omitempty"`
}

// Error renders the value for a reader. The title is this side's finding;
// the why is the library's words, kept apart and named.
func (e *Error) Error() string {
	var b strings.Builder
	b.WriteString(e.Title)
	if e.Why != "" {
		b.WriteString(" (")
		if e.Source != "" {
			b.WriteString(e.Source)
			b.WriteString(" said: ")
		}
		b.WriteString(e.Why)
		b.WriteString(")")
	}
	if e.FFICall != "" {
		b.WriteString(" via ")
		b.WriteString(e.FFICall)
	}
	return b.String()
}

// Unshaped is what crossed when it was not the shape: a panic message, a
// static string, or a writer that has not been taught the shape yet. The text
// is kept whole; nothing is dropped for having arrived in the wrong form.
type Unshaped struct {
	Raw     string
	Decode  error
	FFICall string
}

func (u *Unshaped) Error() string {
	if u.FFICall != "" {
		return u.Raw + " via " + u.FFICall
	}
	return u.Raw
}

// Decode reads what crossed. The shape decodes into an *Error; anything else
// comes back as an *Unshaped carrying the text and why it did not decode.
func Decode(text string) error {
	trimmed := strings.TrimSpace(text)
	if !strings.HasPrefix(trimmed, "{") {
		return &Unshaped{Raw: text, Decode: errors.New("not a JSON object")}
	}
	var e Error
	if err := json.Unmarshal([]byte(trimmed), &e); err != nil {
		return &Unshaped{Raw: text, Decode: err}
	}
	if e.Title == "" && e.ID == "" {
		return &Unshaped{Raw: text, Decode: errors.New("a JSON object with neither id nor title is not the shape")}
	}
	return &e
}
