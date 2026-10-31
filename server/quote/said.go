package quote

import quoted "github.com/teranos/QNTX/ats/quote"

// A Said is one prompt as the namespace holds it: what a person typed, and the
// row that holds it, so a span measured near it says who said the words it
// claims, when, and in which session.
type Said struct {
	Text    string `json:"text"`
	Row     string `json:"row"`
	Who     string `json:"who"`
	At      string `json:"at"`
	Session string `json:"session"`
}

// Texts is the words alone, as the measurement takes them.
func Texts(said []Said) []string {
	texts := make([]string, 0, len(said))
	for _, s := range said {
		texts = append(texts, s.Text)
	}
	return texts
}

// SaidAt is the prompt a measurement names, when it names one.
func SaidAt(said []Said, m quoted.Measurement) (Said, bool) {
	if m.Prompt < 0 || m.Prompt >= len(said) {
		return Said{}, false
	}
	return said[m.Prompt], true
}
