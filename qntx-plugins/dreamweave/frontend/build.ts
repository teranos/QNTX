/**
 * Dreamweave frontend builder
 *
 * Compiles Svelte 5 components via Bun.build() plugin,
 * outputs bundled JS + index.html to dist/
 */

import { compile } from 'svelte/compiler'
import { join, resolve } from 'path'
import { rm, mkdir, copyFile } from 'fs/promises'

const srcDir = join(import.meta.dir, 'src')
const outDir = join(import.meta.dir, 'dist')

// Bun bundler plugin: compile .svelte → JS on load
const sveltePlugin: import('bun').BunPlugin = {
  name: 'svelte',
  setup(build) {
    build.onLoad({ filter: /\.svelte$/ }, async (args) => {
      const source = await Bun.file(args.path).text()
      const result = compile(source, {
        filename: args.path,
        generate: 'client',
      })
      // Inject component CSS via runtime <style> element
      let code = result.js.code
      if (result.css && result.css.code) {
        const escaped = result.css.code
          .replaceAll('\\', '\\\\')
          .replaceAll('`', '\\`')
          .replaceAll('$', '\\$')
        code += `\n;(function(){const s=document.createElement('style');s.textContent=\`${escaped}\`;document.head.appendChild(s)})()\n`
      }
      return { contents: code, loader: 'js' }
    })
  },
}

// Clean + build
await rm(outDir, { recursive: true, force: true }).catch(() => {})
await mkdir(outDir, { recursive: true })

const result = await Bun.build({
  entrypoints: [join(srcDir, 'main.ts')],
  outdir: outDir,
  minify: false,
  sourcemap: 'inline',
  plugins: [sveltePlugin],
})

if (!result.success) {
  console.error('Build failed:')
  for (const msg of result.logs) console.error(msg)
  process.exit(1)
}

// Copy shared design tokens from main web frontend
const tokensSource = resolve(import.meta.dir, '../../../web/css/tokens.css')
await copyFile(tokensSource, join(outDir, 'tokens.css'))

// Copy index.html, rewrite script src
const html = await Bun.file(join(import.meta.dir, 'index.html')).text()
await Bun.write(join(outDir, 'index.html'), html.replace('/src/main.ts', '/main.js'))

console.log('dreamweave built -> dist/')
