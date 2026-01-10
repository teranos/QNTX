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

import { invoke } from '@tauri-apps/api/core';
import { debug, info, error, SEG } from './logger.ts';

interface VidStreamConfig {
    model_path: string;
    confidence_threshold?: number;
    nms_threshold?: number;
    input_width?: number;
    input_height?: number;
}

interface Detection {
    class_id: number;
    label: string;
    confidence: number;
    bbox: {
        x: number;
        y: number;
        width: number;
        height: number;
    };
}

interface ProcessResult {
    detections: Detection[];
    stats: {
        preprocess_us: number;
        inference_us: number;
        postprocess_us: number;
        total_us: number;
        detections_raw: number;
        detections_final: number;
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

    constructor() {
        this.createWindow();
    }

    private createWindow(): void {
        this.window = document.createElement('div');
        this.window.id = 'vidstream-window';
        this.window.className = 'draggable-window';
        this.window.style.width = '664px'; // 640px viewport + padding

        // Check if running in Tauri (desktop) or browser
        const isTauri = typeof window !== 'undefined' && '__TAURI_INTERNALS__' in window;

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
                    ${!isTauri ? 'disabled' : ''}
                />

                <div class="window-controls">
                    <button
                        id="vs-init-btn"
                        class="panel-btn panel-btn-sm ${!isTauri ? 'btn-unavailable' : ''}"
                        ${!isTauri ? 'disabled title="ONNX inference requires desktop mode (Tauri)"' : ''}
                    >Initialize ONNX</button>
                    <button
                        id="vs-start-btn"
                        class="panel-btn panel-btn-sm panel-btn-primary"
                    >Start Camera</button>
                    <button id="vs-stop-btn" class="panel-btn panel-btn-sm" style="display: none;">Stop</button>
                    <span id="vs-status" class="window-status">${isTauri ? 'Ready for camera + ONNX' : 'Browser mode (webcam only, no ONNX)'}</span>
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
        const result = await invoke('vidstream_init', { config });
        this.engineReady = true;
        debug(SEG.VID, 'Engine ready:', result);
    }

    private async startCamera(): Promise<void> {
        debug(SEG.VID, 'startCamera() called');

        const isTauri = typeof window !== 'undefined' && '__TAURI_INTERNALS__' in window;

        this.canvas = this.window?.querySelector('#vs-canvas') as HTMLCanvasElement;
        this.ctx = this.canvas?.getContext('2d') || null;

        try {
            if (isTauri) {
                // Desktop mode: Use CrabCamera 0.7 plugin
                debug(SEG.VID, 'Using CrabCamera 0.7 plugin for native camera access');

                // Get available cameras
                const cameras = await invoke<any[]>('plugin:crabcamera|get_available_cameras');
                debug(SEG.VID, 'Available cameras:', cameras);

                if (cameras.length === 0) {
                    throw new Error('No cameras found');
                }

                // Use first camera
                const camera = cameras[0];
                debug(SEG.VID, 'Initializing camera:', camera);

                // Initialize camera system
                await invoke('plugin:crabcamera|initialize_camera_system', {
                    device_id: camera.id,
                    format: {
                        width: 640,
                        height: 480,
                        fps: 30.0
                    }
                });

                info(SEG.VID, 'CrabCamera 0.7 initialized successfully');
                this.startProcessing();

            } else {
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

        const isTauri = typeof window !== 'undefined' && '__TAURI_INTERNALS__' in window;

        if (isTauri) {
            // Desktop: Release CrabCamera
            try {
                await invoke('plugin:crabcamera|release_camera');
            } catch (err) {
                error(SEG.VID, 'Error releasing camera', err);
            }
        } else {
            // Browser: Stop MediaStream
            if (this.stream) {
                this.stream.getTracks().forEach(track => track.stop());
                this.stream = null;
            }

            if (this.video) {
                this.video.srcObject = null;
            }
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

        const frameStart = performance.now();
        const isTauri = typeof window !== 'undefined' && '__TAURI_INTERNALS__' in window;

        try {
            if (isTauri) {
                // Desktop: Capture frame from CrabCamera
                const photo = await invoke<any>('plugin:crabcamera|capture_single_photo', {
                    quality: 0.9
                });

                // photo.data is base64 encoded image
                const img = new Image();
                img.onload = async () => {
                    this.ctx!.drawImage(img, 0, 0, this.canvas!.width, this.canvas!.height);

                    // Run inference if engine ready
                    if (this.engineReady) {
                        await this.runInference(frameStart);
                    } else {
                        this.updatePreviewStats();
                    }

                    // Continue loop
                    this.animationFrameId = requestAnimationFrame(() => this.processFrame());
                };
                img.src = `data:image/jpeg;base64,${photo.data}`;

            } else {
                // Browser: Draw from video element
                if (!this.video) return;

                this.ctx.drawImage(this.video, 0, 0, this.canvas.width, this.canvas.height);

                if (this.engineReady) {
                    await this.runInference(frameStart);
                } else {
                    this.updatePreviewStats();
                }

                this.animationFrameId = requestAnimationFrame(() => this.processFrame());
            }
        } catch (err) {
            error(SEG.VID, 'Frame processing error', err);
            this.animationFrameId = requestAnimationFrame(() => this.processFrame());
        }
    }

    private async runInference(frameStart: number): Promise<void> {
        if (!this.ctx || !this.canvas) return;

        const imageData = this.ctx.getImageData(0, 0, this.canvas.width, this.canvas.height);
        const frameData = Array.from(imageData.data);

        try {
            const result = await invoke<ProcessResult>('vidstream_process_frame', {
                frameData,
                width: this.canvas.width,
                height: this.canvas.height,
                format: 'rgba8',
                timestampUs: BigInt(Date.now() * 1000),
            });

            this.drawDetections(result.detections);
            this.updateStats(result, performance.now() - frameStart);
        } catch (err) {
            error(SEG.VID, 'Inference error', err);
        }
    }

    private drawDetections(detections: Detection[]): void {
        if (!this.ctx || !this.video) return;

        // Redraw video frame
        this.ctx.drawImage(this.video, 0, 0, this.canvas!.width, this.canvas!.height);

        detections.forEach(det => {
            const { x, y, width, height } = det.bbox;

            // Bounding box
            this.ctx!.strokeStyle = '#0f0';
            this.ctx!.lineWidth = 2;
            this.ctx!.strokeRect(x, y, width, height);

            // Label
            const label = `${det.label} ${(det.confidence * 100).toFixed(0)}%`;
            this.ctx!.font = '12px monospace';
            const metrics = this.ctx!.measureText(label);
            this.ctx!.fillStyle = 'rgba(0, 255, 0, 0.8)';
            this.ctx!.fillRect(x, y - 18, metrics.width + 6, 18);
            this.ctx!.fillStyle = '#000';
            this.ctx!.fillText(label, x + 3, y - 5);
        });
    }

    private updateStats(result: ProcessResult, totalMs: number): void {
        this.frameCount++;
        const now = Date.now();

        if (now - this.lastStatsUpdate >= 1000) {
            this.currentFPS = this.frameCount;
            this.avgLatency = totalMs;
            this.frameCount = 0;
            this.lastStatsUpdate = now;

            const fpsEl = this.window?.querySelector('#vs-fps');
            const latencyEl = this.window?.querySelector('#vs-latency');
            const detectionsEl = this.window?.querySelector('#vs-detections');

            if (fpsEl) fpsEl.textContent = this.currentFPS.toString();
            if (latencyEl) latencyEl.textContent = this.avgLatency.toFixed(1);
            if (detectionsEl) detectionsEl.textContent = result.detections.length.toString();
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
