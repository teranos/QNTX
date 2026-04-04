/**
 * Nebula navigation — keyboard camera controls, pick hover, dim logic,
 * and token stepping for the Metal particle renderer.
 *
 * Extracted from response-glyph.ts to keep the glyph factory focused on
 * lifecycle and DOM concerns.
 */

export interface NebulaNavConfig {
    element: HTMLElement;
    output: HTMLElement;
    canvas: HTMLCanvasElement;
    ctx: CanvasRenderingContext2D;
    sendMessage: (data: string) => void;
}

export interface NebulaNavHandle {
    setNavActive(active: boolean): void;
    setExamine(examine: boolean): void;
    setSelectedSpan(span: HTMLElement | null): void;
    setTokenNav(nav: { unlock: () => void; navigate: (dir: number) => void }): void;
    handlePickResponse(data: string): void;
    applyDim(): void;
    clearDim(): void;
    destroy(): void;
}

export function createNebulaNav(config: NebulaNavConfig): NebulaNavHandle {
    const { element, output, canvas, ctx, sendMessage } = config;

    let navActive = false;
    let examine = false;
    let selectedSpan: HTMLElement | null = null;
    let tokenNav: { unlock: () => void; navigate: (dir: number) => void } | null = null;
    let scrubActive = false;

    // ── Camera bindings ─────────────────────────────────────────────

    const camStep = 0.02;
    const camRotStep = 0.03;
    const bindings: { key: string; display: string; cmd: string; label: string }[] = [
        { key: 'w',          display: 'W',  cmd: `cam:0,0,${1 - camStep},0,0`,   label: 'forward' },
        { key: 's',          display: 'S',  cmd: `cam:0,0,${1 + camStep},0,0`,   label: 'backward' },
        { key: 'a',          display: 'A',  cmd: `cam:${camStep},0,1,0,0`,       label: 'strafe left' },
        { key: 'd',          display: 'D',  cmd: `cam:${-camStep},0,1,0,0`,      label: 'strafe right' },
        { key: 'ArrowUp',    display: '\u2191', cmd: `cam:0,0,1,0,${camRotStep}`,    label: 'look up' },
        { key: 'ArrowDown',  display: '\u2193', cmd: `cam:0,0,1,0,${-camRotStep}`,   label: 'look down' },
        { key: 'ArrowLeft',  display: '\u2190', cmd: `cam:0,0,1,${camRotStep},0`,    label: 'look left' },
        { key: 'ArrowRight', display: '\u2192', cmd: `cam:0,0,1,${-camRotStep},0`,   label: 'look right' },
        { key: 'q',          display: 'Q',  cmd: `cam:0,${camStep},1,0,0`,       label: 'ascend' },
        { key: 'e',          display: 'E',  cmd: `cam:0,${-camStep},1,0,0`,      label: 'descend' },
        { key: 'r',          display: 'R',  cmd: 'cam:r',                        label: 'reset camera' },
        { key: '[',          display: '[',  cmd: '',                             label: 'prev token' },
        { key: ']',          display: ']',  cmd: '',                             label: 'next token' },
        { key: ',',          display: ',',  cmd: '',                             label: 'prev candidate' },
        { key: '.',          display: '.',  cmd: '',                             label: 'next candidate' },
    ];
    const keyMap: Record<string, string> = {};
    for (const b of bindings) keyMap[b.key] = b.cmd;

    // ── Help overlay ────────────────────────────────────────────────

    const helpOverlay = document.createElement('div');
    helpOverlay.style.cssText = 'position:absolute;bottom:4px;left:6px;font:10px monospace;color:rgba(255,255,255,0.5);z-index:2;line-height:1.6;display:none;background:rgba(0,0,0,0.5);padding:4px 8px;border-radius:3px;';
    let helpVisible = false;

    function buildHelp(): void {
        helpOverlay.textContent = '';
        for (const b of bindings) {
            const line = document.createElement('div');
            const keyEl = document.createElement('span');
            keyEl.style.cssText = 'display:inline-block;min-width:18px;color:rgba(255,255,255,0.7);';
            keyEl.textContent = b.display;
            line.appendChild(keyEl);
            line.appendChild(document.createTextNode(' ' + b.label));
            helpOverlay.appendChild(line);
        }
        const escLine = document.createElement('div');
        const escKey = document.createElement('span');
        escKey.style.cssText = 'display:inline-block;min-width:18px;color:rgba(255,255,255,0.7);';
        escKey.textContent = 'Esc';
        escLine.appendChild(escKey);
        escLine.appendChild(document.createTextNode(' exit navigation'));
        helpOverlay.appendChild(escLine);
        const helpLine = document.createElement('div');
        const helpKey = document.createElement('span');
        helpKey.style.cssText = 'display:inline-block;min-width:18px;color:rgba(255,255,255,0.7);';
        helpKey.textContent = '?';
        helpLine.appendChild(helpKey);
        helpLine.appendChild(document.createTextNode(' toggle this help'));
        helpOverlay.appendChild(helpLine);
    }
    element.appendChild(helpOverlay);

    // ── Key handler ─────────────────────────────────────────────────

    function onKey(e: KeyboardEvent): void {
        if (e.target instanceof HTMLTextAreaElement || e.target instanceof HTMLInputElement) return;
        if (e.key === 'Escape' && tokenNav) {
            e.preventDefault();
            tokenNav.unlock();
            return;
        }
        if (!navActive) return;
        if (e.key === '[' || e.key === ']') {
            e.preventDefault();
            if (tokenNav) tokenNav.navigate(e.key === '[' ? -1 : 1);
            return;
        }
        if ((e.key === ',' || e.key === '.') && examine) {
            e.preventDefault();
            sendMessage('nav:' + e.key);
            return;
        }
        if (e.key === '?') {
            e.preventDefault();
            helpVisible = !helpVisible;
            if (helpVisible) { buildHelp(); helpOverlay.style.display = ''; }
            else helpOverlay.style.display = 'none';
            return;
        }
        const cmd = keyMap[e.key];
        if (cmd) {
            e.preventDefault();
            sendMessage(cmd);
        }
    }
    document.addEventListener('keydown', onKey);

    // ── Pick hover ──────────────────────────────────────────────────

    let pickedSpanHighlight: HTMLElement | null = null;

    function handlePickResponse(data: string): void {
        const comma = data.indexOf(',');
        if (comma < 0) return;
        const tokenId = parseInt(data.substring(0, comma), 10);
        const tokenText = data.substring(comma + 1);

        if (pickedSpanHighlight) {
            pickedSpanHighlight.style.outline = '';
            pickedSpanHighlight = null;
        }

        if (tokenId < 0 || !tokenText) return;

        const spans = output.querySelectorAll('span[data-token-index]');
        for (const span of spans) {
            const el = span as HTMLElement;
            if (el.textContent === tokenText) {
                el.style.outline = '1px solid cyan';
                pickedSpanHighlight = el;
                break;
            }
        }
    }

    function clearPick(): void {
        if (pickedSpanHighlight) {
            pickedSpanHighlight.style.outline = '';
            pickedSpanHighlight = null;
        }
        sendMessage('mouse:-1,-1');
    }

    function onMouseMove(e: MouseEvent): void {
        if (!navActive) return;
        const rect = element.getBoundingClientRect();
        const nx = Math.round((e.clientX - rect.left) / rect.width * 1000);
        const ny = Math.round((e.clientY - rect.top) / rect.height * 1000);
        sendMessage('mouse:' + nx + ',' + ny);
    }

    function onMouseLeave(): void {
        clearPick();
    }

    element.addEventListener('mousemove', onMouseMove);
    element.addEventListener('mouseleave', onMouseLeave);

    // ── Dim logic ───────────────────────────────────────────────────

    function applyDim(): void {
        if (examine) { applyExamineDim(); return; }
        if (!scrubActive || !selectedSpan) return;
        const canvasRect = canvas.getBoundingClientRect();
        const imgData = ctx.getImageData(0, 0, canvas.width, canvas.height);
        const pixels = imgData.data;
        const scaleX = canvas.width / canvasRect.width;
        const scaleY = canvas.height / canvasRect.height;

        const w = canvas.width;
        const h = canvas.height;

        function sampleBrightness(px: number, py: number): number {
            const x = Math.round(px);
            const y = Math.round(py);
            if (x < 0 || y < 0 || x >= w || y >= h) return 0;
            const idx = (y * w + x) * 4;
            return (pixels[idx] + pixels[idx + 1] + pixels[idx + 2]) / (3 * 255);
        }

        const spans = output.querySelectorAll('span[data-confidence]');
        for (const span of spans) {
            const rect = (span as HTMLElement).getBoundingClientRect();
            const l = (rect.left - canvasRect.left) * scaleX;
            const r = (rect.right - canvasRect.left) * scaleX;
            const t = (rect.top - canvasRect.top) * scaleY;
            const b = (rect.bottom - canvasRect.top) * scaleY;
            const mx = (l + r) / 2;
            const my = (t + b) / 2;

            const brightness = Math.max(
                sampleBrightness(mx, my),
                sampleBrightness(l, my), sampleBrightness(r, my),
                sampleBrightness(mx, t), sampleBrightness(mx, b),
                sampleBrightness(l, t), sampleBrightness(r, t),
                sampleBrightness(l, b), sampleBrightness(r, b),
            );
            const base = (span === selectedSpan) ? 1.0 : 0.8;
            (span as HTMLElement).dataset.dimOpacity = String(Math.max(0.08, base - brightness * 1.4));
        }

        const allSpans = Array.from(spans) as HTMLElement[];
        for (let i = 1; i < allSpans.length - 1; i++) {
            const self = parseFloat(allSpans[i].dataset.dimOpacity || '1');
            const prev = parseFloat(allSpans[i - 1].dataset.dimOpacity || '1');
            const next = parseFloat(allSpans[i + 1].dataset.dimOpacity || '1');
            if (self - prev > 0.2 && self - next > 0.2) {
                allSpans[i].dataset.dimOpacity = String((prev + next) / 2);
            }
        }

        for (const span of allSpans) {
            span.style.opacity = span.dataset.dimOpacity || '';
        }
    }

    function applyExamineDim(): void {
        if (!selectedSpan) return;
        const spans = Array.from(output.querySelectorAll('span[data-confidence]')) as HTMLElement[];
        const selectedIdx = spans.indexOf(selectedSpan);
        if (selectedIdx < 0) return;

        let sentStart = 0;
        let sentEnd = spans.length - 1;
        for (let i = selectedIdx - 1; i >= 0; i--) {
            const text = spans[i].textContent || '';
            if (text.indexOf('.') >= 0 || text.indexOf('!') >= 0 || text.indexOf('?') >= 0 || text.indexOf('\n') >= 0) {
                sentStart = i + 1;
                break;
            }
        }
        for (let i = selectedIdx; i < spans.length; i++) {
            const text = spans[i].textContent || '';
            if (text.indexOf('.') >= 0 || text.indexOf('!') >= 0 || text.indexOf('?') >= 0 || text.indexOf('\n') >= 0) {
                sentEnd = i;
                break;
            }
        }

        for (let i = 0; i < spans.length; i++) {
            if (i === selectedIdx) {
                spans[i].style.opacity = '1';
            } else if (i >= sentStart && i <= sentEnd) {
                spans[i].style.opacity = '0.6';
            } else if (Math.abs(i - selectedIdx) <= 5) {
                spans[i].style.opacity = '0.25';
            } else {
                spans[i].style.opacity = '0.04';
            }
        }
    }

    function clearDim(): void {
        examine = false;
        const spans = output.querySelectorAll('span[data-confidence]');
        for (const span of spans) {
            (span as HTMLElement).style.opacity = '';
        }
    }

    // ── Public interface ────────────────────────────────────────────

    return {
        setNavActive(active: boolean) { navActive = active; },
        setExamine(ex: boolean) { examine = ex; },
        setSelectedSpan(span: HTMLElement | null) { selectedSpan = span; },
        setTokenNav(nav) { tokenNav = nav; },
        handlePickResponse,
        applyDim() {
            scrubActive = true;
            applyDim();
        },
        clearDim,
        destroy() {
            document.removeEventListener('keydown', onKey);
            element.removeEventListener('mousemove', onMouseMove);
            element.removeEventListener('mouseleave', onMouseLeave);
            clearPick();
            if (helpOverlay.parentNode) helpOverlay.remove();
        },
    };
}
