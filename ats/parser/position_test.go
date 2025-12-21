package parser

import (
	"testing"
)

// TestPositionTracker verifies position tracking through source text
func TestPositionTracker(t *testing.T) {
	tests := []struct {
		name   string
		source string
		want   []struct {
			bytes int
			line  int
			char  int
			off   int
		}
	}{
		{
			name:   "single line",
			source: "hello world",
			want: []struct {
				bytes int
				line  int
				char  int
				off   int
			}{
				{5, 1, 5, 5},   // "hello"
				{1, 1, 6, 6},   // " "
				{5, 1, 11, 11}, // "world"
			},
		},
		{
			name:   "multi-line",
			source: "line1\nline2\nline3",
			want: []struct {
				bytes int
				line  int
				char  int
				off   int
			}{
				{5, 1, 5, 5},  // "line1"
				{1, 2, 0, 6},  // "\n"
				{5, 2, 5, 11}, // "line2"
				{1, 3, 0, 12}, // "\n"
				{5, 3, 5, 17}, // "line3"
			},
		},
		{
			name:   "empty string",
			source: "",
			want: []struct {
				bytes int
				line  int
				char  int
				off   int
			}{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tracker := NewPositionTracker(tt.source)

			// Initial position should be line 1, char 0, offset 0
			pos := tracker.Mark()
			if pos.Line != 1 {
				t.Errorf("initial Line = %d, want 1", pos.Line)
			}
			if pos.Character != 0 {
				t.Errorf("initial Character = %d, want 0", pos.Character)
			}
			if pos.Offset != 0 {
				t.Errorf("initial Offset = %d, want 0", pos.Offset)
			}

			// Advance through expected positions
			for i, want := range tt.want {
				tracker.AdvanceBytes(want.bytes)
				pos := tracker.Mark()

				if pos.Line != want.line {
					t.Errorf("step %d: Line = %d, want %d", i, pos.Line, want.line)
				}
				if pos.Character != want.char {
					t.Errorf("step %d: Character = %d, want %d", i, pos.Character, want.char)
				}
				if pos.Offset != want.off {
					t.Errorf("step %d: Offset = %d, want %d", i, pos.Offset, want.off)
				}
			}
		})
	}
}

// TestPositionTrackerUTF8 verifies correct handling of multi-byte UTF-8 characters
func TestPositionTrackerUTF8(t *testing.T) {
	// Test with emoji and other multi-byte characters
	source := "hello 👋 world"
	tracker := NewPositionTracker(source)

	// "hello " = 6 bytes
	tracker.AdvanceBytes(6)
	pos := tracker.Mark()
	if pos.Line != 1 || pos.Character != 6 || pos.Offset != 6 {
		t.Errorf("after 'hello ': got Line=%d Char=%d Off=%d, want Line=1 Char=6 Off=6",
			pos.Line, pos.Character, pos.Offset)
	}

	// "👋" = 4 bytes (UTF-8 emoji)
	tracker.AdvanceBytes(4)
	pos = tracker.Mark()
	if pos.Offset != 10 {
		t.Errorf("after emoji: Offset = %d, want 10", pos.Offset)
	}
}

// TestRangeFromPositions verifies range creation
func TestRangeFromPositions(t *testing.T) {
	start := Position{Line: 1, Character: 0, Offset: 0}
	end := Position{Line: 1, Character: 5, Offset: 5}

	r := RangeFromPositions(start, end)

	if r.Start != start {
		t.Errorf("Range.Start = %+v, want %+v", r.Start, start)
	}
	if r.End != end {
		t.Errorf("Range.End = %+v, want %+v", r.End, end)
	}
}
