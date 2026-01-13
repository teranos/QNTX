/**
 * Database Statistics Window
 *
 * Displays database statistics similar to `qntx db stats` output
 */

import { Window } from './components/window.ts';
import { sendMessage } from './websocket.ts';
import { DB } from '@generated/sym.js';
import { tooltip } from './components/tooltip.ts';

interface DatabaseStats {
    path: string;
    total_attestations: number;
    unique_actors: number;
    unique_subjects: number;
    unique_contexts: number;
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

        const { path, total_attestations, unique_actors, unique_subjects, unique_contexts } = this.stats;

        content.innerHTML = `
            <div class="db-stats">
                <div class="db-stat-row">
                    <span class="db-stat-label">Database Path:</span>
                    <span class="db-stat-value db-path has-tooltip" data-path="${path}" data-tooltip="Click to open in file manager">${path}</span>
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
