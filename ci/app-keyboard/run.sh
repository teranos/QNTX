#!/usr/bin/env bash
# The app as the phone runs it — QNTX-App's shell, Apple's web view, not Safari —
# booted in the iPhone Simulator. A 12px box is tapped with a real finger (idb),
# and a square on the screen says whether the page zoomed: green it did not,
# anything else it did.
#
#   "you tap a box to type, and the only thing that happens is the keyboard coming up."
#
# Run from the QNTX-App checkout, with QNTX beside it at qntx/ and the page the
# app is to carry already in qntx/internal/server/dist.
#   bash qntx/ci/app-keyboard/run.sh <name>
set -euo pipefail

NAME="$1"
OUT="${OUT:-keyboard-shots}"
DIST=qntx/internal/server/dist
mkdir -p "$OUT"

# The box and the square go into whatever page the app carries.
python3 - "$DIST/index.html" <<'EOF'
import sys
path = sys.argv[1]
page = open(path).read()
probe = open('qntx/ci/app-keyboard/probe.html').read()
at = page.rindex('</body>')
open(path, 'w').write(page[:at] + probe + page[at:])
s = page.index('name="viewport"')
print('viewport:', page[page.rindex('<', 0, s):page.index('>', s) + 1])
EOF

# The page is compiled into the app, so each page under test is its own build.
cargo tauri ios build --target aarch64-sim --no-sign --debug
APP=$(find gen/apple/build -maxdepth 4 -name '*.app' -path '*arm64-sim*' | head -1)
test -n "$APP"
echo "app: $APP"

xcrun simctl terminate "$UDID" nl.sbvh.qntx || true
xcrun simctl uninstall "$UDID" nl.sbvh.qntx || true
xcrun simctl install "$UDID" "$APP"
xcrun simctl launch "$UDID" nl.sbvh.qntx
sleep 10
xcrun simctl io "$UDID" screenshot "$OUT/$NAME-0-before.png"

# The box sits at 30% of the screen, centred, 44pt tall: tap its middle, in points.
read -r W H < <(idb describe --udid "$UDID" --json | python3 -c 'import json,sys; d=json.load(sys.stdin)["screen_dimensions"]; print(d["width_points"], d["height_points"])')
idb ui tap --udid "$UDID" $((W / 2)) $((H * 30 / 100 + 22))
sleep 4
xcrun simctl io "$UDID" screenshot "$OUT/$NAME-1-tapped.png"

# The square, read off the screen, before and after the tap.
python3 - "$OUT/$NAME-0-before.png" "$OUT/$NAME-1-tapped.png" "$NAME" <<'EOF'
import sys
from PIL import Image
def at(path):
    img = Image.open(path).convert('RGB')
    return img.getpixel((img.width // 2, img.height * 12 // 100))
def green(p):
    return p[1] > 150 and p[0] < 100
before, after, name = at(sys.argv[1]), at(sys.argv[2]), sys.argv[3]
if not green(before):
    verdict = 'unreadable'
elif green(after):
    verdict = 'stayed'
else:
    verdict = 'zoomed'
print('%s: before %s, after %s -> %s' % (name, before, after, verdict))
open('verdicts.txt', 'a').write('%s: %s\n' % (name, verdict))
EOF
