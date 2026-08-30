/**
 * Does a glyph hide what it holds? (Containment Axioma)
 *
 * Every other test here runs in jsdom, which has no layout engine — scrollWidth
 * and clientWidth are always zero, so a glyph can cut every column off and the
 * suite still goes green. That is why this kept coming back. This asks a real
 * browser, which is the only thing that can answer.
 *
 *   bun tools/no-clipping.ts
 */

const CHROME = '/Applications/Google Chrome.app/Contents/MacOS/Google Chrome';

const SCREENS = [
    { name: 'laptop', width: 1440 },
    { name: 'narrow', width: 900 },
    { name: 'phone', width: 420 },
    // A window dragged against an edge is squeezed far past any screen.
    { name: 'squeezed', width: 240 },
];

// The shape that broke it: ten monospace columns and a sixty-character DID
// with nowhere to break. A harness holding only content that already fits
// proves nothing — it passed with the fix taken out.
const DID = 'did:key:' + 'z'.repeat(48);
const COLUMNS = ['Label', 'For', 'DID', 'Namespace', 'Reads', 'Writes',
    'Created', 'Last used', 'Status', ''];

/** A row carrying everything the access tokens list ever put in one. */
function tokenRow(label: string, namespace: string): string {
    const cells = [
        label,
        'https://example.invalid/@someone',
        DID,
        namespace,
        'everything',
        'everything',
        '2026-01-01 00:00:00',
        '2026-01-01 00:00:00',
        'active',
    ].map(v => `<td style="padding:4px 8px">${v}</td>`).join('');
    return `<tr>${cells}<td style="padding:4px 8px;text-align:right"><button>Revoke</button></td></tr>`;
}

// The Self glyph's shape: rows of a caption and a value that has nowhere to
// break, inside the .glyph-content the older glyphs render into.
function selfRows(): string {
    const rows = [
        ['Node DID', DID],
        ['You', DID],
        ['Commit', '1426a78'],
    ].map(([caption, value]) =>
        `<div class="glyph-row"><span>${caption}:</span><span>${value}</span></div>`).join('');
    return `<div class="glyph-content"><h3>QNTX Server</h3>${rows}</div>`;
}

function harness(css: string, width: number): string {
    return `<!doctype html><html><head><style>${css}</style><style>
  body { margin:0; width:${width}px; font-family:monospace; }
  .glyph-window { position:fixed; left:20px; top:20px; display:flex;
    flex-direction:column; overflow:hidden; width:fit-content;
    max-width:${Math.floor(width * 0.9)}px; }
</style></head><body>
  <div class="glyph-window">
    <div>Access Tokens</div>
    <div class="glyph-content-area" id="content">
      ${selfRows()}
      <div style="display:flex;flex-direction:column;gap:8px;padding:12px">
        <div><button>+</button><button>Refresh</button></div>
        <table class="tokens-table" style="border-collapse:collapse">
          <thead><tr>${COLUMNS.map(c =>
        `<th style="text-align:left;padding:4px 8px">${c}</th>`).join('')}</tr></thead>
          <tbody>
            ${tokenRow('a-token', 'default')}
            ${tokenRow('a-label-far-longer-than-any-column-should-ever-need-to-be', 'TEST1')}
          </tbody>
        </table>
      </div>
    </div>
  </div>
<script>
window.addEventListener('load', function () {
  const area = document.getElementById('content');
  const win = document.querySelector('.glyph-window');
  const bad = [];
  const wr = win.getBoundingClientRect();
  for (const el of [win, area, ...area.querySelectorAll('*')]) {
    const name = el.tagName.toLowerCase() + (el.className ? '.' + el.className : '');
    // Only a box that clips or scrolls can hide what it holds. An element with
    // overflow visible paints its content past its edge, where it is still read.
    const flow = getComputedStyle(el).overflowX;
    const lost = el.scrollWidth - el.clientWidth;
    if (flow !== 'visible' && lost > 1) bad.push(name + ' hides ' + lost + 'px of what it holds');
    const r = el.getBoundingClientRect();
    if (r.width > 0 && (r.left < wr.left - 1 || r.right > wr.right + 1)) {
      bad.push(name + ' is drawn outside the window');
    }
  }
  // Content inside its box can still be unreadable: cells that cannot get
  // narrower render on top of each other, and every box still measures clean.
  const cells = [...area.querySelectorAll('td, th')];
  for (let i = 0; i < cells.length; i++) {
    for (let j = i + 1; j < cells.length; j++) {
      const a = cells[i].getBoundingClientRect();
      const b = cells[j].getBoundingClientRect();
      if (a.width === 0 || b.width === 0) continue;
      const across = a.left < b.right - 1 && b.left < a.right - 1;
      const down = a.top < b.bottom - 1 && b.top < a.bottom - 1;
      if (across && down) {
        bad.push('cells are drawn on top of each other');
        i = cells.length;
        break;
      }
    }
  }

  const out = document.createElement('pre');
  out.id = 'verdict';
  out.textContent = bad.length ? bad.join('\\n') : 'CONTAINED';
  document.body.appendChild(out);
});
</script></body></html>`;
}

// QNTX's stylesheet plus the rules the glyph package injects itself. A glyph
// holds its content because the package says so, so the package's rules are
// what this measures.
const { CONTAINMENT_CSS } = await import('../../packages/glyphs/containment.ts');
const css = await Bun.file(new URL('../css/window.css', import.meta.url)).text()
    + CONTAINMENT_CSS;
let failed = false;

for (const screen of SCREENS) {
    const page = `/tmp/no-clipping-${screen.name}.html`;
    await Bun.write(page, harness(css, screen.width));

    const chrome = Bun.spawnSync([
        CHROME, '--headless', '--disable-gpu', '--no-sandbox',
        `--window-size=${screen.width},900`,
        '--virtual-time-budget=2000', '--dump-dom', `file://${page}`,
    ]);
    const dom = chrome.stdout.toString();

    const open = dom.indexOf('<pre id="verdict">');
    if (open === -1) {
        console.log(`${screen.name}: the page never reported — chrome said: ${chrome.stderr.toString().slice(0, 200)}`);
        failed = true;
        continue;
    }
    const body = dom.slice(dom.indexOf('>', open) + 1);
    const verdict = body.slice(0, body.indexOf('</pre>'));

    if (verdict.trim() === 'CONTAINED') {
        console.log(`${screen.name} ${screen.width}px: contained`);
    } else {
        console.log(`${screen.name} ${screen.width}px: HIDES CONTENT`);
        for (const line of verdict.split('\n')) console.log(`  ${line}`);
        failed = true;
    }
}

process.exit(failed ? 1 : 0);
