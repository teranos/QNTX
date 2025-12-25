# Logger Package

Structured logging for QNTX with colorized console output and JSON support.

## What It Provides

Global logger instance with dual output modes:
- Console mode: Colorized, human-readable output with theme support (Everforest, Gruvbox)
- JSON mode: Structured logs for production/Lambda environments

## Key Features

- Minimal encoder for calm, compact console output
- Automatic Lambda environment detection
- Verbosity level mapping for CLI integration
- Theme-aware message colorization (brackets, symbols, job IDs)

## When to Use

- CLI applications needing clean console output
- AWS Lambda functions requiring environment-aware logging
- Services wanting structured logging with readable development output

## Architecture Notes

Built on uber-go/zap. Uses global singleton pattern for simplicity in CLI contexts. Lambda initialization auto-detects production vs development environment.

See `logger.go` and `minimal_encoder.go` for implementation details.

## See Also

- [Verbosity Levels](../docs/development/verbosity.md) - CLI verbosity pattern and logger integration
