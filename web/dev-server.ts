#!/usr/bin/env bun
/**
 * Development server with live reload for frontend development
 * Proxies API calls to the Go backend and serves frontend with hot reload
 */

import { watch } from "fs";
import { exec } from "child_process";
import { promisify } from "util";
import { parse as parseToml } from "smol-toml";
import { readFileSync, existsSync } from "fs";
import { join } from "path";

const execAsync = promisify(exec);

// Read am.toml configuration
interface AmConfig {
    Server?: {
        port?: number;
        frontend_port?: number;
    };
}

function readAmConfig(): AmConfig {
    const configPath = join("..", "am.toml");
    if (existsSync(configPath)) {
        try {
            const tomlContent = readFileSync(configPath, "utf-8");
            return parseToml(tomlContent) as AmConfig;
        } catch (error: unknown) {
            console.error(`Failed to parse am.toml: ${error}`);
        }
    }
    return {};
}

const config = readAmConfig();

// Configuration
// Port precedence: ENV > am.toml > defaults (issue #272)
const BACKEND_PORT = parseInt(
    process.env.BACKEND_PORT ||
    process.env.QNTX_SERVER_PORT ||
    String(config.Server?.port || 877),
    10
);
const BACKEND_URL = `http://localhost:${BACKEND_PORT}`;  // Go backend
const DEV_PORT_START = parseInt(
    process.env.FRONTEND_PORT ||
    String(config.Server?.frontend_port || 8820),
    10
);  // Preferred development server port
const DEV_PORT_MAX = DEV_PORT_START + 10;     // Try up to 10 ports above start
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
    } catch (error: unknown) {
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
    const dirs = ["./ts", "./css", "./index.html"];

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

            const absolutePath = join(import.meta.dir, filePath);
            console.error(`${darkPink}404: File not found${reset}`);
            console.error(`${dim}  URL: ${url.pathname}${reset}`);
            console.error(`${dim}  Path: ${absolutePath}${reset}`);

            return new Response("Not Found", { status: 404 });
        }
    });

    console.log(`
${lightPink}Development server running at http://localhost:${port}${reset}
${dim}Backend URL: ${BACKEND_URL} (port ${BACKEND_PORT})${reset}
${dim}Live reload enabled${reset}

${dim}Port config: ENV vars > am.toml > defaults (BACKEND_PORT=${BACKEND_PORT}, FRONTEND_PORT=${port})${reset}
`);
}

// Initial build
await build();

// Setup file watcher
setupWatcher();

// Start server
await startServer();