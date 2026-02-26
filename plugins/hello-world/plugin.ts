/**
 * Hello World TypeScript Plugin
 *
 * Minimal test plugin to verify TypeScript runtime works end-to-end:
 * - Discovery finds the plugin
 * - Bun runtime starts
 * - gRPC communication works
 * - HTTP handlers register correctly
 */

export default {
    name: 'hello-world',
    version: '1.0.0',
    qntx_version: '>= 0.1.0',
    description: 'Hello World test plugin',
    author: 'QNTX Team',
    license: 'MIT',

    /**
     * Initialize plugin
     */
    async init(config: any) {
        console.log('[HelloWorld] Plugin initialized');
        console.log('[HelloWorld] Config:', JSON.stringify(config, null, 2));
        return { success: true };
    },

    /**
     * Register HTTP handlers
     */
    registerHTTP(mux: any) {
        // Simple hello endpoint
        mux.handle('GET', '/hello', (req: any, res: any) => {
            res.json({
                message: 'Hello from TypeScript plugin!',
                plugin: 'hello-world',
                runtime: 'Bun',
                timestamp: new Date().toISOString(),
            });
        });

        // Echo endpoint for testing request/response
        mux.handle('POST', '/echo', async (req: any, res: any) => {
            const body = await req.json();
            res.json({
                echo: body,
                received_at: new Date().toISOString(),
            });
        });
    },

    /**
     * Shutdown plugin
     */
    async shutdown() {
        console.log('[HelloWorld] Plugin shutting down');
    }
};
