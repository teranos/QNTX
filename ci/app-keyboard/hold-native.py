# The shell holds the page where it is: whatever scrolls the web view's own
# scroll view, it is put back at the top. Patched into QNTX-App's Sheet.swift.
import sys
path = sys.argv[1]
s = open(path).read()
field = 'private let anchor = Anchor()\n'
lock = 'scroll.bouncesZoom = false\n'
assert field in s and lock in s
s = s.replace(field, field + '  private var held: NSKeyValueObservation?\n')
    held = scroll.observe(\\.contentOffset, options: [.new]) { view, _ in
      if view.contentOffset != .zero { view.contentOffset = .zero }
    }
''')
open(path, 'w').write(s)
