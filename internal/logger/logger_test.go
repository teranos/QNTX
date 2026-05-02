package logger

import (
	"os"
	"testing"

	"go.uber.org/zap"
)

func TestInitialize(t *testing.T) {
	tests := []struct {
		name       string
		jsonOutput bool
		wantErr    bool
	}{
		{
			name:       "JSON output mode",
			jsonOutput: true,
			wantErr:    false,
		},
		{
			name:       "Console output mode",
			jsonOutput: false,
			wantErr:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Reset global logger
			Logger = nil
			JSONOutput = false

			err := Initialize(tt.jsonOutput)
			if (err != nil) != tt.wantErr {
				t.Errorf("Initialize() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if !tt.wantErr {
				if Logger == nil {
					t.Error("Initialize() did not set global Logger")
				}
				if JSONOutput != tt.jsonOutput {
					t.Errorf("Initialize() JSONOutput = %v, want %v", JSONOutput, tt.jsonOutput)
				}
			}

			// Cleanup
			if Logger != nil {
				Logger.Sync()
				Logger = nil
			}
		})
	}
}

func TestInitializeForLambda(t *testing.T) {
	tests := []struct {
		name        string
		envVars     map[string]string
		wantJSON    bool
		wantLogInfo bool // Whether INFO logs should be visible (dev) or suppressed (prod)
	}{
		{
			name: "Production - AWS_EXECUTION_ENV set",
			envVars: map[string]string{
				"AWS_EXECUTION_ENV": "AWS_Lambda_go1.x",
			},
			wantJSON:    true,
			wantLogInfo: false, // INFO suppressed in production
		},
		{
			name: "Production - ENVIRONMENT=production",
			envVars: map[string]string{
				"ENVIRONMENT": "production",
			},
			wantJSON:    true,
			wantLogInfo: false,
		},
		{
			name: "Production - ENVIRONMENT=prod",
			envVars: map[string]string{
				"ENVIRONMENT": "prod",
			},
			wantJSON:    true,
			wantLogInfo: false,
		},
		{
			name: "Production - LOG_LEVEL=WARN",
			envVars: map[string]string{
				"LOG_LEVEL": "WARN",
			},
			wantJSON:    true,
			wantLogInfo: false,
		},
		{
			name: "Production - LOG_LEVEL=ERROR",
			envVars: map[string]string{
				"LOG_LEVEL": "ERROR",
			},
			wantJSON:    true,
			wantLogInfo: false,
		},
		{
			name:        "Development - no env vars",
			envVars:     map[string]string{},
			wantJSON:    false,
			wantLogInfo: true,
		},
		{
			name: "Development - ENVIRONMENT=dev",
			envVars: map[string]string{
				"ENVIRONMENT": "dev",
			},
			wantJSON:    false,
			wantLogInfo: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Reset global logger and JSONOutput
			Logger = nil
			JSONOutput = false

			// Clear all environment variables first
			os.Unsetenv("AWS_EXECUTION_ENV")
			os.Unsetenv("ENVIRONMENT")
			os.Unsetenv("LOG_LEVEL")

			// Set test environment variables
			for k, v := range tt.envVars {
				os.Setenv(k, v)
			}

			// Cleanup environment variables after test
			defer func() {
				for k := range tt.envVars {
					os.Unsetenv(k)
				}
			}()

			err := InitializeForLambda()
			if err != nil {
				t.Errorf("InitializeForLambda() error = %v", err)
				return
			}

			if Logger == nil {
				t.Error("InitializeForLambda() did not set global Logger")
			}

			if JSONOutput != tt.wantJSON {
				t.Errorf("InitializeForLambda() JSONOutput = %v, want %v", JSONOutput, tt.wantJSON)
			}

			// Verify environment detection worked correctly
			isProd := isProductionEnvironment()
			expectedProd := tt.wantJSON // Production should have JSON output
			if isProd != expectedProd {
				t.Errorf("isProductionEnvironment() = %v, want %v", isProd, expectedProd)
			}

			// Cleanup
			if Logger != nil {
				Logger.Sync()
				Logger = nil
			}
		})
	}
}

func TestIsProductionEnvironment(t *testing.T) {
	tests := []struct {
		name    string
		envVars map[string]string
		want    bool
	}{
		{
			name:    "Production - AWS_EXECUTION_ENV set",
			envVars: map[string]string{"AWS_EXECUTION_ENV": "AWS_Lambda_go1.x"},
			want:    true,
		},
		{
			name:    "Production - ENVIRONMENT=production",
			envVars: map[string]string{"ENVIRONMENT": "production"},
			want:    true,
		},
		{
			name:    "Production - ENVIRONMENT=prod",
			envVars: map[string]string{"ENVIRONMENT": "prod"},
			want:    true,
		},
		{
			name:    "Production - ENVIRONMENT=PRODUCTION (uppercase)",
			envVars: map[string]string{"ENVIRONMENT": "PRODUCTION"},
			want:    true,
		},
		{
			name:    "Production - LOG_LEVEL=WARN",
			envVars: map[string]string{"LOG_LEVEL": "WARN"},
			want:    true,
		},
		{
			name:    "Production - LOG_LEVEL=ERROR",
			envVars: map[string]string{"LOG_LEVEL": "ERROR"},
			want:    true,
		},
		{
			name:    "Production - LOG_LEVEL=warn (lowercase)",
			envVars: map[string]string{"LOG_LEVEL": "warn"},
			want:    true,
		},
		{
			name:    "Development - no env vars",
			envVars: map[string]string{},
			want:    false,
		},
		{
			name:    "Development - ENVIRONMENT=dev",
			envVars: map[string]string{"ENVIRONMENT": "dev"},
			want:    false,
		},
		{
			name:    "Development - ENVIRONMENT=development",
			envVars: map[string]string{"ENVIRONMENT": "development"},
			want:    false,
		},
		{
			name:    "Development - LOG_LEVEL=INFO",
			envVars: map[string]string{"LOG_LEVEL": "INFO"},
			want:    false,
		},
		{
			name:    "Development - LOG_LEVEL=DEBUG",
			envVars: map[string]string{"LOG_LEVEL": "DEBUG"},
			want:    false,
		},
		{
			name:    "Edge case - empty ENVIRONMENT value",
			envVars: map[string]string{"ENVIRONMENT": ""},
			want:    false,
		},
		{
			name:    "Edge case - mixed case ENVIRONMENT=Prod",
			envVars: map[string]string{"ENVIRONMENT": "Prod"},
			want:    true, // Should be case-insensitive
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Clear all relevant environment variables first
			os.Unsetenv("AWS_EXECUTION_ENV")
			os.Unsetenv("ENVIRONMENT")
			os.Unsetenv("LOG_LEVEL")

			// Set test environment variables
			for k, v := range tt.envVars {
				os.Setenv(k, v)
			}

			// Cleanup environment variables after test
			defer func() {
				for k := range tt.envVars {
					os.Unsetenv(k)
				}
			}()

			got := isProductionEnvironment()
			if got != tt.want {
				t.Errorf("isProductionEnvironment() = %v, want %v (env vars: %v)", got, tt.want, tt.envVars)
			}
		})
	}
}

func TestGetEnvironmentType(t *testing.T) {
	tests := []struct {
		name    string
		envVars map[string]string
		want    string
	}{
		{
			name:    "Production environment",
			envVars: map[string]string{"ENVIRONMENT": "production"},
			want:    "production",
		},
		{
			name:    "Development environment",
			envVars: map[string]string{"ENVIRONMENT": "dev"},
			want:    "development",
		},
		{
			name:    "Default development",
			envVars: map[string]string{},
			want:    "development",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Clear environment variables
			os.Unsetenv("AWS_EXECUTION_ENV")
			os.Unsetenv("ENVIRONMENT")
			os.Unsetenv("LOG_LEVEL")

			// Set test environment variables
			for k, v := range tt.envVars {
				os.Setenv(k, v)
			}

			defer func() {
				for k := range tt.envVars {
					os.Unsetenv(k)
				}
			}()

			got := getEnvironmentType()
			if got != tt.want {
				t.Errorf("getEnvironmentType() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestGetLogLevel(t *testing.T) {
	tests := []struct {
		name    string
		envVars map[string]string
		want    string
	}{
		{
			name:    "Production log level",
			envVars: map[string]string{"ENVIRONMENT": "production"},
			want:    "WARN+",
		},
		{
			name:    "Development log level",
			envVars: map[string]string{"ENVIRONMENT": "dev"},
			want:    "INFO+",
		},
		{
			name:    "Default development log level",
			envVars: map[string]string{},
			want:    "INFO+",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Clear environment variables
			os.Unsetenv("AWS_EXECUTION_ENV")
			os.Unsetenv("ENVIRONMENT")
			os.Unsetenv("LOG_LEVEL")

			// Set test environment variables
			for k, v := range tt.envVars {
				os.Setenv(k, v)
			}

			defer func() {
				for k := range tt.envVars {
					os.Unsetenv(k)
				}
			}()

			got := getLogLevel()
			if got != tt.want {
				t.Errorf("getLogLevel() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestCleanup(t *testing.T) {
	tests := []struct {
		name        string
		setupLogger bool
		expectPanic bool
	}{
		{
			name:        "Cleanup with initialized logger",
			setupLogger: true,
			expectPanic: false,
		},
		{
			name:        "Cleanup with nil logger (should not panic)",
			setupLogger: false,
			expectPanic: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Setup
			if tt.setupLogger {
				config := zap.NewDevelopmentConfig()
				zapLogger, err := config.Build()
				if err != nil {
					t.Fatalf("Failed to create test logger: %v", err)
				}
				Logger = zapLogger.Sugar()
			} else {
				Logger = nil
			}

			// Test cleanup
			defer func() {
				if r := recover(); r != nil && !tt.expectPanic {
					t.Errorf("Cleanup() panicked unexpectedly: %v", r)
				}
			}()

			Cleanup()

			// Cleanup should not leave logger in an unusable state
			// If it was set, it should still be set
			if tt.setupLogger && Logger == nil {
				t.Error("Cleanup() should not nil out the logger")
			}

			// Additional cleanup
			if Logger != nil {
				Logger = nil
			}
		})
	}
}

// TestHelperForLogger is a test helper that initializes a test logger
// without affecting global state. This addresses FIX-6.
func TestHelperForLogger(t *testing.T) {
	// Create a test logger without setting global Logger
	testLogger := newTestLogger(t)

	if testLogger == nil {
		t.Error("newTestLogger() returned nil")
	}

	// Verify global logger is not affected
	if Logger != nil {
		t.Error("newTestLogger() should not modify global Logger")
	}

	// Test that the logger is functional
	testLogger.Info("Test message")
	testLogger.Infow("Structured test", "key", "value")
	testLogger.Error("Test error")
}

// newTestLogger creates a logger for testing without modifying global state
func newTestLogger(t *testing.T) *zap.SugaredLogger {
	t.Helper()

	config := zap.NewDevelopmentConfig()
	config.Level = zap.NewAtomicLevelAt(zap.InfoLevel)

	zapLogger, err := config.Build()
	if err != nil {
		t.Fatalf("Failed to create test logger: %v", err)
	}

	return zapLogger.Sugar()
}

// TestLoggingFunctions tests the package-level logging functions
func TestLoggingFunctions(t *testing.T) {
	// Initialize a test logger
	Logger = newTestLogger(t)
	defer func() {
		if Logger != nil {
			Logger.Sync()
			Logger = nil
		}
	}()

	// Test all logging functions (should not panic)
	t.Run("Info functions", func(t *testing.T) {
		Info("test")
		Infof("test %s", "format")
		Infow("test", "key", "value")
	})

	t.Run("Error functions", func(t *testing.T) {
		Error("test")
		Errorf("test %s", "format")
		Errorw("test", "key", "value")
	})

	t.Run("Warn functions", func(t *testing.T) {
		Warn("test")
		Warnf("test %s", "format")
		Warnw("test", "key", "value")
	})

	t.Run("Debug functions", func(t *testing.T) {
		Debug("test")
		Debugf("test %s", "format")
		Debugw("test", "key", "value")
	})

	t.Run("With nil logger (should not panic)", func(t *testing.T) {
		Logger = nil

		// All these should be safe to call with nil logger
		Info("test")
		Infof("test %s", "format")
		Infow("test", "key", "value")
		Error("test")
		Errorf("test %s", "format")
		Errorw("test", "key", "value")
		Warn("test")
		Warnf("test %s", "format")
		Warnw("test", "key", "value")
		Debug("test")
		Debugf("test %s", "format")
		Debugw("test", "key", "value")
	})
}

// Benchmark tests for logger performance

// BenchmarkInitialize benchmarks logger initialization
func BenchmarkInitialize(b *testing.B) {
	for i := 0; i < b.N; i++ {
		Logger = nil
		Initialize(false)
		if Logger != nil {
			Logger.Sync()
		}
	}
}

// BenchmarkInitializeJSON benchmarks JSON logger initialization
func BenchmarkInitializeJSON(b *testing.B) {
	for i := 0; i < b.N; i++ {
		Logger = nil
		Initialize(true)
		if Logger != nil {
			Logger.Sync()
		}
	}
}

// newBenchmarkLogger creates a logger for benchmarking without modifying global state
func newBenchmarkLogger() *zap.SugaredLogger {
	config := zap.NewDevelopmentConfig()
	config.Level = zap.NewAtomicLevelAt(zap.InfoLevel)

	zapLogger, err := config.Build()
	if err != nil {
		panic(err)
	}

	return zapLogger.Sugar()
}

// BenchmarkInfo benchmarks Info logging
func BenchmarkInfo(b *testing.B) {
	Logger = newBenchmarkLogger()
	defer func() {
		if Logger != nil {
			Logger.Sync()
			Logger = nil
		}
	}()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		Info("test message")
	}
}

// BenchmarkInfof benchmarks formatted Info logging
func BenchmarkInfof(b *testing.B) {
	Logger = newBenchmarkLogger()
	defer func() {
		if Logger != nil {
			Logger.Sync()
			Logger = nil
		}
	}()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		Infof("test message %d", i)
	}
}

// BenchmarkInfow benchmarks structured Info logging
func BenchmarkInfow(b *testing.B) {
	Logger = newBenchmarkLogger()
	defer func() {
		if Logger != nil {
			Logger.Sync()
			Logger = nil
		}
	}()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		Infow("test message", "iteration", i, "key", "value")
	}
}

// BenchmarkError benchmarks Error logging
func BenchmarkError(b *testing.B) {
	Logger = newBenchmarkLogger()
	defer func() {
		if Logger != nil {
			Logger.Sync()
			Logger = nil
		}
	}()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		Error("test error")
	}
}

// BenchmarkErrorw benchmarks structured Error logging
func BenchmarkErrorw(b *testing.B) {
	Logger = newBenchmarkLogger()
	defer func() {
		if Logger != nil {
			Logger.Sync()
			Logger = nil
		}
	}()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		Errorw("test error", "iteration", i, "error_code", "TEST_ERROR")
	}
}

// BenchmarkIsProductionEnvironment benchmarks environment detection
func BenchmarkIsProductionEnvironment(b *testing.B) {
	// Setup environment
	os.Setenv("ENVIRONMENT", "production")
	defer os.Unsetenv("ENVIRONMENT")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = isProductionEnvironment()
	}
}

// BenchmarkParallelLogging benchmarks concurrent logging
func BenchmarkParallelLogging(b *testing.B) {
	Logger = newBenchmarkLogger()
	defer func() {
		if Logger != nil {
			Logger.Sync()
			Logger = nil
		}
	}()

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			Infow("parallel log", "goroutine_iteration", i)
			i++
		}
	})
}
