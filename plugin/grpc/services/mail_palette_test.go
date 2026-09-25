package services

import (
	"image/color"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// "but i do still want the dark themed qntx tokens css email template"
//
// A mail client reads no CSS variables, so the palette is written out as
// values. This holds each one to the token it is copied from, so the two
// cannot drift apart.
func TestTheDarkPaletteIsQNTXsTokens(t *testing.T) {
	css, err := os.ReadFile("../../../web/css/tokens.css")
	require.NoError(t, err)

	tokens := map[string]string{}
	for _, line := range strings.Split(string(css), "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "--") {
			continue
		}
		name, value, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		value, _, _ = strings.Cut(value, ";")
		tokens[name] = strings.TrimSpace(value)
	}

	for token, value := range map[string]string{
		"--bg-canvas":                  Canvas.Background,
		"--bg-tertiary":                Canvas.TitleBar,
		"--border-on-dark":             Canvas.Border,
		"--accent-on-dark":             Canvas.Ax.Title,
		"--element-status-error-text":  Canvas.Alert.Value,
		"--text-on-dark-secondary":     Canvas.Alert.Keyword,
		"--element-status-error-bg":    AlertTitleBar,
	} {
		assert.Equal(t, tokens[token], value, token)
	}

	for token, value := range map[string]string{
		"--bg-almost-black":            Dark.Background,
		"--bg-secondary":               Dark.Surface,
		"--bg-tertiary":                Dark.Raised,
		"--border-on-dark":             Dark.Border,
		"--text-on-dark":               Dark.Text,
		"--text-on-dark-secondary":     Dark.Secondary,
		"--text-on-dark-emphasis":      Dark.Emphasis,
		"--accent-on-dark":             Dark.Accent,
		"--element-status-success-bg":  Dark.AccentDim,
		"--element-status-error-text":  Dark.Error,
		"--font-mono":                  Dark.Mono,
	} {
		assert.Equal(t, tokens[token], value, token)
	}
}

// constsIn reads the one-line string constants of a TypeScript file:
// const NAME = '#abcdef';
func constsIn(t *testing.T, path string) map[string]string {
	t.Helper()
	src, err := os.ReadFile(path)
	require.NoError(t, err)
	consts := map[string]string{}
	for _, line := range strings.Split(string(src), "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "const ") {
			continue
		}
		name, value, ok := strings.Cut(strings.TrimPrefix(line, "const "), "=")
		if !ok {
			continue
		}
		value = strings.Trim(strings.TrimSpace(value), "';")
		consts[strings.TrimSpace(name)] = value
	}
	return consts
}

func fileHolds(t *testing.T, path, text string) {
	t.Helper()
	src, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Contains(t, string(src), text, path)
}

// "what is the bg color of the canvas, what pattern does it use, what are the colors of the ax element, and the type element, and the sigma element, and the attestation element, and the triplet."
//
// Each colour a mail is drawn in is held to the element it is taken from, so a
// mail keeps looking like QNTX when QNTX changes.
func TestTheCanvasInkIsQNTXsElements(t *testing.T) {
	const web = "../../../web/"

	triplet := constsIn(t, web+"ts/components/element/triplet-element.ts")
	assert.Equal(t, triplet["TRIPLET_VALUE"], Canvas.Triplet.Value)
	assert.Equal(t, triplet["TRIPLET_KEYWORD"], Canvas.Triplet.Keyword)
	assert.Equal(t, triplet["TRIPLET"], Canvas.Triplet.Title)

	sigma := constsIn(t, web+"ts/components/element/sigma-element.ts")
	assert.Equal(t, sigma["AMBER_VALUE"], Canvas.Sigma.Value)
	assert.Equal(t, sigma["AMBER_DIM"], Canvas.Sigma.Keyword)
	assert.Equal(t, sigma["AMBER"], Canvas.Sigma.Title)
	assert.Equal(t, sigma["AMBER_BAR"], Canvas.SigmaBar)
	assert.Equal(t, sigma["AMBER_BAR_BG"], Canvas.SigmaBarShade)

	azure := constsIn(t, web+"ts/components/element/bioviz/fasta-renderer.ts")
	assert.Equal(t, azure["AZURE_VALUE"], Canvas.Attestation.Value)
	assert.Equal(t, azure["AZURE_KEYWORD"], Canvas.Attestation.Keyword)
	fileHolds(t, web+"ts/components/element/attestation-element.ts", "color: "+Canvas.Attestation.Title)

	fileHolds(t, web+"ts/components/element/attestation-result-row.ts",
		"{ value: '"+Canvas.Ax.Value+"', keyword: '"+Canvas.Ax.Keyword+"' }")
	fileHolds(t, web+"ts/components/element/ax-element.ts", "rgba(25, 25, 30, 0.95)")
	assert.Equal(t, "#19191e", Canvas.Window, "rgb(25, 25, 30)")

	fileHolds(t, web+"css/type-definition-window.css", "color: "+Canvas.Type.Value)
	fileHolds(t, web+"css/type-definition-window.css", "color: "+Canvas.Type.Title)
	fileHolds(t, web+"ts/type-definition-window.ts", "'"+Canvas.Type.Keyword+"'")

	fileHolds(t, web+"css/canvas.css", Canvas.Grid+" 23px")
}

// A graph is pixels, so it reads the palette's colours as values.
func TestTheGraphReadsThePalettesColours(t *testing.T) {
	bar, err := ColorOf(Canvas.SigmaBar)
	require.NoError(t, err)
	assert.Equal(t, color.NRGBA{R: 0xc4, G: 0x9a, B: 0x6c, A: 0xff}, bar)

	shade, err := ColorOf(Canvas.SigmaBarShade)
	require.NoError(t, err)
	assert.Equal(t, color.NRGBA{R: 140, G: 110, B: 80, A: 64}, shade)

	for _, notOne := range []string{"#c49a6", "c49a6c", "#zz9a6c", "rgba(140, 110, 80)", "rgba(140, 110, 80, 2)", "rgb(1, 2, 3)"} {
		_, err := ColorOf(notOne)
		assert.Error(t, err, notOne)
	}
}
