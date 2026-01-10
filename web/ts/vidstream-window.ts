/**
 * VidStreamWindow - Draggable video inference window
 *
 * Real-time webcam + ONNX inference in a draggable/resizable window.
 * Uses IX (⨳) segment for logging, 📽 icon for UI (rendered monochrome).
 */

import { invoke } from '@tauri-apps/api/core';
import { log, SEG } from './logger.ts';
import { CSS } from './css-classes.ts';

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
        this.window.className = 'vidstream-window';
        this.window.style.cssText = `
            position: fixed;
            top: 100px;
            left: 100px;
            width: 680px;
            background: var(--bg-primary, #1a1a1a);
            border: 1px solid var(--border-primary, #333);
            border-radius: 8px;
            box-shadow: 0 4px 12px rgba(0,0,0,0.3);
            z-index: 9999;
            display: none;
            font-family: var(--font-mono, monospace);
        `;

        this.window.innerHTML = `
            <div class="vidstream-header" style="
                padding: 0.75rem;
                border-bottom: 1px solid var(--border-primary, #333);
                cursor: move;
                display: flex;
                align-items: center;
                justify-content: space-between;
                user-select: none;
            ">
                <div style="display: flex; align-items: center; gap: 0.5rem;">
                    <span style="font-size: 1.2rem; filter: grayscale(100%);">📽</span>
                    <span style="font-weight: 600;">VidStream</span>
                </div>
                <button class="vidstream-close" style="
                    background: none;
                    border: none;
                    color: var(--text-secondary, #999);
                    cursor: pointer;
                    font-size: 1.2rem;
                    padding: 0;
                    line-height: 1;
                ">&times;</button>
            </div>

            <div class="vidstream-content" style="padding: 1rem;">
                <!-- Model config -->
                <div style="margin-bottom: 1rem;">
                    <input
                        type="text"
                        id="vs-model-path"
                        value="ats/vidstream/models/yolo11n.onnx"
                        placeholder="path/to/model.onnx"
                        style="
                            width: 100%;
                            padding: 0.5rem;
                            background: var(--bg-secondary, #222);
                            border: 1px solid var(--border-primary, #333);
                            color: var(--text-primary, #fff);
                            border-radius: 4px;
                            font-family: inherit;
                            font-size: 0.9rem;
                        "
                    />
                </div>

                <!-- Controls -->
                <div style="display: flex; gap: 0.5rem; margin-bottom: 1rem;">
                    <button id="vs-init-btn" style="
                        padding: 0.5rem 1rem;
                        background: var(--accent-primary, #0066cc);
                        color: white;
                        border: none;
                        border-radius: 4px;
                        cursor: pointer;
                        font-family: inherit;
                    ">Initialize</button>
                    <button id="vs-start-btn" disabled style="
                        padding: 0.5rem 1rem;
                        background: var(--accent-secondary, #00aa00);
                        color: white;
                        border: none;
                        border-radius: 4px;
                        cursor: pointer;
                        font-family: inherit;
                    ">Start Camera</button>
                    <button id="vs-stop-btn" style="
                        padding: 0.5rem 1rem;
                        background: var(--accent-danger, #aa0000);
                        color: white;
                        border: none;
                        border-radius: 4px;
                        cursor: pointer;
                        font-family: inherit;
                        display: none;
                    ">Stop</button>
                    <span id="vs-status" style="
                        margin-left: auto;
                        align-self: center;
                        color: var(--text-secondary, #999);
                        font-size: 0.85rem;
                    ">Not initialized</span>
                </div>

                <!-- Video viewport -->
                <div style="
                    position: relative;
                    width: 640px;
                    height: 480px;
                    background: #000;
                    margin-bottom: 1rem;
                    border-radius: 4px;
                    overflow: hidden;
                ">
                    <video
                        id="vs-video"
                        autoplay
                        playsinline
                        style="position: absolute; top: 0; left: 0; width: 100%; height: 100%;"
                    ></video>
                    <canvas
                        id="vs-canvas"
                        width="640"
                        height="480"
                        style="position: absolute; top: 0; left: 0; width: 100%; height: 100%;"
                    ></canvas>
                </div>

                <!-- Stats -->
                <div style="
                    display: grid;
                    grid-template-columns: repeat(3, 1fr);
                    gap: 1rem;
                    font-size: 0.85rem;
                    color: var(--text-secondary, #999);
                ">
                    <div>FPS: <span id="vs-fps">0</span></div>
                    <div>Latency: <span id="vs-latency">0</span> ms</div>
                    <div>Detections: <span id="vs-detections">0</span></div>
                </div>
            </div>
        `;

        document.body.appendChild(this.window);
        this.setupEventListeners();
    }

    private setupEventListeners(): void {
        const header = this.window?.querySelector('.vidstream-header') as HTMLElement;
        const closeBtn = this.window?.querySelector('.vidstream-close') as HTMLButtonElement;
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

        // Initialize engine
        initBtn?.addEventListener('click', async () => {
            const input = this.window?.querySelector('#vs-model-path') as HTMLInputElement;
            const modelPath = input.value.trim();
            if (!modelPath) return;

            try {
                initBtn.disabled = true;
                initBtn.textContent = 'Initializing...';
                await this.initializeEngine({ model_path: modelPath });
                const status = this.window?.querySelector('#vs-status');
                if (status) {
                    status.textContent = '✓ Ready';
                    status.style.color = '#0a0';
                }
                startBtn.disabled = false;
                log(SEG.INGEST, 'VidStream engine initialized');
            } catch (err) {
                const status = this.window?.querySelector('#vs-status');
                if (status) {
                    status.textContent = `✗ ${err}`;
                    status.style.color = '#a00';
                }
            } finally {
                initBtn.disabled = false;
                initBtn.textContent = 'Initialize';
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
        log(SEG.INGEST, 'Engine ready:', result);
    }

    private async startCamera(): Promise<void> {
        if (!this.engineReady) {
            alert('Initialize engine first');
            return;
        }

        this.stream = await navigator.mediaDevices.getUserMedia({
            video: { width: 640, height: 480 },
            audio: false,
        });

        this.video = this.window?.querySelector('#vs-video') as HTMLVideoElement;
        this.canvas = this.window?.querySelector('#vs-canvas') as HTMLCanvasElement;
        this.ctx = this.canvas?.getContext('2d') || null;

        if (this.video) {
            this.video.srcObject = this.stream;
            this.video.onloadedmetadata = () => {
                this.video?.play();
                this.startProcessing();
            };
        }

        log(SEG.INGEST, 'Camera started');
    }

    private stopCamera(): void {
        if (this.animationFrameId) {
            cancelAnimationFrame(this.animationFrameId);
            this.animationFrameId = null;
        }

        if (this.stream) {
            this.stream.getTracks().forEach(track => track.stop());
            this.stream = null;
        }

        if (this.video) {
            this.video.srcObject = null;
        }

        this.isProcessing = false;
        log(SEG.INGEST, 'Camera stopped');
    }

    private startProcessing(): void {
        this.isProcessing = true;
        this.lastStatsUpdate = Date.now();
        this.processFrame();
    }

    private async processFrame(): Promise<void> {
        if (!this.isProcessing || !this.video || !this.canvas || !this.ctx) return;

        const frameStart = performance.now();

        // Draw video to canvas
        this.ctx.drawImage(this.video, 0, 0, this.canvas.width, this.canvas.height);

        // Get RGBA data
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
            console.error('Frame processing error:', err);
        }

        this.animationFrameId = requestAnimationFrame(() => this.processFrame());
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

    public show(): void {
        if (this.window) {
            this.window.style.display = 'block';
        }
    }

    public hide(): void {
        this.stopCamera();
        if (this.window) {
            this.window.style.display = 'none';
        }
    }

    public toggle(): void {
        if (this.window?.style.display === 'none') {
            this.show();
        } else {
            this.hide();
        }
    }
}
