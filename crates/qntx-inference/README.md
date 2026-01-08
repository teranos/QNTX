# QNTX Inference Plugin

Local embedding generation for semantic search using ONNX models.

## What is ONNX?

**ONNX** (Open Neural Network Exchange) is an open format for representing machine learning models. It allows models trained in frameworks like PyTorch or TensorFlow to be exported and run efficiently using optimized runtimes.

**Why ONNX for QNTX?**
- **Portable**: Run models without Python or heavy ML frameworks
- **Fast**: ONNX Runtime is highly optimized for inference
- **Flexible**: Use any compatible embedding model (sentence-transformers, etc.)

## Obtaining Models

### Option 1: Export from HuggingFace (Recommended)

Use the `optimum` library to export sentence-transformer models:

```bash
pip install optimum[exporters] onnx

# Export a model (e.g., all-MiniLM-L6-v2)
optimum-cli export onnx \
  --model sentence-transformers/all-MiniLM-L6-v2 \
  --task feature-extraction \
  ./models/minilm/
```

This creates:
- `model.onnx` - The model weights
- `tokenizer.json` - The tokenizer configuration

### Option 2: Download Pre-converted Models

Some models are available pre-converted:
- [sentence-transformers ONNX models](https://huggingface.co/sentence-transformers)
- [Xenova/transformers.js models](https://huggingface.co/Xenova) (ONNX format)

### Recommended Models

| Model | Dimensions | Speed | Quality | Use Case |
|-------|-----------|-------|---------|----------|
| `all-MiniLM-L6-v2` | 384 | Fast | Good | General purpose |
| `all-mpnet-base-v2` | 768 | Medium | Better | Higher accuracy |
| `bge-small-en-v1.5` | 384 | Fast | Good | Retrieval-focused |

## Configuration

Configure via QNTX's plugin settings (≡ am):

```toml
[plugins.inference]
model_path = "~/.qntx/models/minilm/model.onnx"
tokenizer_path = "~/.qntx/models/minilm/tokenizer.json"
max_length = 512        # Max tokens per text
normalize = true        # L2-normalize embeddings
num_threads = 0         # 0 = auto-detect
```

Or via the UI plugin configuration panel.

## API Endpoints

### Generate Embeddings

```bash
POST /plugins/inference/embed
Content-Type: application/json

# Single text
{"input": "hello world"}

# Batch
{"input": ["hello world", "semantic search", "QNTX attestations"]}
```

Response:
```json
{
  "embeddings": [[0.123, -0.456, ...], ...],
  "model": "/path/to/model.onnx",
  "dimensions": 384
}
```

### OpenAI-Compatible Endpoint

```bash
POST /plugins/inference/v1/embeddings
Content-Type: application/json

{"input": ["text to embed"], "model": "ignored"}
```

### Health Check

```bash
GET /plugins/inference/health
```

```json
{
  "healthy": true,
  "model_loaded": true,
  "message": "model loaded and ready"
}
```

## Usage Examples

### Semantic Search over Attestations

```bash
# 1. Generate embedding for query
curl -X POST http://localhost:877/plugins/inference/embed \
  -H "Content-Type: application/json" \
  -d '{"input": "people who worked at Google"}'

# 2. Use embedding with ax for similarity search
# (Integration with ax similarity search coming soon)
```

### Batch Processing

```python
import requests

texts = [
    "Alice is a software engineer",
    "Bob works in marketing",
    "Charlie is the CEO"
]

response = requests.post(
    "http://localhost:877/plugins/inference/embed",
    json={"input": texts}
)

embeddings = response.json()["embeddings"]
# Each embedding is a 384-dimensional vector (for MiniLM)
```

## Model Directory Structure

Recommended layout for managing models:

```
~/.qntx/models/
├── minilm/
│   ├── model.onnx
│   └── tokenizer.json
├── mpnet/
│   ├── model.onnx
│   └── tokenizer.json
└── custom/
    ├── model.onnx
    └── tokenizer.json
```

## Performance Tips

1. **Use smaller models** for real-time search (MiniLM, BGE-small)
2. **Batch requests** when embedding multiple texts
3. **Set `num_threads`** explicitly on multi-core systems
4. **Pre-compute embeddings** for static content (attestation predicates, contexts)

## Troubleshooting

### "model not loaded"
- Check `model_path` and `tokenizer_path` exist
- Verify the model is valid ONNX format
- Check plugin logs for loading errors

### Slow inference
- Use a smaller model
- Reduce `max_length` if texts are short
- Increase `num_threads`

### Out of memory
- Use a smaller model
- Reduce batch sizes
- Check for memory leaks in long-running processes

## Building from Source

Requires Nix for reproducible builds:

```bash
nix build .#qntx-inference
```

Or within the development shell:

```bash
nix develop
cargo build -p qntx-inference --release
```
