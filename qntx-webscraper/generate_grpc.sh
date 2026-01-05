#!/bin/bash
# Generate Python gRPC stubs from QNTX proto files

set -e

PROTO_DIR="../plugin/grpc/protocol"
OUT_DIR="qntx_webscraper/grpc"

# Ensure output directory exists
mkdir -p "$OUT_DIR"

# Generate Python stubs
python -m grpc_tools.protoc \
    -I"$PROTO_DIR" \
    --python_out="$OUT_DIR" \
    --grpc_python_out="$OUT_DIR" \
    "$PROTO_DIR/atsstore.proto" \
    "$PROTO_DIR/domain.proto" \
    "$PROTO_DIR/queue.proto"

# Fix imports in generated files (protoc generates absolute imports)
# Use sed -i '' for macOS compatibility
sed -i '' 's/^import atsstore_pb2/from . import atsstore_pb2/' "$OUT_DIR"/*_pb2_grpc.py
sed -i '' 's/^import domain_pb2/from . import domain_pb2/' "$OUT_DIR"/*_pb2_grpc.py
sed -i '' 's/^import queue_pb2/from . import queue_pb2/' "$OUT_DIR"/*_pb2_grpc.py

echo "Generated Python gRPC stubs in $OUT_DIR"
