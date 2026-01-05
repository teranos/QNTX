/**
 * Webscraper Panel - UI for web scraping operations
 *
 * Provides interface for:
 * - Entering URLs to scrape
 * - Configuring scraping options
 * - Viewing scraping results
 * - Monitoring job status
 */

import { BasePanel } from './base-panel.ts';
import { sendMessage } from './websocket.ts';
import { toast } from './toast.ts';
import { debugLog } from './debug.ts';

interface ScrapeRequest {
    url: string;
    javascript: boolean;
    wait_ms: number;
    extract_links: boolean;
    extract_images: boolean;
}

interface ScrapeResult {
    url: string;
    title?: string;
    description?: string;
    content?: string;
    links?: string[];
    images?: string[];
    error?: string;
    timestamp: string;
}

class WebscraperPanel extends BasePanel {
    private currentResults: ScrapeResult[] = [];
    private isScraping: boolean = false;

    constructor() {
        super({
            id: 'webscraper-panel',
            classes: ['panel-slide-left', 'webscraper-panel'],
            useOverlay: true,
            closeOnEscape: true
        });
    }

    protected getTemplate(): string {
        return `
            <div class="panel-header">
                <h3 class="panel-title">Web Scraper</h3>
                <button class="panel-close" aria-label="Close">&#10005;</button>
            </div>

            <div class="panel-content">
                <!-- URL Input Section -->
                <div class="scraper-input-section">
                    <label for="scraper-url" class="scraper-label">URL to Scrape:</label>
                    <div class="scraper-input-group">
                        <input
                            type="url"
                            id="scraper-url"
                            class="scraper-url-input"
                            placeholder="https://example.com/article"
                            autocomplete="url"
                        />
                        <button id="scraper-submit" class="scraper-submit-btn">
                            Scrape
                        </button>
                    </div>
                </div>

                <!-- Options Section -->
                <div class="scraper-options">
                    <label class="scraper-option">
                        <input type="checkbox" id="scraper-js" checked />
                        <span>Render JavaScript</span>
                    </label>
                    <label class="scraper-option">
                        <input type="checkbox" id="scraper-links" checked />
                        <span>Extract Links</span>
                    </label>
                    <label class="scraper-option">
                        <input type="checkbox" id="scraper-images" />
                        <span>Extract Images</span>
                    </label>
                    <label class="scraper-option">
                        <span>Wait (ms):</span>
                        <input type="number" id="scraper-wait" value="2000" min="0" max="10000" step="500" />
                    </label>
                </div>

                <!-- Status Section -->
                <div id="scraper-status" class="scraper-status hidden">
                    <div class="scraper-status-text"></div>
                    <div class="scraper-progress hidden">
                        <div class="scraper-progress-bar"></div>
                    </div>
                </div>

                <!-- Results Section -->
                <div id="scraper-results" class="scraper-results">
                    <h4 class="scraper-results-title hidden">Results</h4>
                    <div class="scraper-results-content"></div>
                </div>

                <!-- History Section -->
                <div class="scraper-history">
                    <h4 class="scraper-history-title">Recent Scrapes</h4>
                    <div id="scraper-history-list" class="scraper-history-list">
                        <div class="scraper-history-empty">No recent scrapes</div>
                    </div>
                </div>
            </div>
        `;
    }

    protected setupEventListeners(): void {
        // URL input - Enter key submits
        const urlInput = this.$<HTMLInputElement>('#scraper-url');
        urlInput?.addEventListener('keypress', (e: KeyboardEvent) => {
            if (e.key === 'Enter') {
                this.handleScrape();
            }
        });

        // Submit button
        this.$('#scraper-submit')?.addEventListener('click', () => {
            this.handleScrape();
        });

        // Close button
        this.$('.panel-close')?.addEventListener('click', () => {
            this.hide();
        });
    }

    private async handleScrape(): Promise<void> {
        const urlInput = this.$<HTMLInputElement>('#scraper-url');
        const url = urlInput?.value.trim();

        if (!url) {
            toast.error('Please enter a URL to scrape');
            return;
        }

        // Validate URL
        try {
            new URL(url);
        } catch {
            toast.error('Please enter a valid URL');
            return;
        }

        if (this.isScraping) {
            toast.warning('Already scraping, please wait...');
            return;
        }

        // Get options
        const jsEnabled = this.$<HTMLInputElement>('#scraper-js')?.checked ?? true;
        const extractLinks = this.$<HTMLInputElement>('#scraper-links')?.checked ?? true;
        const extractImages = this.$<HTMLInputElement>('#scraper-images')?.checked ?? false;
        const waitMs = parseInt(this.$<HTMLInputElement>('#scraper-wait')?.value ?? '2000', 10);

        // Prepare request
        const request: ScrapeRequest = {
            url,
            javascript: jsEnabled,
            wait_ms: waitMs,
            extract_links: extractLinks,
            extract_images: extractImages
        };

        this.startScraping(url);

        // Send scrape request via WebSocket
        sendMessage({
            type: 'webscraper_request',
            data: request
        });

        debugLog('WebscraperPanel', 'Sent scrape request', request);
    }

    private startScraping(url: string): void {
        this.isScraping = true;

        // Update UI
        const submitBtn = this.$<HTMLButtonElement>('#scraper-submit');
        if (submitBtn) {
            submitBtn.textContent = 'Scraping...';
            submitBtn.disabled = true;
        }

        // Show status
        this.updateStatus(`Scraping ${url}...`, 'info');
        this.showProgress();

        // Clear previous results
        const resultsContent = this.$('.scraper-results-content');
        if (resultsContent) {
            resultsContent.innerHTML = '';
        }
    }

    private stopScraping(): void {
        this.isScraping = false;

        // Update UI
        const submitBtn = this.$<HTMLButtonElement>('#scraper-submit');
        if (submitBtn) {
            submitBtn.textContent = 'Scrape';
            submitBtn.disabled = false;
        }

        this.hideProgress();
    }

    private updateStatus(message: string, type: 'info' | 'success' | 'error' = 'info'): void {
        const statusEl = this.$('#scraper-status');
        const statusText = this.$('.scraper-status-text');

        if (statusEl && statusText) {
            statusEl.classList.remove('hidden');
            statusText.textContent = message;
            statusEl.className = `scraper-status scraper-status-${type}`;
        }
    }

    private showProgress(): void {
        const progress = this.$('.scraper-progress');
        if (progress) {
            progress.classList.remove('hidden');
        }
    }

    private hideProgress(): void {
        const progress = this.$('.scraper-progress');
        if (progress) {
            progress.classList.add('hidden');
        }
    }

    private displayResult(result: ScrapeResult): void {
        const resultsContent = this.$('.scraper-results-content');
        const resultsTitle = this.$('.scraper-results-title');

        if (!resultsContent) return;

        // Show title
        resultsTitle?.classList.remove('hidden');

        // Create result card
        const card = document.createElement('div');
        card.className = 'scraper-result-card';

        if (result.error) {
            card.innerHTML = `
                <div class="scraper-result-error">
                    <strong>Error:</strong> ${this.escapeHtml(result.error)}
                </div>
            `;
        } else {
            let html = `
                <div class="scraper-result-header">
                    <h5 class="scraper-result-title">${this.escapeHtml(result.title || 'Untitled')}</h5>
                    <a href="${this.escapeHtml(result.url)}" target="_blank" class="scraper-result-link">🔗</a>
                </div>
            `;

            if (result.description) {
                html += `<p class="scraper-result-description">${this.escapeHtml(result.description)}</p>`;
            }

            if (result.content) {
                const preview = result.content.substring(0, 200);
                html += `<div class="scraper-result-content">${this.escapeHtml(preview)}${result.content.length > 200 ? '...' : ''}</div>`;
            }

            if (result.links && result.links.length > 0) {
                html += `<div class="scraper-result-links">
                    <strong>Links found:</strong> ${result.links.length}
                </div>`;
            }

            if (result.images && result.images.length > 0) {
                html += `<div class="scraper-result-images">
                    <strong>Images found:</strong> ${result.images.length}
                </div>`;
            }

            card.innerHTML = html;
        }

        resultsContent.appendChild(card);

        // Add to history
        this.addToHistory(result);
    }

    private addToHistory(result: ScrapeResult): void {
        this.currentResults.unshift(result);

        // Keep only last 10 results
        if (this.currentResults.length > 10) {
            this.currentResults = this.currentResults.slice(0, 10);
        }

        this.updateHistoryDisplay();
    }

    private updateHistoryDisplay(): void {
        const historyList = this.$('#scraper-history-list');
        if (!historyList) return;

        if (this.currentResults.length === 0) {
            historyList.innerHTML = '<div class="scraper-history-empty">No recent scrapes</div>';
            return;
        }

        historyList.innerHTML = this.currentResults.map(result => `
            <div class="scraper-history-item" data-url="${this.escapeHtml(result.url)}">
                <div class="scraper-history-url">${this.escapeHtml(result.title || result.url)}</div>
                <div class="scraper-history-time">${new Date(result.timestamp).toLocaleTimeString()}</div>
            </div>
        `).join('');

        // Add click handlers to history items
        historyList.querySelectorAll('.scraper-history-item').forEach((item: Element) => {
            item.addEventListener('click', () => {
                const url = (item as HTMLElement).dataset.url;
                if (url) {
                    const urlInput = this.$<HTMLInputElement>('#scraper-url');
                    if (urlInput) {
                        urlInput.value = url;
                    }
                }
            });
        });
    }

    private escapeHtml(text: string): string {
        const div = document.createElement('div');
        div.textContent = text;
        return div.innerHTML;
    }

    /**
     * Handle webscraper response from server
     */
    public handleScraperResponse(data: any): void {
        debugLog('WebscraperPanel', 'Received scraper response', data);

        this.stopScraping();

        if (data.error) {
            this.updateStatus(`Error: ${data.error}`, 'error');
            toast.error(`Scraping failed: ${data.error}`);

            const result: ScrapeResult = {
                url: data.url || '',
                error: data.error,
                timestamp: new Date().toISOString()
            };
            this.displayResult(result);
        } else {
            this.updateStatus('Scraping complete!', 'success');

            const result: ScrapeResult = {
                url: data.url,
                title: data.title,
                description: data.description || data.meta_description,
                content: data.content,
                links: data.links,
                images: data.images,
                timestamp: new Date().toISOString()
            };
            this.displayResult(result);

            toast.success('Scraping completed successfully');
        }

        // Clear URL input for next scrape
        const urlInput = this.$<HTMLInputElement>('#scraper-url');
        if (urlInput) {
            urlInput.value = '';
            urlInput.focus();
        }
    }

    /**
     * Handle scraping progress updates
     */
    public handleScraperProgress(data: any): void {
        if (data.message) {
            this.updateStatus(data.message, 'info');
        }

        if (data.progress !== undefined) {
            const progressBar = this.$('.scraper-progress-bar') as HTMLElement;
            if (progressBar) {
                progressBar.style.width = `${data.progress}%`;
            }
        }
    }
}

// Create and export singleton instance
export const webscraperPanel = new WebscraperPanel();