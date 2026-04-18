/**
 * Design system token viewer — builder
 *
 * Bundles src/main.ts into dist/, copies tokens.css and index.html.
 */

import { join, resolve } from 'path'
import { rm, mkdir, copyFile } from 'fs/promises'

const srcDir = join(import.meta.dir, 'src')
const outDir = join(import.meta.dir, 'dist')

await rm(outDir, { recursive: true, force: true }).catch(() => {})
await mkdir(outDir, { recursive: true })

const result = await Bun.build({
  entrypoints: [join(srcDir, 'main.ts')],
  outdir: outDir,
  minify: false,
  sourcemap: 'inline',
})

if (!result.success) {
  console.error('Build failed:')
  for (const msg of result.logs) console.error(msg)
  process.exit(1)
}

// Copy CSS from web/css/
const tokensSource = resolve(import.meta.dir, '../css/tokens.css')
await copyFile(tokensSource, join(outDir, 'tokens.css'))
const componentsSource = resolve(import.meta.dir, '../css/components.css')
await copyFile(componentsSource, join(outDir, 'components.css'))
const titleBarSource = resolve(import.meta.dir, '../css/glyph/title-bar.css')
await copyFile(titleBarSource, join(outDir, 'title-bar.css'))
const windowSource = resolve(import.meta.dir, '../css/window.css')
await copyFile(windowSource, join(outDir, 'window.css'))
const canvasPlacedSource = resolve(import.meta.dir, '../css/glyph/states/canvas-placed.css')
await copyFile(canvasPlacedSource, join(outDir, 'canvas-placed.css'))
const canvasCssSource = resolve(import.meta.dir, '../css/canvas.css')
await copyFile(canvasCssSource, join(outDir, 'canvas.css'))
const dotSource = resolve(import.meta.dir, '../css/glyph/states/dot.css')
await copyFile(dotSource, join(outDir, 'dot.css'))
const morphSource = resolve(import.meta.dir, '../css/glyph/transitions/morph.css')
await copyFile(morphSource, join(outDir, 'morph.css'))

// Copy index.html, rewrite script src
const html = await Bun.file(join(import.meta.dir, 'index.html')).text()
await Bun.write(join(outDir, 'index.html'), html.replace('/src/main.ts', '/main.js'))

console.log('design-system built -> dist/')
