#!/usr/bin/env bun
/**
 * Development server with live reload for frontend development
 * Proxies API calls to the Go backend and serves frontend with hot reload
 */

import { watch } from "fs";
import { exec } from "child_process";
import { promisify } from "util";

const execAsync = promisify(exec);

// Configuration
// TODO(#272): Support configurable dev ports via env vars or am.toml for multi-variant testing
const BACKEND_URL = "http://localhost:877";  // Go backend
const DEV_PORT_START = 8820;  // Preferred development server port
const DEV_PORT_MAX = 8830;     // Maximum port to try
const BUILD_DEBOUNCE = 300; // ms to wait before rebuilding

let buildTimeout: NodeJS.Timeout | null = null;
let isBuilding = false;
let clients: Set<any> = new Set();

// Light pink color palette
const lightPink = "\x1b[38;5;225m";
const pink = "\x1b[38;5;218m";
const darkPink = "\x1b[38;5;211m";
const reset = "\x1b[0m";
const dim = "\x1b[2m";

// Build function
async function build() {
    if (isBuilding) {
        console.log(`${dim}Build already in progress, skipping...${reset}`);
        return;
    }

    isBuilding = true;
    console.log(`${pink}Building...${reset}`);
    try {
        await execAsync("bun run build.ts");
        console.log(`${lightPink}Build complete${reset}`);
        // Notify all connected clients to reload
        broadcastReload();
    } catch (error) {
        console.error(`${darkPink}Build failed:${reset}`, error);
    } finally {
        isBuilding = false;
    }
}

// Broadcast reload to all connected clients
function broadcastReload() {
    const message = "data: reload\n\n";
    clients.forEach(client => {
        client.write(message);
    });
}

// Check if port is available
async function isPortAvailable(port: number): Promise<boolean> {
    try {
        const server = Bun.serve({
            port,
            fetch() {
                return new Response("test");
            }
        });
        server.stop();
        return true;
    } catch {
        return false;
    }
}

// Find next available port
async function findAvailablePort(startPort: number, maxPort: number): Promise<number> {
    for (let port = startPort; port <= maxPort; port++) {
        if (await isPortAvailable(port)) {
            return port;
        }
    }
    throw new Error(`No available ports found between ${startPort} and ${maxPort}`);
}

// Watch for file changes
function setupWatcher() {
    const dirs = ["./ts", "./css", "./ctp2", "./index.html"];

    dirs.forEach(dir => {
        watch(dir, { recursive: true }, (eventType, filename) => {
            if (filename?.endsWith('.ts') || filename?.endsWith('.css') || filename?.endsWith('.html')) {
                console.log(`${pink}Changed: ${filename}${reset}`);

                // Debounce builds
                if (buildTimeout) clearTimeout(buildTimeout);
                buildTimeout = setTimeout(build, BUILD_DEBOUNCE);
            }
        });
    });

    console.log(`${dim}Watching for changes in: ${dirs.join(", ")}${reset}`);
}

// Find available port and create development server
async function startServer() {
    // Find available port
    const port = await findAvailablePort(DEV_PORT_START, DEV_PORT_MAX);

    if (port !== DEV_PORT_START) {
        console.log(`${pink}Port ${DEV_PORT_START} in use, using port ${port} instead${reset}`);
    }

    const server = Bun.serve({
        port,

        async fetch(req) {
            const url = new URL(req.url);

            // Server-Sent Events endpoint for live reload
            if (url.pathname === "/__dev_reload__") {
                return new Response(
                    new ReadableStream({
                        start(controller) {
                            const client = {
                                write: (data: string) => controller.enqueue(data)
                            };
                            clients.add(client);

                            // Clean up on disconnect
                            req.signal.addEventListener("abort", () => {
                                clients.delete(client);
                            });
                        }
                    }),
                    {
                        headers: {
                            "Content-Type": "text/event-stream",
                            "Cache-Control": "no-cache",
                            "Connection": "keep-alive",
                        }
                    }
                );
            }

            // Frontend connects directly to backend - no proxying needed

            // Serve static files from dist
            if (url.pathname === "/" || url.pathname === "") {
                const html = await Bun.file("../internal/server/dist/index.html").text();
                // Inject backend URL and live reload script
                const modifiedHtml = html.replace(
                    "</head>",
                    `<script>
                        // Backend URL for WebSocket connections in dev mode
                        window.__BACKEND_URL__ = "${BACKEND_URL}";
                    </script>
                    </head>`
                ).replace(
                    "</body>",
                    `<script>
                        // Live reload for development
                        const evtSource = new EventSource("/__dev_reload__");
                        evtSource.onmessage = (event) => {
                            if (event.data === "reload") {
                                console.log("Reloading...");
                                location.reload();
                            }
                        };
                    </script>
                    </body>`
                );
                return new Response(modifiedHtml, {
                    headers: { "Content-Type": "text/html" }
                });
            }

            // Serve other static files
            const filePath = "../internal/server/dist" + url.pathname;
            const file = Bun.file(filePath);

            if (await file.exists()) {
                return new Response(file);
            }

            return new Response("Not Found", { status: 404 });
        }
    });

    console.log(`
${lightPink}Development server running at http://localhost:${port}${reset}
${dim}Backend URL: ${BACKEND_URL}${reset}
${dim}Live reload enabled${reset}

${dim}Make sure your Go backend is running on port ${BACKEND_URL.split(":")[2]}${reset}
`);
}

// Initial build
await build();

// Setup file watcher
setupWatcher();

// Start server
await startServer();