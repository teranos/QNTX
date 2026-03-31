export const render = async (glyph, ui) => {
    const { element, content } = ui.glyph({
        defaults: {
            x: glyph.x ?? 200,
            y: glyph.y ?? 200,
            width: glyph.width ?? 504,
            height: glyph.height ?? 504,
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

    content.style.position = 'relative';

    // Projection mode state — initialized before status polling references it
    let currentProjection = 'pca';

    // Status label (bottom-left) — shows version + PCA readiness
    const statusLabel = document.createElement('div');
    statusLabel.style.cssText = 'position:absolute;bottom:4px;left:8px;font:10px monospace;color:rgba(255,255,255,0.3);';
    statusLabel.textContent = 'loading...';
    content.appendChild(statusLabel);

    let pcaReady = false;
    function pollStatus() {
        fetch('/api/llama-cpp/status').then(r => r.ok ? r.json() : null).then(s => {
            if (!s) return;
            if (s.state === 'computing_positions') {
                statusLabel.textContent = 'v' + s.version + ' computing positions\u2026';
                statusLabel.style.color = 'rgba(255,180,80,0.5)';
                setTimeout(pollStatus, 500);
            } else if (s.state === 'ready') {
                pcaReady = true;
                if (s.projection && s.projection !== currentProjection) {
                    currentProjection = s.projection;
                    if (typeof projBtn !== 'undefined') {
                        projBtn.textContent = s.projection.toUpperCase();
                        projBtn.style.color = s.projection === 'hyp' ? '#8cf' : '#c84';
                    }
                }
                if (s.activity) {
                    statusLabel.textContent = s.activity;
                    statusLabel.style.color = 'rgba(255,180,80,0.5)';
                    setTimeout(pollStatus, 300);
                } else {
                    statusLabel.textContent = 'v' + s.version;
                    statusLabel.style.color = 'rgba(255,255,255,0.2)';
                    setTimeout(pollStatus, 2000);
                }
            } else {
                statusLabel.textContent = 'v' + s.version + ' no model';
                statusLabel.style.color = 'rgba(255,255,255,0.2)';
                setTimeout(pollStatus, 2000);
            }
        }).catch(() => {
            statusLabel.textContent = 'offline';
            setTimeout(pollStatus, 2000);
        });
    }
    pollStatus();

    // Status indicator (bottom-right)
    const status = document.createElement('div');
    status.style.cssText = 'position:absolute;bottom:4px;right:8px;font:10px monospace;color:rgba(255,255,255,0.3);';
    status.textContent = 'connecting...';
    content.appendChild(status);

    let ws = null;
    let frameCount = 0;
    let fpsFrames = 0;
    let fpsLast = performance.now();
    let fpsDisplay = 0;

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
                    fpsFrames++;
                    const now = performance.now();
                    if (now - fpsLast >= 1000) {
                        fpsDisplay = Math.round(fpsFrames * 1000 / (now - fpsLast));
                        fpsFrames = 0;
                        fpsLast = now;
                        status.textContent = fpsDisplay + ' fps';
                    }
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

    // Projection mode toggle: PCA ↔ HYP (Poincaré ball)
    const projRow = document.createElement('div');
    projRow.style.cssText = 'display:flex;align-items:center;gap:6px;margin:6px 0 2px;';
    const projLabel = document.createElement('span');
    projLabel.style.cssText = 'width:80px;text-align:right;';
    projLabel.textContent = 'projection';
    const projBtn = document.createElement('button');
    projBtn.style.cssText = 'flex:1;height:22px;background:#222;border:1px solid #444;color:#c84;' +
        'font:10px monospace;cursor:pointer;border-radius:3px;';
    projBtn.textContent = 'PCA';
    projBtn.onclick = () => {
        const next = currentProjection === 'pca' ? 'hyp' : 'pca';
        fetch('/api/llama-cpp/projection', {
            method: 'POST',
            body: JSON.stringify({ mode: next }),
        }).then(r => r.ok ? r.json() : null).then(data => {
            if (data) {
                currentProjection = data.mode;
                projBtn.textContent = data.mode.toUpperCase();
                projBtn.style.color = data.mode === 'hyp' ? '#8cf' : '#c84';
            }
        });
    };
    projRow.appendChild(projLabel);
    projRow.appendChild(projBtn);
    controls.appendChild(projRow);

    content.appendChild(controls);

    canvas.addEventListener('contextmenu', (e) => {
        e.preventDefault();
        controls.style.display = controls.style.display === 'none' ? 'block' : 'none';
    });

    // WASD camera — hover over nebula to activate, no click/focus needed
    let nebulaHovered = false;
    content.addEventListener('mouseenter', () => { nebulaHovered = true; });
    content.addEventListener('mouseleave', () => { nebulaHovered = false; heldKeys.clear(); });

    const heldKeys = new Set();
    let camLoopId = null;

    function sendCam(dx, dy, dz) {
        if (ws && ws.readyState === WebSocket.OPEN) {
            const msg = { type: 1, data: btoa('cam:' + dx + ',' + dy + ',' + dz), headers: {}, timestamp: 0 };
            ws.send(JSON.stringify(msg));
        }
    }

    function sendCamFull(dx, dy, dz, dyaw, dpitch) {
        if (ws && ws.readyState === WebSocket.OPEN) {
            const msg = { type: 1, data: btoa('cam:' + dx + ',' + dy + ',' + dz + ',' + dyaw + ',' + dpitch), headers: {}, timestamp: 0 };
            ws.send(JSON.stringify(msg));
        }
    }

    function camLoop() {
        if (heldKeys.size === 0) { camLoopId = null; return; }
        let dx = 0, dy = 0, dz = 1.0, dyaw = 0, dpitch = 0;
        const panStep = 0.4;
        const rotStep = 0.03;
        if (heldKeys.has('a')) dx -= panStep;
        if (heldKeys.has('d')) dx += panStep;
        if (heldKeys.has('w')) dz = 1.04;
        if (heldKeys.has('s')) dz = 0.96;
        if (heldKeys.has('arrowleft')) dyaw += rotStep;
        if (heldKeys.has('arrowright')) dyaw -= rotStep;
        if (heldKeys.has('arrowup')) dpitch -= rotStep;
        if (heldKeys.has('arrowdown')) dpitch += rotStep;
        sendCamFull(dx, dy, dz, dyaw, dpitch);
        camLoopId = requestAnimationFrame(camLoop);
    }

    const camKeys = new Set(['w','a','s','d','arrowleft','arrowright','arrowup','arrowdown','escape','p']);

    function onDocKeyDown(e) {
        if (!nebulaHovered) return;
        const k = e.key.toLowerCase();
        if (k === 'escape') {
            if (ws && ws.readyState === WebSocket.OPEN) {
                const msg = { type: 1, data: btoa('cam:r'), headers: {}, timestamp: 0 };
                ws.send(JSON.stringify(msg));
            }
            return;
        }
        if (k === 'p') {
            e.preventDefault();
            projBtn.onclick();
            return;
        }
        if (!camKeys.has(k)) return;
        e.preventDefault();
        if (heldKeys.has(k)) return;
        heldKeys.add(k);
        if (!camLoopId) camLoopId = requestAnimationFrame(camLoop);
    }

    function onDocKeyUp(e) {
        heldKeys.delete(e.key.toLowerCase());
    }

    document.addEventListener('keydown', onDocKeyDown);
    document.addEventListener('keyup', onDocKeyUp);

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
        document.removeEventListener('keydown', onDocKeyDown);
        document.removeEventListener('keyup', onDocKeyUp);
        ro.disconnect();
        if (ws && ws.readyState !== WebSocket.CLOSED) ws.close();
    });

    return element;
};
