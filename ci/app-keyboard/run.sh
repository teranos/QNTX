#!/usr/bin/env bash
# The app as the phone runs it — QNTX-App's shell, Apple's web view, not Safari —
# booted in the iPhone Simulator. A 12px box is tapped with a real finger (idb),
# and two squares on the screen say what happened: whether the box took the
# keyboard, and whether the page zoomed.
#
#   "you tap a box to type, and the only thing that happens is the keyboard coming up."
#
# Run from the QNTX-App checkout, with QNTX beside it at qntx/ and the page the
# app is to carry already in qntx/internal/server/dist.
#   bash qntx/ci/app-keyboard/run.sh <name>
set -euxo pipefail

NAME="$1"
OUT="${OUT:-keyboard-shots}"
DIST=qntx/internal/server/dist
mkdir -p "$OUT"

# The box and the squares go into whatever page the app carries.
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

# The page is compiled into the app, so each page under test is its own build,
# from a clean output: the last build's app is in the way of the next.
rm -rf gen/apple/build
cargo tauri ios build --target aarch64-sim --no-sign --debug
APP=$(find gen/apple/build -maxdepth 4 -name '*.app' -path '*arm64-sim*' | head -1)
test -n "$APP"
echo "app: $APP"

xcrun simctl terminate "$UDID" nl.sbvh.qntx || true
xcrun simctl uninstall "$UDID" nl.sbvh.qntx || true
xcrun simctl install "$UDID" "$APP"
xcrun simctl launch "$UDID" nl.sbvh.qntx

# What the squares say, read off a screenshot.
squares() {
    xcrun simctl io "$UDID" screenshot "$1" > /dev/null
    python3 - "$1" <<'EOF'
import sys
from PIL import Image
img = Image.open(sys.argv[1]).convert('RGB')
y = img.height * 15 // 100
def name(p):
    r, g, b = p
    if b > 200 and r < 100: return 'blue'
    if g > 150 and r < 100 and b < 150: return 'green'
    if r > 150 and g < 100 and b < 100: return 'red'
    if abs(r - g) < 20 and abs(g - b) < 20 and 100 < r < 160: return 'grey'
    return 'other%s' % (p,)
print(name(img.getpixel((img.width * 30 // 100, y))), name(img.getpixel((img.width * 70 // 100, y))))
EOF
}

# The page is loaded once the squares stand: grey (no keyboard yet), green (its own scale).
for i in $(seq 1 60); do
    seen=$(squares "$OUT/$NAME-0-before.png")
    [ "$seen" = "grey green" ] && break
    sleep 1
done
echo "before: $seen"
if [ "$seen" != "grey green" ]; then
    echo "$NAME: page never showed" >> verdicts.txt
    exit 1
fi

# The box, where the screen shows it: the white run down the middle of the
# screenshot, turned from pixels into the points a finger is placed in.
read -r X Y < <(python3 - "$OUT/$NAME-0-before.png" "$(idb describe --udid "$UDID" --json)" <<'EOF2'
import json, sys
from PIL import Image
img = Image.open(sys.argv[1]).convert('RGB')
points = json.loads(sys.argv[2])["screen_dimensions"]
scale = img.width / points["width_points"]
x = img.width // 2
run = []
for y in range(img.height // 5, img.height * 3 // 4):
    r, g, b = img.getpixel((x, y))
    if r > 235 and g > 235 and b > 235:
        run.append(y)
    elif len(run) > 40:
        break
    else:
        run = []
if len(run) <= 40:
    sys.exit('no box on the screen')
print(int(x / scale), int((run[0] + run[-1]) / 2 / scale))
EOF2
)
echo "the box is at $X,$Y"
idb ui tap --udid "$UDID" "$X" "$Y"
sleep 4
xcrun simctl io "$UDID" screenshot "$OUT/$NAME-1-tapped.png" > /dev/null

# Where the squares stand, before and after: the left one's colour says whether
# the box took the keyboard, the right one's whether the page zoomed, and the
# row they start on whether anything moved.
verdict=$(python3 - "$OUT/$NAME-0-before.png" "$OUT/$NAME-1-tapped.png" <<'EOF2'
import sys
from PIL import Image
def look(path):
    img = Image.open(path).convert('RGB')
    left = img.width * 30 // 100
    top, focused, red = None, False, False
    for y in range(0, img.height // 2):
        r, g, b = img.getpixel((left, y))
        grey = abs(r - 128) < 12 and abs(g - 128) < 12 and abs(b - 128) < 12
        blue = b > 200 and r < 100
        if top is None and (grey or blue):
            top = y
        if blue:
            focused = True
        for x in range(0, img.width, 8):
            r, g, b = img.getpixel((x, y))
            if r > 190 and g < 30 and b < 30:
                red = True
                break
    return top, focused, red
before, after = look(sys.argv[1]), look(sys.argv[2])
print('before top %s, after top %s, focused %s, red %s' % (before[0], after[0], after[1], after[2]), file=sys.stderr)
if after[2]:
    print('zoomed')
elif not after[1]:
    print('box not taken')
elif after[0] is None or before[0] is None or abs(after[0] - before[0]) > 3:
    print('moved')
else:
    print('stayed')
EOF2
)
echo "$NAME: $verdict"
echo "$NAME: $verdict" >> verdicts.txt
