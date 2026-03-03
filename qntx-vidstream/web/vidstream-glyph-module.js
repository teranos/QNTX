// qntx-vidstream/web/vidstream-glyph-module.ts
// VidStream glyph — real-time webcam + ONNX inference on the canvas.
// Uses HTTP endpoints via pluginFetch (replaces the old WebSocket-based VidStreamWindow).

var render = async (glyph, ui) => {
  const { element } = ui.container({
    defaults: {
      x: glyph.x ?? 200,
      y: glyph.y ?? 200,
      width: 680,
      height: 620,
    },
    titleBar: { label: 'VidStream' },
    resizable: true,
  });

  // State
  let engineReady = false;
  let stream = null;
  let video = null;
  let canvas = null;
  let ctx = null;
  let animFrameId = null;
  let isProcessing = false;
  let latestDetections = [];
  let lastInferenceTime = 0;
  const INFERENCE_INTERVAL_MS = 200; // 5 FPS max

  // Stats
  let frameCount = 0;
  let lastStatsUpdate = 0;
  let currentFPS = 0;
  let avgLatency = 0;

  // Build UI
  const content = document.createElement('div');
  content.style.flex = '1';
  content.style.overflow = 'hidden';
  content.style.display = 'flex';
  content.style.flexDirection = 'column';
  content.style.gap = '6px';
  content.style.padding = '8px';
  content.style.fontFamily = 'monospace';
  content.style.fontSize = '12px';

  // Controls
  const controlRow = document.createElement('div');
  controlRow.style.display = 'flex';
  controlRow.style.gap = '4px';
  controlRow.style.alignItems = 'center';
  controlRow.style.flexShrink = '0';

  const modelInput = ui.input({
    label: 'Model',
    value: 'ats/vidstream/models/yolo11n.onnx',
    placeholder: 'Path to ONNX model',
  });
  modelInput.style.flex = '1';
  ui.preventDrag(modelInput);

  const initBtn = ui.button({
    label: 'Initialize ONNX',
    onClick: () => initEngine(),
  });

  const startBtn = ui.button({
    label: 'Start Camera',
    primary: true,
    onClick: () => startCamera(),
  });

  const stopBtn = ui.button({
    label: 'Stop',
    onClick: () => stopCamera(),
  });
  stopBtn.style.display = 'none';

  controlRow.appendChild(modelInput);
  controlRow.appendChild(initBtn);
  controlRow.appendChild(startBtn);
  controlRow.appendChild(stopBtn);

  // Status line
  const status = ui.statusLine();

  // Viewport (video + canvas overlay)
  const viewport = document.createElement('div');
  viewport.style.position = 'relative';
  viewport.style.flex = '1';
  viewport.style.minHeight = '0';
  viewport.style.background = '#000';
  viewport.style.borderRadius = '3px';
  viewport.style.overflow = 'hidden';

  video = document.createElement('video');
  video.autoplay = true;
  video.playsInline = true;
  video.muted = true;
  video.style.width = '100%';
  video.style.height = '100%';
  video.style.objectFit = 'contain';
  video.style.display = 'block';

  canvas = document.createElement('canvas');
  canvas.width = 640;
  canvas.height = 480;
  canvas.style.position = 'absolute';
  canvas.style.top = '0';
  canvas.style.left = '0';
  canvas.style.width = '100%';
  canvas.style.height = '100%';
  canvas.style.pointerEvents = 'none';
  ctx = canvas.getContext('2d');

  viewport.appendChild(video);
  viewport.appendChild(canvas);

  // Stats bar
  const statsBar = document.createElement('div');
  statsBar.style.display = 'flex';
  statsBar.style.gap = '12px';
  statsBar.style.fontSize = '11px';
  statsBar.style.color = 'var(--muted-foreground, #888)';
  statsBar.style.flexShrink = '0';

  const fpsEl = document.createElement('span');
  fpsEl.textContent = 'FPS: 0';
  const latencyEl = document.createElement('span');
  latencyEl.textContent = 'Latency: -';
  const detectEl = document.createElement('span');
  detectEl.textContent = 'Detections: 0';

  statsBar.appendChild(fpsEl);
  statsBar.appendChild(latencyEl);
  statsBar.appendChild(detectEl);

  content.appendChild(controlRow);
  content.appendChild(status.element);
  content.appendChild(viewport);
  content.appendChild(statsBar);
  element.appendChild(content);

  // Load persisted config
  const config = await ui.loadConfig();
  if (config && config.model_path) {
    const input = modelInput.querySelector('input');
    if (input) input.value = config.model_path;
  }

  // Engine initialization
  async function initEngine() {
    const input = modelInput.querySelector('input');
    const modelPath = input ? input.value.trim() : '';
    if (!modelPath) {
      status.show('Model path is required', true);
      return;
    }

    initBtn.disabled = true;
    initBtn.textContent = 'Initializing...';
    status.show('Initializing ONNX engine...');

    try {
      const resp = await ui.pluginFetch('/init', {
        method: 'POST',
        body: {
          model_path: modelPath,
          confidence_threshold: 0.5,
          nms_threshold: 0.45,
        },
      });

      if (resp.ok) {
        const data = await resp.json();
        engineReady = true;
        status.show('Engine ready (' + data.width + 'x' + data.height + ')');
        ui.log.info('ONNX engine initialized: ' + modelPath);

        // Persist model path
        ui.saveConfig({ model_path: modelPath });
      } else {
        const body = await resp.json().catch(() => ({}));
        status.show(body.error || 'Init failed', true);
        ui.log.error('Engine init failed: ' + (body.error || resp.status));
      }
    } catch (e) {
      status.show(e.message, true);
      ui.log.error('Engine init error: ' + e.message);
    } finally {
      initBtn.disabled = false;
      initBtn.textContent = 'Initialize ONNX';
    }
  }

  // Camera
  async function startCamera() {
    try {
      stream = await navigator.mediaDevices.getUserMedia({
        video: { width: 640, height: 480 },
        audio: false,
      });

      video.srcObject = stream;
      video.onloadedmetadata = () => {
        video.play();
        isProcessing = true;
        lastStatsUpdate = Date.now();
        processFrame();
      };

      startBtn.style.display = 'none';
      stopBtn.style.display = '';
      const mode = engineReady ? 'with inference' : 'preview only';
      status.show('Camera started (' + mode + ')');
      ui.log.info('Camera started (' + mode + ')');
    } catch (e) {
      status.show('Camera error: ' + e.message, true);
      ui.log.error('Camera start failed: ' + e.message);
    }
  }

  function stopCamera() {
    isProcessing = false;
    if (animFrameId) {
      cancelAnimationFrame(animFrameId);
      animFrameId = null;
    }
    if (stream) {
      stream.getTracks().forEach((t) => t.stop());
      stream = null;
    }
    if (video) video.srcObject = null;
    latestDetections = [];

    stopBtn.style.display = 'none';
    startBtn.style.display = '';
    status.show('Camera stopped');
    ui.log.info('Camera stopped');
  }

  // Frame processing loop
  function processFrame() {
    if (!isProcessing || !canvas || !ctx) return;

    // Draw video to canvas (for detection overlay)
    ctx.clearRect(0, 0, canvas.width, canvas.height);

    if (video && video.readyState >= 2) {
      ctx.drawImage(video, 0, 0, canvas.width, canvas.height);
      drawDetections();

      if (engineReady) {
        const now = performance.now();
        if (now - lastInferenceTime >= INFERENCE_INTERVAL_MS) {
          lastInferenceTime = now;
          runInference();
        }
      } else {
        updatePreviewStats();
      }
    }

    animFrameId = requestAnimationFrame(processFrame);
  }

  // Send frame to plugin for inference via JSON POST
  async function runInference() {
    if (!ctx || !canvas) return;

    const imageData = ctx.getImageData(0, 0, canvas.width, canvas.height);

    try {
      const resp = await ui.pluginFetch('/frame', {
        method: 'POST',
        body: {
          frame_data: Array.from(imageData.data),
          width: canvas.width,
          height: canvas.height,
          format: 'rgba8',
        },
      });

      if (resp.ok) {
        const data = await resp.json();
        latestDetections = data.detections || [];
        const totalMs = data.stats.total_us / 1000;
        updateInferenceStats(latestDetections.length, totalMs);
      }
    } catch (e) {
      // Silent — don't spam errors during frame loop
      ui.log.debug('Frame error: ' + e.message);
    }
  }

  // Draw bounding boxes
  function drawDetections() {
    if (!ctx || !latestDetections.length) return;

    for (const det of latestDetections) {
      const { X: x, Y: y, Width: w, Height: h } = det.BBox;

      ctx.strokeStyle = '#0f0';
      ctx.lineWidth = 2;
      ctx.strokeRect(x, y, w, h);

      const label = det.Label + ' ' + (det.Confidence * 100).toFixed(0) + '%';
      ctx.font = '12px monospace';
      const metrics = ctx.measureText(label);
      ctx.fillStyle = 'rgba(0, 255, 0, 0.8)';
      ctx.fillRect(x, y - 18, metrics.width + 6, 18);
      ctx.fillStyle = '#000';
      ctx.fillText(label, x + 3, y - 5);
    }
  }

  // Stats updates
  function updateInferenceStats(detectionCount, latencyMs) {
    frameCount++;
    const now = Date.now();
    if (now - lastStatsUpdate >= 1000) {
      currentFPS = frameCount;
      avgLatency = latencyMs;
      frameCount = 0;
      lastStatsUpdate = now;

      fpsEl.textContent = 'FPS: ' + currentFPS;
      latencyEl.textContent = 'Latency: ' + avgLatency.toFixed(1) + ' ms';
      detectEl.textContent = 'Detections: ' + detectionCount;
    }
  }

  function updatePreviewStats() {
    frameCount++;
    const now = Date.now();
    if (now - lastStatsUpdate >= 1000) {
      currentFPS = frameCount;
      frameCount = 0;
      lastStatsUpdate = now;

      fpsEl.textContent = 'FPS: ' + currentFPS;
      latencyEl.textContent = 'Latency: -';
      detectEl.textContent = 'Detections: -';
    }
  }

  // Cleanup on glyph close
  ui.onCleanup(() => {
    stopCamera();
  });

  return element;
};

export { render };
