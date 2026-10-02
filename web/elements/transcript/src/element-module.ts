// The transcript element: one session Ground recorded, read as what was said
// and done. Published as an attestation (docs/elements/publishing.md).

import { mount, unmount } from 'svelte';
import App from './App.svelte';

export const elementDef = {
    symbol: '🧵',
    title: 'TRANSCRIPT',
    label: 'transcript',
    defaultWidth: 520,
    defaultHeight: 640,
};

// eslint-disable-next-line @typescript-eslint/no-explicit-any
export const render = (item: any, ui: any): HTMLElement => {
    const { element, content } = ui.element({
        defaults: { x: item.x ?? 200, y: item.y ?? 200, width: elementDef.defaultWidth, height: elementDef.defaultHeight },
        titleBar: { label: elementDef.title },
        resizable: true,
    });
    content.style.padding = '0';
    content.style.overflow = 'hidden';
    // A turn is pressed, not dragged: the title bar is what moves the element.
    ui.preventDrag(content);
    const app = mount(App, { target: content, props: { ui } });
    ui.onCleanup(() => unmount(app));
    return element;
};
