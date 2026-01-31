#!/bin/bash
# Simple HTTP server to test WASM module

echo "Starting QNTX WASM demo server..."
echo "Open http://localhost:8000/demo/ in your browser"
echo ""
cd "$(dirname "$0")"
python3 -m http.server 8000