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

import { log, SEG } from './logger.ts';
import { sendMessage, registerHandler } from './websocket.ts';
import { Window } from './components/window.ts';
import { apiFetch } from './api.ts';
import { tooltip } from './components/tooltip.ts';
import type { QueryMessage } from '@generated/server.js';
import type {
    MessageHandler,
    VidStreamInitSuccessMessage,
    VidStreamInitErrorMessage,
    VidStreamDetectionsMessage,
    VidStreamFrameErrorMessage,
} from '../types/websocket';

// VidStream configuration (subset of QueryMessage fields for vidstream_init)
// All fields optional since we provide defaults
interface VidStreamConfig {
    model_path: string;
    confidence_threshold?: number;
    nms_threshold?: number;
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
    private window: Window;
    private video: HTMLVideoElement | null = null;
    private canvas: HTMLCanvasElement | null = null;
    private ctx: CanvasRenderingContext2D | null = null;
    private stream: MediaStream | null = null;
    private animationFrameId: number | null = null;
    private isProcessing: boolean = false;
    private engineReady: boolean = false;

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

    // Version tracking
    private version: string | null = null;

    constructor() {
        this.window = new Window({
            id: 'vidstream-window',
            title: 'VidStream',
            width: '664px', // 640px viewport + padding
            onClose: () => this.stopCamera(),
            onShow: () => this.loadModelPathFromConfig(),
        });

        this.setupContent();
        this.setupMessageHandlers();
        this.setupTooltips();
    }

    private setupContent(): void {
        const content = `
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
                <button
                    id="vs-config-btn"
                    class="panel-btn panel-btn-sm has-tooltip"
                    data-tooltip="Configure ONNX model"
                >⚙️</button>
                <span id="vs-status" class="window-status">Ready (camera + ONNX via WebSocket)</span>
            </div>
            <div class="window-model-info">
                Model: <span id="vs-model-path">ats/vidstream/models/yolo11n.onnx</span>
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
        `;

        this.window.setContent(content);

        // Set up footer with stats
        const footerContent = `
            <div class="window-stats">
                <div>FPS: <span id="vs-fps" class="window-stat-value">0</span></div>
                <div>Latency: <span id="vs-latency" class="window-stat-value">0</span> ms</div>
                <div>Detections: <span id="vs-detections" class="window-stat-value">0</span></div>
            </div>
        `;
        this.window.setFooterContent(footerContent);

        this.setupEventListeners();
    }

    private setupMessageHandlers(): void {
        // Handle engine initialization response
        registerHandler('vidstream_init_success', ((data: VidStreamInitSuccessMessage) => {
            this.engineReady = true;
            log.debug(SEG.VID, 'Engine ready:', data);
        }) as MessageHandler);

        registerHandler('vidstream_init_error', ((data: VidStreamInitErrorMessage) => {
            log.error(SEG.VID, 'Engine init error:', data.error);
            this.showError(data.error);
        }) as MessageHandler);

        // Handle frame processing response
        registerHandler('vidstream_detections', ((data: VidStreamDetectionsMessage) => {
            // Store detections to be drawn on next animation frame
            this.latestDetections = data.detections || [];
            const totalMs = data.stats.total_us / 1000;
            this.updateStatsFromServer(data.detections.length, totalMs);
        }) as MessageHandler);

        registerHandler('vidstream_frame_error', ((data: VidStreamFrameErrorMessage) => {
            log.error(SEG.VID, 'Frame processing error:', data.error);
        }) as MessageHandler);
    }

    private setupEventListeners(): void {
        const windowEl = this.window.getElement();
        const configBtn = windowEl.querySelector('#vs-config-btn') as HTMLButtonElement;
        const initBtn = windowEl.querySelector('#vs-init-btn') as HTMLButtonElement;
        const startBtn = windowEl.querySelector('#vs-start-btn') as HTMLButtonElement;
        const stopBtn = windowEl.querySelector('#vs-stop-btn') as HTMLButtonElement;

        // Configure button - opens AI Provider panel
        configBtn?.addEventListener('click', () => {
            // Import and call toggleAIProvider
            import('./ai-provider-window.ts').then(module => {
                module.toggleAIProvider();
            });
        });

        // Initialize engine (optional - enables inference)
        initBtn?.addEventListener('click', async () => {
            const modelPathSpan = windowEl.querySelector('#vs-model-path') as HTMLSpanElement;
            const modelPath = modelPathSpan?.textContent?.trim();
            if (!modelPath) return;

            try {
                initBtn.disabled = true;
                initBtn.textContent = 'Initializing...';
                await this.initializeEngine({ model_path: modelPath });
                const status = windowEl.querySelector('#vs-status') as HTMLElement;
                if (status) {
                    status.textContent = '✓ Inference ready';
                    status.style.color = '#0a0';
                }
                log.info(SEG.VID, 'VidStream ONNX engine initialized');
            } catch (error: unknown) {
                const status = windowEl.querySelector('#vs-status') as HTMLElement;
                if (status) {
                    status.textContent = `✗ Engine init failed`;
                    status.style.color = '#a00';
                }
                log.error(SEG.VID, 'ONNX engine initialization failed', error);
                this.showError(error instanceof Error ? error.message : String(error));
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
        const payload: Pick<QueryMessage, 'type' | 'model_path' | 'confidence_threshold' | 'nms_threshold'> = {
            type: 'vidstream_init',
            model_path: config.model_path,
            confidence_threshold: config.confidence_threshold || 0.5,
            nms_threshold: config.nms_threshold || 0.45,
        };

        const sent = sendMessage(payload);

        if (!sent) {
            throw new Error('WebSocket not connected');
        }

        log.debug(SEG.VID, 'Sent vidstream_init message');
    }

    private async startCamera(): Promise<void> {
        log.debug(SEG.VID, 'startCamera() called');

        const windowEl = this.window.getElement();
        this.canvas = windowEl.querySelector('#vs-canvas') as HTMLCanvasElement;
        this.ctx = this.canvas?.getContext('2d') || null;

        try {
            // Browser mode: Use MediaDevices API
            log.debug(SEG.VID, 'Using browser MediaDevices API');

            this.stream = await navigator.mediaDevices.getUserMedia({
                video: { width: 640, height: 480 },
                audio: false,
            });

            this.video = windowEl.querySelector('#vs-video') as HTMLVideoElement;
            if (this.video) {
                this.video.srcObject = this.stream;
                this.video.onloadedmetadata = () => {
                    this.video?.play();
                    this.startProcessing();
                };
            }

            const mode = this.engineReady ? 'with inference' : 'preview only';
            log.info(SEG.VID, `Camera started (${mode})`);
        } catch (error: unknown) {
            log.error(SEG.VID, 'Failed to start camera', error);
            this.showError(error instanceof Error ? error.message : String(error));
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

        log.info(SEG.VID, 'Camera stopped');
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
        } catch (error: unknown) {
            log.error(SEG.VID, 'Frame processing error', error);
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
            const payload: Pick<QueryMessage, 'type' | 'frame_data' | 'width' | 'height' | 'format'> = {
                type: 'vidstream_frame',
                frame_data: frameArray,
                width: this.canvas.width,
                height: this.canvas.height,
                format: 'rgba8',
            };

            // Log payload size (JSON encoding inflates size significantly)
            const payloadJSON = JSON.stringify(payload);
            const payloadSizeMB = payloadJSON.length / (1024 * 1024);
            log.debug(SEG.VID, `Frame payload: ${payloadSizeMB.toFixed(2)} MB (${payloadJSON.length} bytes)`);

            const sent = sendMessage(payload);

            if (!sent) {
                log.error(SEG.VID, 'Failed to send frame: WebSocket not connected');
            }
        } catch (error: unknown) {
            log.error(SEG.VID, 'Inference error', error);
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

            const windowEl = this.window.getElement();
            const fpsEl = windowEl.querySelector('#vs-fps');
            const latencyEl = windowEl.querySelector('#vs-latency');
            const detectionsEl = windowEl.querySelector('#vs-detections');

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

            const windowEl = this.window.getElement();
            const fpsEl = windowEl.querySelector('#vs-fps');
            const latencyEl = windowEl.querySelector('#vs-latency');
            const detectionsEl = windowEl.querySelector('#vs-detections');

            if (fpsEl) fpsEl.textContent = this.currentFPS.toString();
            if (latencyEl) latencyEl.textContent = '-';
            if (detectionsEl) detectionsEl.textContent = '-';
        }
    }

    public show(): void {
        log.debug(SEG.VID, 'show() called');
        this.window.show();
    }

    public hide(): void {
        this.stopCamera();
        this.window.hide();
    }

    public toggle(): void {
        log.debug(SEG.VID, `toggle() called, currently visible: ${this.window.isVisible()}`);
        this.window.toggle();
    }

    private showError(message: string): void {
        const windowEl = this.window.getElement();
        const errorEl = windowEl.querySelector('#vs-error');
        if (errorEl) {
            errorEl.textContent = message;
            (errorEl as HTMLElement).style.display = 'block';
        }
    }

    private async loadModelPathFromConfig(): Promise<void> {
        try {
            const response = await apiFetch('/api/config?introspection=true');
            if (!response.ok) return;

            const config = await response.json();
            const pathSetting = config.settings?.find(
                (s: any) => s.key === 'local_inference.onnx_model_path'
            );

            if (pathSetting?.value) {
                const windowEl = this.window.getElement();
                const modelPathSpan = windowEl.querySelector<HTMLSpanElement>('#vs-model-path');
                if (modelPathSpan) {
                    modelPathSpan.textContent = pathSetting.value as string;
                }
            }
        } catch (error: unknown) {
            // Silent failure - default path already set in HTML
            log.debug(SEG.VID, 'Failed to load model path from config:', error);
        }
    }

    /**
     * Update window version (called from system-capabilities handler)
     */
    public updateVersion(version: string): void {
        this.version = version;
        this.updateTitle();
        log.debug(SEG.VID, `VidStream version updated: ${version}`);
    }

    /**
     * Setup tooltip system for window header elements
     */
    private setupTooltips(): void {
        const windowEl = this.window.getElement();
        const header = windowEl.querySelector('.draggable-window-header');
        if (header) {
            tooltip.attach(header as HTMLElement, '.has-tooltip');
        }
    }

    /**
     * Build and set window title with optional version
     */
    private updateTitle(): void {
        let titleHTML = 'VidStream';

        if (this.version) {
            // Version with tooltip showing backend details
            const tooltipText = `VidStream v${this.version}\nBackend: ONNX Runtime\nReal-time object detection`;
            titleHTML += ` <span class="has-tooltip" data-tooltip="${tooltipText}" style="color: #999; font-weight: 400; font-size: 11px; cursor: help;">v${this.version}</span>`;
        }

        this.window.setTitle(titleHTML);
    }
}
