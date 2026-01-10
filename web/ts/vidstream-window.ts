/**
 * VidStreamWindow - Draggable video inference window
 *
 * Real-time webcam + ONNX inference in a draggable/resizable window.
 * Uses VID (⮀) segment for logging.
 *
 * CAMERA ACCESS:
 * Desktop: Uses CrabCamera 0.7.0 plugin (https://github.com/Michael-A-Kuykendall/crabcamera)
 * Workaround: CrabCamera lacks Tauri v2 ACL support - see issue #266
 * Temporary ACL bypass: web/src-tauri/capabilities/crabcamera-dev.json
 * TODO: Remove bypass once CrabCamera implements ACL (watch releases)
 */

import { debug, info, error, SEG } from './logger.ts';
import { sendMessage, registerHandler } from './websocket.ts';

interface VidStreamConfig {
    model_path: string;
    confidence_threshold?: number;
    nms_threshold?: number;
    input_width?: number;
    input_height?: number;
}

interface Detection {
    ClassID: number;
    Label: string;
    Confidence: number;
    BBox: {
        X: number;
        Y: number;
        Width: number;
        Height: number;
    };
}

export class VidStreamWindow {
    private window: HTMLElement | null = null;
    private video: HTMLVideoElement | null = null;
    private canvas: HTMLCanvasElement | null = null;
    private ctx: CanvasRenderingContext2D | null = null;
    private stream: MediaStream | null = null;
    private animationFrameId: number | null = null;
    private isProcessing: boolean = false;
    private engineReady: boolean = false;

    // Drag state
    private isDragging: boolean = false;
    private dragOffsetX: number = 0;
    private dragOffsetY: number = 0;

    // Stats tracking
    private frameCount: number = 0;
    private lastStatsUpdate: number = 0;
    private currentFPS: number = 0;
    private avgLatency: number = 0;

    // Frame throttling (limit inference rate to avoid WebSocket overload)
    private lastInferenceTime: number = 0;
    private readonly INFERENCE_INTERVAL_MS = 200; // 5 FPS max for inference

    // Latest detections (drawn on every frame)
    private latestDetections: Detection[] = [];

    constructor() {
        this.createWindow();
        this.setupMessageHandlers();
    }

    private setupMessageHandlers(): void {
        // Handle engine initialization response
        registerHandler('vidstream_init_success', (data: any) => {
            this.engineReady = true;
            debug(SEG.VID, 'Engine ready:', data);
        });

        registerHandler('vidstream_init_error', (data: any) => {
            error(SEG.VID, 'Engine init error:', data.error);
            this.showError(data.error);
        });

        // Handle frame processing response
        registerHandler('vidstream_detections', (data: any) => {
            // Store detections to be drawn on next animation frame
            this.latestDetections = data.detections || [];
            const totalMs = data.stats.total_us / 1000;
            this.updateStatsFromServer(data.detections.length, totalMs);
        });

        registerHandler('vidstream_frame_error', (data: any) => {
            error(SEG.VID, 'Frame processing error:', data.error);
        });
    }

    private createWindow(): void {
        this.window = document.createElement('div');
        this.window.id = 'vidstream-window';
        this.window.className = 'draggable-window';
        this.window.style.width = '664px'; // 640px viewport + padding

        this.window.innerHTML = `
            <div class="draggable-window-header">
                <span class="draggable-window-title">VidStream</span>
                <button class="panel-close" aria-label="Close">&times;</button>
            </div>

            <div class="draggable-window-content">
                <input
                    type="text"
                    id="vs-model-path"
                    class="window-input"
                    value="ats/vidstream/models/yolo11n.onnx"
                    placeholder="path/to/model.onnx"
                />

                <div class="window-controls">
                    <button
                        id="vs-init-btn"
                        class="panel-btn panel-btn-sm"
                    >Initialize ONNX</button>
                    <button
                        id="vs-start-btn"
                        class="panel-btn panel-btn-sm panel-btn-primary"
                    >Start Camera</button>
                    <button id="vs-stop-btn" class="panel-btn panel-btn-sm" style="display: none;">Stop</button>
                    <span id="vs-status" class="window-status">Ready (camera + ONNX via WebSocket)</span>
                </div>

                <div id="vs-error" class="window-error" style="display: none;"></div>

                <div class="window-viewport">
                    <video
                        id="vs-video"
                        class="window-video"
                        autoplay
                        playsinline
                    ></video>
                    <canvas
                        id="vs-canvas"
                        class="window-canvas"
                        width="640"
                        height="480"
                    ></canvas>
                </div>

                <div class="window-stats">
                    <div>FPS: <span id="vs-fps" class="window-stat-value">0</span></div>
                    <div>Latency: <span id="vs-latency" class="window-stat-value">0</span> ms</div>
                    <div>Detections: <span id="vs-detections" class="window-stat-value">0</span></div>
                </div>
            </div>
        `;

        document.body.appendChild(this.window);
        this.setupEventListeners();
    }

    private setupEventListeners(): void {
        const header = this.window?.querySelector('.draggable-window-header') as HTMLElement;
        const closeBtn = this.window?.querySelector('.panel-close') as HTMLButtonElement;
        const initBtn = this.window?.querySelector('#vs-init-btn') as HTMLButtonElement;
        const startBtn = this.window?.querySelector('#vs-start-btn') as HTMLButtonElement;
        const stopBtn = this.window?.querySelector('#vs-stop-btn') as HTMLButtonElement;

        // Dragging
        header?.addEventListener('mousedown', (e) => {
            this.isDragging = true;
            const rect = this.window!.getBoundingClientRect();
            this.dragOffsetX = e.clientX - rect.left;
            this.dragOffsetY = e.clientY - rect.top;
            header.style.cursor = 'grabbing';
        });

        document.addEventListener('mousemove', (e) => {
            if (!this.isDragging) return;
            const x = e.clientX - this.dragOffsetX;
            const y = e.clientY - this.dragOffsetY;
            this.window!.style.left = `${x}px`;
            this.window!.style.top = `${y}px`;
        });

        document.addEventListener('mouseup', () => {
            if (this.isDragging) {
                this.isDragging = false;
                header.style.cursor = 'move';
            }
        });

        // Close
        closeBtn?.addEventListener('click', () => this.hide());

        // Initialize engine (optional - enables inference)
        initBtn?.addEventListener('click', async () => {
            const input = this.window?.querySelector('#vs-model-path') as HTMLInputElement;
            const modelPath = input.value.trim();
            if (!modelPath) return;

            try {
                initBtn.disabled = true;
                initBtn.textContent = 'Initializing...';
                await this.initializeEngine({ model_path: modelPath });
                const status = this.window?.querySelector('#vs-status') as HTMLElement;
                if (status) {
                    status.textContent = '✓ Inference ready';
                    status.style.color = '#0a0';
                }
                info(SEG.VID, 'VidStream ONNX engine initialized');
            } catch (err) {
                const status = this.window?.querySelector('#vs-status') as HTMLElement;
                if (status) {
                    status.textContent = `✗ Engine init failed`;
                    status.style.color = '#a00';
                }
                error(SEG.VID, 'ONNX engine initialization failed', err);
                this.showError(err instanceof Error ? err.message : String(err));
            } finally {
                initBtn.disabled = false;
                initBtn.textContent = 'Initialize ONNX';
            }
        });

        // Start/stop camera
        startBtn?.addEventListener('click', async () => {
            await this.startCamera();
            startBtn.style.display = 'none';
            stopBtn.style.display = 'inline-block';
        });

        stopBtn?.addEventListener('click', () => {
            this.stopCamera();
            stopBtn.style.display = 'none';
            startBtn.style.display = 'inline-block';
        });
    }

    private async initializeEngine(config: VidStreamConfig): Promise<void> {
        const sent = sendMessage({
            type: 'vidstream_init',
            model_path: config.model_path,
            confidence_threshold: config.confidence_threshold || 0.5,
            nms_threshold: config.nms_threshold || 0.45,
        });

        if (!sent) {
            throw new Error('WebSocket not connected');
        }

        debug(SEG.VID, 'Sent vidstream_init message');
    }

    private async startCamera(): Promise<void> {
        debug(SEG.VID, 'startCamera() called');

        this.canvas = this.window?.querySelector('#vs-canvas') as HTMLCanvasElement;
        this.ctx = this.canvas?.getContext('2d') || null;

        try {
            // Browser mode: Use MediaDevices API
            debug(SEG.VID, 'Using browser MediaDevices API');

            this.stream = await navigator.mediaDevices.getUserMedia({
                video: { width: 640, height: 480 },
                audio: false,
            });

            this.video = this.window?.querySelector('#vs-video') as HTMLVideoElement;
            if (this.video) {
                this.video.srcObject = this.stream;
                this.video.onloadedmetadata = () => {
                    this.video?.play();
                    this.startProcessing();
                };
            }

            const mode = this.engineReady ? 'with inference' : 'preview only';
            info(SEG.VID, `Camera started (${mode})`);
        } catch (err) {
            error(SEG.VID, 'Failed to start camera', err);
            this.showError(err instanceof Error ? err.message : String(err));
        }
    }

    private async stopCamera(): Promise<void> {
        if (this.animationFrameId) {
            cancelAnimationFrame(this.animationFrameId);
            this.animationFrameId = null;
        }

        this.isProcessing = false;

        // Browser: Stop MediaStream
        if (this.stream) {
            this.stream.getTracks().forEach(track => track.stop());
            this.stream = null;
        }

        if (this.video) {
            this.video.srcObject = null;
        }

        info(SEG.VID, 'Camera stopped');
    }

    private startProcessing(): void {
        this.isProcessing = true;
        this.lastStatsUpdate = Date.now();
        this.processFrame();
    }

    private async processFrame(): Promise<void> {
        if (!this.isProcessing || !this.canvas || !this.ctx) return;

        try {
            // Browser: Draw from video element
            if (!this.video) return;

            this.ctx.drawImage(this.video, 0, 0, this.canvas.width, this.canvas.height);

            // Draw latest detections on top of video frame
            this.drawDetections();

            if (this.engineReady) {
                await this.runInference();
            } else {
                this.updatePreviewStats();
            }

            this.animationFrameId = requestAnimationFrame(() => this.processFrame());
        } catch (err) {
            error(SEG.VID, 'Frame processing error', err);
            this.animationFrameId = requestAnimationFrame(() => this.processFrame());
        }
    }

    private async runInference(): Promise<void> {
        if (!this.ctx || !this.canvas) return;

        // Throttle inference to avoid overwhelming WebSocket with massive JSON payloads
        // 640x480 RGBA = 307KB per frame, JSON encoding = ~1MB per frame
        const now = performance.now();
        if (now - this.lastInferenceTime < this.INFERENCE_INTERVAL_MS) {
            return; // Skip this frame
        }
        this.lastInferenceTime = now;

        const imageData = this.ctx.getImageData(0, 0, this.canvas.width, this.canvas.height);

        try {
            const frameArray = Array.from(imageData.data);
            const payload = {
                type: 'vidstream_frame',
                frame_data: frameArray,
                width: this.canvas.width,
                height: this.canvas.height,
                format: 'rgba8',
            };

            // Log payload size (JSON encoding inflates size significantly)
            const payloadJSON = JSON.stringify(payload);
            const payloadSizeMB = payloadJSON.length / (1024 * 1024);
            debug(SEG.VID, `Frame payload: ${payloadSizeMB.toFixed(2)} MB (${payloadJSON.length} bytes)`);

            const sent = sendMessage(payload);

            if (!sent) {
                error(SEG.VID, 'Failed to send frame: WebSocket not connected');
            }
        } catch (err) {
            error(SEG.VID, 'Inference error', err);
        }
    }

    private drawDetections(): void {
        if (!this.ctx || !this.latestDetections.length) return;

        this.latestDetections.forEach(det => {
            const { X: x, Y: y, Width: width, Height: height } = det.BBox;

            // Bounding box
            this.ctx!.strokeStyle = '#0f0';
            this.ctx!.lineWidth = 2;
            this.ctx!.strokeRect(x, y, width, height);

            // Label
            const label = `${det.Label} ${(det.Confidence * 100).toFixed(0)}%`;
            this.ctx!.font = '12px monospace';
            const metrics = this.ctx!.measureText(label);
            this.ctx!.fillStyle = 'rgba(0, 255, 0, 0.8)';
            this.ctx!.fillRect(x, y - 18, metrics.width + 6, 18);
            this.ctx!.fillStyle = '#000';
            this.ctx!.fillText(label, x + 3, y - 5);
        });
    }

    private updateStatsFromServer(detectionCount: number, latencyMs: number): void {
        this.frameCount++;
        const now = Date.now();

        if (now - this.lastStatsUpdate >= 1000) {
            this.currentFPS = this.frameCount;
            this.avgLatency = latencyMs;
            this.frameCount = 0;
            this.lastStatsUpdate = now;

            const fpsEl = this.window?.querySelector('#vs-fps');
            const latencyEl = this.window?.querySelector('#vs-latency');
            const detectionsEl = this.window?.querySelector('#vs-detections');

            if (fpsEl) fpsEl.textContent = this.currentFPS.toString();
            if (latencyEl) latencyEl.textContent = this.avgLatency.toFixed(1);
            if (detectionsEl) detectionsEl.textContent = detectionCount.toString();
        }
    }

    private updatePreviewStats(): void {
        this.frameCount++;
        const now = Date.now();

        if (now - this.lastStatsUpdate >= 1000) {
            this.currentFPS = this.frameCount;
            this.frameCount = 0;
            this.lastStatsUpdate = now;

            const fpsEl = this.window?.querySelector('#vs-fps');
            const latencyEl = this.window?.querySelector('#vs-latency');
            const detectionsEl = this.window?.querySelector('#vs-detections');

            if (fpsEl) fpsEl.textContent = this.currentFPS.toString();
            if (latencyEl) latencyEl.textContent = '-';
            if (detectionsEl) detectionsEl.textContent = '-';
        }
    }

    public show(): void {
        debug(SEG.VID, 'show() called');
        if (this.window) {
            this.window.setAttribute('data-visible', 'true');
            debug(SEG.VID, 'Window visibility set to true');
        } else {
            error(SEG.VID, 'Window element not found!');
        }
    }

    public hide(): void {
        this.stopCamera();
        if (this.window) {
            this.window.setAttribute('data-visible', 'false');
        }
    }

    public toggle(): void {
        const isVisible = this.window?.getAttribute('data-visible') === 'true';
        debug(SEG.VID, `toggle() called, currently visible: ${isVisible}`);
        if (isVisible) {
            this.hide();
        } else {
            this.show();
        }
    }

    private showError(message: string): void {
        const errorEl = this.window?.querySelector('#vs-error');
        if (errorEl) {
            errorEl.textContent = message;
            (errorEl as HTMLElement).style.display = 'block';
        }
    }
}
