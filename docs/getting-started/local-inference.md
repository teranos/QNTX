# Local Inference Setup

**QNTX supports multiple LLM providers that can be enabled/disabled independently:**
- **OpenRouter** - Cloud-based, supports GPT-4, Claude, etc. (configured via `openrouter.*` settings)
- **Local inference** - Ollama or LocalAI running on your hardware (configured via `local_inference.*` settings)

When `local_inference.enabled = true`, QNTX uses your local Ollama/LocalAI server. When false, it falls back to OpenRouter or other configured providers.

This guide covers setting up and enabling local inference providers.

## Why Local Inference?

**Privacy, cost, and control.** Cloud LLM APIs are convenient but:
- **Cost**: $0.001-0.01+ per API call adds up fast
- **Privacy**: Your data goes to third-party servers
- **Latency**: Network round-trips add 200-1000ms
- **Availability**: Internet required, rate limits apply

**Local inference** runs models on your hardware. Zero API cost, complete privacy, works offline.

## Why Ollama?

**Simplest path from zero to working LLM.** No Python, no virtual environments, no CUDA drivers (unless you want GPU acceleration). Download binary, pull model, run.

Alternative (LocalAI) exists but Ollama's UX is unmatched for getting started.

## Quick Start

### 1. Install Ollama

```bash
# macOS / Linux
curl -fsSL https://ollama.com/install.sh | sh

# Or download: https://ollama.com/download
```

### 2. Download a Model

```bash
# Default: Smallest, fastest (3B params, 2GB)
ollama pull llama3.2:3b

# Popular alternatives:
ollama pull mistral          # General-purpose (7B, 4GB) - better quality
ollama pull qwen2.5-coder:7b # Code-optimized (7B, 4.5GB) - best for code
ollama pull phi3:mini        # Microsoft's model (3.8B, 2.3GB) - good balance
ollama pull gemma2:2b        # Google's smallest (2B, 1.6GB) - very fast
ollama pull deepseek-coder:6.7b # Code specialist (6.7B, 3.8GB)
```

**Why these models?** Balance of size/speed/quality. Smaller models (2-3B) are fast on CPU. Larger models (7B) give better results but need more RAM.

### 3. Start Ollama

```bash
ollama serve
```

Runs on `http://localhost:11434` by default.

### 4. Configure QNTX

Edit `~/.qntx/am.toml` (or project `am.toml`):

```toml
[local_inference]
enabled = true  # Disabled by default, set to true to use local models
base_url = "http://localhost:11434"
model = "llama3.2:3b"  # Default model
timeout_seconds = 360  # 6 minutes timeout for slow inference
context_size = 16384   # Context window (0 = use model default)
```

**Note:** Local inference is disabled by default. Set `enabled = true` to activate.

**Done.** All QNTX LLM operations now use local inference.

## Verify Setup

```bash
# Check Ollama is running
curl http://localhost:11434/api/tags

# Test inference
curl http://localhost:11434/v1/chat/completions \
  -H "Content-Type: application/json" \
  -d '{
    "model": "llama3.2:3b",
    "messages": [{"role": "user", "content": "Hello!"}]
  }'
```

## Performance: CPU vs GPU

**CPU inference works but is slow** (5-10 tokens/sec). Fine for development, frustrating for production.

**GPU acceleration** makes local inference viable:
- **Apple Silicon (M1/M2/M3)**: Auto-detected, 5-20x faster than CPU
- **NVIDIA GPU**: Requires CUDA, 10-100x faster than CPU

Ollama automatically uses GPU if available. No configuration needed.

### Model Size Tradeoffs

| Model | Size | CPU Speed | GPU Speed | Quality | Best For |
|-------|------|-----------|-----------|---------|----------|
| gemma2:2b | 1.6GB | Slow | Fast | Fair | Minimal resources |
| llama3.2:3b | 2GB | Slow | Fast | Good | Default, balanced |
| phi3:mini | 2.3GB | Slow | Fast | Good | General purpose |
| mistral | 4GB | Very Slow | Very Fast | Excellent | Quality output |
| qwen2.5-coder:7b | 4.5GB | Very Slow | Very Fast | Excellent | Code/technical |

**Why not 13B or 70B models?** Possible but require 8GB+ VRAM and are slower. Diminishing returns for most QNTX operations.

## Environment Variables

**Quick override without editing config files:**

```bash
# Switch to local provider (Ollama/LocalAI) instead of OpenRouter
export QNTX_LOCAL_INFERENCE_ENABLED=true

# Point to different Ollama server
export QNTX_LOCAL_INFERENCE_BASE_URL=http://gpu-server:11434

# Use different model
export QNTX_LOCAL_INFERENCE_MODEL=mistral

# Start QNTX with overrides
make dev
```

**Note:** Environment variables take precedence over all config files. When `QNTX_LOCAL_INFERENCE_ENABLED=true`, QNTX uses Ollama/LocalAI instead of OpenRouter (cloud provider).

## Cost Tracking

**Local inference has zero API cost** but uses local resources (GPU/CPU time, electricity).

QNTX budget system is API-cost focused. Future versions may track GPU time:

```toml
[pulse]
# Current: Only tracks cloud API spend
daily_budget_usd = 5.00

# Future: Track GPU resource usage
# max_gpu_minutes_per_day = 30.0
```

See `pulse/budget/` package TODOs for GPU resource tracking plans.

## Switching Between Local and Cloud

**Why switch?** Local for bulk operations (save money), cloud for occasional high-quality needs (GPT-4, Claude).

**Option 1: Edit config file**
```toml
# ~/.qntx/am.toml
[local_inference]
enabled = true  # or false
```

**Option 2: Environment variable**
```bash
QNTX_LOCAL_INFERENCE_ENABLED=false make dev
```

Configuration reloads automatically. No restart required.

## Troubleshooting

### Connection Refused

**Cause**: Ollama server not running.

**Fix**: `ollama serve`

### Model Not Found

**Cause**: Model not downloaded.

**Fix**: `ollama pull mistral`

### Slow on CPU

**Cause**: CPU inference is inherently slow.

**Options**:
1. Use smaller model: `ollama pull llama3.2:3b`
2. Increase timeout: `timeout_seconds = 300`
3. Get GPU-enabled hardware
4. Use cloud APIs for now

### GPU Not Detected (NVIDIA)

**Check**: `nvidia-smi`

**Fix**: Install CUDA toolkit (Ollama will use it automatically)

**Apple Silicon**: Works automatically, no setup needed

## Advanced: Custom Models

**Why custom models?** Specialized system prompts, custom parameters, fine-tuned weights.

Create `Modelfile`:

```
FROM llama3.2:3b

SYSTEM You are a code review assistant. Focus on security and correctness.

PARAMETER temperature 0.7
PARAMETER num_predict 2048
```

Build and use:

```bash
ollama create qntx-reviewer -f Modelfile
```

Configure in `am.toml`:

```toml
[local_inference]
model = "qntx-reviewer"
```

## When to Use Local vs Cloud

**Use local if:**
- You have GPU (Apple Silicon or NVIDIA)
- Bulk operations (cost savings matter)
- Privacy/security requirements
- Offline usage needed

**Use cloud if:**
- No GPU and need speed
- Occasional/low-volume usage
- Want frontier models (GPT-4, Claude)
- Minimal setup time

## Alternative: LocalAI

**Why consider LocalAI?** Written in Go (like QNTX), supports many model formats, self-contained binary.

**Why Ollama instead?** Better UX, faster iteration, larger community, simpler setup.

LocalAI is viable if you need specific model formats Ollama doesn't support.

### Quick Start (Docker)

```bash
docker run -p 8080:8080 localai/localai:latest
```

Configure:

```toml
[local_inference]
enabled = true
base_url = "http://localhost:8080"
model = "your-model-name"
```

## Resources

- **Ollama**: https://ollama.com
- **Model Library**: https://ollama.com/library
- **LocalAI**: https://localai.io
- **QNTX GPU Resource Plans**: `pulse/budget/` package TODOs
