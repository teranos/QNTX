package services

// "but i do still want the dark themed qntx tokens css email template"

// Dark is QNTX's dark palette as a mail carries it: web/css/tokens.css written
// out as values, because a mail client reads no CSS variables. Each value is
// held to its token by a test.
var Dark = struct {
	Background string // --bg-almost-black
	Surface    string // --bg-secondary
	Raised     string // --bg-tertiary
	Border     string // --border-on-dark
	Text       string // --text-on-dark
	Secondary  string // --text-on-dark-secondary
	Emphasis   string // --text-on-dark-emphasis
	Accent     string // --accent-on-dark
	AccentDim  string // --element-status-success-bg
	Error      string // --element-status-error-text
	Mono       string // --font-mono
}{
	Background: "#1a1b1a",
	Surface:    "#252625",
	Raised:     "#2e2f2e",
	Border:     "#3f4140",
	Text:       "#dfe1e0",
	Secondary:  "#a9abaa",
	Emphasis:   "#fefffe",
	Accent:     "#7dba8a",
	AccentDim:  "#1f3d1f",
	Error:      "#ff6b6b",
	Mono:       "'JetBrains Mono', 'SF Mono', 'Monaco', 'Fira Code', 'Consolas', monospace",
}
