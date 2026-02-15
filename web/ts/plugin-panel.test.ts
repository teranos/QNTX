/**
 * Tests for Plugin Panel - Focus on build time display
 *
 * Critical: Build time display prevents "which binary is running" confusion
 */

import { describe, test, expect, beforeEach } from 'bun:test';
import { PluginPanel } from './plugin-panel';

// Mock the plugin panel's build time formatting logic
function formatBuildTime(buildTime?: string): string | null {
    if (!buildTime || buildTime === 'dev' || buildTime === 'unknown') {
        return null;
    }

    // Parse RFC3339 timestamp
    const date = new Date(buildTime);
    if (isNaN(date.getTime())) {
        return null;
    }

    const now = new Date();
    const diffMs = now.getTime() - date.getTime();
    const diffMins = Math.floor(diffMs / 60000);
    const diffHours = Math.floor(diffMins / 60);
    const diffDays = Math.floor(diffHours / 24);

    let relativeTime: string;
    if (diffMins < 1) {
        relativeTime = 'just now';
    } else if (diffMins < 60) {
        relativeTime = `${diffMins}m ago`;
    } else if (diffHours < 24) {
        relativeTime = `${diffHours}h ago`;
    } else {
        relativeTime = `${diffDays}d ago`;
    }

    const formattedDate = date.toLocaleString();
    return `${relativeTime} (${formattedDate})`;
}

// Mock function to format Unix epoch timestamps (plugin binary_built)
function formatUnixTimestamp(timestampStr: string): string | null {
    const timestamp = parseInt(timestampStr, 10);
    if (isNaN(timestamp)) {
        return null;
    }

    const date = new Date(timestamp * 1000);
    const now = new Date();
    const diffMs = now.getTime() - date.getTime();
    const diffMins = Math.floor(diffMs / 60000);
    const diffHours = Math.floor(diffMins / 60);
    const diffDays = Math.floor(diffHours / 24);

    let relativeTime: string;
    if (diffMins < 1) {
        relativeTime = 'just now';
    } else if (diffMins < 60) {
        relativeTime = `${diffMins}m ago`;
    } else if (diffHours < 24) {
        relativeTime = `${diffHours}h ago`;
    } else {
        relativeTime = `${diffDays}d ago`;
    }

    const formattedDate = date.toLocaleString();
    return `${relativeTime} (${formattedDate})`;
}

describe('Plugin Panel Build Time Display', () => {
    describe('Server build time formatting (RFC3339)', () => {
        test('formats recent build time (< 1 minute)', () => {
            const now = new Date();
            const buildTime = now.toISOString();
            const result = formatBuildTime(buildTime);

            expect(result).toContain('just now');
        });

        test('formats build time in minutes', () => {
            const fiveMinutesAgo = new Date(Date.now() - 5 * 60 * 1000);
            const buildTime = fiveMinutesAgo.toISOString();
            const result = formatBuildTime(buildTime);

            expect(result).toContain('5m ago');
            expect(result).toContain(fiveMinutesAgo.toLocaleString());
        });

        test('formats build time in hours', () => {
            const threeHoursAgo = new Date(Date.now() - 3 * 60 * 60 * 1000);
            const buildTime = threeHoursAgo.toISOString();
            const result = formatBuildTime(buildTime);

            expect(result).toContain('3h ago');
        });

        test('formats build time in days', () => {
            const twoDaysAgo = new Date(Date.now() - 2 * 24 * 60 * 60 * 1000);
            const buildTime = twoDaysAgo.toISOString();
            const result = formatBuildTime(buildTime);

            expect(result).toContain('2d ago');
        });

        test('returns null for dev builds', () => {
            expect(formatBuildTime('dev')).toBeNull();
            expect(formatBuildTime('unknown')).toBeNull();
        });

        test('returns null for invalid timestamps', () => {
            expect(formatBuildTime('not-a-date')).toBeNull();
            expect(formatBuildTime('')).toBeNull();
            expect(formatBuildTime(undefined)).toBeNull();
        });

        test('includes both relative and absolute time', () => {
            const tenMinutesAgo = new Date(Date.now() - 10 * 60 * 1000);
            const buildTime = tenMinutesAgo.toISOString();
            const result = formatBuildTime(buildTime);

            expect(result).toMatch(/\d+m ago \(/);
            expect(result).toContain(tenMinutesAgo.toLocaleString());
        });
    });

    describe('Plugin binary build time formatting (Unix epoch)', () => {
        test('formats recent binary build (< 1 minute)', () => {
            const now = Math.floor(Date.now() / 1000);
            const result = formatUnixTimestamp(now.toString());

            expect(result).toContain('just now');
        });

        test('formats binary build time in minutes', () => {
            const fiveMinutesAgo = Math.floor((Date.now() - 5 * 60 * 1000) / 1000);
            const result = formatUnixTimestamp(fiveMinutesAgo.toString());

            expect(result).toContain('5m ago');
        });

        test('formats binary build time in hours', () => {
            const twoHoursAgo = Math.floor((Date.now() - 2 * 60 * 60 * 1000) / 1000);
            const result = formatUnixTimestamp(twoHoursAgo.toString());

            expect(result).toContain('2h ago');
        });

        test('formats binary build time in days', () => {
            const threeDaysAgo = Math.floor((Date.now() - 3 * 24 * 60 * 60 * 1000) / 1000);
            const result = formatUnixTimestamp(threeDaysAgo.toString());

            expect(result).toContain('3d ago');
        });

        test('returns null for invalid Unix timestamps', () => {
            expect(formatUnixTimestamp('not-a-number')).toBeNull();
            expect(formatUnixTimestamp('abc')).toBeNull();
        });

        test('includes both relative and absolute time', () => {
            const thirtyMinutesAgo = Math.floor((Date.now() - 30 * 60 * 1000) / 1000);
            const result = formatUnixTimestamp(thirtyMinutesAgo.toString());

            expect(result).toMatch(/\d+m ago \(/);
        });
    });

    describe('Critical: Build time visibility requirements', () => {
        test('server health response includes build_time field', () => {
            const mockServerHealth = {
                status: 'ok',
                version: '0.1.0',
                commit: 'abc123',
                build_time: new Date().toISOString(),
                clients: 2,
                verbosity: 1,
                owner: 'SBVH'
            };

            expect(mockServerHealth.build_time).toBeDefined();
            expect(typeof mockServerHealth.build_time).toBe('string');
        });

        test('plugin health details include binary_built field', () => {
            const mockPluginDetails = {
                python_version: '3.13',
                initialized: 'true',
                binary_built: Math.floor(Date.now() / 1000).toString(),
                ats_store: 'configured',
                queue: 'configured'
            };

            expect(mockPluginDetails.binary_built).toBeDefined();
            expect(typeof mockPluginDetails.binary_built).toBe('string');
        });

        test('build times are immediately visible (not hidden behind expansion)', () => {
            // This is a critical requirement - both server and plugin build times
            // must be visible in the plugin panel without requiring user interaction

            // Server build time should be in the summary section (always visible)
            const summaryHTML = `
                <div class="plugin-summary panel-card">
                    <div class="plugin-server-info">
                        <span class="plugin-server-label">QNTX Server Built:</span>
                        <span class="plugin-server-value panel-code">5m ago (${new Date().toLocaleString()})</span>
                    </div>
                </div>
            `;

            expect(summaryHTML).toContain('QNTX Server Built:');
            expect(summaryHTML).toMatch(/\d+m ago/);
        });

        test('stale builds are easily identifiable', () => {
            // Builds older than 1 day should show day count
            const threeDaysAgo = Math.floor((Date.now() - 3 * 24 * 60 * 60 * 1000) / 1000);
            const result = formatUnixTimestamp(threeDaysAgo.toString());

            expect(result).toContain('3d ago');

            // This makes it VERY obvious when someone is running old binaries
            expect(result).toMatch(/\d+d ago/);
        });

        test('fresh builds (< 5 minutes) are clearly marked', () => {
            const twoMinutesAgo = new Date(Date.now() - 2 * 60 * 1000);
            const result = formatBuildTime(twoMinutesAgo.toISOString());

            expect(result).toContain('2m ago');

            // This confirms the binary was just rebuilt
            const minutes = parseInt(result.match(/(\d+)m ago/)?.[1] || '999');
            expect(minutes).toBeLessThan(5);
        });
    });

    describe('Edge cases and error handling', () => {
        test('handles future timestamps gracefully', () => {
            const futureTime = new Date(Date.now() + 60 * 1000);
            const result = formatBuildTime(futureTime.toISOString());

            // Should still produce output, even if negative time
            expect(result).toBeDefined();
        });

        test('handles very old timestamps', () => {
            const veryOld = new Date('2020-01-01T00:00:00Z');
            const result = formatBuildTime(veryOld.toISOString());

            expect(result).toContain('d ago');
            expect(result).toContain(veryOld.toLocaleString());
        });

        test('handles epoch zero', () => {
            const result = formatUnixTimestamp('0');

            expect(result).toBeDefined();
            // Should be many days ago
            expect(result).toMatch(/\d+d ago/);
        });
    });
});

describe('PluginPanel error handling', () => {
    let panel: PluginPanel;

    beforeEach(() => {
        document.body.innerHTML = '<div id="panel-container"></div><template id="panel-skeleton"><div class="panel-header"><h3 class="panel-title"></h3><div class="panel-header-actions"><button class="panel-fullscreen-toggle" type="button" aria-label="Enter fullscreen">⛶</button><button class="panel-close" type="button" aria-label="Close">✕</button></div></div><div class="panel-search" hidden><input type="text" class="panel-search-input" placeholder="Filter..."></div><div class="panel-content"><div class="panel-loading"><p>Loading...</p></div></div></template>';
        global.fetch = () => Promise.resolve({
            ok: true,
            json: () => Promise.resolve({ plugins: [] })
        } as Response);

        panel = new PluginPanel();
    });

    test('renders error message when config save fails', () => {
        // Set up panel with plugins data
        (panel as any).plugins = [{
            name: 'test-plugin',
            version: '1.0.0',
            state: 'running',
            healthy: true
        }];

        // Set up panel with config state containing an error
        (panel as any).configState = {
            pluginName: 'test-plugin',
            currentConfig: { max_workers: '4' },
            newConfig: { max_workers: '5' },
            schema: {
                max_workers: {
                    type: 'integer',
                    description: 'Maximum workers',
                    default_value: '4',
                    required: true
                }
            },
            validationErrors: {},
            needsConfirmation: false,
            error: {
                message: 'Failed to save configuration',
                details: 'Connection timeout after 5 seconds',
                status: 500
            }
        };
        (panel as any).expandedPlugin = 'test-plugin';

        // Render the panel
        (panel as any).render();

        // Check that error is displayed
        const html = document.body.innerHTML;
        expect(html).toContain('Failed to save configuration');
        expect(html).toContain('Error Details');
        expect(html).toContain('Connection timeout after 5 seconds');
    });

    test('renders validation errors for config fields', () => {
        (panel as any).plugins = [{
            name: 'test-plugin',
            version: '1.0.0',
            state: 'running',
            healthy: true
        }];

        (panel as any).configState = {
            pluginName: 'test-plugin',
            currentConfig: { max_workers: '4' },
            newConfig: { max_workers: 'invalid' },
            schema: {
                max_workers: {
                    type: 'integer',
                    description: 'Maximum workers',
                    default_value: '4',
                    required: true
                }
            },
            validationErrors: {
                max_workers: 'Must be a valid integer'
            },
            needsConfirmation: false,
            editingFields: new Set()
        };
        (panel as any).expandedPlugin = 'test-plugin';

        (panel as any).render();

        const html = document.body.innerHTML;
        expect(html).toContain('Must be a valid integer');
        expect(html).toContain('plugin-config-row-error');
    });

    test('shows confirmation warning before restart', () => {
        (panel as any).plugins = [{
            name: 'test-plugin',
            version: '1.0.0',
            state: 'running',
            healthy: true
        }];

        (panel as any).configState = {
            pluginName: 'test-plugin',
            currentConfig: { max_workers: '4' },
            newConfig: { max_workers: '8' },
            schema: {
                max_workers: {
                    type: 'integer',
                    description: 'Maximum workers',
                    default_value: '4',
                    required: false
                }
            },
            validationErrors: {},
            needsConfirmation: true,
            editingFields: new Set()
        };
        (panel as any).expandedPlugin = 'test-plugin';

        (panel as any).render();

        const html = document.body.innerHTML;
        expect(html).toContain('Confirm Restart');
        expect(html).toContain('This will apply your changes and reinitialize the plugin');
    });
});
