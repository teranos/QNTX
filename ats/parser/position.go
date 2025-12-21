package parser

// Position represents a line/column position in source text
// Uses LSP conventions: 1-based line numbers, 0-based character offsets
type Position struct {
	Line      int `json:"line"`      // 1-based line number
	Character int `json:"character"` // 0-based character offset within line
	Offset    int `json:"offset"`    // 0-based byte offset in entire source
}

// Range represents a source code span from start to end position
type Range struct {
	Start Position `json:"start"`
	End   Position `json:"end"`
}

// PositionTracker maintains line/column/offset state during tokenization
// Advances through source text, tracking position for each consumed character
type PositionTracker struct {
	source    string
	line      int // 1-based
	character int // 0-based within line
	offset    int // 0-based in source
}

// NewPositionTracker creates a tracker starting at beginning of source
func NewPositionTracker(source string) *PositionTracker {
	return &PositionTracker{
		source:    source,
		line:      1,
		character: 0,
		offset:    0,
	}
}

// Advance updates position after consuming text
// Handles newlines by incrementing line and resetting character position
func (pt *PositionTracker) Advance(text string) {
	for _, ch := range text {
		if ch == '\n' {
			pt.line++
			pt.character = 0
		} else {
			pt.character++
		}
		pt.offset += len(string(ch)) // Handle multi-byte UTF-8
	}
}

// CurrentPosition returns the current position snapshot
func (pt *PositionTracker) CurrentPosition() Position {
	return Position{
		Line:      pt.line,
		Character: pt.character,
		Offset:    pt.offset,
	}
}

// AdvanceBytes advances by n bytes (for precise offset control)
func (pt *PositionTracker) AdvanceBytes(n int) {
	for i := 0; i < n && pt.offset < len(pt.source); i++ {
		ch := rune(pt.source[pt.offset])
		if ch == '\n' {
			pt.line++
			pt.character = 0
		} else {
			pt.character++
		}
		pt.offset++
	}
}

// Mark returns current position (alias for CurrentPosition for clarity)
func (pt *PositionTracker) Mark() Position {
	return pt.CurrentPosition()
}

// RangeFromPositions creates a range from two positions
func RangeFromPositions(start, end Position) Range {
	return Range{Start: start, End: end}
}
