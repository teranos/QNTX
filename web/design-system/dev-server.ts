#!/usr/bin/env bun
/**
 * Design system dev server
 *
 * Builds, serves from dist/, watches for changes with live reload.
 */

import { watch } from 'fs'
import { exec } from 'child_process'
import { promisify } from 'util'
import { join } from 'path'

const execAsync = promisify(exec)

const DEV_PORT = 5179
const distDir = join(import.meta.dir, 'dist')

let isBuilding = false
let buildTimeout: ReturnType<typeof setTimeout> | null = null
const clients: Set<{ write: (data: string) => void }> = new Set()

async function build() {
  if (isBuilding) return
  isBuilding = true
  console.log('Building...')
  try {
    await execAsync('bun run build.ts', { cwd: import.meta.dir })
    console.log('Build complete')
    const msg = 'data: reload\n\n'
    clients.forEach(c => c.write(msg))
  } catch (e: any) {
    console.error('Build failed:', e.stderr || e.message)
  } finally {
    isBuilding = false
  }
}

watch(join(import.meta.dir, 'src'), { recursive: true }, (_event, filename) => {
  if (!filename) return
  console.log(`Changed: ${filename}`)
  if (buildTimeout) clearTimeout(buildTimeout)
  buildTimeout = setTimeout(build, 300)
})

watch(join(import.meta.dir, 'index.html'), () => {
  if (buildTimeout) clearTimeout(buildTimeout)
  buildTimeout = setTimeout(build, 300)
})

// Also watch tokens.css itself
watch(join(import.meta.dir, '../css/tokens.css'), () => {
  console.log('tokens.css changed')
  if (buildTimeout) clearTimeout(buildTimeout)
  buildTimeout = setTimeout(build, 300)
})

await build()

const server = Bun.serve({
  port: DEV_PORT,
  async fetch(req) {
    const url = new URL(req.url)

    if (url.pathname === '/__dev_reload__') {
      return new Response(
        new ReadableStream({
          start(controller) {
            const client = {
              write: (data: string) => controller.enqueue(new TextEncoder().encode(data)),
            }
            clients.add(client)
            req.signal.addEventListener('abort', () => clients.delete(client))
          },
        }),
        { headers: { 'Content-Type': 'text/event-stream', 'Cache-Control': 'no-cache' } },
      )
    }

    if (url.pathname === '/' || url.pathname === '') {
      let html = await Bun.file(join(distDir, 'index.html')).text()
      html = html.replace(
        '</body>',
        `<script>
          const es = new EventSource("/__dev_reload__");
          es.onmessage = () => location.reload();
        </script></body>`,
      )
      return new Response(html, { headers: { 'Content-Type': 'text/html' } })
    }

    const file = Bun.file(join(distDir, url.pathname))
    if (await file.exists()) return new Response(file)

    return new Response(Bun.file(join(distDir, 'index.html')), {
      headers: { 'Content-Type': 'text/html' },
    })
  },
})

console.log(`design-system: http://localhost:${server.port}`)
