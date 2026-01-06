/**
 * Accessibility utilities for QNTX web UI
 *
 * Provides screen reader announcements, focus management,
 * and other a11y helpers following WCAG 2.1 guidelines.
 */

/**
 * Screen reader announcement utility
 * Creates and manages ARIA live regions for dynamic content updates
 */
class ScreenReaderAnnouncer {
    private liveRegion: HTMLElement | null = null;
    private assertiveRegion: HTMLElement | null = null;

    constructor() {
        this.initializeLiveRegions();
    }

    private initializeLiveRegions(): void {
        // Create polite announcements region (waits for screen reader to finish)
        if (!document.getElementById('sr-live-polite')) {
            this.liveRegion = document.createElement('div');
            this.liveRegion.id = 'sr-live-polite';
            this.liveRegion.setAttribute('aria-live', 'polite');
            this.liveRegion.setAttribute('aria-atomic', 'true');
            this.liveRegion.className = 'sr-only';
            document.body.appendChild(this.liveRegion);
        } else {
            this.liveRegion = document.getElementById('sr-live-polite');
        }

        // Create assertive announcements region (interrupts screen reader)
        if (!document.getElementById('sr-live-assertive')) {
            this.assertiveRegion = document.createElement('div');
            this.assertiveRegion.id = 'sr-live-assertive';
            this.assertiveRegion.setAttribute('aria-live', 'assertive');
            this.assertiveRegion.setAttribute('aria-atomic', 'true');
            this.assertiveRegion.className = 'sr-only';
            document.body.appendChild(this.assertiveRegion);
        } else {
            this.assertiveRegion = document.getElementById('sr-live-assertive');
        }
    }

    /**
     * Announce message to screen readers (polite - waits for current speech)
     */
    announce(message: string): void {
        if (!this.liveRegion) return;

        // Clear and set new message with a small delay to ensure announcement
        this.liveRegion.textContent = '';
        setTimeout(() => {
            if (this.liveRegion) {
                this.liveRegion.textContent = message;
            }
        }, 100);
    }

    /**
     * Announce urgent message to screen readers (assertive - interrupts)
     */
    announceUrgent(message: string): void {
        if (!this.assertiveRegion) return;

        this.assertiveRegion.textContent = '';
        setTimeout(() => {
            if (this.assertiveRegion) {
                this.assertiveRegion.textContent = message;
            }
        }, 100);
    }
}

/**
 * Focus management utilities
 */
export class FocusManager {
    private focusStack: HTMLElement[] = [];

    /**
     * Save current focus and optionally move to new element
     */
    saveFocus(newFocus?: HTMLElement): void {
        const currentFocus = document.activeElement as HTMLElement;
        if (currentFocus) {
            this.focusStack.push(currentFocus);
        }

        if (newFocus) {
            newFocus.focus();
        }
    }

    /**
     * Restore previously saved focus
     */
    restoreFocus(): void {
        const previousFocus = this.focusStack.pop();
        if (previousFocus && previousFocus.focus) {
            previousFocus.focus();
        }
    }

    /**
     * Trap focus within a container (for modals/panels)
     */
    trapFocus(container: HTMLElement): () => void {
        const focusableElements = container.querySelectorAll<HTMLElement>(
            'a[href], button:not([disabled]), textarea:not([disabled]), input:not([disabled]), select:not([disabled]), [tabindex]:not([tabindex="-1"])'
        );

        if (focusableElements.length === 0) return () => {};

        const firstElement = focusableElements[0];
        const lastElement = focusableElements[focusableElements.length - 1];

        const handleKeyDown = (e: KeyboardEvent) => {
            if (e.key !== 'Tab') return;

            if (e.shiftKey) {
                // Shift + Tab
                if (document.activeElement === firstElement) {
                    e.preventDefault();
                    lastElement.focus();
                }
            } else {
                // Tab
                if (document.activeElement === lastElement) {
                    e.preventDefault();
                    firstElement.focus();
                }
            }
        };

        container.addEventListener('keydown', handleKeyDown);

        // Return cleanup function
        return () => {
            container.removeEventListener('keydown', handleKeyDown);
        };
    }
}

/**
 * Skip link utility for keyboard navigation
 */
export function createSkipLink(targetId: string, text: string = 'Skip to main content'): HTMLElement {
    const skipLink = document.createElement('a');
    skipLink.href = `#${targetId}`;
    skipLink.className = 'skip-link';
    skipLink.textContent = text;
    skipLink.addEventListener('click', (e) => {
        e.preventDefault();
        const target = document.getElementById(targetId);
        if (target) {
            target.focus();
            target.scrollIntoView({ behavior: 'smooth' });
        }
    });
    return skipLink;
}

/**
 * Add keyboard shortcut with screen reader announcement
 */
export function addKeyboardShortcut(
    key: string,
    modifiers: string[],
    action: () => void,
    description: string
): void {
    document.addEventListener('keydown', (e: KeyboardEvent) => {
        const modifiersMatch =
            (!modifiers.includes('ctrl') || e.ctrlKey) &&
            (!modifiers.includes('alt') || e.altKey) &&
            (!modifiers.includes('shift') || e.shiftKey) &&
            (!modifiers.includes('meta') || e.metaKey);

        if (e.key.toLowerCase() === key.toLowerCase() && modifiersMatch) {
            e.preventDefault();
            action();
            announcer.announce(description);
        }
    });
}

// Export singleton instance
export const announcer = new ScreenReaderAnnouncer();
export const focusManager = new FocusManager();

// Initialize on DOM ready
if (document.readyState === 'loading') {
    document.addEventListener('DOMContentLoaded', () => {
        // Add CSS for screen reader only content
        if (!document.getElementById('sr-styles')) {
            const style = document.createElement('style');
            style.id = 'sr-styles';
            style.textContent = `
                .sr-only {
                    position: absolute;
                    width: 1px;
                    height: 1px;
                    padding: 0;
                    margin: -1px;
                    overflow: hidden;
                    clip: rect(0, 0, 0, 0);
                    white-space: nowrap;
                    border: 0;
                }

                .skip-link {
                    position: absolute;
                    top: -40px;
                    left: 0;
                    background: #000;
                    color: #fff;
                    padding: 8px;
                    text-decoration: none;
                    z-index: 10000;
                }

                .skip-link:focus {
                    top: 0;
                }
            `;
            document.head.appendChild(style);
        }
    });
}