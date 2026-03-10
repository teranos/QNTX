/**
 * Title Bar Controls — shared helpers for adding/removing window controls
 * (minimize, close) to any glyph title bar.
 *
 * Manifestations call addWindowControls() when morphing to window/panel,
 * and removeWindowControls() when returning to canvas-placed.
 */

const WINDOW_CONTROLS_CLASS = 'glyph-window-controls';

export interface WindowControlsConfig {
    onMinimize: () => void;
    onClose?: () => void;
}

/** Append minimize/close buttons to an existing glyph title bar. */
export function addWindowControls(titleBar: HTMLElement, config: WindowControlsConfig): void {
    // Guard against double-add
    if (titleBar.querySelector(`.${WINDOW_CONTROLS_CLASS}`)) return;

    const container = document.createElement('span');
    container.className = WINDOW_CONTROLS_CLASS;
    container.style.display = 'inline-flex';
    container.style.gap = '2px';
    container.style.flexShrink = '0';
    container.style.marginLeft = 'auto';

    const minimizeBtn = document.createElement('button');
    minimizeBtn.textContent = '\u2212'; // −
    minimizeBtn.title = 'Minimize';
    minimizeBtn.setAttribute('aria-label', 'Minimize');
    minimizeBtn.onclick = config.onMinimize;
    container.appendChild(minimizeBtn);

    if (config.onClose) {
        const closeBtn = document.createElement('button');
        closeBtn.textContent = '\u00d7'; // ×
        closeBtn.title = 'Close';
        closeBtn.setAttribute('aria-label', 'Close');
        closeBtn.onclick = config.onClose;
        container.appendChild(closeBtn);
    }

    titleBar.appendChild(container);
}

/** Remove window controls from a title bar (when returning to canvas-placed). */
export function removeWindowControls(titleBar: HTMLElement): void {
    const controls = titleBar.querySelector(`.${WINDOW_CONTROLS_CLASS}`);
    if (controls) controls.remove();
}
