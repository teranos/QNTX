/**
 * Database Statistics Window
 *
 * Displays database statistics similar to `qntx db stats` output
 */

import { Window } from './components/window.ts';
import { sendMessage } from './websocket.ts';
import { DB } from '@generated/sym.js';
import { tooltip } from './components/tooltip.ts';

interface RichFieldInfo {
    field: string;
    count: number;
    source_types: string[];
}

interface DatabaseStats {
    path: string;
    storage_backend?: string;
    storage_optimized?: boolean;
    storage_version?: string;
    total_attestations: number;
    unique_actors: number;
    unique_subjects: number;
    unique_contexts: number;
    rich_fields?: string[] | RichFieldInfo[];
}

class DatabaseStatsWindow {
    private window: Window;
    private stats: DatabaseStats | null = null;

    constructor() {
        this.window = new Window({
            id: 'database-stats-window',
            title: `${DB} Database Statistics`,
            width: '600px',
            height: 'auto',
            onShow: () => this.onShow()
        });

        this.render();
    }

    private onShow(): void {
        // Request fresh stats from server when window is shown
        sendMessage({ type: 'get_database_stats' });
    }

    toggle(): void {
        this.window.toggle();
    }

    /**
     * Update stats from server response
     */
    updateStats(stats: DatabaseStats): void {
        this.stats = stats;
        this.render();
    }

    private render(): void {
        const content = this.window.getContentElement();

        if (!this.stats) {
            content.innerHTML = `
                <div class="db-stats-loading">
                    <p>Loading database statistics...</p>
                </div>
            `;
            return;
        }

        const { path, storage_optimized, storage_version, total_attestations, unique_actors, unique_subjects, unique_contexts, rich_fields } = this.stats;

        // Format storage backend info
        let storageBackendDisplay = 'go (fallback)';
        if (storage_optimized) {
            storageBackendDisplay = `rust (optimized) v${storage_version}`;
        }

        // Format rich fields for display
        let richFieldsSection = '';
        if (rich_fields && rich_fields.length > 0) {
            let fieldsDisplay = '';

            // Check if we have enhanced field information or just strings
            if (typeof rich_fields[0] === 'object' && 'field' in rich_fields[0]) {
                // Enhanced field information with counts and source types
                const enhancedFields = rich_fields as RichFieldInfo[];
                fieldsDisplay = enhancedFields
                    .sort((a, b) => b.count - a.count) // Sort by count descending
                    .map(info => {
                        const sourcesText = info.source_types.length > 0
                            ? ` [from: ${info.source_types.join(', ')}]`
                            : '';
                        return `<span class="rich-field-item has-tooltip"
                                      data-tooltip="${info.count.toLocaleString()} attestations${sourcesText}">
                                    ${info.field} (${info.count.toLocaleString()})${sourcesText}
                                </span>`;
                    })
                    .join('');
            } else {
                // Simple string array fallback
                const simpleFields = rich_fields as string[];
                fieldsDisplay = simpleFields
                    .sort()
                    .map(field => `<span class="rich-field-item">${field}</span>`)
                    .join('');
            }

            richFieldsSection = `
                <div class="db-stat-section">
                    <h3 class="db-stat-section-title">Searchable Rich Text Fields</h3>
                    <div class="rich-fields-list">
                        ${fieldsDisplay}
                    </div>
                </div>
            `;
        }

        content.innerHTML = `
            <div class="db-stats">
                <div class="db-stat-row">
                    <span class="db-stat-label">Database Path:</span>
                    <span class="db-stat-value db-path has-tooltip" data-path="${path}" data-tooltip="Click to open in file manager">${path}</span>
                </div>
                <div class="db-stat-row">
                    <span class="db-stat-label">Storage Backend:</span>
                    <span class="db-stat-value">${storageBackendDisplay}</span>
                </div>
                <div class="db-stat-row">
                    <span class="db-stat-label">Total Attestations:</span>
                    <span class="db-stat-value">${total_attestations.toLocaleString()}</span>
                </div>
                <div class="db-stat-row">
                    <span class="db-stat-label">Unique Actors:</span>
                    <span class="db-stat-value">${unique_actors.toLocaleString()}</span>
                </div>
                <div class="db-stat-row">
                    <span class="db-stat-label">Unique Subjects:</span>
                    <span class="db-stat-value">${unique_subjects.toLocaleString()}</span>
                </div>
                <div class="db-stat-row">
                    <span class="db-stat-label">Unique Contexts:</span>
                    <span class="db-stat-value">${unique_contexts.toLocaleString()}</span>
                </div>
                ${richFieldsSection}
            </div>
        `;

        // Add click handler for path
        const pathElement = content.querySelector('.db-path') as HTMLElement;
        if (pathElement) {
            pathElement.style.cursor = 'pointer';
            pathElement.addEventListener('click', () => {
                sendMessage({
                    type: 'open_path_in_finder',
                    path: path
                });
            });
        }

        // Setup tooltips
        this.setupTooltips();
    }

    private setupTooltips(): void {
        const content = this.window.getContentElement();
        tooltip.attach(content, '.has-tooltip');
    }
}

export const databaseStatsWindow = new DatabaseStatsWindow();
