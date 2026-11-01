package quote

import (
	"strings"
	"unicode"
)

// A Wrote is one write an agent made, as ground attested it: the text a tool
// put in a file, in which session, by which model. This is how a landed line
// is known to be the agent's for sure, and not by a trailer in a commit.
type Wrote struct {
	Text    string `json:"text"`
	File    string `json:"file"`
	Session string `json:"session"`
	Model   string `json:"model"`
	Row     string `json:"row"`
	At      string `json:"at"`
}

// WroteLine is the write that holds the line, when ground saw an agent write
// it: the line's words, trimmed, inside the text a tool put in a file.
func WroteLine(writes []Wrote, line string) (Wrote, bool) {
	want := strings.TrimSpace(line)
	if want == "" {
		return Wrote{}, false
	}
	for _, w := range writes {
		if strings.Contains(w.Text, want) {
			return w, true
		}
	}
	return Wrote{}, false
}

// ModelLabel is a model as the notation names it: claude-fable-5-1 is Fable
// 5.1, claude-opus-5-5 is Opus 5.5. A name in no such shape is kept as it is.
func ModelLabel(model string) string {
	rest, found := strings.CutPrefix(model, "claude-")
	if !found {
		return model
	}
	parts := strings.Split(rest, "-")
	var words, digits []string
	for _, part := range parts {
		if part == "" {
			continue
		}
		if strings.IndexFunc(part, unicode.IsDigit) == 0 {
			digits = append(digits, part)
			continue
		}
		words = append(words, strings.ToUpper(part[:1])+part[1:])
	}
	label := strings.Join(words, " ")
	if len(digits) > 0 {
		label += " " + strings.Join(digits, ".")
	}
	return strings.TrimSpace(label)
}
