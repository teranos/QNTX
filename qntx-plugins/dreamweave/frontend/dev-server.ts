#!/usr/bin/env bun
/**
 * Dreamweave dev server
 *
 * Builds the frontend, serves from dist/, proxies /api/ to the
 * dreamweave plugin, watches for source changes with live reload.
 */

import { watch } from 'fs'
import { exec } from 'child_process'
import { promisify } from 'util'
import { join } from 'path'
import http2 from 'http2'

const execAsync = promisify(exec)

const DREAMWEAVE_PORT = process.env.DREAMWEAVE_PORT || '38701'
const DEV_PORT = 5177
const distDir = join(import.meta.dir, 'dist')

let isBuilding = false
let buildTimeout: ReturnType<typeof setTimeout> | null = null
const clients: Set<{ write: (data: string) => void }> = new Set()

// Build the frontend
async function build() {
  if (isBuilding) return
  isBuilding = true
  console.log('Building...')
  try {
    await execAsync('bun run build.ts', { cwd: import.meta.dir })
    console.log('Build complete')
    // Tell connected browsers to reload
    const msg = 'data: reload\n\n'
    clients.forEach(c => c.write(msg))
  } catch (e: any) {
    console.error('Build failed:', e.stderr || e.message)
  } finally {
    isBuilding = false
  }
}

// Watch src/ for changes, rebuild with debounce
watch(join(import.meta.dir, 'src'), { recursive: true }, (_event, filename) => {
  if (!filename) return
  console.log(`Changed: ${filename}`)
  if (buildTimeout) clearTimeout(buildTimeout)
  buildTimeout = setTimeout(build, 300)
})

// Also watch index.html
watch(join(import.meta.dir, 'index.html'), () => {
  if (buildTimeout) clearTimeout(buildTimeout)
  buildTimeout = setTimeout(build, 300)
})

// h2c proxy helper — connects via HTTP/2 cleartext to the plugin
function h2cRequest(path: string): Promise<string> {
  return new Promise((resolve, reject) => {
    const client = http2.connect(`http://127.0.0.1:${DREAMWEAVE_PORT}`)
    client.on('error', (err) => {
      client.close()
      reject(err)
    })
    const req = client.request({ ':method': 'GET', ':path': path })
    const chunks: Buffer[] = []
    req.on('data', (chunk: Buffer) => chunks.push(chunk))
    req.on('end', () => {
      client.close()
      resolve(Buffer.concat(chunks).toString())
    })
    req.on('error', (err) => {
      client.close()
      reject(err)
    })
    req.end()
  })
}

// Initial build
await build()

// Start server
const server = Bun.serve({
  port: DEV_PORT,
  async fetch(req) {
    const url = new URL(req.url)

    // SSE endpoint for live reload
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

    // Proxy /api/ to dreamweave plugin (h2c — plugin speaks HTTP/2 only)
    if (url.pathname.startsWith('/api/')) {
      try {
        const body = await h2cRequest(url.pathname + url.search)
        return new Response(body, {
          headers: { 'content-type': 'application/json' },
        })
      } catch {
        return new Response('dreamweave plugin unavailable', { status: 503 })
      }
    }

    // Serve index.html at root (inject live reload script)
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

    // Serve static files from dist/
    const file = Bun.file(join(distDir, url.pathname))
    if (await file.exists()) return new Response(file)

    // SPA fallback
    return new Response(Bun.file(join(distDir, 'index.html')), {
      headers: { 'Content-Type': 'text/html' },
    })
  },
})

console.log(`dreamweave dev: http://localhost:${server.port}`)
console.log(`proxying /api/ -> http://127.0.0.1:${DREAMWEAVE_PORT}`)
