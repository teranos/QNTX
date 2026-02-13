# QNTX Embeddings

See [docs/embeddings.md](https://github.com/teranos/QNTX/blob/main/docs/embeddings.md) for architecture, API reference, and open work.

## Model Files

The ONNX model files are not in git (~86MB). To obtain them:

```bash
pip install transformers optimum[onnxruntime]
cd ats/embeddings
python export_model.py
```

This downloads all-MiniLM-L6-v2 from HuggingFace and exports to ONNX format:
- `models/all-MiniLM-L6-v2/model.onnx`
- `models/all-MiniLM-L6-v2/tokenizer.json`
- `models/all-MiniLM-L6-v2/tokenizer_config.json`
