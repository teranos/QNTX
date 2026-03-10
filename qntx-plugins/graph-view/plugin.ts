/**
 * Graph View Plugin
 *
 * Renders the QNTX attestation graph with WebGL (regl).
 * Force-directed layout, pan/zoom, colored by node type.
 */

import { readFileSync } from 'fs';
import { join, dirname } from 'path';

const moduleJsPath = join(dirname(import.meta.path), 'web', 'dist', 'graph-view-module.js');

export default {
    name: 'graph-view',
    version: '1.1.0',
    qntx_version: '>= 0.1.0',
    description: 'WebGL graph visualization',
    author: 'QNTX',
    license: 'MIT',

    glyphs: [{
        symbol: '\uD83D\uDD78\uFE0F',
        title: 'Graph View',
        label: 'graph-view',
        module_path: '/graph-view-module.js',
        default_width: 800,
        default_height: 600,
    }],

    async init(config: any) {
        console.log('[GraphView] Plugin initialized');
        return { success: true };
    },

    registerHTTP(mux: any) {
        mux.handle('GET', '/graph-view-module.js', (req: any, res: any) => {
            try {
                const js = readFileSync(moduleJsPath, 'utf-8');
                res.send(js, 'application/javascript');
            } catch (err) {
                console.error('[GraphView] Failed to read module JS:', err);
                res.send('// graph-view-module.js not built yet — run: make graph-view-plugin', 'application/javascript');
            }
        });
    },

    async shutdown() {
        console.log('[GraphView] Plugin shutting down');
    }
};
