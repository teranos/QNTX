/**
 * Dot Registry - Standard QNTX Dots (dot-as-primitive)
 *
 * Registers dots on startup. Dots are the primary primitive - they exist in
 * the tray zone and expand into windows, rather than windows minimizing to dots.
 *
 * Initially registers windows only (VidStream, DatabaseStats, Self).
 * Panels will be migrated later.
 */

import { windowTray } from './components/window-tray';
import { log, SEG } from './logger';

/**
 * Initialize dots
 * Called once on startup from main.ts
 */
export function initializeDots(): void {
    log.debug(SEG.UI, 'Initializing dots...');

    // VidStream dot
    // TODO: Extract VidStream content rendering from VidStreamWindow
    windowTray.register({
        id: 'vidstream-window',
        title: 'VidStream - Real-time Video Inference',
        symbol: '⮀',
        renderContent: () => {
            // Placeholder: will be replaced with actual VidStream content
            const div = document.createElement('div');
            div.style.padding = '20px';
            div.innerHTML = `
                <h2>VidStream</h2>
                <p>Real-time video inference</p>
                <p style="color: #666;">Content rendering to be implemented</p>
            `;
            return div;
        },
        initialWidth: '700px',
        initialHeight: '500px',
        defaultX: 100,
        defaultY: 100,
        onExpand: () => {
            log(SEG.VID, 'VidStream expanded');
        },
        onCollapse: () => {
            log(SEG.VID, 'VidStream collapsed');
        }
    });

    // Database stats dot
    // TODO: Extract database stats content rendering from DatabaseStatsWindow
    windowTray.register({
        id: 'db-stats-window',
        title: 'Database Statistics',
        symbol: '⊔',
        renderContent: () => {
            // Placeholder: will be replaced with actual DB stats content
            const div = document.createElement('div');
            div.style.padding = '20px';
            div.innerHTML = `
                <h2>Database Statistics</h2>
                <p>Storage layer metrics</p>
                <p style="color: #666;">Content rendering to be implemented</p>
            `;
            return div;
        },
        initialWidth: '600px',
        initialHeight: '450px',
        defaultX: 150,
        defaultY: 150,
        onExpand: () => {
            log(SEG.UI, 'Database stats expanded');
        },
        onCollapse: () => {
            log(SEG.UI, 'Database stats collapsed');
        }
    });

    // Self window dot
    // TODO: Extract self diagnostic content rendering from SelfWindow
    windowTray.register({
        id: 'self-window',
        title: 'System Diagnostic',
        symbol: '⍟',
        renderContent: () => {
            // Placeholder: will be replaced with actual self diagnostic content
            const div = document.createElement('div');
            div.style.padding = '20px';
            div.innerHTML = `
                <h2>System Diagnostic</h2>
                <p>Self/operator vantage point</p>
                <p style="color: #666;">Content rendering to be implemented</p>
            `;
            return div;
        },
        initialWidth: '500px',
        initialHeight: '400px',
        defaultX: 200,
        defaultY: 200,
        onExpand: () => {
            log(SEG.SELF, 'Self diagnostic expanded');
        },
        onCollapse: () => {
            log(SEG.SELF, 'Self diagnostic collapsed');
        }
    });

    log.debug(SEG.UI, `Dots initialized: ${windowTray.count} registered`);
}
