# qntx-imagegen Manual Testing

## Model Setup (~4GB download)

```bash
brew install git-lfs
git lfs install

git clone --depth 1 --branch onnx \
  https://huggingface.co/runwayml/stable-diffusion-v1-5 \
  /tmp/sd15-onnx

mkdir -p ~/.qntx/models/stable-diffusion-v1-5/{text_encoder,unet,vae_decoder,tokenizer}
cp /tmp/sd15-onnx/text_encoder/model.onnx ~/.qntx/models/stable-diffusion-v1-5/text_encoder/
cp /tmp/sd15-onnx/unet/model.onnx ~/.qntx/models/stable-diffusion-v1-5/unet/
cp /tmp/sd15-onnx/vae_decoder/model.onnx ~/.qntx/models/stable-diffusion-v1-5/vae_decoder/
cp /tmp/sd15-onnx/tokenizer/tokenizer.json ~/.qntx/models/stable-diffusion-v1-5/tokenizer/

# Clean up clone
rm -rf /tmp/sd15-onnx
```

Expected layout:
```
~/.qntx/models/stable-diffusion-v1-5/
├── text_encoder/model.onnx     (~490MB)
├── unet/model.onnx             (~3.4GB)
├── vae_decoder/model.onnx      (~95MB)
└── tokenizer/tokenizer.json    (~2MB)
```

## Start the Plugin

Terminal 1:
```bash
cargo run -p qntx-imagegen-plugin -- --port 9876
```

Expected: `QNTX_PLUGIN_PORT=9876` on stdout, `Starting gRPC server on 0.0.0.0:9876` in logs.

## Tests Without Models (skeleton verification)

These work without downloading anything. Install `grpcurl` if needed: `brew install grpcurl`.

### 1. Version flag
```bash
cargo run -p qntx-imagegen-plugin -- --version
# Expect: qntx-imagegen-plugin 0.1.0
```

### 2. Metadata
```bash
grpcurl -plaintext -import-path plugin/grpc/protocol -proto domain.proto \
  localhost:9876 protocol.DomainPluginService/Metadata
# Expect: name="imagegen", version="0.1.0"
```

### 3. Health (before init)
```bash
grpcurl -plaintext -import-path plugin/grpc/protocol -proto domain.proto \
  localhost:9876 protocol.DomainPluginService/Health
# Expect: healthy=false, message="Not initialized"
```

### 4. Initialize
```bash
grpcurl -plaintext -import-path plugin/grpc/protocol -proto domain.proto \
  -d '{"config":{}}' \
  localhost:9876 protocol.DomainPluginService/Initialize
# Expect: handler_names=["imagegen.generate"]
# Logs warn about missing model files
```

### 5. Health (after init, no models)
```bash
grpcurl -plaintext -import-path plugin/grpc/protocol -proto domain.proto \
  localhost:9876 protocol.DomainPluginService/Health
# Expect: healthy=true, pipeline_loaded=false
```

### 6. Models check
```bash
grpcurl -plaintext -import-path plugin/grpc/protocol -proto domain.proto \
  -d '{"method":"POST","path":"/models/check"}' \
  localhost:9876 protocol.DomainPluginService/HandleHTTP
# Expect: JSON body with all_present=false, each file listed
```

### 7. Generate fails gracefully
```bash
grpcurl -plaintext -import-path plugin/grpc/protocol -proto domain.proto \
  -d '{"handler_name":"imagegen.generate","job_id":"test-1","payload":"eyJwcm9tcHQiOiJhIGNhdCJ9"}' \
  localhost:9876 protocol.DomainPluginService/ExecuteJob
# Expect: success=false, error mentions missing models
```

### 8. Status endpoint
```bash
grpcurl -plaintext -import-path plugin/grpc/protocol -proto domain.proto \
  -d '{"method":"GET","path":"/status"}' \
  localhost:9876 protocol.DomainPluginService/HandleHTTP
# Expect: initialized=true, pipeline_loaded=false, generating=false
```

### 9. Config schema
```bash
grpcurl -plaintext -import-path plugin/grpc/protocol -proto domain.proto \
  localhost:9876 protocol.DomainPluginService/ConfigSchema
# Expect: 7 fields (models_dir, output_dir, num_inference_steps, etc.)
```

### 10. Unknown handler rejected
```bash
grpcurl -plaintext -import-path plugin/grpc/protocol -proto domain.proto \
  -d '{"handler_name":"imagegen.bogus","job_id":"x","payload":"e30="}' \
  localhost:9876 protocol.DomainPluginService/ExecuteJob
# Expect: NOT_FOUND error
```

### 11. Port retry
```bash
# Start two instances on the same port:
cargo run -p qntx-imagegen-plugin -- --port 9876 &
cargo run -p qntx-imagegen-plugin -- --port 9876
# Expect: second instance logs "Port 9876 in use, trying 9877"
# and announces QNTX_PLUGIN_PORT=9877
kill %1
```

## Tests With Models (full pipeline)

Restart the plugin after placing models (or call Initialize again).

### 12. Initialize loads pipeline
```bash
# Restart plugin, then:
grpcurl -plaintext -import-path plugin/grpc/protocol -proto domain.proto \
  -d '{"config":{}}' \
  localhost:9876 protocol.DomainPluginService/Initialize
# Expect: logs show "Loading CLIP tokenizer...", "Loading UNet...", etc.
# "Pipeline loaded successfully"
```

### 13. Health confirms pipeline
```bash
grpcurl -plaintext -import-path plugin/grpc/protocol -proto domain.proto \
  localhost:9876 protocol.DomainPluginService/Health
# Expect: healthy=true, pipeline_loaded=true
```

### 14. Models check — all present
```bash
grpcurl -plaintext -import-path plugin/grpc/protocol -proto domain.proto \
  -d '{"method":"POST","path":"/models/check"}' \
  localhost:9876 protocol.DomainPluginService/HandleHTTP
# Expect: all_present=true, each file with size_bytes
```

### 15. Generate an image via ExecuteJob
```bash
# payload: {"prompt":"a cat sitting on a mountain","steps":20,"seed":42}
grpcurl -plaintext -import-path plugin/grpc/protocol -proto domain.proto \
  -d '{"handler_name":"imagegen.generate","job_id":"test-cat","payload":"eyJwcm9tcHQiOiJhIGNhdCBzaXR0aW5nIG9uIGEgbW91bnRhaW4iLCJzdGVwcyI6MjAsInNlZWQiOjQyfQ=="}' \
  localhost:9876 protocol.DomainPluginService/ExecuteJob
# Expect: success=true, result JSON with filename, sha256, duration_ms
# Takes 30-120s on CPU
# File at ~/.qntx/imagegen/output/test-cat.png
open ~/.qntx/imagegen/output/test-cat.png
```

### 16. Generate via HTTP endpoint
```bash
grpcurl -plaintext -import-path plugin/grpc/protocol -proto domain.proto \
  -d '{"method":"POST","path":"/generate","body":"eyJwcm9tcHQiOiJhIHN1bnNldCBvdmVyIHRoZSBvY2VhbiIsInN0ZXBzIjoxMCwic2VlZCI6MTIzfQ=="}' \
  localhost:9876 protocol.DomainPluginService/HandleHTTP
# Expect: status_code=200, JSON with filename and sha256
```

### 17. Serve generated image
```bash
# Use the filename from step 15
grpcurl -plaintext -import-path plugin/grpc/protocol -proto domain.proto \
  -d '{"method":"GET","path":"/image/test-cat.png"}' \
  localhost:9876 protocol.DomainPluginService/HandleHTTP
# Expect: status_code=200, Content-Type=image/png
```

### 18. Path traversal rejected
```bash
grpcurl -plaintext -import-path plugin/grpc/protocol -proto domain.proto \
  -d '{"method":"GET","path":"/image/../../../etc/passwd"}' \
  localhost:9876 protocol.DomainPluginService/HandleHTTP
# Expect: 400 "Invalid filename"
```

### 19. Deterministic output (same seed = same image)
```bash
# Run step 15 twice with identical payload
# Compare sha256 in both responses — should match
```

### 20. Shutdown
```bash
grpcurl -plaintext -import-path plugin/grpc/protocol -proto domain.proto \
  localhost:9876 protocol.DomainPluginService/Shutdown
# Expect: logs "Shutting down imagegen plugin"
# Health now returns healthy=false
```
