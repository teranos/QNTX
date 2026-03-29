/**
 * Token Popup — hover overlay for stream glyph tokens.
 *
 * Single-column view: final candidates with origin markers showing where
 * each word started (raw probability) vs where it ended up. The color
 * of the shift indicates which sampler stage had the biggest effect.
 *
 * Interactive: mouse can enter the popup. Lingering expands a color legend.
 */

import type { LLMTokenCandidate, SamplerStageSignal } from '@generated/server';

export interface TokenPopup {
    show(span: HTMLSpanElement): void;
    hide(): void;
    /** Call on mouseleave from the token span — starts a delayed hide */
    scheduleHide(): void;
    /** Call to cancel a pending hide (e.g. mouse entered the popup) */
    cancelHide(): void;
    destroy(): void;
}

/** Escape whitespace characters for display */
function escapeToken(text: string): string {
    let out = '';
    for (let i = 0; i < text.length; i++) {
        const ch = text[i];
        if (ch === '\n') { out += '\\n'; }
        else if (ch === '\t') { out += '\\t'; }
        else if (ch === '\r') { out += '\\r'; }
        else { out += ch; }
    }
    return out;
}

// Stage colors — each sampler gets a distinct hue
const STAGE_COLORS: Record<string, string> = {
    penalties: 'hsla(0, 70%, 55%, 0.8)',     // red — suppression
    top_k:     'hsla(210, 60%, 55%, 0.6)',   // blue — filtering
    top_p:     'hsla(210, 60%, 55%, 0.6)',   // blue — filtering
    min_p:     'hsla(270, 50%, 55%, 0.6)',   // purple — filtering
    typical:   'hsla(270, 50%, 55%, 0.6)',   // purple — filtering
    temp:      'hsla(40, 80%, 55%, 0.8)',    // amber — sharpening
};

const STAGE_LABELS: Record<string, string> = {
    penalties: 'repetition penalty',
    top_k:     'top-k filter',
    top_p:     'nucleus filter',
    min_p:     'min-p filter',
    typical:   'typical filter',
    temp:      'temperature',
};

const LEGEND_ITEMS = [
    { color: STAGE_COLORS.temp, label: 'temperature sharpened' },
    { color: STAGE_COLORS.penalties, label: 'repetition penalty' },
    { color: STAGE_COLORS.top_k, label: 'top-k / nucleus filtered' },
    { color: STAGE_COLORS.min_p, label: 'min-p / typical filtered' },
];

/**
 * For each candidate in the final stage, trace back through earlier stages
 * to find the raw (first) probability. Also find which stage caused the
 * biggest shift for this token.
 */
function traceCandidate(
    tokenId: number,
    stages: SamplerStageSignal[],
): { rawProb: number; biggestStage: string } {
    let rawProb = 0;
    let biggestStage = '';
    let biggestDelta = 0;
    let prevProb = 0;

    for (const stage of stages) {
        if (!stage.top_k) continue;
        const match = stage.top_k.find(c => c.id === tokenId);
        const prob = match ? match.prob : 0;

        if (stage.name === 'logits') {
            rawProb = prob;
            prevProb = prob;
            continue;
        }

        const delta = Math.abs(prob - prevProb);
        if (delta > biggestDelta) {
            biggestDelta = delta;
            biggestStage = stage.name;
        }
        prevProb = prob;
    }

    return { rawProb, biggestStage };
}

/**
 * Return a background color for the token based on which sampler stage
 * had the biggest effect on the chosen token. Alpha scales with how
 * much the probability shifted — subtle when samplers barely changed
 * anything, vivid when they made a big difference.
 */
export function samplerInfluenceColor(stages: SamplerStageSignal[]): string {
    if (stages.length === 0) return 'transparent';

    // Find the chosen token (highest prob in final stage)
    const finalStage = stages[stages.length - 1];
    if (!finalStage.top_k || finalStage.top_k.length === 0) return 'transparent';

    let chosenId = finalStage.top_k[0].id;
    let chosenProb = finalStage.top_k[0].prob;
    for (const c of finalStage.top_k) {
        if (c.prob > chosenProb) {
            chosenId = c.id;
            chosenProb = c.prob;
        }
    }

    const trace = traceCandidate(chosenId, stages);
    const delta = Math.abs(chosenProb - trace.rawProb);

    if (delta < 0.01) return 'transparent';

    // Map the stage to a hue
    const stageHues: Record<string, number> = {
        penalties: 0,
        top_k: 210,
        top_p: 210,
        min_p: 270,
        typical: 270,
        temp: 40,
    };
    const hue = stageHues[trace.biggestStage] ?? 30;
    // Alpha proportional to the shift magnitude, capped at 0.45
    const alpha = Math.min(0.45, delta * 1.5);
    if (alpha < 0.02) return 'transparent';
    return `hsla(${hue}, 70%, 50%, ${alpha.toFixed(3)})`;
}

export function createTokenPopup(): TokenPopup {
    const el = document.createElement('div');
    el.className = 'token-popup';
    el.style.display = 'none';
    document.body.appendChild(el);

    let hideTimer: ReturnType<typeof setTimeout> | null = null;
    let legendTimer: ReturnType<typeof setTimeout> | null = null;
    let legendVisible = false;
    let legendEl: HTMLDivElement | null = null;

    // Nested detail tooltip
    const detailEl = document.createElement('div');
    detailEl.className = 'token-popup-detail';
    detailEl.style.display = 'none';
    document.body.appendChild(detailEl);

    function showDetail(target: HTMLElement, text: string) {
        detailEl.textContent = text;
        detailEl.style.display = '';
        const r = target.getBoundingClientRect();
        let left = r.right + 6;
        let top = r.top;
        // Keep on screen
        const dw = detailEl.offsetWidth;
        if (left + dw > window.innerWidth - 4) {
            left = r.left - dw - 6;
        }
        if (left < 4) left = 4;
        detailEl.style.left = `${left}px`;
        detailEl.style.top = `${top}px`;
    }

    function hideDetail() {
        detailEl.style.display = 'none';
    }

    function clearTimers() {
        if (hideTimer) { clearTimeout(hideTimer); hideTimer = null; }
        if (legendTimer) { clearTimeout(legendTimer); legendTimer = null; }
    }

    function showLegend() {
        if (legendVisible || !legendEl) return;
        legendVisible = true;
        legendEl.style.display = '';
    }

    function hideLegend() {
        if (!legendVisible || !legendEl) return;
        legendVisible = false;
        legendEl.style.display = 'none';
    }

    // Keep popup alive when mouse enters it, expand legend after 400ms
    el.addEventListener('mouseenter', () => {
        if (hideTimer) { clearTimeout(hideTimer); hideTimer = null; }
        if (legendTimer) clearTimeout(legendTimer);
        legendTimer = setTimeout(showLegend, 400);
    });

    el.addEventListener('mouseleave', () => {
        clearTimers();
        hideLegend();
        hideTimer = setTimeout(() => {
            el.style.display = 'none';
        }, 100);
    });

    function show(span: HTMLSpanElement): void {
        clearTimers();
        hideLegend();

        const confidence = span.dataset.confidence;
        if (!confidence) return;

        const topKRaw = span.dataset.topK;
        const stagesRaw = span.dataset.samplerStages;

        let candidates: LLMTokenCandidate[] = [];
        if (topKRaw) {
            try { candidates = JSON.parse(topKRaw); } catch { /* skip */ }
        }

        let stages: SamplerStageSignal[] = [];
        if (stagesRaw) {
            try { stages = JSON.parse(stagesRaw); } catch { /* skip */ }
        }

        el.innerHTML = '';

        const chosenText = span.textContent ?? '';

        // Use the final stage's candidates if available, otherwise pre-sampler
        const finalStage = stages.length > 0
            ? stages[stages.length - 1]
            : null;
        const displayCandidates = finalStage?.top_k ?? candidates;

        if (displayCandidates.length > 0) {
            const list = document.createElement('div');
            list.className = 'token-popup-candidates';

            for (const c of displayCandidates) {
                const row = document.createElement('div');
                row.className = 'token-popup-candidate';
                const isChosen = c.text === chosenText;
                if (isChosen) row.classList.add('token-popup-chosen');

                // Token text
                const tokenText = document.createElement('span');
                tokenText.className = 'token-popup-token-text';
                tokenText.textContent = escapeToken(c.text);
                row.appendChild(tokenText);

                // Bar track with final bar + origin marker
                const barTrack = document.createElement('div');
                barTrack.className = 'token-popup-bar-track';

                const finalPct = c.prob * 100;

                if (stages.length > 0) {
                    const trace = traceCandidate(c.id, stages);
                    const rawPct = trace.rawProb * 100;
                    const color = STAGE_COLORS[trace.biggestStage] ?? 'hsla(30, 100%, 50%, 0.7)';

                    // Final probability bar — colored by biggest contributing stage
                    const bar = document.createElement('div');
                    bar.className = 'token-popup-bar';
                    bar.style.width = `${finalPct.toFixed(1)}%`;
                    bar.style.background = color;
                    const stageLabel = STAGE_LABELS[trace.biggestStage] ?? trace.biggestStage;
                    const direction = finalPct > rawPct ? 'boosted' : 'reduced';
                    const barDetail = rawPct > 0
                        ? `${stageLabel} ${direction} from ${rawPct.toFixed(1)}% to ${finalPct.toFixed(1)}%`
                        : `${finalPct.toFixed(1)}% after ${stageLabel}`;
                    bar.addEventListener('mouseenter', () => showDetail(bar, barDetail));
                    bar.addEventListener('mouseleave', hideDetail);
                    barTrack.appendChild(bar);

                    // Origin marker — thin line showing raw probability
                    if (Math.abs(rawPct - finalPct) > 1) {
                        const marker = document.createElement('div');
                        marker.className = 'token-popup-origin';
                        marker.style.left = `${rawPct.toFixed(1)}%`;
                        const markerDetail = `started at ${rawPct.toFixed(1)}% before sampling`;
                        marker.addEventListener('mouseenter', () => showDetail(marker, markerDetail));
                        marker.addEventListener('mouseleave', hideDetail);
                        barTrack.appendChild(marker);
                    }
                } else {
                    // No stage data — plain bar
                    const bar = document.createElement('div');
                    bar.className = 'token-popup-bar';
                    bar.style.width = `${finalPct.toFixed(1)}%`;
                    barTrack.appendChild(bar);
                }

                row.appendChild(barTrack);

                // Probability label
                const prob = document.createElement('span');
                prob.className = 'token-popup-prob';
                prob.textContent = `${finalPct.toFixed(1)}%`;
                row.appendChild(prob);

                list.appendChild(row);
            }

            el.appendChild(list);
        }

        // Legend — hidden initially, shown after lingering
        legendEl = document.createElement('div');
        legendEl.className = 'token-popup-legend';
        legendEl.style.display = 'none';
        for (const item of LEGEND_ITEMS) {
            const row = document.createElement('div');
            row.className = 'token-popup-legend-item';

            const swatch = document.createElement('span');
            swatch.className = 'token-popup-legend-swatch';
            swatch.style.background = item.color;
            row.appendChild(swatch);

            const label = document.createElement('span');
            label.textContent = item.label;
            row.appendChild(label);

            legendEl.appendChild(row);
        }
        // Origin marker explanation
        const originRow = document.createElement('div');
        originRow.className = 'token-popup-legend-item';
        const originSwatch = document.createElement('span');
        originSwatch.className = 'token-popup-legend-swatch token-popup-legend-origin';
        originRow.appendChild(originSwatch);
        const originLabel = document.createElement('span');
        originLabel.textContent = 'original probability';
        originRow.appendChild(originLabel);
        legendEl.appendChild(originRow);

        el.appendChild(legendEl);

        // Position near the span, viewport-constrained
        el.style.display = '';
        const spanRect = span.getBoundingClientRect();
        const popupWidth = el.offsetWidth;
        const popupHeight = el.offsetHeight;
        const margin = 4;

        let left = spanRect.left;
        let top = spanRect.bottom + margin;

        if (top + popupHeight > window.innerHeight - margin) {
            top = spanRect.top - popupHeight - margin;
        }

        if (left + popupWidth > window.innerWidth - margin) {
            left = window.innerWidth - popupWidth - margin;
        }
        if (left < margin) left = margin;

        el.style.left = `${left}px`;
        el.style.top = `${top}px`;
    }

    function scheduleHide(): void {
        if (hideTimer) clearTimeout(hideTimer);
        hideTimer = setTimeout(() => {
            el.style.display = 'none';
            hideLegend();
        }, 150);
    }

    function cancelHide(): void {
        if (hideTimer) { clearTimeout(hideTimer); hideTimer = null; }
    }

    function hide(): void {
        clearTimers();
        hideLegend();
        hideDetail();
        el.style.display = 'none';
    }

    function destroy(): void {
        clearTimers();
        hideDetail();
        el.remove();
        detailEl.remove();
    }

    return { show, hide, scheduleHide, cancelHide, destroy };
}
