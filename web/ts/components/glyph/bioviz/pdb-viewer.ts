/**
 * Inline PDB 3D structure viewer — renders raw PDB text via Mol*.
 * Detects ATOM/HETATM records in attribute values and shows a 3D card.
 */

import { preventDrag } from '@qntx/glyphs';
import { createMolstarViewer } from './molstar-loader';

/**
 * Detect whether a string value contains PDB coordinate data.
 * Looks for ATOM or HETATM records at line starts.
 */
export function isPdbData(value: string): boolean {
    if (value.length < 50) return false;
    const first = value.slice(0, 200);
    return first.startsWith('ATOM') || first.startsWith('HETATM') ||
           first.indexOf('\nATOM') !== -1 || first.indexOf('\nHETATM') !== -1;
}

export function buildPdbViewer(pdbData: string, label: string): HTMLElement {
    const wrapper = document.createElement('div');
    wrapper.style.width = '100%';
    wrapper.style.height = '200px';
    wrapper.style.marginBottom = '8px';
    wrapper.style.position = 'relative';
    wrapper.style.backgroundColor = '#273235';
    wrapper.style.borderRadius = '4px';
    wrapper.style.overflow = 'hidden';

    const viewerDiv = document.createElement('div');
    viewerDiv.id = `molstar-pdb-${Date.now()}`;
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
    placeholder.textContent = `Loading ${label}...`;
    wrapper.appendChild(placeholder);

    createMolstarViewer(viewerDiv).then(async (viewer) => {
        placeholder.remove();
        await viewer.loadStructureFromData(pdbData, 'pdb');
    }).catch(() => {
        placeholder.textContent = `Failed to load 3D viewer`;
    });

    return wrapper;
}
