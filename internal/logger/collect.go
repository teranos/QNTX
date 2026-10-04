package logger

import (
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

// collectQuiet is how long a line must stay away before its next occurrence
// counts as the first again.
const collectQuiet = time.Hour

// collectKept bounds the lines remembered; past it, the quiet ones are forgotten.
const collectKept = 4096

// CollectRepeats wraps every output the logger has with one collector, so an
// identical line repeating is written as its 1st, 4th, 16th, 64th… occurrence
// to the console, the file and Sentry alike, each carrying how many it stands for.
func CollectRepeats() {
	Logger = zap.New(newCollector(Logger.Desugar().Core(), time.Now)).Sugar()
}

// collector is a zapcore.Core in front of the tee. Identical means the same
// level, caller, message and fields, so a line naming a different subject is
// its own line and is never folded into another.
type collector struct {
	inner  zapcore.Core
	fields string
	seen   *seenLines
}

type seenLines struct {
	mu    sync.Mutex
	now   func() time.Time
	lines map[string]*seenLine
}

type seenLine struct {
	count int
	last  time.Time
}

func newCollector(inner zapcore.Core, now func() time.Time) *collector {
	return &collector{
		inner: inner,
		seen:  &seenLines{now: now, lines: map[string]*seenLine{}},
	}
}

func (c *collector) Enabled(level zapcore.Level) bool { return c.inner.Enabled(level) }

func (c *collector) With(fields []zapcore.Field) zapcore.Core {
	return &collector{
		inner:  c.inner.With(fields),
		fields: c.fields + encodeFields(fields),
		seen:   c.seen,
	}
}

func (c *collector) Check(ent zapcore.Entry, ce *zapcore.CheckedEntry) *zapcore.CheckedEntry {
	if c.Enabled(ent.Level) {
		return ce.AddCore(ent, c)
	}
	return ce
}

// Write hands the line to the cores that would have taken it, through their own
// Check, so a sink's level still decides what reaches it.
func (c *collector) Write(ent zapcore.Entry, fields []zapcore.Field) error {
	if ent.Level < zapcore.DPanicLevel {
		count, write := c.seen.occur(c.key(ent, fields))
		if !write {
			return nil
		}
		if count > 1 {
			fields = append(fields[:len(fields):len(fields)], zap.Int("occurrences", count))
		}
	}
	if ce := c.inner.Check(ent, nil); ce != nil {
		ce.Write(fields...)
	}
	return nil
}

func (c *collector) Sync() error { return c.inner.Sync() }

func (c *collector) key(ent zapcore.Entry, fields []zapcore.Field) string {
	return strings.Join([]string{ent.Level.String(), ent.LoggerName, ent.Caller.TrimmedPath(), ent.Message, c.fields, encodeFields(fields)}, "\x00")
}

// occur counts one more of key and says whether this one is written: the 1st,
// then every power of four.
func (s *seenLines) occur(key string) (int, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := s.now()
	line, ok := s.lines[key]
	if !ok || now.Sub(line.last) >= collectQuiet {
		if len(s.lines) >= collectKept {
			s.forgetQuiet(now)
		}
		line = &seenLine{}
		s.lines[key] = line
	}
	line.count++
	line.last = now
	return line.count, isPowerOfFour(line.count)
}

func (s *seenLines) forgetQuiet(now time.Time) {
	for key, line := range s.lines {
		if now.Sub(line.last) >= collectQuiet {
			delete(s.lines, key)
		}
	}
}

func isPowerOfFour(n int) bool {
	for n > 1 && n%4 == 0 {
		n /= 4
	}
	return n == 1
}

func encodeFields(fields []zapcore.Field) string {
	if len(fields) == 0 {
		return ""
	}
	enc := zapcore.NewMapObjectEncoder()
	for _, field := range fields {
		field.AddTo(enc)
	}
	keys := make([]string, 0, len(enc.Fields))
	for key := range enc.Fields {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	var b strings.Builder
	for _, key := range keys {
		fmt.Fprintf(&b, "%s=%v\x00", key, enc.Fields[key])
	}
	return b.String()
}
