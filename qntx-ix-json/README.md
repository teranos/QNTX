# ix-json — Generic JSON API Ingestor

A QNTX plugin that polls JSON APIs and transforms responses into attestations.

## ✨ Configuration via Attestations

**No TOML editing required!** All configuration is stored as attestations:

1. **Enable plugin** in `am.toml` (DevOps only):
   ```toml
   [plugin]
   enabled = ["ix-json"]
   ```

2. **Configure in UI**: Each glyph instance has its own config stored as attestations
3. **Per-glyph config**: Different glyphs can poll different APIs simultaneously

## Features

- **Per-glyph configuration**: Each ix-json glyph on the canvas has independent config
- **Attestation-based config**: All settings stored as attestations (versioned, auditable, part of knowledge graph)
- **Three operational modes:**
  - **Data-shaping**: Explore API responses and configure mappings
  - **Paused**: Schedule suspended, allows reconfiguration
  - **Active-running**: Automatically polls API on schedule

- **Configurable mapping**: Map JSON fields to Attestation Subject/Predicate/Context
- **Heuristic inference**: Automatically suggests default mappings based on JSON structure
- **Authentication**: Supports Bearer token auth
- **Scheduled execution**: Configure polling interval per glyph

## Usage

1. **Enable**: Add `enabled = ["ix-json"]` to `am.toml` under `[plugin]`
2. **Start**: Run `make dev`
3. **Spawn**: Create an ix-json glyph (🔄) on the canvas
4. **Configure**: Enter API URL, auth token, polling interval in the UI
5. **Save**: Click "Save Config" - creates an attestation
6. **Test**: Click "Test Fetch" to see API response
7. **Activate**: Set polling interval > 0 to start automatic polling

### Multiple Instances

Spawn multiple ix-json glyphs with different configs:
- Glyph 1: Poll GitHub at `https://api.github.com/repos/...` every 5 min
- Glyph 2: Poll Stripe at `https://api.stripe.com/events` every 1 min
- Glyph 3: Poll internal API at `http://localhost:3000/metrics` every 30 sec

Each glyph's config is independent!

## Building

```bash
make ix-json-plugin  # Builds and installs to ~/.qntx/plugins/
```

## Glyph UI

The ix-json glyph provides:
- **Editable config fields**: API URL, auth token, polling interval
- **Save button**: Creates attestation for THIS glyph instance
- **API response preview**: Pretty-printed JSON
- **Attestation mapping**: Shows inferred Subject/Predicate/Context
- **Test fetch**: Manual API call for exploration
- **Mode indicator**: Data-shaping / paused / active-running

## Architecture

- **Per-glyph config**: Subject = `ix-json-glyph-{glyph_id}`, Predicate = `configured`
- **Plugin**: External gRPC process
- **Storage**: Config and data both stored as attestations
- **Melding**: Can be connected to other glyphs (e.g., py) to trigger downstream processing

## Example Attestation

When you save config, it creates:
```
Subject: ix-json-glyph-abc123
Predicate: configured
Context: _
Attributes: {
  "api_url": "https://api.github.com/repos/teranos/qntx/issues",
  "auth_token": "ghp_...",
  "poll_interval_seconds": 300
}
Source: ix-json-ui
```
