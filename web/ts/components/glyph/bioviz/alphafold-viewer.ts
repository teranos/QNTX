/**
 * AlphaFold 3D structure viewer — uses Mol* Viewer directly.
 * Renders inline in attestation glyphs when AlphaFold structure data is detected.
 */

import { preventDrag } from '@qntx/glyphs';
import { createMolstarViewer } from './molstar-loader';

/**
 * Build an AlphaFold 3D structure viewer element using Mol* directly.
 */
export function buildAlphaFoldViewer(structureId: string, _accession: string, cifUrl: string): HTMLElement {
    const wrapper = document.createElement('div');
    wrapper.style.width = '100%';
    wrapper.style.height = '144px';
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

    createMolstarViewer(viewerDiv).then(async (viewer) => {
        placeholder.remove();
        await viewer.loadStructureFromUrl(cifUrl, 'mmcif');
    }).catch((err: unknown) => {
        placeholder.textContent = `Failed to load viewer for ${structureId}: ${err}`;
    });

    return wrapper;
}
