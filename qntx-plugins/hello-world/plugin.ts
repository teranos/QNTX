/**
 * Hello World TypeScript Plugin
 *
 * Minimal test plugin to verify TypeScript runtime works end-to-end:
 * - Discovery finds the plugin
 * - Bun runtime starts
 * - gRPC communication works
 * - HTTP handlers register correctly
 * - Glyph registration works
 */

export default {
    name: 'hello-world',
    version: '1.1.0',
    qntx_version: '>= 0.1.0',
    description: 'Hello World test plugin',
    author: 'QNTX Team',
    license: 'MIT',

    glyphs: [{
        symbol: '\uD83D\uDDFA\uFE0F',
        title: 'Hello World',
        label: 'hello-world',
        module_path: '/hello-world-module.js',
        default_width: 400,
        default_height: 300,
    }],

    /**
     * Initialize plugin
     */
    async init(config: any) {
        console.log('[HelloWorld] Plugin initialized');
        return { success: true };
    },

    /**
     * Register HTTP handlers
     */
    registerHTTP(mux: any) {
        mux.handle('GET', '/hello', (req: any, res: any) => {
            res.json({
                message: 'Hello from TypeScript plugin!',
                plugin: 'hello-world',
                runtime: 'Bun',
                timestamp: new Date().toISOString(),
            });
        });

        mux.handle('POST', '/echo', async (req: any, res: any) => {
            const body = await req.json();
            res.json({
                echo: body,
                received_at: new Date().toISOString(),
            });
        });

        mux.handle('GET', '/hello-world-module.js', (req: any, res: any) => {
            res.send(`
export function render(glyph, ui) {
  const container = document.createElement('div');
  container.style.cssText = 'padding: 20px; font-family: monospace; color: #33ff33; background: #0a0a0f; height: 100%; display: flex; align-items: center; justify-content: center; font-size: 24px;';
  container.textContent = 'Hello, World!';
  return container;
}
            `.trim(), 'application/javascript');
        });
    },

    /**
     * Shutdown plugin
     */
    async shutdown() {
        console.log('[HelloWorld] Plugin shutting down');
    }
};
