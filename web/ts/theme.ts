/**
 * Theme system - Light/Dark mode toggle
 */

type Theme = 'light' | 'dark';

class ThemeManager {
    private currentTheme: Theme;
    private toggleButton: HTMLButtonElement | null = null;
    private iconSpan: HTMLSpanElement | null = null;

    constructor() {
        // Load saved theme or default to dark
        this.currentTheme = (localStorage.getItem('theme') as Theme) || 'dark';
        this.init();
    }

    private init(): void {
        // Set initial theme
        document.body.setAttribute('data-theme', this.currentTheme);

        // Setup toggle button
        this.toggleButton = document.getElementById('theme-toggle') as HTMLButtonElement;
        this.iconSpan = this.toggleButton?.querySelector('.theme-icon') as HTMLSpanElement;

        if (this.toggleButton) {
            this.updateIcon();
            this.toggleButton.addEventListener('click', () => this.toggle());
        }
    }

    private updateIcon(): void {
        if (!this.iconSpan) return;
        // Sun icon for light mode (click to get light), moon for dark mode (click to get dark)
        this.iconSpan.textContent = this.currentTheme === 'dark' ? '☀' : '🌙';
    }

    private toggle(): void {
        this.currentTheme = this.currentTheme === 'dark' ? 'light' : 'dark';
        document.body.setAttribute('data-theme', this.currentTheme);
        localStorage.setItem('theme', this.currentTheme);
        this.updateIcon();
    }

    public getTheme(): Theme {
        return this.currentTheme;
    }
}

// Initialize theme system
export const themeManager = new ThemeManager();
