package logger

import (
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

// "why inst an error"
// "only the thing that is also shown to a user"
// "why arent we looking at it as"
// "only things worth solving"
// "i havent faced 4000 + issues in my manual usage of my software"
// "why cant we guarantee human errors are prioritised"
//
// 「[Opus 5.5] An issue in Sentry is what a person faced: their request, failed on the」
// 「[Opus 5.5] node's side. Faced is the one way an entry raises one. Every other line,」
// 「[Opus 5.5] at whatever level, is a log line: 3,634 of qntx-go's 4,854 errors in 30」
// 「[Opus 5.5] days were one fleet node being down, and the failures a person met were」
// 「[Opus 5.5] not reported at all. The mark is a type no other package can make, so」
// 「[Opus 5.5] an issue cannot be raised around this.」

// facedKey is the field a faced entry carries.
const facedKey = "faced"

type facedMark struct{}

// Faced says, at error, what a person was answered with when their request
// failed on the node's side, and raises it as an issue. errorID is the id the
// person was shown, so what they report finds the issue.
func Faced(l *zap.SugaredLogger, msg, errorID string, keysAndValues ...any) {
	kv := append([]any{"error_id", errorID}, keysAndValues...)
	l.WithOptions(zap.AddCallerSkip(1)).Errorw(msg, append(kv, zap.Any(facedKey, facedMark{}))...)
}

// faced is whether an entry was said through Faced.
func faced(sets ...[]zapcore.Field) bool {
	for _, set := range sets {
		for _, field := range set {
			if field.Key == facedKey {
				if _, ok := field.Interface.(facedMark); ok {
					return true
				}
			}
		}
	}
	return false
}
