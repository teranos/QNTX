/**
 * Test Glyphs - Demonstration of the glyph-primitive vision
 *
 * This file registers test glyphs to demonstrate the morphing behavior
 * where glyphs transform into windows and back.
 */

import { glyphRun } from './components/glyph-run';

// Register test glyphs once DOM is ready
export function registerTestGlyphs(): void {
    // VidStream Glyph
    glyphRun.add({
        id: 'vidstream-glyph',
        title: 'VidStream',
        renderContent: () => {
            const content = document.createElement('div');
            content.style.padding = '20px';
            content.innerHTML = `
                <h2 style="margin: 0 0 16px 0;">VidStream</h2>
                <p>Video streaming analytics and monitoring.</p>
                <div style="margin-top: 20px; padding: 16px; background: rgba(0,0,0,0.2); border-radius: 4px;">
                    <div>Active Streams: 42</div>
                    <div>Bandwidth: 1.2 GB/s</div>
                    <div>Viewers: 12,483</div>
                </div>
            `;
            return content;
        },
        initialWidth: '400px',
        initialHeight: '300px'
    });

    // Database Statistics Glyph
    glyphRun.add({
        id: 'database-glyph',
        title: 'Database Statistics',
        renderContent: () => {
            const content = document.createElement('div');
            content.style.padding = '20px';
            content.innerHTML = `
                <h2 style="margin: 0 0 16px 0;">Database Statistics</h2>
                <p>Real-time database performance metrics.</p>
                <div style="margin-top: 20px;">
                    <div style="margin-bottom: 12px;">
                        <strong>Queries/sec:</strong> 1,247
                    </div>
                    <div style="margin-bottom: 12px;">
                        <strong>Avg Response:</strong> 23ms
                    </div>
                    <div style="margin-bottom: 12px;">
                        <strong>Active Connections:</strong> 89
                    </div>
                    <div style="margin-bottom: 12px;">
                        <strong>Cache Hit Rate:</strong> 94.2%
                    </div>
                </div>
            `;
            return content;
        },
        initialWidth: '450px',
        initialHeight: '350px'
    });

    // Self Diagnostics Glyph
    glyphRun.add({
        id: 'self-glyph',
        title: 'Self',
        renderContent: () => {
            const content = document.createElement('div');
            content.style.padding = '20px';
            content.innerHTML = `
                <h2 style="margin: 0 0 16px 0;">Self Diagnostics</h2>
                <p>QNTX system health and performance.</p>
                <div style="margin-top: 20px;">
                    <div style="margin-bottom: 8px; color: #4ade80;">
                        ✓ All systems operational
                    </div>
                    <hr style="margin: 16px 0; opacity: 0.2;">
                    <div style="font-size: 12px; opacity: 0.8;">
                        <div>Memory: 234 MB</div>
                        <div>CPU: 12%</div>
                        <div>Uptime: 3d 14h 22m</div>
                        <div>Version: ${window.location.hostname}</div>
                    </div>
                </div>
            `;
            return content;
        },
        initialWidth: '380px',
        initialHeight: '320px'
    });

    console.log('Test glyphs registered:', {
        vidstream: 'VidStream monitoring',
        database: 'Database Statistics',
        self: 'Self diagnostics'
    });
}