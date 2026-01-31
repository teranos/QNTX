#!/usr/bin/env node

/**
 * Node.js server that runs the SAME WASM module as the browser!
 *
 * This demonstrates true "write once, run anywhere":
 * - Browser: Uses wasm-bindgen JavaScript API
 * - Server: Uses WASI system interface
 * - Same Rust code, same verification logic
 */

const { readFile } = require('fs/promises');
const { WASI } = require('wasi');
const { argv, env } = require('process');
const http = require('http');

// Load and instantiate WASM module
async function loadWasmModule() {
    const wasm = await WebAssembly.compile(
        await readFile('../../target/wasm32-wasip1/release/qntx-wasi.wasm')
    );

    return wasm;
}

// Run WASI module with input
async function runWasi(wasmModule, input) {
    // Create WASI instance with minimal permissions
    const wasi = new WASI({
        args: argv,
        env,
        preopens: {} // No filesystem access needed
    });

    // Instantiate with WASI imports
    const instance = await WebAssembly.instantiate(wasmModule, {
        wasi_snapshot_preview1: wasi.wasiImport
    });

    // Capture output
    let output = '';
    const originalWrite = process.stdout.write;
    process.stdout.write = (chunk) => {
        output += chunk;
        return true;
    };

    // Provide input via stdin
    const originalRead = process.stdin.read;
    let inputProvided = false;
    process.stdin.read = () => {
        if (!inputProvided) {
            inputProvided = true;
            return Buffer.from(input);
        }
        return null;
    };

    // Run the WASI module
    try {
        wasi.start(instance);
    } finally {
        // Restore original functions
        process.stdout.write = originalWrite;
        process.stdin.read = originalRead;
    }

    return output;
}

// Create HTTP server
async function createServer() {
    const wasmModule = await loadWasmModule();
    console.log('✓ WASM module loaded (241KB)');

    const server = http.createServer(async (req, res) => {
        // Enable CORS
        res.setHeader('Access-Control-Allow-Origin', '*');
        res.setHeader('Access-Control-Allow-Methods', 'POST, OPTIONS');
        res.setHeader('Access-Control-Allow-Headers', 'Content-Type');

        if (req.method === 'OPTIONS') {
            res.writeHead(200);
            res.end();
            return;
        }

        if (req.method === 'POST' && req.url === '/verify') {
            let body = '';
            req.on('data', chunk => body += chunk);
            req.on('end', async () => {
                try {
                    const input = JSON.parse(body);

                    // Create WASI command
                    const wasiCommand = {
                        cmd: 'Verify',
                        attestation: input.attestation,
                        subject: input.subject,
                        predicate: input.predicate,
                        context: input.context,
                        actor: input.actor
                    };

                    // Run WASI module
                    const output = await runWasi(
                        wasmModule,
                        JSON.stringify(wasiCommand)
                    );

                    const result = JSON.parse(output);

                    res.writeHead(200, { 'Content-Type': 'application/json' });
                    res.end(JSON.stringify(result));
                } catch (error) {
                    res.writeHead(500, { 'Content-Type': 'application/json' });
                    res.end(JSON.stringify({
                        status: 'Error',
                        message: error.message
                    }));
                }
            });
        } else if (req.method === 'POST' && req.url === '/filter') {
            let body = '';
            req.on('data', chunk => body += chunk);
            req.on('end', async () => {
                try {
                    const input = JSON.parse(body);

                    // Create WASI command
                    const wasiCommand = {
                        cmd: 'Filter',
                        attestations: input.attestations,
                        subject: input.subject,
                        predicate: input.predicate,
                        context: input.context,
                        actor: input.actor
                    };

                    // Run WASI module
                    const output = await runWasi(
                        wasmModule,
                        JSON.stringify(wasiCommand)
                    );

                    const result = JSON.parse(output);

                    res.writeHead(200, { 'Content-Type': 'application/json' });
                    res.end(JSON.stringify(result));
                } catch (error) {
                    res.writeHead(500, { 'Content-Type': 'application/json' });
                    res.end(JSON.stringify({
                        status: 'Error',
                        message: error.message
                    }));
                }
            });
        } else {
            res.writeHead(200, { 'Content-Type': 'text/html' });
            res.end(`
                <h1>QNTX WASI Server</h1>
                <p>Running WASM attestation verification on Node.js!</p>
                <p>Same binary runs in:</p>
                <ul>
                    <li>✅ Browsers (105KB)</li>
                    <li>✅ Node.js via WASI (241KB)</li>
                    <li>✅ Wasmtime</li>
                    <li>✅ Cloudflare Workers</li>
                    <li>✅ Smart contracts</li>
                </ul>
                <h2>API Endpoints:</h2>
                <ul>
                    <li>POST /verify - Verify single attestation</li>
                    <li>POST /filter - Filter multiple attestations</li>
                </ul>
            `);
        }
    });

    const PORT = 3000;
    server.listen(PORT, () => {
        console.log(`
🚀 QNTX WASI Server running on http://localhost:${PORT}

The SAME WASM module is now running on:
- This Node.js server (via WASI)
- Browser demo at http://localhost:8000/demo/ (via wasm-bindgen)

Try:
curl -X POST http://localhost:${PORT}/verify \\
  -H "Content-Type: application/json" \\
  -d '{
    "attestation": {
      "id": "test-001",
      "subjects": ["user:alice"],
      "predicates": ["created"],
      "contexts": ["dev"],
      "actors": ["system"],
      "timestamp": 1704067200,
      "source": "api",
      "attributes": {}
    },
    "subject": "alice"
  }'
        `);
    });
}

// Start server
createServer().catch(console.error);