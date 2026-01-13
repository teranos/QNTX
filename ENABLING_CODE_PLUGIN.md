# Enabling the Go Code Plugin

## Current Status

By default, **no plugins are enabled** in QNTX. The code domain plugin needs explicit configuration to be loaded.

## Configuration Required

### Option 1: User-Level Installation (Recommended)

1. **Build the plugin binary:**
   ```bash
   go build -o qntx-code-plugin ./qntx-code/cmd/qntx-code-plugin
   ```

2. **Install to user plugins directory:**
   ```bash
   mkdir -p ~/.qntx/plugins
   cp qntx-code-plugin ~/.qntx/plugins/
   chmod +x ~/.qntx/plugins/qntx-code-plugin
   ```

3. **Enable the plugin in configuration:**

   Create or edit `~/.qntx/am.toml`:
   ```toml
   [plugin]
   enabled = ["code"]  # Enable the code domain plugin
   ```

4. **Start QNTX:**
   ```bash
   qntx server
   ```

### Option 2: Project-Level Installation

1. **Build the plugin:**
   ```bash
   go build -o qntx-code-plugin ./qntx-code/cmd/qntx-code-plugin
   ```

2. **Keep it in project directory** or move to `./plugins/`:
   ```bash
   mkdir -p ./plugins
   mv qntx-code-plugin ./plugins/
   ```

3. **Enable in project `am.toml`:**
   ```toml
   [plugin]
   enabled = ["code"]
   paths = ["./plugins", "~/.qntx/plugins"]  # Search project first
   ```

## Plugin Discovery

QNTX searches for plugins in these locations (in order):
1. `~/.qntx/plugins/` (user-level)
2. `./plugins/` (project-level)

For each enabled plugin named `X`, it looks for binaries:
- `qntx-X-plugin` (recommended)
- `qntx-X`
- `X`

## Verification

### Check Plugin is Found

```bash
# This will show plugin loading logs
qntx server 2>&1 | grep -i plugin
```

Expected output:
```
INFO  plugin-loader  Searching for 'code' plugin binary in 2 paths
INFO  plugin-loader  Found 'code' plugin binary: /Users/you/.qntx/plugins/qntx-code-plugin
INFO  plugin-loader  Will load 'code' plugin from binary: /Users/you/.qntx/plugins/qntx-code-plugin
INFO  plugin-loader  Started 'code' plugin process (pid=12345, port=9000, addr=127.0.0.1:9000)
INFO  plugin-loader  Plugin 'code' v0.1.0 loaded and ready - Software development domain (git, GitHub, gopls, code editor)
```

### Test Plugin is Responding

```bash
# Check if code endpoints are available
curl http://localhost:877/api/code
```

Should return JSON with code tree.

## Plugin Configuration (Optional)

Create `~/.qntx/plugins/code.toml` to configure the code plugin:

```toml
[config]
# gopls configuration
"gopls.workspace_root" = "."
"gopls.enabled" = "true"

# GitHub integration (optional)
"github.token" = "ghp_your_token_here"
"github.default_owner" = "teranos"
"github.default_repo" = "QNTX"
```

This configuration will be passed to the plugin during initialization.

## Troubleshooting

### Plugin Not Found

**Error:** `Plugin 'code' not found - searched paths: [...], tried names: [qntx-code-plugin, qntx-code, code]`

**Solution:**
1. Verify binary exists: `ls -la ~/.qntx/plugins/qntx-code-plugin`
2. Verify it's executable: `chmod +x ~/.qntx/plugins/qntx-code-plugin`
3. Test it manually: `~/.qntx/plugins/qntx-code-plugin --version`

### Plugin Process Won't Start

**Error:** `failed to launch plugin code (binary=..., port=9000)`

**Solution:**
1. Check dependencies: `ldd ~/.qntx/plugins/qntx-code-plugin` (Linux) or `otool -L` (macOS)
2. Run manually with debug: `~/.qntx/plugins/qntx-code-plugin --port 9000 --log-level debug`
3. Check for port conflicts: `lsof -i :9000`

### Plugin Starts but Times Out

**Error:** `timeout waiting for plugin gRPC service at 127.0.0.1:9000`

**Solution:**
1. Check plugin logs (printed to QNTX server output)
2. Verify gRPC is working: Test connection manually
3. Increase timeout in future (currently hardcoded to 5 seconds)

### No Plugins Enabled

**Info:** `No plugins enabled - QNTX running in minimal core mode`

**Solution:** This is expected! Add `enabled = ["code"]` to `[plugin]` section in am.toml

## Default Configuration

From `am/defaults.go`:
```go
v.SetDefault("plugin.enabled", []string{})  // No plugins by default
v.SetDefault("plugin.paths", []string{
    "~/.qntx/plugins",  // User-level plugins
    "./plugins",         // Project-level plugins
})
```

## Architecture Notes

- All plugins run as **separate processes** communicating via gRPC
- Plugins start on ports 9000+ (assigned dynamically)
- HTTP requests are routed: QNTX server (877) → gRPC → plugin
- Plugins can be restarted without restarting QNTX server

## Next Steps

After enabling the plugin:
1. Test HTTP endpoints: `curl http://localhost:877/api/code`
2. Test WebSocket (if applicable): gopls language server
3. Test GitHub integration (if configured)
4. Access UI at `http://localhost:877` to see code domain features

## See Also

- `TESTING_SUMMARY.md` - Comprehensive test results for this branch
- `qntx-code/README.md` - Code plugin documentation
- `am/README.md` - Configuration system documentation
