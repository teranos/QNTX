package logger

import (
	"fmt"
	"regexp"
	"strings"

	"go.uber.org/zap"
	"go.uber.org/zap/buffer"
	"go.uber.org/zap/zapcore"
)

// Symbol constants (duplicated from ats/symbols to avoid circular dependency)
// TODO(#33): Extract symbols to standalone package to eliminate duplication
const (
	symPulse = "꩜" // Pulse symbol for async operations
)

// TODO(#36): Encoder architecture - caller-controlled formatting
//
// FUNDAMENTAL PROBLEM: Callers have no flexibility in how their logs are presented.
// Zap forces rigid key-value flattening, which mangles complex data structures
// and prevents progress bars, tables, or custom layouts.
//
// WHAT I WANT: "Logging as a service" where callers control presentation,
// with granular verbosity beyond 3 levels and intelligent structured data handling.
//
// See: https://github.com/teranos/QNTX/issues/36
//
// DEFER UNTIL: Have time and strong appetite for significant logging redesign.
// This works well enough for now, but it's not the system I actually want.

// Color palettes for different themes
const (
	colorReset = "\x1b[0m"
	colorBold  = "\x1b[1m"
)

// Gruvbox Dark color palette (warm, muted, easy on eyes)
type gruvboxColors struct {
	fg       string
	aqua     string
	orange   string
	yellow   string
	green    string
	blue     string
	purple   string
	red      string
	redBg    string
	yellowBg string
}

var gruvbox = gruvboxColors{
	fg:       "\x1b[38;5;223m", // Soft cream (#ebdbb2)
	aqua:     "\x1b[38;5;108m", // Muted cyan-green (#8ec07c)
	orange:   "\x1b[38;5;208m", // Warm orange (#fe8019)
	yellow:   "\x1b[38;5;214m", // Soft yellow (#fabd2f)
	green:    "\x1b[38;5;142m", // Muted green (#b8bb26)
	blue:     "\x1b[38;5;109m", // Soft blue (#83a598)
	purple:   "\x1b[38;5;175m", // Muted purple (#d3869b)
	red:      "\x1b[38;5;167m", // Warm red (#fb4934)
	redBg:    "\x1b[48;5;88m",  // Dark red background
	yellowBg: "\x1b[48;5;58m",  // Dark yellow background
}

// Everforest Dark color palette (natural forest greens, strong green presence)
type everforestColors struct {
	fg          string
	greenBright string // Bright leaf green
	greenMid    string // Mid forest green
	greenDeep   string // Deep forest green
	aqua        string // Blue-green water
	orange      string // Autumn orange
	yellow      string // Warm yellow
	red         string // Error red
	redBg       string
	yellowBg    string
}

var everforest = everforestColors{
	fg:          "\x1b[38;5;223m", // Soft beige (#d3c6aa)
	greenBright: "\x1b[38;5;108m", // Bright green (#a7c080) - prominent
	greenMid:    "\x1b[38;5;107m", // Mid green (#83c092) - timestamps
	greenDeep:   "\x1b[38;5;65m",  // Deep green (#7fbbb3) - secondary
	aqua:        "\x1b[38;5;109m", // Blue-green (#7fbbb3) - client/network
	orange:      "\x1b[38;5;208m", // Warm orange (#e69875) - components
	yellow:      "\x1b[38;5;179m", // Soft yellow (#dbbc7f) - warnings
	red:         "\x1b[38;5;167m", // Warm red (#e67e80) - errors
	redBg:       "\x1b[48;5;52m",  // Dark red background
	yellowBg:    "\x1b[48;5;58m",  // Dark yellow background
}

// Current active theme (set by logger.Initialize from config)
var currentTheme = "everforest"

// SetTheme configures the color scheme for log output
func SetTheme(theme string) {
	if theme == "everforest" || theme == "gruvbox" {
		currentTheme = theme
	}
}

// getColors returns the color palette for the current theme
func getColors() interface{} {
	if currentTheme == "everforest" {
		return everforest
	}
	return gruvbox
}

// Theme-aware color getters
func colorTime() string {
	if currentTheme == "everforest" {
		return everforest.greenMid // Green timestamps for forest theme
	}
	return gruvbox.aqua
}

func colorComponent(name string) string {
	// Hash for consistent color per component
	hash := 0
	for _, c := range name {
		hash += int(c)
	}

	if currentTheme == "everforest" {
		// Rotate between bright green and orange for strong green presence
		if hash%3 == 0 {
			return everforest.greenBright
		} else if hash%3 == 1 {
			return everforest.greenDeep
		}
		return everforest.orange
	}

	// Gruvbox: rotate orange/yellow
	if hash%2 == 0 {
		return gruvbox.orange
	}
	return gruvbox.yellow
}

func colorMessage(msg string) string {
	lower := strings.ToLower(msg)

	if currentTheme == "everforest" {
		// Strong green presence: most operations are green
		if strings.Contains(lower, "query") || strings.Contains(lower, "processing") ||
			strings.Contains(lower, "completed") || strings.Contains(lower, "ax") {
			return everforest.greenBright // Prominent green for queries
		}
		if strings.Contains(lower, "client") || strings.Contains(lower, "connected") ||
			strings.Contains(lower, "websocket") || strings.Contains(lower, "browser") {
			return everforest.greenMid // Mid green for client events
		}
		if strings.Contains(lower, "starting") || strings.Contains(lower, "started") ||
			strings.Contains(lower, "daemon") || strings.Contains(lower, "config") {
			return everforest.greenDeep // Deep green for server lifecycle
		}
		return everforest.fg
	}

	// Gruvbox: semantic diversity
	if strings.Contains(lower, "client") || strings.Contains(lower, "connected") ||
		strings.Contains(lower, "websocket") || strings.Contains(lower, "browser") {
		return gruvbox.blue
	}
	if strings.Contains(lower, "query") || strings.Contains(lower, "processing") ||
		strings.Contains(lower, "completed") || strings.Contains(lower, "ax") {
		return gruvbox.green
	}
	if strings.Contains(lower, "starting") || strings.Contains(lower, "started") ||
		strings.Contains(lower, "daemon") || strings.Contains(lower, "config") {
		return gruvbox.orange
	}
	return gruvbox.fg
}

// colorizeMessage parses a log message and applies context-aware colorization
// to different components: job IDs, stage markers, symbols, etc.
// Returns the fully colorized message string with embedded ANSI codes.
func colorizeMessage(msg string) string {
	// Pattern for bracketed contexts: [job:XXX], [stage], etc.
	bracketPattern := regexp.MustCompile(`\[([^\]]+)\]`)

	// Define color functions for different bracket types
	getJobIDColor := func() string {
		if currentTheme == "everforest" {
			return everforest.aqua
		}
		return gruvbox.blue
	}

	getStageColor := func() string {
		if currentTheme == "everforest" {
			return everforest.orange
		}
		return gruvbox.orange
	}

	getSymbolColor := func() string {
		if currentTheme == "everforest" {
			return everforest.greenBright
		}
		return gruvbox.green
	}

	getBaseTextColor := func() string {
		if currentTheme == "everforest" {
			return everforest.fg
		}
		return gruvbox.fg
	}

	// Track position for building colorized string
	result := strings.Builder{}
	lastIndex := 0

	// Find all bracketed contexts and colorize them
	matches := bracketPattern.FindAllStringSubmatchIndex(msg, -1)
	for _, match := range matches {
		// Append text before bracket in base color
		textBefore := msg[lastIndex:match[0]]
		if textBefore != "" {
			// Colorize symbols in text before bracket
			textBefore = colorizeSymbols(textBefore, getSymbolColor())
			result.WriteString(getBaseTextColor())
			result.WriteString(textBefore)
			result.WriteString(colorReset)
		}

		// Extract bracket content
		bracketStart := match[0]
		bracketEnd := match[1]
		content := msg[match[2]:match[3]]

		// Determine color based on bracket content
		var color string
		if strings.HasPrefix(content, "job:") {
			color = getJobIDColor()
		} else {
			// Stage markers like [fetch_jd], [persist_complete], etc.
			color = getStageColor()
		}

		// Append colored bracket
		result.WriteString(color)
		result.WriteString(msg[bracketStart:bracketEnd])
		result.WriteString(colorReset)

		lastIndex = bracketEnd
	}

	// Append remaining text
	remaining := msg[lastIndex:]
	if remaining != "" {
		// Colorize symbols in remaining text
		remaining = colorizeSymbols(remaining, getSymbolColor())
		result.WriteString(getBaseTextColor())
		result.WriteString(remaining)
		result.WriteString(colorReset)
	}

	return result.String()
}

// colorizeSymbols replaces Pulse symbols with colorized versions
func colorizeSymbols(text string, symbolColor string) string {
	text = strings.ReplaceAll(text, symPulse, symbolColor+symPulse+colorReset)
	text = strings.ReplaceAll(text, "✿", symbolColor+"✿"+colorReset)
	text = strings.ReplaceAll(text, "❀", symbolColor+"❀"+colorReset)
	return text
}

func colorID() string {
	if currentTheme == "everforest" {
		return everforest.aqua // Blue-green for IDs
	}
	return gruvbox.blue
}

func colorNumber() string {
	if currentTheme == "everforest" {
		return everforest.greenBright // Bright green for numbers (strong presence)
	}
	return gruvbox.purple
}

func colorFg() string {
	if currentTheme == "everforest" {
		return everforest.fg
	}
	return gruvbox.fg
}

func colorWarn() (string, string) {
	if currentTheme == "everforest" {
		return everforest.yellow, everforest.yellowBg
	}
	return gruvbox.yellow, gruvbox.yellowBg
}

func colorError() (string, string) {
	if currentTheme == "everforest" {
		return everforest.red, everforest.redBg
	}
	return gruvbox.red, gruvbox.redBg
}

// minimalEncoder implements a calm, compact console encoder with theme support
// Format: "13:04:35  g.server  Client connected  127.0.0.1:52289"
type minimalEncoder struct {
	zapcore.Encoder // Embed a base encoder for field serialization
	buf             *buffer.Buffer
}

func newMinimalEncoder() *minimalEncoder {
	// Create a base JSON encoder for field serialization (internal use only)
	baseEncoder := zapcore.NewJSONEncoder(zap.NewProductionEncoderConfig())

	return &minimalEncoder{
		Encoder: baseEncoder,
		buf:     buffer.NewPool().Get(),
	}
}

func (enc *minimalEncoder) Clone() zapcore.Encoder {
	return &minimalEncoder{
		Encoder: enc.Encoder.Clone(),
		buf:     buffer.NewPool().Get(),
	}
}

func (enc *minimalEncoder) EncodeEntry(ent zapcore.Entry, fields []zapcore.Field) (*buffer.Buffer, error) {
	final := buffer.NewPool().Get()

	// Time: theme-aware color
	final.AppendString(colorTime())
	final.AppendString(ent.Time.Format("15:04:05"))
	final.AppendString(colorReset)

	// Level: only show for WARN/ERROR with bold + background
	if ent.Level != zapcore.InfoLevel {
		final.AppendString("  ")
		final.AppendString(levelColorString(ent.Level))
	}

	// Component name (abbreviated): theme-aware color for visual grouping
	if ent.LoggerName != "" {
		final.AppendString("  ")
		final.AppendString(colorComponent(ent.LoggerName))
		final.AppendString(abbreviateName(ent.LoggerName))
		final.AppendString(colorReset)
	}

	// Message: context-aware colorization of brackets, symbols, and content
	final.AppendString("  ")
	final.AppendString(colorizeMessage(ent.Message))

	// Fields: extract and color values
	if len(fields) > 0 {
		final.AppendString("  ")
		final.AppendString(extractFieldValues(fields))
	}

	final.AppendString("\n")
	return final, nil
}

// levelColorString returns bold + colored + background for WARN/ERROR
func levelColorString(level zapcore.Level) string {
	warnColor, warnBg := colorWarn()
	errColor, errBg := colorError()

	switch level {
	case zapcore.WarnLevel:
		return colorBold + warnBg + warnColor + "WARN" + colorReset
	case zapcore.ErrorLevel:
		return colorBold + errBg + errColor + "ERROR" + colorReset
	case zapcore.DPanicLevel, zapcore.PanicLevel, zapcore.FatalLevel:
		return colorBold + errBg + errColor + level.CapitalString() + colorReset
	default:
		return ""
	}
}

// abbreviateName shortens component names: server -> s, graph.builder -> g.builder
func abbreviateName(name string) string {
	parts := strings.Split(name, ".")
	if len(parts) > 1 {
		return string(parts[0][0]) + "." + strings.Join(parts[1:], ".")
	}
	return name
}

// getFieldValue extracts the value from a zap field, handling different field types
func getFieldValue(field zapcore.Field) string {
	// Try String first (most common for IDs)
	if field.Type == zapcore.StringType {
		return field.String
	}

	// For numeric types
	if field.Type == zapcore.Int64Type || field.Type == zapcore.Int32Type ||
		field.Type == zapcore.Int16Type || field.Type == zapcore.Int8Type ||
		field.Type == zapcore.Uint64Type || field.Type == zapcore.Uint32Type ||
		field.Type == zapcore.Uint16Type || field.Type == zapcore.Uint8Type {
		return fmt.Sprintf("%d", field.Integer)
	}

	// For other interface types
	if field.Interface != nil {
		return fmt.Sprintf("%v", field.Interface)
	}

	return ""
}

// extractFieldValues pulls just the values from structured fields with theme-aware colors
// Input: {"query_id": "q_123", "nodes": 19, "links": 0}
// Output: "q_123 (19 nodes, 0 links)" (with colored IDs and numbers)
func extractFieldValues(fields []zapcore.Field) string {
	var values []string
	var nodeCount, linkCount string

	for _, field := range fields {
		switch field.Key {
		case "query_id", "client_id":
			// IDs in theme-aware color (blue/aqua)
			// Handle both String and Interface field types
			val := getFieldValue(field)
			if val != "" {
				values = append(values, colorID()+val+colorReset)
			}
		case "nodes":
			nodeCount = getFieldValue(field)
		case "links":
			linkCount = getFieldValue(field)
		case "query_length":
			// Numbers in theme-aware color (purple/green)
			val := getFieldValue(field)
			if val != "" {
				values = append(values, colorNumber()+val+colorReset+" chars")
			}
		case "duration_ms":
			// Duration in theme-aware color
			val := getFieldValue(field)
			if val != "" {
				values = append(values, colorNumber()+val+colorReset+"ms")
			}
		}
	}

	// Special formatting for graph stats
	if nodeCount != "" && linkCount != "" {
		fg := colorFg()
		num := colorNumber()
		values = append(values, fg+"("+num+nodeCount+colorReset+fg+" nodes, "+num+linkCount+colorReset+fg+" links)"+colorReset)
	}

	if len(values) == 0 {
		return ""
	}

	return strings.Join(values, " ")
}
