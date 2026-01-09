# Configuration System Architecture

## Overview

QNTX uses a layered configuration system with five sources, merged with clear precedence rules. This design allows system-wide defaults, user preferences, team settings, and environment overrides to coexist cleanly. For the UI implementation of this system, see [Config Panel](../development/config-panel.md). For the REST API, see [Configuration API](../api/configuration.md).

## Configuration Sources

Configuration is loaded from multiple sources in this order (lowest to highest precedence):

```
1. System      /etc/qntx/config.toml               # System-wide defaults
2. User        ~/.qntx/config.toml                 # User manual configuration
3. User UI     ~/.qntx/config_from_ui.toml         # UI-managed configuration
4. Project     ./config.toml                       # Project/team configuration
5. Environment QNTX_* environment variables        # Runtime overrides
```

### Source Precedence

Each layer overrides values from lower layers. For example:
- System sets `daily_budget_usd = 5.0`
- User manually sets `daily_budget_usd = 10.0` in `~/.qntx/config.toml`
- Project sets `daily_budget_usd = 20.0` in project `config.toml`
- **Result**: `20.0` (project wins)

## File Responsibilities

### System Config (`/etc/qntx/config.toml`)
- Installed by package manager or system administrator
- Contains sensible defaults for all users on the system
- Rarely modified by end users
- Example: Default API endpoints, system-wide rate limits

### User Config (`~/.qntx/config.toml`)
- User's manually edited configuration
- Preserves comments and formatting
- For settings the user wants to persist across projects
- Example: Personal API keys, preferred models

### UI Config (`~/.qntx/config_from_ui.toml`)
- **Auto-generated** - created and managed by the web UI
- Comments are not preserved (regenerated on each UI change)
- Git-safe: Never accidentally committed to project repos
- Example: Ollama vs OpenRouter toggle, model selection

### Project Config (`./config.toml`)
- Project-specific configuration
- Checked into git for team sharing
- Highest precedence among files (only environment beats it)
- Example: Team API keys, project-specific models

### Environment Variables
- Highest precedence - overrides all files
- Format: `QNTX_SECTION_KEY=value`
- Example: `QNTX_PULSE_DAILY_BUDGET_USD=50.0`

## Config Update Strategy

### UI Updates (Web Interface)

All UI changes write to `~/.qntx/config_from_ui.toml`:

```go
// Example: Toggle Ollama
config.UpdateLocalInferenceEnabled(true)
```

**Implementation:**
1. Load existing UI config (or create if missing)
2. Update specific field
3. Marshal entire config struct to TOML
4. Create backup (.back1, .back2, .back3 rotation)
5. Write to `~/.qntx/config_from_ui.toml`

**Benefits:**
- Type-safe updates (no regex)
- Atomic file writes
- Backup system for rollback
- Never touches project config

### Manual Updates

Users edit `~/.qntx/config.toml` directly:
- Comments and formatting preserved
- Full control over structure
- Precedence: Higher than UI config but lower than project config

## Source Tracking (Introspection)

### Why Introspection?

**SRE approach to configuration.** Multi-source config creates observability problems. When something doesn't work, you need to know *why*.

**Debugging**: "Why isn't my config working?" User toggles Ollama in UI, nothing happens. Introspection shows project config is overriding user_ui. Now they know what to fix.

**Trust/transparency**: Without visibility, UI changes feel broken. Introspection proves changes took effect (or shows what's overriding them).

**Security audit**: See if environment vars are leaking into places they shouldn't. Know what's coming from where.

### How It Works

The introspection endpoint (`/api/config`) shows where each value comes from:

```json
{
  "local_inference": {
    "enabled": {
      "value": true,
      "source": "user_ui",        // From ~/.qntx/config_from_ui.toml
      "type": "bool"
    },
    "model": {
      "value": "llama3.2:3b",
      "source": "project",         // From ./config.toml
      "type": "string"
    }
  }
}
```

**Source values:**
- `system` - From /etc/qntx/config.toml
- `user` - From ~/.qntx/config.toml
- `user_ui` - From ~/.qntx/config_from_ui.toml
- `project` - From ./config.toml
- `environment` - From QNTX_* env vars

## Implementation Details

### Config Loading

```go
// Internal/config/config.go
func LoadConfig() (*Config, error) {
    // 1. Parse each source separately
    systemCfg := parseConfig("/etc/qntx/config.toml")
    userCfg := parseConfig("~/.qntx/config.toml")
    uiCfg := parseConfig("~/.qntx/config_from_ui.toml")
    projectCfg := parseConfig("./config.toml")

    // 2. Build source map (track where each value comes from)
    sources := buildSourceMap(systemCfg, userCfg, uiCfg, projectCfg)

    // 3. Merge with precedence
    merged := mergeConfigs(systemCfg, userCfg, uiCfg, projectCfg)

    // 4. Apply environment overrides
    applyEnvOverrides(merged)

    return merged, nil
}
```

### Config Persistence

```go
// Internal/config/persist.go
func UpdateLocalInferenceEnabled(enabled bool) error {
    // Get UI config path
    path := GetUIConfigPath()  // ~/.qntx/config_from_ui.toml

    // Load or initialize
    config, err := loadOrInitializeUIConfig()
    if err != nil {
        return err
    }

    // Update field (type-safe)
    config.LocalInference.Enabled = enabled

    // Save with backups
    return saveUIConfig(path, config)
}
```

## Migration and Compatibility

### Existing Configs

No migration required:
- Existing `config.toml` files work unchanged
- First UI update creates `~/.qntx/config_from_ui.toml`
- Precedence ensures no breaking changes

### Comment Preservation

- **Manual config** (`~/.qntx/config.toml`): Comments preserved
- **UI config** (`~/.qntx/config_from_ui.toml`): Comments not preserved (auto-generated)
- **Recommendation**: Put important comments in manual config, not UI config

## Design Rationale

### Why Separate UI Config?

**Problem:** Original design wrote UI changes to project `config.toml`, risking accidental git commits of user preferences.

**Solution:** Separate `config_from_ui.toml` ensures:
1. UI changes never touch project config
2. Project config remains team-shared
3. User preferences stay local

### Why TOML Marshaling vs Regex?

**Original approach:** Regex pattern matching to preserve comments:
```go
re := regexp.MustCompile(`(?sm)(\[local_inference\][^\[]*?^enabled\s*=\s*)(true|false)(.*)`)
updated := re.ReplaceAllString(content, fmt.Sprintf("${1}%s${3}", newValue))
```

**Problems:**
- Fragile (breaks with formatting changes)
- Hard to maintain
- No type safety
- Error-prone

**Current approach:** Proper TOML marshaling:
```go
config.LocalInference.Enabled = enabled
toml.Marshal(config)
```

**Benefits:**
- Type-safe
- Simple, maintainable
- Validates TOML structure
- Self-documenting

**Tradeoff:** Comments in UI config are lost (acceptable for auto-generated file).

## Related Documentation

- **UI Design**: [Config Panel](../development/config-panel.md) - Config panel UI/UX specification and future vision
- **Glossary**: [Configuration Terms](../GLOSSARY.md#configuration) - Symbol and command reference
- **User Guide**: How to configure QNTX (TBD)

## Future Enhancements

Potential additions (not currently in scope):

1. **Config validation**: Warn about invalid values before save
2. **Reset to defaults**: UI button to clear user_ui config
3. **Export merged config**: Download effective configuration
4. **Config diff view**: Show what's different from defaults
5. **Multi-environment support**: Dev/staging/prod profiles
