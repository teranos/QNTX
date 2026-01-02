#!/bin/bash
set -e

# Convert qntx.jpg to app icons for Tauri

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
SOURCE_IMG="$SCRIPT_DIR/../qntx.jpg"
ICONS_DIR="$SCRIPT_DIR/icons"

if [ ! -f "$SOURCE_IMG" ]; then
    echo "❌ Source image not found: $SOURCE_IMG"
    exit 1
fi

mkdir -p "$ICONS_DIR"

# Check if icons are already up-to-date
if [ -f "$ICONS_DIR/icon.icns" ] && [ "$ICONS_DIR/icon.icns" -nt "$SOURCE_IMG" ]; then
    echo "✓ Icons already up-to-date (newer than qntx.jpg)"
    exit 0
fi

echo "Generating QNTX app icons from qntx.jpg..."

# Check for image conversion tool
if command -v magick &> /dev/null; then
    CONVERT="magick"
elif command -v convert &> /dev/null; then
    CONVERT="convert"
else
    echo "❌ ImageMagick not found. Install with: brew install imagemagick"
    exit 1
fi

# Generate PNG icons for different sizes (force RGBA format)
for size in 32 128 256 512; do
    $CONVERT "$SOURCE_IMG" -resize ${size}x${size} -define png:color-type=6 "$ICONS_DIR/${size}x${size}.png"
    echo "  ✓ Created ${size}x${size}.png"
done

# Create @2x version
cp "$ICONS_DIR/256x256.png" "$ICONS_DIR/128x128@2x.png"
echo "  ✓ Created 128x128@2x.png"

# Create icon.png (for tray icon)
cp "$ICONS_DIR/128x128.png" "$ICONS_DIR/icon.png"
echo "  ✓ Created icon.png"

# Create .icns for macOS (requires iconutil)
if command -v iconutil &> /dev/null; then
    ICONSET="$ICONS_DIR/icon.iconset"
    mkdir -p "$ICONSET"

    # iconutil requires specific naming
    cp "$ICONS_DIR/32x32.png" "$ICONSET/icon_32x32.png"
    cp "$ICONS_DIR/128x128.png" "$ICONSET/icon_128x128.png"
    cp "$ICONS_DIR/256x256.png" "$ICONSET/icon_256x256.png"
    cp "$ICONS_DIR/512x512.png" "$ICONSET/icon_512x512.png"

    iconutil -c icns "$ICONSET" -o "$ICONS_DIR/icon.icns"
    rm -rf "$ICONSET"
    echo "  ✓ Created icon.icns (macOS)"
else
    echo "  ⚠️  iconutil not found (macOS only)"
fi

# Create .ico for Windows
$CONVERT "$ICONS_DIR/256x256.png" "$ICONS_DIR/128x128.png" "$ICONS_DIR/32x32.png" "$ICONS_DIR/icon.ico"
echo "  ✓ Created icon.ico (Windows)"

echo ""
echo "✅ All icons generated in $ICONS_DIR"
