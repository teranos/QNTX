/**
 * Token Popup — hover overlay for stream glyph tokens.
 *
 * Shows signal data (confidence, entropy, top_gap) and top-K candidate
 * tokens with probability bars. One popup instance per stream glyph,
 * positioned near the hovered span, viewport-constrained.
 */

import type { LLMTokenCandidate } from '@generated/server';

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

export function createTokenPopup(): TokenPopup {
    const el = document.createElement('div');
    el.className = 'token-popup';
    el.style.display = 'none';
    document.body.appendChild(el);

    function show(span: HTMLSpanElement): void {
        const confidence = span.dataset.confidence;
        if (!confidence) return;

        const entropy = span.dataset.entropy ?? '—';
        const topGap = span.dataset.topGap ?? '—';
        const topKRaw = span.dataset.topK;

        let candidates: LLMTokenCandidate[] = [];
        if (topKRaw) {
            try { candidates = JSON.parse(topKRaw); } catch { /* skip */ }
        }

        // Build content
        el.innerHTML = '';

        // Signal row
        const signals = document.createElement('div');
        signals.className = 'token-popup-signals';
        const conf = parseFloat(confidence);
        const ent = parseFloat(entropy);
        const gap = parseFloat(topGap);
        signals.textContent = [
            `P=${isNaN(conf) ? '—' : conf.toFixed(3)}`,
            `H=${isNaN(ent) ? '—' : ent.toFixed(2)}`,
            `Δ=${isNaN(gap) ? '—' : gap.toFixed(3)}`,
        ].join('  ');
        el.appendChild(signals);

        // Candidates
        if (candidates.length > 0) {
            const list = document.createElement('div');
            list.className = 'token-popup-candidates';

            const chosenText = span.textContent ?? '';

            for (const c of candidates) {
                const row = document.createElement('div');
                row.className = 'token-popup-candidate';
                if (c.text === chosenText) row.classList.add('token-popup-chosen');

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
                prob.textContent = c.prob.toFixed(3);
                row.appendChild(prob);

                list.appendChild(row);
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

        // Flip up if near bottom
        if (top + popupHeight > window.innerHeight - margin) {
            top = spanRect.top - popupHeight - margin;
        }

        // Constrain horizontal
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
