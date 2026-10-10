#!/usr/bin/env bash
# The app as the phone runs it — QNTX-App's shell, Apple's web view, not Safari —
# booted in the iPhone Simulator. A 12px box is tapped with a real finger (idb),
# and the screen says whether the page zoomed: green it did not, red it did.
#
#   "you tap a box to type, and the only thing that happens is the keyboard coming up."
#
# Run from the QNTX-App checkout, with QNTX beside it at qntx/.
#   bash qntx/ci/app-keyboard/run.sh <name> "<viewport content>"
set -euo pipefail

NAME="$1"
VIEWPORT="$2"
OUT="${OUT:-keyboard-shots}"
mkdir -p "$OUT"

# The page the app carries, with the viewport line under test.
DIST=qntx/internal/server/dist
rm -rf "$DIST"
mkdir -p "$DIST"
python3 - "$VIEWPORT" "$DIST/index.html" <<'EOF'
import sys
viewport, out = sys.argv[1], sys.argv[2]
page = open('qntx/ci/app-keyboard/page.html').read()
line = '<meta name="viewport" content="%s">' % viewport
open(out, 'w').write(page.replace('<!-- VIEWPORT -->', line))
print('viewport:', line)
EOF

# The page is compiled into the app, so each line under test is its own build.
cargo tauri ios build --target aarch64-sim --no-sign --debug
APP=$(find gen/apple/build -name '*.app' -path '*arm64-sim*' -maxdepth 4 | head -1)
test -n "$APP"
echo "app: $APP"

xcrun simctl terminate "$UDID" nl.sbvh.qntx || true
xcrun simctl uninstall "$UDID" nl.sbvh.qntx || true
xcrun simctl install "$UDID" "$APP"
xcrun simctl launch "$UDID" nl.sbvh.qntx
sleep 8
xcrun simctl io "$UDID" screenshot "$OUT/$NAME-0-before.png"

# The box sits at 55% of the screen, centred: tap it, in points.
read -r W H < <(idb describe --udid "$UDID" --json | python3 -c 'import json,sys; d=json.load(sys.stdin)["screen_dimensions"]; print(d["width_points"], d["height_points"])')
idb ui tap --udid "$UDID" $((W / 2)) $((H * 55 / 100 + 22))
sleep 4
xcrun simctl io "$UDID" screenshot "$OUT/$NAME-1-tapped.png"

# What the page says, read off the screen: a pixel near the top of what is shown.
python3 - "$OUT/$NAME-1-tapped.png" "$NAME" <<'EOF'
import sys
from PIL import Image
img = Image.open(sys.argv[1]).convert('RGB')
r, g, b = img.getpixel((img.width // 2, img.height // 5))
verdict = 'zoomed' if r > 150 and g < 100 else ('not zoomed' if g > 150 and r < 100 else 'unreadable')
print('%s: pixel %s -> %s' % (sys.argv[2], (r, g, b), verdict))
open('verdicts.txt', 'a').write('%s: %s\n' % (sys.argv[2], verdict))
EOF
