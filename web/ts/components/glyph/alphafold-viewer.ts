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

        const script = document.createElement('script');
        script.src = 'https://cdn.jsdelivr.net/npm/molstar@4/build/viewer/molstar.js';
        script.onload = () => { loaded = true; resolve(); };
        script.onerror = () => reject(new Error('Failed to load Mol*'));
        document.head.appendChild(script);
    });
    return loading;
}

/**
 * Detect AlphaFold structure data in parsed attestation attributes.
 * Returns { structureId, accession, cifUrl } if found, null otherwise.
 */
export function detectAlphaFold(attrs: Record<string, unknown>): { structureId: string; accession: string; cifUrl: string } | null {
    let data: Record<string, unknown> = attrs;
    if (typeof attrs.response === 'string') {
        try {
            const parsed = JSON.parse(attrs.response);
            data = Array.isArray(parsed) ? parsed[0] : parsed;
        } catch { return null; }
    }
    if (!data) return null;

    const modelEntityId = data.modelEntityId;
    const accession = data.uniprotAccession;
    const cifUrl = data.cifUrl;
    if (typeof modelEntityId === 'string' && modelEntityId.startsWith('AF-') &&
        typeof accession === 'string' && typeof cifUrl === 'string') {
        return { structureId: modelEntityId, accession, cifUrl };
    }
    return null;
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
    wrapper.style.backgroundColor = '#1a1a1a';

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
            backgroundColor: 0x363638 as unknown as string,
        });

        placeholder.remove();
        await viewer.loadStructureFromUrl(cifUrl, 'mmcif');
    }).catch(() => {
        placeholder.textContent = `Failed to load viewer for ${structureId}`;
    });

    return wrapper;
}
