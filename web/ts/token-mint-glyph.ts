/**
 * Mint Token Glyph — creating one access token (ADR-025, TOKATTEST).
 * Split out of the Access Tokens glyph, which stays the place you see every
 * token at once. The raw value is shown once, here and nowhere else.
 */

import type { Glyph } from '@qntx/glyphs';
import { glyphRun } from '@qntx/glyphs';
import { apiJson } from './client/http';
import { createPrimaryButton } from './components/button';
import { openTokenGlyph } from './token-glyph';

/** What a token may touch. Empty is none, and '*' is everything. */
interface TokenScope {
    read: string[];
    write: string[];
}

interface CreateTokenResponse {
    id: string;
    label: string;
    token: string;
    created_at: string;
    expires_at?: string;
}

const GLYPH_ID = 'token-mint-glyph';

// What to tell when a token is minted. The list that opened this glyph is the
// thing that goes out of date the moment minting works, so it hears about it
// rather than carrying a button that asks you to notice.
let onMinted: (() => void) | undefined;

async function createToken(
    label: string,
    level: string,
    namespaces: string[],
    scope: TokenScope,
): Promise<CreateTokenResponse> {
    return await apiJson<CreateTokenResponse>('/auth/tokens', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ label, level, namespaces, scope }),
    });
}

/**
 * The two kinds a token is minted as, named the way the node names them
 * (server/auth/admission.go). The generated AccessLevel is a User's ladder and
 * numbers its members, so it does not answer for what a mint sends.
 */
export const SUPER = 'SUPER';
export const ATTESTOR = 'ATTESTOR';

/** Which of the two kinds is being minted. Naming neither is not an option. */
function kindField(): HTMLSelectElement {
    const select = document.createElement('select');
    select.style.padding = '6px 8px';
    select.style.fontFamily = 'var(--font-mono)';
    select.style.color = 'var(--text-on-dark)';
    select.style.background = 'var(--bg-dark-light)';
    select.style.border = '1px solid var(--border-on-dark)';
    select.style.borderRadius = 'var(--border-radius)';

    for (const [kind, says] of [
        [SUPER, 'does pretty much everything'],
        [ATTESTOR, 'attests the way you set it up'],
    ]) {
        const option = document.createElement('option');
        option.value = kind;
        option.textContent = `${kind} — ${says}`;
        select.appendChild(option);
    }
    return select;
}

/** A field that takes a comma-separated list. */
function listField(placeholder: string): HTMLInputElement {
    const input = document.createElement('input');
    input.type = 'text';
    input.placeholder = placeholder;
    input.size = 28;
    input.style.padding = '6px 8px';
    input.style.fontFamily = 'var(--font-mono)';
    input.style.color = 'var(--text-on-dark)';
    input.style.background = 'var(--bg-dark-light)';
    input.style.border = '1px solid var(--border-on-dark)';
    input.style.borderRadius = 'var(--border-radius)';
    return input;
}

/** Splits what was typed. A blank field is an empty list, not a list of one. */
export function asList(typed: string): string[] {
    return typed
        .split(',')
        .map(entry => entry.trim())
        .filter(entry => entry.length > 0);
}

function labelled(text: string, field: HTMLElement): HTMLElement {
    const wrap = document.createElement('label');
    wrap.style.display = 'flex';
    wrap.style.flexDirection = 'column';
    wrap.style.gap = '4px';

    const caption = document.createElement('span');
    caption.style.color = 'var(--text-on-dark-tertiary)';
    caption.textContent = text;

    wrap.append(caption, field);
    return wrap;
}

function mintGlyph(): Glyph {
    return {
        id: GLYPH_ID,
        title: 'Mint Token',
        symbol: '⚿',
        onClose: () => { glyphRun.remove(GLYPH_ID); },
        renderContent: () => {
            const content = document.createElement('div');
            content.className = 'token-mint-content';
            content.style.display = 'flex';
            content.style.flexDirection = 'column';
            content.style.gap = '10px';
            content.style.padding = '12px';
            content.style.fontFamily = 'var(--font-mono)';

            const label = listField('what this token is for');
            const kind = kindField();
            const namespaces = listField('default');
            const reads = listField('* for everything');
            const writes = listField('* for everything');

            // A SUPER token is not narrowed, so the three fields that narrow
            // one are not asked for when that is what is being minted.
            const narrowing: HTMLElement[] = [];
            const showNarrowing = () => {
                const narrowed = kind.value === ATTESTOR;
                for (const row of narrowing) row.hidden = !narrowed;
            };
            kind.addEventListener('change', showNarrowing);

            // Beside the button, not on top of it: selectable, and a press
            // copies it (same acknowledgement as tokens-glyph.ts didCell()).
            const refusal = document.createElement('div');
            refusal.className = 'tokens-refusal';
            refusal.style.color = 'var(--color-error)';
            refusal.style.wordBreak = 'break-word';
            refusal.style.overflowWrap = 'break-word';
            refusal.style.cursor = 'pointer';
            refusal.addEventListener('click', () => {
                const message = refusal.textContent;
                if (!message || message === 'copied' || message === 'refused') return;
                void navigator.clipboard.writeText(message).then(
                    () => { refusal.textContent = 'copied'; setTimeout(() => { refusal.textContent = message; }, 1200); },
                    () => { refusal.textContent = 'refused'; setTimeout(() => { refusal.textContent = message; }, 1200); },
                );
            });

            const mint = createPrimaryButton('Mint token', async () => {
                refusal.textContent = '';
                try {
                    const named = label.value.trim();
                    if (!named) {
                        throw new Error('no label');
                    }
                    const narrowed = kind.value === ATTESTOR;
                    const scope: TokenScope = narrowed
                        ? { read: asList(reads.value), write: asList(writes.value) }
                        : { read: [], write: [] };
                    const resp = await createToken(
                        named, kind.value, narrowed ? asList(namespaces.value) : [], scope);
                    label.value = '';
                    onMinted?.();
                    // The token that now exists is where the raw value belongs:
                    // one place that is about this token and nothing else.
                    openTokenGlyph(resp.id, resp.label, resp.token);
                } catch (e) {
                    refusal.textContent = e instanceof Error ? e.message : String(e);
                    throw e;
                }
            });

            narrowing.push(
                labelled('Namespaces', namespaces),
                labelled('Predicates it may read', reads),
                labelled('Predicates it may write', writes),
            );
            content.append(
                labelled('Label', label),
                labelled('Kind', kind),
                ...narrowing,
                mint.element,
                refusal,
            );
            showNarrowing();
            return content;
        },
    };
}

/**
 * Opens the mint glyph, from the Access Tokens glyph. It is built on the way in
 * and removed on close rather than registered at boot: minting is somewhere you
 * go from the list, not a thing standing in the tray beside Database and Pulse.
 */
export function openTokenMintGlyph(minted?: () => void): void {
    onMinted = minted;
    if (!glyphRun.has(GLYPH_ID)) {
        glyphRun.add(mintGlyph());
    }
    glyphRun.openGlyph(GLYPH_ID);
}
