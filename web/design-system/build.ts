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

// Copy tokens.css from web/css/
const tokensSource = resolve(import.meta.dir, '../css/tokens.css')
await copyFile(tokensSource, join(outDir, 'tokens.css'))

// Copy index.html, rewrite script src
const html = await Bun.file(join(import.meta.dir, 'index.html')).text()
await Bun.write(join(outDir, 'index.html'), html.replace('/src/main.ts', '/main.js'))

console.log('design-system built -> dist/')
