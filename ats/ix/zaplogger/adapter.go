package zaplogger

import (
	"github.com/teranos/QNTX/ats/ix"
	"go.uber.org/zap"
)

// ZapAdapter adapts zap.SugaredLogger to implement ats/ix.Logger interface.
// This allows QNTX to use zap while the ATS library remains logger-agnostic.
type ZapAdapter struct {
	logger *zap.SugaredLogger
}

// NewZapAdapter creates a logger adapter from a zap SugaredLogger.
func NewZapAdapter(zapLogger *zap.SugaredLogger) ix.Logger {
	if zapLogger == nil {
		return ix.NewNopLogger()
	}
	return &ZapAdapter{logger: zapLogger}
}

// Info implements ix.Logger.Info using zap's Infow.
func (z *ZapAdapter) Info(msg string, fields ...interface{}) {
	z.logger.Infow(msg, fields...)
}

// Warn implements ix.Logger.Warn using zap's Warnw.
func (z *ZapAdapter) Warn(msg string, fields ...interface{}) {
	z.logger.Warnw(msg, fields...)
}

// Error implements ix.Logger.Error using zap's Errorw.
func (z *ZapAdapter) Error(msg string, fields ...interface{}) {
	z.logger.Errorw(msg, fields...)
}
