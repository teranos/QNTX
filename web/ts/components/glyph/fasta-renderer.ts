/**
 * FASTA sequence renderer for attestation glyphs.
 *
 * Parses FASTA-formatted string data and renders a pager
 * with colored nucleotide bases (A/T/G/C).
 */

import { preventDrag } from '@qntx/glyphs';

const AZURE_KEYWORD = '#919599';
const AZURE_VALUE = '#d7dee3';

const BASE_COLORS: Record<string, string> = {
    A: '#66bb6a',
    T: '#ef5350',
    G: '#ffca28',
    C: '#42a5f5',
};

interface FastaEntry {
    header: string;
    sequence: string;
}

function parseFasta(data: string): FastaEntry[] {
    const entries: FastaEntry[] = [];
    const lines = data.split('\n');
    let header = '';
    let seq = '';
    for (const line of lines) {
        if (line.startsWith('>')) {
            if (header) entries.push({ header, sequence: seq });
            header = line.slice(1).trim();
            seq = '';
        } else {
            seq += line.trim();
        }
    }
    if (header) entries.push({ header, sequence: seq });
    return entries;
}

function renderSequence(seq: string): HTMLElement {
    const el = document.createElement('div');
    el.style.fontFamily = 'monospace';
    el.style.fontSize = '13px';
    el.style.letterSpacing = '1px';
    el.style.lineHeight = '1.6';
    el.style.wordBreak = 'break-all';
    let html = '';
    for (let i = 0; i < seq.length; i++) {
        const base = seq[i].toUpperCase();
        const color = BASE_COLORS[base] || AZURE_VALUE;
        html += `<span style="color:${color}">${base}</span>`;
    }
    el.innerHTML = html;
    return el;
}

/**
 * Check if attributes contain FASTA data.
 */
export function isFastaAttribute(attrs: Record<string, unknown>, key: string): boolean {
    return attrs['format'] === 'fasta' && (key === 'data' || key === 'format');
}

/**
 * Build a paged FASTA viewer from the data string.
 */
export function buildFastaViewer(data: string): HTMLElement {
    const entries = parseFasta(data);
    if (entries.length === 0) {
        const empty = document.createElement('div');
        empty.style.color = AZURE_KEYWORD;
        empty.textContent = 'No FASTA entries';
        return empty;
    }

    const PAGE_SIZE = 5;
    const wrapper = document.createElement('div');
    let page = 0;
    const totalPages = Math.ceil(entries.length / PAGE_SIZE);

    const nav = document.createElement('div');
    nav.style.display = 'flex';
    nav.style.alignItems = 'center';
    nav.style.gap = '8px';
    nav.style.marginBottom = '6px';

    const prevBtn = document.createElement('button');
    prevBtn.textContent = '\u25C0';
    prevBtn.style.background = 'none';
    prevBtn.style.border = '1px solid var(--border)';
    prevBtn.style.color = AZURE_VALUE;
    prevBtn.style.cursor = 'pointer';
    prevBtn.style.padding = '2px 6px';
    prevBtn.style.fontSize = '11px';
    prevBtn.style.borderRadius = '3px';

    const nextBtn = document.createElement('button');
    nextBtn.textContent = '\u25B6';
    nextBtn.style.background = 'none';
    nextBtn.style.border = '1px solid var(--border)';
    nextBtn.style.color = AZURE_VALUE;
    nextBtn.style.cursor = 'pointer';
    nextBtn.style.padding = '2px 6px';
    nextBtn.style.fontSize = '11px';
    nextBtn.style.borderRadius = '3px';

    const counter = document.createElement('span');
    counter.style.color = AZURE_KEYWORD;
    counter.style.fontSize = '11px';
    counter.style.fontFamily = 'monospace';

    preventDrag(prevBtn);
    preventDrag(nextBtn);
    nav.append(prevBtn, counter, nextBtn);
    wrapper.appendChild(nav);

    const itemContainer = document.createElement('div');
    wrapper.appendChild(itemContainer);

    const show = () => {
        const start = page * PAGE_SIZE;
        const end = Math.min(start + PAGE_SIZE, entries.length);
        counter.textContent = `${start + 1}–${end} / ${entries.length}`;
        prevBtn.style.opacity = page === 0 ? '0.3' : '1';
        nextBtn.style.opacity = page >= totalPages - 1 ? '0.3' : '1';
        itemContainer.replaceChildren();

        for (let i = start; i < end; i++) {
            const entry = document.createElement('div');
            entry.style.marginBottom = '6px';

            const headerEl = document.createElement('div');
            headerEl.style.color = AZURE_KEYWORD;
            headerEl.style.fontSize = '11px';
            headerEl.style.marginBottom = '2px';
            headerEl.style.fontFamily = 'monospace';
            headerEl.textContent = '>' + entries[i].header;
            entry.appendChild(headerEl);

            entry.appendChild(renderSequence(entries[i].sequence));
            itemContainer.appendChild(entry);
        }
    };

    prevBtn.addEventListener('click', (e) => {
        e.stopPropagation();
        if (page > 0) { page--; show(); }
    });
    nextBtn.addEventListener('click', (e) => {
        e.stopPropagation();
        if (page < totalPages - 1) { page++; show(); }
    });

    wrapper.tabIndex = 0;
    wrapper.style.outline = 'none';
    wrapper.addEventListener('keydown', (e) => {
        if (e.key === 'ArrowLeft' && page > 0) {
            page--; show(); e.preventDefault(); e.stopPropagation();
        } else if (e.key === 'ArrowRight' && page < totalPages - 1) {
            page++; show(); e.preventDefault(); e.stopPropagation();
        }
    });

    show();
    return wrapper;
}
