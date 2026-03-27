export const render = async (glyph, ui) => {
    const { element, content } = ui.glyph({
        defaults: {
            x: glyph.x ?? 200,
            y: glyph.y ?? 200,
            width: glyph.width ?? 420,
            height: glyph.height ?? 420,
        },
        titleBar: { label: 'Nebula' },
        resizable: { minWidth: 200, minHeight: 200 },
        className: 'canvas-nebula-glyph',
    });

    content.style.padding = '0';
    content.style.overflow = 'hidden';
    content.style.backgroundColor = '#050208';

    const canvas = document.createElement('canvas');
    canvas.style.width = '100%';
    canvas.style.height = '100%';
    canvas.style.display = 'block';
    content.appendChild(canvas);

    const ctx2d = canvas.getContext('2d');

    // Resize canvas to match container
    function fitCanvas() {
        const rect = content.getBoundingClientRect();
        canvas.width = Math.round(rect.width * devicePixelRatio);
        canvas.height = Math.round(rect.height * devicePixelRatio);
    }
    fitCanvas();
    const ro = new ResizeObserver(fitCanvas);
    ro.observe(content);

    // Status indicator
    const status = document.createElement('div');
    status.style.position = 'absolute';
    status.style.bottom = '4px';
    status.style.right = '8px';
    status.style.fontSize = '10px';
    status.style.fontFamily = 'monospace';
    status.style.color = 'rgba(255,255,255,0.3)';
    status.textContent = 'connecting...';
    content.style.position = 'relative';
    content.appendChild(status);

    let ws = null;
    let frameCount = 0;

    function connect() {
        ws = ui.pluginWebSocket();

        ws.onopen = () => {
            status.textContent = 'connected';
            ui.log.debug('Nebula WebSocket connected');
        };

        ws.onmessage = (event) => {
            try {
                const msg = JSON.parse(event.data);
                if (msg.type !== 1 || !msg.data) return;

                // Decode base64 PNG and draw on canvas
                const binary = atob(msg.data);
                const bytes = new Uint8Array(binary.length);
                for (let i = 0; i < binary.length; i++) {
                    bytes[i] = binary.charCodeAt(i);
                }
                const blob = new Blob([bytes], { type: 'image/png' });
                createImageBitmap(blob).then((bmp) => {
                    if (!ctx2d) return;
                    ctx2d.clearRect(0, 0, canvas.width, canvas.height);
                    ctx2d.drawImage(bmp, 0, 0, canvas.width, canvas.height);
                    bmp.close();
                    frameCount++;
                });
            } catch (e) {
                ui.log.error('Nebula frame error', e);
            }
        };

        ws.onerror = () => {
            status.textContent = 'error';
        };

        ws.onclose = () => {
            status.textContent = 'disconnected';
        };
    }

    connect();

    // Double-click canvas to reconnect (refresh after code changes)
    canvas.addEventListener('dblclick', () => {
        if (ws) ws.close();
        frameCount = 0;
        status.textContent = 'reconnecting...';
        setTimeout(connect, 300);
    });

    // Controls panel — toggle with right-click
    function sendParam(key, value) {
        if (ws && ws.readyState === WebSocket.OPEN) {
            const msg = { type: 1, data: btoa('param:' + key + ':' + value), headers: {}, timestamp: 0 };
            ws.send(JSON.stringify(msg));
        }
    }

    const controls = document.createElement('div');
    controls.style.cssText = 'position:absolute;top:4px;left:4px;right:4px;display:none;' +
        'background:rgba(0,0,0,0.8);padding:8px;border-radius:4px;font:10px monospace;color:#aaa;';

    function addSlider(label, key, min, max, step, initial) {
        const row = document.createElement('div');
        row.style.cssText = 'display:flex;align-items:center;gap:6px;margin:2px 0;';
        const lbl = document.createElement('span');
        lbl.style.cssText = 'width:80px;text-align:right;';
        lbl.textContent = label;
        const input = document.createElement('input');
        input.type = 'range';
        input.min = min; input.max = max; input.step = step; input.value = initial;
        input.style.cssText = 'flex:1;height:12px;accent-color:#c84;';
        const val = document.createElement('span');
        val.style.width = '40px';
        val.textContent = initial;
        input.oninput = () => { val.textContent = input.value; sendParam(key, input.value); };
        row.appendChild(lbl); row.appendChild(input); row.appendChild(val);
        controls.appendChild(row);
    }

    addSlider('orbit period', 'orbit_period', 64, 4096, 64, 1024);
    addSlider('orbit radius', 'orbit_radius', 0.5, 10, 0.5, 3);
    addSlider('particle size', 'particle_scale', 0.1, 5, 0.1, 1);
    content.appendChild(controls);

    canvas.addEventListener('contextmenu', (e) => {
        e.preventDefault();
        controls.style.display = controls.style.display === 'none' ? 'block' : 'none';
    });

    // Listen for nebula-scrub events from stream glyph token hover
    function onScrub(e) {
        if (ws && ws.readyState === WebSocket.OPEN) {
            const msg = { type: 1, data: btoa('scrub:' + e.detail.index), headers: {}, timestamp: 0 };
            ws.send(JSON.stringify(msg));
        }
    }
    document.addEventListener('nebula-scrub', onScrub);

    ui.onCleanup(() => {
        document.removeEventListener('nebula-scrub', onScrub);
        ro.disconnect();
        if (ws && ws.readyState !== WebSocket.CLOSED) ws.close();
    });

    return element;
};
