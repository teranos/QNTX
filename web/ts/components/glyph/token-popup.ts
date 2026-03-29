/**
 * Token Popup — hover overlay for stream glyph tokens.
 *
 * Shows multiple candidate lists — one per sampler stage — so you see
 * how each stage changed which words were in play and their probabilities.
 */

import type { LLMTokenCandidate, SamplerStageSignal } from '@generated/server';

export interface TokenPopup {
    show(span: HTMLSpanElement): void;
    hide(): void;
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

/** Human-readable stage label */
function stageLabel(name: string, count: number): string {
    switch (name) {
        case 'logits': return `raw (${formatCount(count)})`;
        case 'penalties': return `after penalties (${formatCount(count)})`;
        case 'top_k': return `top ${formatCount(count)}`;
        case 'top_p': return `nucleus → ${formatCount(count)}`;
        case 'min_p': return `min-p → ${formatCount(count)}`;
        case 'typical': return `typical → ${formatCount(count)}`;
        case 'temp': return `final (${formatCount(count)})`;
        default: return `${name} (${formatCount(count)})`;
    }
}

function formatCount(n: number): string {
    if (n >= 1000) return Math.round(n / 1000) + 'K';
    return String(n);
}

function renderCandidateRow(c: LLMTokenCandidate, chosen: boolean): HTMLDivElement {
    const row = document.createElement('div');
    row.className = 'token-popup-candidate';
    if (chosen) row.classList.add('token-popup-chosen');

    const tokenText = document.createElement('span');
    tokenText.className = 'token-popup-token-text';
    tokenText.textContent = escapeToken(c.text);
    row.appendChild(tokenText);

    const barTrack = document.createElement('div');
    barTrack.className = 'token-popup-bar-track';
    const bar = document.createElement('div');
    bar.className = 'token-popup-bar';
    bar.style.width = `${(c.prob * 100).toFixed(1)}%`;
    barTrack.appendChild(bar);
    row.appendChild(barTrack);

    const prob = document.createElement('span');
    prob.className = 'token-popup-prob';
    prob.textContent = `${(c.prob * 100).toFixed(1)}%`;
    row.appendChild(prob);

    return row;
}

export function createTokenPopup(): TokenPopup {
    const el = document.createElement('div');
    el.className = 'token-popup';
    el.style.display = 'none';
    document.body.appendChild(el);

    function show(span: HTMLSpanElement): void {
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

        if (stages.length > 0) {
            const row = document.createElement('div');
            row.className = 'token-popup-stages-row';

            // Show each stage's candidate list side by side
            let prevCount = 0;
            for (const stage of stages) {
                // Skip if this stage didn't change the distribution
                if (stage.active_count === prevCount && stage.name !== 'logits' && stage.name !== 'temp') {
                    prevCount = stage.active_count;
                    continue;
                }
                prevCount = stage.active_count;

                if (!stage.top_k || stage.top_k.length === 0) continue;

                const section = document.createElement('div');
                section.className = 'token-popup-stage-section';

                const header = document.createElement('div');
                header.className = 'token-popup-stage-header';
                header.textContent = stageLabel(stage.name, stage.active_count);
                section.appendChild(header);

                const list = document.createElement('div');
                list.className = 'token-popup-candidates';
                for (const c of stage.top_k) {
                    list.appendChild(renderCandidateRow(c, c.text === chosenText));
                }
                section.appendChild(list);

                row.appendChild(section);
            }

            el.appendChild(row);
        } else if (candidates.length > 0) {
            // Fallback: no stage data, show pre-sampler candidates
            const list = document.createElement('div');
            list.className = 'token-popup-candidates';
            for (const c of candidates) {
                list.appendChild(renderCandidateRow(c, c.text === chosenText));
            }
            el.appendChild(list);
        }

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

    function hide(): void {
        el.style.display = 'none';
    }

    function destroy(): void {
        el.remove();
    }

    return { show, hide, destroy };
}
