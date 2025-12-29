# Local Inference Setup

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
# Recommended: Fast, general-purpose (7B params, 4GB)
ollama pull mistral

# Alternative: Smaller, faster (3B params, 2GB)
ollama pull llama3.2:3b

# For code: Optimized for technical content (7B, 4.5GB)
ollama pull qwen2.5-coder:7b
```

**Why these models?** Balance of size/speed/quality. Smaller models (3B) are fast on CPU. Larger models (7B) give better results but need more RAM.

### 3. Start Ollama

```bash
ollama serve
```

Runs on `http://localhost:11434` by default.

### 4. Configure QNTX

Edit `~/.qntx/am.toml` (or project `am.toml`):

```toml
[local_inference]
enabled = true
base_url = "http://localhost:11434"
model = "mistral"
timeout_seconds = 120
```

**Done.** All QNTX LLM operations now use local inference.

## Verify Setup

```bash
# Check Ollama is running
curl http://localhost:11434/api/tags

# Test inference
curl http://localhost:11434/v1/chat/completions \
  -H "Content-Type: application/json" \
  -d '{
    "model": "mistral",
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
| llama3.2:3b | 2GB | Very Slow | Fast | Good | Testing, quick tasks |
| mistral | 4GB | Slow | Very Fast | Excellent | General purpose |
| qwen2.5-coder:7b | 4.5GB | Slow | Very Fast | Excellent | Code/technical |

**Why not 13B or 70B models?** Possible but require 8GB+ VRAM and are slower. Diminishing returns for most QNTX operations.

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

```bash
# Enable local inference
qntx config set local_inference.enabled true

# Disable (use cloud APIs)
qntx config set local_inference.enabled false
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
FROM mistral

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
