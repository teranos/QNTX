# Logger Package

Structured logging for QNTX. Logs should be readable and get out of your way.

## Why Minimal Encoder?

Zap's default console encoder is overly verbose. Custom encoder provides calm, compact output with theme support (Everforest, Gruvbox).

## Architecture

Global singleton for CLI simplicity. Server package creates enhanced multi-output logger (console + WebSocket + file) when needed.

Built on uber-go/zap (chosen for convenience, open to alternatives).

## See Also

- [Verbosity Levels](../docs/development/verbosity.md) - CLI verbosity pattern and logger integration
