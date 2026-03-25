/// Glyph module source served via HandleHTTP at /viz-module.js.
/// The frontend dynamically imports this module for the Metal Visualizer glyph.
let glyphModuleSource = """
export function render(glyph, ui) {
    const container = document.createElement('div');
    container.style.cssText = 'width: 100%; height: 100%; display: flex; flex-direction: column; background: var(--surface-0); color: var(--text-primary);';

    const canvas = document.createElement('canvas');
    canvas.width = 800;
    canvas.height = 600;
    canvas.style.cssText = 'flex: 1; width: 100%; image-rendering: pixelated;';

    const status = document.createElement('div');
    status.style.cssText = 'padding: 8px; font-family: var(--font-mono); font-size: 12px; opacity: 0.7;';
    status.textContent = 'Metal Visualizer — awaiting data';

    container.appendChild(canvas);
    container.appendChild(status);

    // Fetch rendered frame from plugin backend
    async function fetchRender(payload) {
        try {
            const resp = await fetch('/api/swift-metal/render', {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify(payload),
            });
            if (!resp.ok) {
                const err = await resp.json();
                status.textContent = 'Render error: ' + (err.error || resp.statusText);
                return;
            }
            const blob = await resp.blob();
            const url = URL.createObjectURL(blob);
            const img = new Image();
            img.onload = () => {
                const ctx = canvas.getContext('2d');
                ctx.drawImage(img, 0, 0, canvas.width, canvas.height);
                URL.revokeObjectURL(url);
                status.textContent = 'Rendered via Metal (' + canvas.width + 'x' + canvas.height + ')';
            };
            img.src = url;
        } catch (e) {
            status.textContent = 'Connection error: ' + e.message;
        }
    }

    return container;
}
"""
