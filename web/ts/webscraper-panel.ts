/**
 * Webscraper Panel - UI for web scraping operations
 *
 * Provides interface for:
 * - Entering URLs to scrape
 * - Configuring scraping options
 * - Viewing scraping results
 * - Monitoring job status
 *
 * TODO: Add visual indicator for disabled/unavailable plugin state
 * When the webscraper plugin is not loaded or unavailable, the panel should:
 * 1. Show diagonal gray stripes background (CSS repeating-linear-gradient pattern)
 * 2. Disable interactive elements
 * 3. Display "Plugin Unavailable" message
 * This requires:
 * - Backend API to indicate plugin availability status
 * - CSS class `.panel-unavailable` with diagonal stripes
 * - Symbol palette integration to show stripes on symbols for disabled plugins
 * - Similar to how fuzzy-ax shows availability in command palette
 */

import { BasePanel } from './base-panel.ts';
import { escapeHtml } from './html-utils.ts';
import { toast } from './toast.ts';
import { log, SEG } from './logger';
import { createRichErrorState, type RichError } from './base-panel-error.ts';
import { handleError } from './error-handler.ts';

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

    // Two-click confirmation state
    private needsConfirmation: boolean = false;
    private confirmationTimeout: number | null = null;
    private pendingUrl: string = '';

    constructor() {
        super({
            id: 'webscraper-panel',
            classes: ['panel-slide-left', 'webscraper-panel'],
            useOverlay: true,
            closeOnEscape: true
        });
    }

    /**
     * Build a rich error from a scraper error
     * Provides helpful context and suggestions for common issues
     */
    private buildScraperError(error: unknown, url: string): RichError {
        const errorMessage = error instanceof Error ? error.message : String(error);
        const errorStack = error instanceof Error ? error.stack : undefined;

        // Check for plugin connection issues (404)
        if (errorMessage.includes('404')) {
            return {
                title: 'Plugin Not Found',
                message: 'The webscraper plugin endpoint is not available',
                suggestion: 'Ensure the webscraper plugin is installed and running. Check the Plugin Panel (⚙) for plugin status.',
                details: `URL: ${url}\n\n${errorStack || errorMessage}`
            };
        }

        // Check for plugin unavailable (502/503)
        if (errorMessage.includes('502') || errorMessage.includes('503')) {
            return {
                title: 'Plugin Unavailable',
                message: 'Cannot connect to the webscraper plugin',
                suggestion: 'The plugin may be starting up or has crashed. Check the Plugin Panel (⚙) and try restarting the plugin.',
                details: `URL: ${url}\n\n${errorStack || errorMessage}`
            };
        }

        // Check for network errors
        if (errorMessage.includes('NetworkError') || errorMessage.includes('Failed to fetch') || errorMessage.includes('Network')) {
            return {
                title: 'Network Error',
                message: 'Unable to connect to the QNTX server',
                suggestion: 'Check your network connection and ensure the QNTX server is running.',
                details: `URL: ${url}\n\n${errorStack || errorMessage}`
            };
        }

        // Check for timeout
        if (errorMessage.includes('timeout') || errorMessage.includes('Timeout')) {
            return {
                title: 'Request Timeout',
                message: 'The scraping request took too long to complete',
                suggestion: 'The target website may be slow or unresponsive. Try again or increase the wait time in options.',
                details: `URL: ${url}\n\n${errorStack || errorMessage}`
            };
        }

        // Check for invalid URL
        if (errorMessage.includes('Invalid URL') || errorMessage.includes('invalid url')) {
            return {
                title: 'Invalid URL',
                message: 'The provided URL is not valid',
                suggestion: 'Make sure the URL starts with http:// or https:// and is properly formatted.',
                details: `URL: ${url}`
            };
        }

        // Generic HTTP error
        const httpMatch = errorMessage.match(/HTTP\s*(\d{3})/i);
        if (httpMatch) {
            const status = parseInt(httpMatch[1], 10);
            return {
                title: `HTTP Error ${status}`,
                message: errorMessage,
                status: status,
                suggestion: status >= 500
                    ? 'A server error occurred. Try again later.'
                    : 'Check the URL and try again.',
                details: `URL: ${url}\n\n${errorStack || errorMessage}`
            };
        }

        // Generic error
        return {
            title: 'Scraping Failed',
            message: errorMessage,
            suggestion: 'An unexpected error occurred. Check the error details for more information.',
            details: `URL: ${url}\n\n${errorStack || errorMessage}`
        };
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
                            type="text"
                            id="scraper-url"
                            class="scraper-url-input"
                            placeholder="example.com or https://example.com"
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
                <div id="scraper-status" class="scraper-status u-hidden">
                    <div class="scraper-status-text"></div>
                    <div class="scraper-progress u-hidden">
                        <div class="scraper-progress-bar"></div>
                    </div>
                </div>

                <!-- Results Section -->
                <div id="scraper-results" class="scraper-results">
                    <h4 class="scraper-results-title u-hidden">Results</h4>
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

        // Reset confirmation when URL changes
        urlInput?.addEventListener('input', () => {
            if (this.needsConfirmation) {
                this.needsConfirmation = false;
                this.pendingUrl = '';
                this.updateSubmitButton();
            }
        });

        // Submit button
        this.$('#scraper-submit')?.addEventListener('click', () => {
            this.handleScrape();
        });
        // Note: Close button is handled by BasePanel automatically
    }

    private async handleScrape(): Promise<void> {
        const urlInput = this.$<HTMLInputElement>('#scraper-url');
        let url = urlInput?.value.trim();

        if (!url) {
            toast.error('Please enter a URL to scrape');
            return;
        }

        // Add protocol if missing
        if (!url.startsWith('http://') && !url.startsWith('https://')) {
            url = 'https://' + url;
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

        // First click: show confirmation state
        if (!this.needsConfirmation || this.pendingUrl !== url) {
            this.needsConfirmation = true;
            this.pendingUrl = url;
            this.updateSubmitButton();

            // Auto-reset confirmation after 5 seconds
            if (this.confirmationTimeout) {
                clearTimeout(this.confirmationTimeout);
            }
            this.confirmationTimeout = window.setTimeout(() => {
                this.needsConfirmation = false;
                this.pendingUrl = '';
                this.updateSubmitButton();
            }, 5000);

            return;
        }

        // Second click: actually scrape
        this.needsConfirmation = false;
        this.pendingUrl = '';
        if (this.confirmationTimeout) {
            clearTimeout(this.confirmationTimeout);
            this.confirmationTimeout = null;
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

        // Send scrape request via HTTP POST to the plugin endpoint
        try {
            const response = await fetch('/api/webscraper/scrape', {
                method: 'POST',
                headers: {
                    'Content-Type': 'application/json',
                },
                body: JSON.stringify({
                    url: url,
                    // The plugin API doesn't support these options yet, but we could extend it
                    javascript: jsEnabled,
                    wait_ms: waitMs,
                    extract_links: extractLinks,
                    extract_images: extractImages
                })
            });

            if (!response.ok) {
                const errorText = await response.text();
                throw new Error(`HTTP ${response.status}: ${errorText || response.statusText}`);
            }

            const data = await response.json();
            this.handleScraperResponse(data);
        } catch (error: unknown) {
            handleError(error, 'Scraping failed', { context: SEG.INGEST, silent: true });

            // Build rich error with helpful suggestions
            const richError = this.buildScraperError(error, url);

            this.handleScraperResponse({
                url: url,
                error: richError.message,
                _richError: richError
            } as ScrapeResult & { _richError: RichError });
        }

        log.debug(SEG.INGEST, 'Sent scrape request', request);
    }

    private startScraping(url: string): void {
        this.isScraping = true;

        // Update UI
        this.updateSubmitButton();

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
        this.updateSubmitButton();

        this.hideProgress();
    }

    private updateSubmitButton(): void {
        const submitBtn = this.$<HTMLButtonElement>('#scraper-submit');
        if (!submitBtn) return;

        // Remove existing state classes
        submitBtn.classList.remove('panel-btn-warning', 'panel-btn-success');

        if (this.isScraping) {
            submitBtn.textContent = 'Scraping...';
            submitBtn.disabled = true;
        } else if (this.needsConfirmation) {
            submitBtn.textContent = 'Confirm Scrape';
            submitBtn.classList.add('panel-btn-warning');
            submitBtn.disabled = false;
        } else {
            submitBtn.textContent = 'Scrape';
            submitBtn.disabled = false;
        }

        // Update or remove hint
        const existingHint = this.$('.scraper-confirm-hint');
        if (this.needsConfirmation && !this.isScraping) {
            if (!existingHint) {
                const hint = document.createElement('div');
                hint.className = 'scraper-confirm-hint panel-confirm-hint';
                hint.textContent = 'Click again to start scraping';
                submitBtn.parentElement?.appendChild(hint);
            }
        } else {
            existingHint?.remove();
        }
    }

    private updateStatus(message: string, type: 'info' | 'success' | 'error' = 'info'): void {
        const statusEl = this.$('#scraper-status');
        const statusText = this.$('.scraper-status-text');

        if (statusEl && statusText) {
            statusEl.classList.remove('u-hidden');
            statusText.textContent = message;
            statusEl.className = `scraper-status scraper-status-${type}`;
        }
    }

    private showProgress(): void {
        const progress = this.$('.scraper-progress');
        if (progress) {
            progress.classList.remove('u-hidden');
        }
    }

    private hideProgress(): void {
        const progress = this.$('.scraper-progress');
        if (progress) {
            progress.classList.add('u-hidden');
        }
    }

    private displayResult(result: ScrapeResult): void {
        const resultsContent = this.$('.scraper-results-content');
        const resultsTitle = this.$('.scraper-results-title');

        if (!resultsContent) return;

        // Show title
        resultsTitle?.classList.remove('u-hidden');

        // Create result card
        const card = document.createElement('div');
        card.className = 'scraper-result-card';

        if (result.error) {
            // TODO: Error is displayed both in panel error state and in results box,
            // causing duplication. Should only show in one place or clearly differentiate
            // between inline panel errors and result history errors.

            // Use rich error display if available
            const richError = (result as ScrapeResult & { _richError?: RichError })._richError;
            if (richError) {
                card.appendChild(createRichErrorState(richError, () => {
                    // Retry scraping the same URL
                    const urlInput = this.$<HTMLInputElement>('#scraper-url');
                    if (urlInput && result.url) {
                        urlInput.value = result.url;
                        this.handleScrape();
                    }
                }));
            } else {
                // Fallback to simple error display
                card.innerHTML = `
                    <div class="scraper-result-error">
                        <strong>Error:</strong> ${escapeHtml(result.error)}
                    </div>
                `;
            }
        } else {
            let html = `
                <div class="scraper-result-header">
                    <h5 class="scraper-result-title">${escapeHtml(result.title || 'Untitled')}</h5>
                    <a href="${escapeHtml(result.url)}" target="_blank" class="scraper-result-link">🔗</a>
                </div>
            `;

            if (result.description) {
                html += `<p class="scraper-result-description">${escapeHtml(result.description)}</p>`;
            }

            if (result.content) {
                const preview = result.content.substring(0, 200);
                html += `<div class="scraper-result-content">${escapeHtml(preview)}${result.content.length > 200 ? '...' : ''}</div>`;
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
            <div class="scraper-history-item" data-url="${escapeHtml(result.url)}">
                <div class="scraper-history-url">${escapeHtml(result.title || result.url)}</div>
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


    /**
     * Handle webscraper response from server
     */
    public handleScraperResponse(data: any): void {
        log.debug(SEG.INGEST, 'Received scraper response', data);

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