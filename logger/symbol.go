package logger

import (
	"github.com/teranos/QNTX/sym"
	"go.uber.org/zap"
)

// Symbol-aware logging helpers.
// These functions log with the symbol as a structured field, not in the message.
//
// Usage:
//
//	// Instead of:
//	logger.Infow(sym.Pulse + " Job started", "job_id", id)
//
//	// Use:
//	logger.PulseInfow("Job started", "job_id", id)
//
// This makes logs queryable by symbol and keeps messages clean.

// PulseInfow logs an info message with the Pulse symbol (꩜)
func PulseInfow(msg string, keysAndValues ...interface{}) {
	if Logger != nil {
		fields := append([]interface{}{FieldSymbol, sym.Pulse}, keysAndValues...)
		Logger.Infow(msg, fields...)
	}
}

// PulseDebugw logs a debug message with the Pulse symbol (꩜)
func PulseDebugw(msg string, keysAndValues ...interface{}) {
	if Logger != nil {
		fields := append([]interface{}{FieldSymbol, sym.Pulse}, keysAndValues...)
		Logger.Debugw(msg, fields...)
	}
}

// PulseWarnw logs a warning message with the Pulse symbol (꩜)
func PulseWarnw(msg string, keysAndValues ...interface{}) {
	if Logger != nil {
		fields := append([]interface{}{FieldSymbol, sym.Pulse}, keysAndValues...)
		Logger.Warnw(msg, fields...)
	}
}

// PulseErrorw logs an error message with the Pulse symbol (꩜)
func PulseErrorw(msg string, keysAndValues ...interface{}) {
	if Logger != nil {
		fields := append([]interface{}{FieldSymbol, sym.Pulse}, keysAndValues...)
		Logger.Errorw(msg, fields...)
	}
}

// PulseOpenInfow logs an info message with the PulseOpen symbol (✿)
// Used for graceful startup operations
func PulseOpenInfow(msg string, keysAndValues ...interface{}) {
	if Logger != nil {
		fields := append([]interface{}{FieldSymbol, sym.PulseOpen}, keysAndValues...)
		Logger.Infow(msg, fields...)
	}
}

// PulseCloseInfow logs an info message with the PulseClose symbol (❀)
// Used for graceful shutdown operations
func PulseCloseInfow(msg string, keysAndValues ...interface{}) {
	if Logger != nil {
		fields := append([]interface{}{FieldSymbol, sym.PulseClose}, keysAndValues...)
		Logger.Infow(msg, fields...)
	}
}

// AxInfow logs an info message with the Ax symbol (⋈)
// Used for query/expand operations
func AxInfow(msg string, keysAndValues ...interface{}) {
	if Logger != nil {
		fields := append([]interface{}{FieldSymbol, sym.AX}, keysAndValues...)
		Logger.Infow(msg, fields...)
	}
}

// AxDebugw logs a debug message with the Ax symbol (⋈)
func AxDebugw(msg string, keysAndValues ...interface{}) {
	if Logger != nil {
		fields := append([]interface{}{FieldSymbol, sym.AX}, keysAndValues...)
		Logger.Debugw(msg, fields...)
	}
}

// DBInfow logs an info message with the DB symbol (⊔)
// Used for database/storage operations
func DBInfow(msg string, keysAndValues ...interface{}) {
	if Logger != nil {
		fields := append([]interface{}{FieldSymbol, sym.DB}, keysAndValues...)
		Logger.Infow(msg, fields...)
	}
}

// DBDebugw logs a debug message with the DB symbol (⊔)
func DBDebugw(msg string, keysAndValues ...interface{}) {
	if Logger != nil {
		fields := append([]interface{}{FieldSymbol, sym.DB}, keysAndValues...)
		Logger.Debugw(msg, fields...)
	}
}

// WithSymbol returns a logger with the given symbol as a field.
// For ad-hoc symbol usage not covered by the helpers above.
//
// Example:
//
//	symbolLogger := logger.WithSymbol(sym.IX)
//	symbolLogger.Infow("Ingesting data", "source", src)
func WithSymbol(symbol string) *zap.SugaredLogger {
	return Logger.With(FieldSymbol, symbol)
}

// SymbolInfow logs with any symbol - for dynamic symbol usage
func SymbolInfow(symbol, msg string, keysAndValues ...interface{}) {
	if Logger != nil {
		fields := append([]interface{}{FieldSymbol, symbol}, keysAndValues...)
		Logger.Infow(msg, fields...)
	}
}
