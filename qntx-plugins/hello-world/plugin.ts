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
    version: '1.1.0',
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
     * Register custom glyph types
     */
    registerGlyphs() {
        return [
            {
                symbol: '🌐',
                title: 'Hello World',
                label: 'hello-world',
                content_path: '/glyph',
                default_width: 360,
                default_height: 240,
            },
        ];
    },

    /**
     * Register HTTP handlers
     */
    registerHTTP(mux: any) {
        // Glyph content endpoint
        mux.handle('GET', '/glyph', (req: any, res: any) => {
            res.setHeader('Content-Type', 'text/html');
            res.send(`
                <div style="padding: 16px; font-family: system-ui, sans-serif; color: #e2e8f0;">
                    <h2 style="margin: 0 0 12px 0; font-size: 18px;">🌐 Hello World</h2>
                    <p style="margin: 0 0 8px 0; font-size: 13px; color: #94a3b8;">
                        TypeScript plugin running on Bun.
                    </p>
                    <div id="hw-time" style="font-family: monospace; font-size: 12px; color: #60a5fa;"></div>
                    <script>
                        const el = document.getElementById('hw-time');
                        function tick() { el.textContent = new Date().toISOString(); }
                        tick(); setInterval(tick, 1000);
                    </script>
                </div>
            `);
        });

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
