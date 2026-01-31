#!/bin/bash
# Test WASI module with wasmtime

echo "Testing QNTX WASI module..."
echo ""

# Path to WASI module
WASM_PATH="../../target/wasm32-wasip1/release/qntx-wasi.wasm"

# Test 1: Verify a single attestation
echo "Test 1: Verify attestation"
echo '
{
  "cmd": "Verify",
  "attestation": {
    "id": "test-001",
    "subjects": ["user:alice", "project:qntx"],
    "predicates": ["created", "owns"],
    "contexts": ["dev", "test"],
    "actors": ["system", "admin"],
    "timestamp": 1704067200,
    "source": "wasi-test",
    "attributes": {}
  },
  "subject": "alice",
  "predicate": "created"
}
' | ~/.wasmtime/bin/wasmtime $WASM_PATH

echo ""
echo "Test 2: Filter multiple attestations"
echo '
{
  "cmd": "Filter",
  "attestations": [
    {
      "id": "att-1",
      "subjects": ["user:alice", "project:qntx"],
      "predicates": ["created"],
      "contexts": ["production"],
      "actors": ["system"],
      "timestamp": 1704067200,
      "source": "api",
      "attributes": {}
    },
    {
      "id": "att-2",
      "subjects": ["user:bob", "project:demo"],
      "predicates": ["modified"],
      "contexts": ["staging"],
      "actors": ["admin"],
      "timestamp": 1704067300,
      "source": "cli",
      "attributes": {}
    },
    {
      "id": "att-3",
      "subjects": ["user:alice", "file:readme.md"],
      "predicates": ["edited"],
      "contexts": ["production"],
      "actors": ["user:alice"],
      "timestamp": 1704067400,
      "source": "web",
      "attributes": {}
    }
  ],
  "subject": "alice",
  "context": "production"
}
' | ~/.wasmtime/bin/wasmtime $WASM_PATH

echo ""
echo "Done! WASI module tested successfully."