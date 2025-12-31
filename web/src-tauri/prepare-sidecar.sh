#!/bin/bash
set -e

# Prepare QNTX binary as Tauri sidecar
# This script builds qntx and places it in the correct location for bundling

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
PROJECT_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
SIDECAR_DIR="$SCRIPT_DIR/bin"

# Detect architecture
ARCH=$(uname -m)
case "$ARCH" in
    arm64)
        TARGET="aarch64-apple-darwin"
        ;;
    x86_64)
        TARGET="x86_64-apple-darwin"
        ;;
    *)
        echo "❌ Unsupported architecture: $ARCH"
        exit 1
        ;;
esac

SIDECAR_NAME="qntx-$TARGET"

echo "Building QNTX for Tauri sidecar..."
echo "  Architecture: $ARCH"
echo "  Target: $TARGET"

# Build qntx binary
cd "$PROJECT_ROOT"
echo ""
echo "Building qntx..."
go build -ldflags="-s -w" -o "$PROJECT_ROOT/bin/qntx" ./cmd/qntx

# Create sidecar directory
mkdir -p "$SIDECAR_DIR"

# Copy to sidecar location with correct name
cp "$PROJECT_ROOT/bin/qntx" "$SIDECAR_DIR/$SIDECAR_NAME"
chmod +x "$SIDECAR_DIR/$SIDECAR_NAME"

echo ""
echo "✅ Sidecar prepared: $SIDECAR_DIR/$SIDECAR_NAME"
echo ""
echo "File size: $(du -h "$SIDECAR_DIR/$SIDECAR_NAME" | cut -f1)"
echo ""
echo "Ready for Tauri build!"
