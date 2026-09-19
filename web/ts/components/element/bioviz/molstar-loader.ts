/**
 * Shared Mol* loader — lazy-loads Mol* Viewer v4 from CDN once,
 * reused by alphafold-viewer.ts and pdb-viewer.ts.
 */

let loaded = false;
let loading: Promise<void> | null = null;

export function ensureMolstar(): Promise<void> {
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

export interface MolstarViewer {
    loadStructureFromUrl(url: string, format: string): Promise<void>;
    loadStructureFromData(data: string, format: string): Promise<void>;
    plugin: { canvas3d: { setProps(p: unknown): void } | undefined } | undefined;
}

const VIEWER_OPTIONS = {
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
};

const BG_COLOR = 0x273235;

export async function createMolstarViewer(element: HTMLElement): Promise<MolstarViewer> {
    await ensureMolstar();
    const molstar = (window as unknown as Record<string, unknown>).molstar as Record<string, unknown>;
    if (!molstar) throw new Error('molstar not on window');

    const Viewer = molstar.Viewer as {
        create(el: HTMLElement, options: Record<string, unknown>): Promise<MolstarViewer>;
    };

    const viewer = await Viewer.create(element, VIEWER_OPTIONS);

    const plugin = (viewer as unknown as Record<string, unknown>).plugin as Record<string, unknown> | undefined;
    const canvas3d = plugin?.canvas3d as { setProps(p: unknown): void } | undefined;
    if (canvas3d) {
        canvas3d.setProps({
            renderer: { backgroundColor: BG_COLOR },
            camera: { helper: { axes: { name: 'off', params: {} } } },
        });
    }

    return viewer;
}
