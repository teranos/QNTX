package services

import (
	"image/color"
	"math"
	"strconv"
	"strings"

	"github.com/teranos/errors"
)

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

// "what is the bg color of the canvas, what pattern does it use, what are the colors of the ax element, and the type element, and the sigma element, and the attestation element, and the triplet."

// Ink is the colours one of QNTX's own elements writes in: its values, its
// keywords, and the colour its symbol and title take.
type Ink struct {
	Value   string
	Keyword string
	Title   string
}

// Canvas is what a mail from QNTX is drawn on: the canvas and its grid, and
// the windows standing on it, each in the ink of the element it resembles.
// Written out as values; each is held by a test to the file it comes from.
var Canvas = struct {
	Background string // --bg-canvas
	Grid       string // canvas.css, the 24px grid line
	Window     string // ax-element.ts and attestation-element.ts, rgba(25, 25, 30)
	TitleBar   string // --bg-tertiary
	Border     string // --border-on-dark

	Ax          Ink // attestation-result-row.ts RESULT_ROW_PALETTE; --accent-on-dark
	Attestation Ink // bioviz/fasta-renderer.ts AZURE_VALUE, AZURE_KEYWORD; the signer, attestation-element.ts
	Triplet     Ink // triplet-element.ts TRIPLET_VALUE, TRIPLET_KEYWORD, TRIPLET
	Sigma       Ink // sigma-element.ts AMBER_VALUE, AMBER_DIM, AMBER
	Type        Ink // type-definition-window.css, the name and the glyph
	Alert       Ink // --element-status-error-text; --text-on-dark-secondary; --element-status-error-bg for its title bar

	SigmaBar      string // sigma-element.ts AMBER_BAR
	SigmaBarShade string // sigma-element.ts AMBER_BAR_BG
}{
	Background: "#2d2e36",
	Grid:       "#2f353c",
	Window:     "#19191e",
	TitleBar:   "#2e2f2e",
	Border:     "#3f4140",

	SigmaBar:      "#c49a6c",
	SigmaBarShade: "rgba(140, 110, 80, 0.25)",

	Ax:          Ink{Value: "#d4f0d4", Keyword: "#6b7b6b", Title: "#7dba8a"},
	Attestation: Ink{Value: "#d7dee3", Keyword: "#919599", Title: "#00d4aa"},
	Triplet:     Ink{Value: "#b0bcc6", Keyword: "#6e7a84", Title: "#96a4b0"},
	Sigma:       Ink{Value: "#e8d0b4", Keyword: "#8a7560", Title: "#d4a574"},
	Type:        Ink{Value: "#e8d88a", Keyword: "#888888", Title: "#ffeb3b"},
	Alert:       Ink{Value: "#ff6b6b", Keyword: "#a9abaa", Title: "#ff6b6b"},
}

// AlertTitleBar is the title bar an alert's window takes: --element-status-error-bg.
const AlertTitleBar = "#3d1f1f"

// ColorOf reads a colour the way the palette writes one, "#rrggbb" or
// "rgba(r, g, b, a)", for what is drawn as pixels rather than styled.
func ColorOf(css string) (color.NRGBA, error) {
	if hex, ok := strings.CutPrefix(css, "#"); ok && len(hex) == 6 {
		n, err := strconv.ParseUint(hex, 16, 32)
		if err != nil {
			return color.NRGBA{}, errors.Wrapf(err, "%q is not a colour", css)
		}
		return color.NRGBA{R: uint8(n >> 16), G: uint8(n >> 8), B: uint8(n), A: 0xff}, nil
	}
	if inner, ok := strings.CutPrefix(css, "rgba("); ok {
		parts := strings.Split(strings.TrimSuffix(inner, ")"), ",")
		if len(parts) != 4 {
			return color.NRGBA{}, errors.Newf("%q is not a colour: rgba takes four parts", css)
		}
		var channels [3]uint8
		for i, p := range parts[:3] {
			n, err := strconv.ParseUint(strings.TrimSpace(p), 10, 8)
			if err != nil {
				return color.NRGBA{}, errors.Wrapf(err, "%q is not a colour", css)
			}
			channels[i] = uint8(n)
		}
		alpha, err := strconv.ParseFloat(strings.TrimSpace(parts[3]), 64)
		if err != nil || alpha < 0 || alpha > 1 {
			return color.NRGBA{}, errors.Newf("%q is not a colour: its alpha is not between 0 and 1", css)
		}
		return color.NRGBA{R: channels[0], G: channels[1], B: channels[2], A: uint8(math.Round(alpha * 0xff))}, nil
	}
	return color.NRGBA{}, errors.Newf("%q is not a colour: neither #rrggbb nor rgba(r, g, b, a)", css)
}
