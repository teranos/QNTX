#!/usr/bin/env bun
/**
 * TypeScript Plugin Runtime for QNTX
 *
 * Loads a TypeScript plugin and exposes it via gRPC DomainPluginService.
 * Uses Bun as the runtime for fast startup and native TypeScript support.
 */

import * as grpc from '@grpc/grpc-js';
import * as protoLoader from '@grpc/proto-loader';
import { fileURLToPath } from 'url';
import { dirname, join, resolve } from 'path';
import { Server, ServerCredentials } from '@grpc/grpc-js';

const __filename = fileURLToPath(import.meta.url);
const __dirname = dirname(__filename);

// Parse CLI arguments
function parseArgs() {
    const args = process.argv.slice(2);
    let pluginPath: string | null = null;
    let grpcPort = 0; // 0 = auto-allocate

    for (let i = 0; i < args.length; i++) {
        if (args[i] === '--plugin-path' && i + 1 < args.length) {
            pluginPath = args[i + 1];
            i++;
        } else if (args[i] === '--grpc-port' && i + 1 < args.length) {
            grpcPort = parseInt(args[i + 1], 10);
            i++;
        }
    }

    if (!pluginPath) {
        console.error('Error: --plugin-path is required');
        process.exit(1);
    }

    return { pluginPath, grpcPort };
}

// Load the proto file
function loadProto() {
    const PROTO_PATH = resolve(__dirname, '../../grpc/protocol/domain.proto');
    const packageDefinition = protoLoader.loadSync(PROTO_PATH, {
        keepCase: true,
        longs: String,
        enums: String,
        defaults: true,
        oneofs: true,
    });
    const protoDescriptor = grpc.loadPackageDefinition(packageDefinition) as any;
    return protoDescriptor.protocol;
}

// Start the gRPC server
async function startServer(pluginPath: string, port: number) {
    // Load the plugin module
    const absolutePluginPath = resolve(pluginPath);
    console.log(`[Runtime] Loading plugin from: ${absolutePluginPath}`);

    let pluginModule;
    try {
        pluginModule = await import(absolutePluginPath);
    } catch (error) {
        console.error(`[Runtime] Failed to load plugin: ${error}`);
        process.exit(1);
    }

    const plugin = pluginModule.default || pluginModule;

    if (!plugin || !plugin.name) {
        console.error('[Runtime] Plugin must export default object with "name" property');
        process.exit(1);
    }

    console.log(`[Runtime] Loaded plugin: ${plugin.name}`);

    // Load proto definition
    const proto = loadProto();

    // Create gRPC server
    const server = new Server();

    // Implement DomainPluginService
    server.addService(proto.DomainPluginService.service, {
        Metadata: (call: any, callback: any) => {
            callback(null, {
                name: plugin.name || 'unknown',
                version: plugin.version || '1.0.0',
                qntx_version: plugin.qntx_version || '>= 0.1.0',
                description: plugin.description || '',
                author: plugin.author || '',
                license: plugin.license || 'MIT',
            });
        },

        Initialize: async (call: any, callback: any) => {
            try {
                if (plugin.init) {
                    await plugin.init(call.request);
                }
                callback(null, {
                    handler_names: plugin.handler_names || [],
                    schedules: plugin.schedules || [],
                });
            } catch (error) {
                console.error(`[Runtime] Initialize error: ${error}`);
                callback(error);
            }
        },

        Shutdown: async (call: any, callback: any) => {
            try {
                if (plugin.shutdown) {
                    await plugin.shutdown();
                }
                callback(null, {});
            } catch (error) {
                console.error(`[Runtime] Shutdown error: ${error}`);
                callback(error);
            }
        },

        HandleHTTP: async (call: any, callback: any) => {
            try {
                const request = call.request;

                // Create a simple HTTP mux
                const routes = new Map<string, any>();
                const mux = {
                    handle: (method: string, path: string, handler: any) => {
                        if (typeof method === 'string' && typeof path === 'string') {
                            routes.set(`${method.toUpperCase()} ${path}`, handler);
                        } else {
                            // Support simplified signature: handle(path, handler)
                            routes.set(`GET ${method}`, path);
                        }
                    }
                };

                // Let plugin register routes
                if (plugin.registerHTTP) {
                    plugin.registerHTTP(mux);
                }

                // Find matching route
                const routeKey = `${request.method} ${request.path}`;
                const handler = routes.get(routeKey);

                if (!handler) {
                    callback(null, {
                        status_code: 404,
                        headers: [],
                        body: Buffer.from(JSON.stringify({ error: 'Not found' })),
                    });
                    return;
                }

                // Create request/response objects
                const req = {
                    method: request.method,
                    path: request.path,
                    headers: request.headers,
                    body: request.body,
                    json: async () => JSON.parse(request.body.toString()),
                };

                let responseData: any = { status_code: 200, headers: [], body: Buffer.from('') };

                const res = {
                    json: (data: any) => {
                        responseData = {
                            status_code: responseData.status_code || 200,
                            headers: [{ name: 'Content-Type', values: ['application/json'] }],
                            body: Buffer.from(JSON.stringify(data)),
                        };
                    },
                    text: (data: string) => {
                        responseData = {
                            status_code: responseData.status_code || 200,
                            headers: [{ name: 'Content-Type', values: ['text/plain'] }],
                            body: Buffer.from(data),
                        };
                    },
                    send: (data: string, contentType: string) => {
                        responseData = {
                            status_code: responseData.status_code || 200,
                            headers: [{ name: 'Content-Type', values: [contentType] }],
                            body: Buffer.from(data),
                        };
                    },
                    status: (code: number) => {
                        responseData.status_code = code;
                        return res;
                    },
                };

                // Call handler
                await handler(req, res);

                callback(null, responseData);
            } catch (error) {
                console.error(`[Runtime] HandleHTTP error: ${error}`);
                callback(null, {
                    status_code: 500,
                    headers: [],
                    body: Buffer.from(JSON.stringify({ error: String(error) })),
                });
            }
        },

        HandleWebSocket: (call: any) => {
            // WebSocket not implemented for Phase 1
            console.warn('[Runtime] WebSocket not implemented yet');
        },

        Health: (call: any, callback: any) => {
            callback(null, {
                healthy: true,
                message: 'Plugin is running',
                details: {},
            });
        },

        ConfigSchema: (call: any, callback: any) => {
            callback(null, { fields: {} });
        },

        RegisterGlyphs: (call: any, callback: any) => {
            callback(null, { glyphs: plugin.glyphs || [] });
        },

        ExecuteJob: async (call: any, callback: any) => {
            callback(null, {
                success: false,
                error: 'ExecuteJob not implemented',
                result: Buffer.from(''),
                log_entries: [],
            });
        },
    });

    // Bind to port
    return new Promise<number>((resolve, reject) => {
        server.bindAsync(
            `127.0.0.1:${port}`,
            ServerCredentials.createInsecure(),
            (err, assignedPort) => {
                if (err) {
                    reject(err);
                    return;
                }

                server.start();

                // Print port for Go discovery (matches protocol in discovery.go:636)
                console.log(`QNTX_PLUGIN_PORT=${assignedPort}`);
                console.log(`[Runtime] ${plugin.name} v${plugin.version || '1.0.0'} ready on port ${assignedPort}`);

                resolve(assignedPort);
            }
        );
    });
}

// Main
const { pluginPath, grpcPort } = parseArgs();

startServer(pluginPath, grpcPort)
    .then((port) => {
        // Server is running
    })
    .catch((error) => {
        console.error(`[Runtime] Failed to start server: ${error}`);
        process.exit(1);
    });
