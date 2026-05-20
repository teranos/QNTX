/**
 * AlphaFold 3D structure viewer — uses Mol* Viewer directly.
 * Renders inline in attestation glyphs when AlphaFold structure data is detected.
 */

import { preventDrag } from '@qntx/glyphs';

let loaded = false;
let loading: Promise<void> | null = null;

function ensureMolstar(): Promise<void> {
    if (loaded) return Promise.resolve();
    if (loading) return loading;
    loading = new Promise<void>((resolve, reject) => {
        const link = document.createElement('link');
        link.rel = 'stylesheet';
        link.href = 'https://cdn.jsdelivr.net/npm/molstar@4/build/viewer/molstar.css';
        document.head.appendChild(link);

        // Override Mol* canvas border
        const style = document.createElement('style');
        style.textContent = [
            '.msp-plugin, .msp-plugin *, .msp-layout-static, .msp-viewport-canvas { border: none !important; outline: none !important; box-shadow: none !important; }',
            '.msp-viewport-controls, .msp-highlight-info { display: none !important; }',
        ].join('\n');
        document.head.appendChild(style);

        const script = document.createElement('script');
        script.src = 'https://cdn.jsdelivr.net/npm/molstar@4/build/viewer/molstar.js';
        script.onload = () => { loaded = true; resolve(); };
        script.onerror = () => reject(new Error('Failed to load Mol*'));
        document.head.appendChild(script);
    });
    return loading;
}

/**
 * Build an AlphaFold 3D structure viewer element using Mol* directly.
 */
export function buildAlphaFoldViewer(structureId: string, _accession: string, cifUrl: string): HTMLElement {
    const wrapper = document.createElement('div');
    wrapper.style.width = '100%';
    wrapper.style.height = '180px';
    wrapper.style.marginBottom = '8px';
    wrapper.style.position = 'relative';
    wrapper.style.backgroundColor = '#273235';

    const viewerDiv = document.createElement('div');
    viewerDiv.id = `molstar-${structureId}-${Date.now()}`;
    viewerDiv.style.width = '100%';
    viewerDiv.style.height = '100%';
    preventDrag(viewerDiv);
    viewerDiv.addEventListener('wheel', (e) => e.stopPropagation(), { passive: false });
    wrapper.appendChild(viewerDiv);

    const placeholder = document.createElement('div');
    placeholder.style.position = 'absolute';
    placeholder.style.top = '8px';
    placeholder.style.left = '8px';
    placeholder.style.color = '#6b7175';
    placeholder.style.fontSize = '11px';
    placeholder.style.fontFamily = 'monospace';
    placeholder.textContent = `Loading ${structureId}...`;
    wrapper.appendChild(placeholder);

    ensureMolstar().then(async () => {
        const molstar = (window as unknown as Record<string, unknown>).molstar as Record<string, unknown>;
        if (!molstar) throw new Error('molstar not on window');

        const Viewer = molstar.Viewer as {
            create(el: HTMLElement, options: Record<string, unknown>): Promise<{
                loadStructureFromUrl(url: string, format: string): Promise<void>;
            }>;
        };

        const viewer = await Viewer.create(viewerDiv, {
            layoutIsExpanded: false,
            layoutShowControls: false,
            layoutShowRemoteState: false,
            layoutShowSequence: false,
            layoutShowLog: false,
            layoutShowLeftPanel: false,
            viewportShowExpand: false,
            viewportShowSelectionMode: false,
            viewportShowAnimation: false,
            viewportShowControls: false,
        });

        // Set dark background via canvas3d renderer
        const plugin = (viewer as unknown as Record<string, unknown>).plugin as Record<string, unknown> | undefined;
        const canvas3d = plugin?.canvas3d as { setProps(p: unknown): void } | undefined;
        if (canvas3d) {
            canvas3d.setProps({
                renderer: { backgroundColor: 0x273235 },
                camera: { helper: { axes: { name: 'off', params: {} } } },
            });
        }

        placeholder.remove();
        await viewer.loadStructureFromUrl(cifUrl, 'mmcif');
    }).catch(() => {
        placeholder.textContent = `Failed to load viewer for ${structureId}`;
    });

    return wrapper;
}
