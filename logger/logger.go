package logger

import (
	"os"
	"strings"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

// TODO(#35): ServerLogger UX improvements
// Current state: Functional but could be enhanced
// See: https://github.com/teranos/QNTX/issues/35
// Defer until: Have time and appetite for significant logger refactoring

var (
	// Global logger instance
	Logger *zap.SugaredLogger
	// Flag to track if JSON output is enabled
	JSONOutput bool
)

func init() {
	// Initialize with a safe no-op logger at package load time
	// This prevents nil pointer panics if logger is used before Initialize() is called
	Logger = zap.NewNop().Sugar()
}

// Initialize sets up the global logger based on the JSON output preference
func Initialize(jsonOutput bool) error {
	JSONOutput = jsonOutput

	// Load theme from config if available
	loadThemeFromConfig()

	var zapLogger *zap.Logger
	var err error

	if jsonOutput {
		// JSON structured output for machine consumption
		config := zap.NewProductionConfig()
		config.Level = zap.NewAtomicLevelAt(zap.InfoLevel)
		zapLogger, err = config.Build()
	} else {
		// Human-readable console output with minimal, calm formatting
		zapLogger = zap.New(
			zapcore.NewCore(
				newMinimalEncoder(),
				zapcore.AddSync(os.Stdout),
				zap.InfoLevel,
			),
		)
	}

	if err != nil {
		return err
	}

	Logger = zapLogger.Sugar()
	return nil
}

// loadThemeFromConfig attempts to load log theme from environment.
// TODO(#34): After config package extraction, read from config file instead.
// Currently only supports QNTX_LOG_THEME env var.
// Default theme is set in minimal_encoder.go (currentTheme = "everforest").
func loadThemeFromConfig() {
	if theme := os.Getenv("QNTX_LOG_THEME"); theme != "" {
		SetTheme(theme)
	}
}

// InitializeForLambda sets up the global logger for Lambda functions with environment-based configuration
func InitializeForLambda() error {
	isProduction := isProductionEnvironment()

	var zapLogger *zap.Logger
	var err error

	if isProduction {
		// Production: JSON structured output with WARN+ level (suppress INFO)
		JSONOutput = true
		config := zap.NewProductionConfig()
		config.Level = zap.NewAtomicLevelAt(zap.WarnLevel) // Suppress INFO logs
		config.OutputPaths = []string{"stdout"}
		config.ErrorOutputPaths = []string{"stderr"}
		zapLogger, err = config.Build()
	} else {
		// Development/Testing: Human-readable output with minimal formatting
		JSONOutput = false
		zapLogger = zap.New(
			zapcore.NewCore(
				newMinimalEncoder(),
				zapcore.AddSync(os.Stdout),
				zap.InfoLevel,
			),
		)
	}

	if err != nil {
		return err
	}

	Logger = zapLogger.Sugar()

	// Log initialization message (only visible in dev/test, suppressed in prod)
	Logger.Infow("Lambda logger initialized",
		"environment", getEnvironmentType(),
		"log_level", getLogLevel(),
		"production", isProduction)

	return nil
}

// isProductionEnvironment determines if Lambda is running in production
func isProductionEnvironment() bool {
	// AWS_EXECUTION_ENV indicates we're running in a real Lambda environment
	if awsEnv := os.Getenv("AWS_EXECUTION_ENV"); awsEnv != "" {
		return true
	}

	// Check explicit environment flag
	if env := strings.ToLower(os.Getenv("ENVIRONMENT")); env == "production" || env == "prod" {
		return true
	}

	// Check LOG_LEVEL explicitly set to suppress INFO
	if logLevel := strings.ToUpper(os.Getenv("LOG_LEVEL")); logLevel == "WARN" || logLevel == "ERROR" {
		return true
	}

	// Default to development for local testing
	return false
}

// getEnvironmentType returns a string description of the environment
func getEnvironmentType() string {
	if isProductionEnvironment() {
		return "production"
	}
	return "development"
}

// getLogLevel returns the current log level as a string
func getLogLevel() string {
	if isProductionEnvironment() {
		return "WARN+"
	}
	return "INFO+"
}

// Cleanup flushes any buffered log entries.
// Returns an error if the flush fails, though errors are often ignorable
// for stdout/stderr (especially on macOS/Linux where Sync returns EINVAL).
func Cleanup() error {
	if Logger != nil {
		return Logger.Sync()
	}
	return nil
}

// Info logs an info message
func Info(args ...interface{}) {
	if Logger != nil {
		Logger.Info(args...)
	}
}

// Infof logs a formatted info message
func Infof(format string, args ...interface{}) {
	if Logger != nil {
		Logger.Infof(format, args...)
	}
}

// Infow logs an info message with structured fields
func Infow(msg string, keysAndValues ...interface{}) {
	if Logger != nil {
		Logger.Infow(msg, keysAndValues...)
	}
}

// Error logs an error message
func Error(args ...interface{}) {
	if Logger != nil {
		Logger.Error(args...)
	}
}

// Errorf logs a formatted error message
func Errorf(format string, args ...interface{}) {
	if Logger != nil {
		Logger.Errorf(format, args...)
	}
}

// Errorw logs an error message with structured fields
func Errorw(msg string, keysAndValues ...interface{}) {
	if Logger != nil {
		Logger.Errorw(msg, keysAndValues...)
	}
}

// Warn logs a warning message
func Warn(args ...interface{}) {
	if Logger != nil {
		Logger.Warn(args...)
	}
}

// Warnf logs a formatted warning message
func Warnf(format string, args ...interface{}) {
	if Logger != nil {
		Logger.Warnf(format, args...)
	}
}

// Warnw logs a warning message with structured fields
func Warnw(msg string, keysAndValues ...interface{}) {
	if Logger != nil {
		Logger.Warnw(msg, keysAndValues...)
	}
}

// Debug logs a debug message
func Debug(args ...interface{}) {
	if Logger != nil {
		Logger.Debug(args...)
	}
}

// Debugf logs a formatted debug message
func Debugf(format string, args ...interface{}) {
	if Logger != nil {
		Logger.Debugf(format, args...)
	}
}

// Debugw logs a debug message with structured fields
func Debugw(msg string, keysAndValues ...interface{}) {
	if Logger != nil {
		Logger.Debugw(msg, keysAndValues...)
	}
}
