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

async function createToken(
    label: string,
    namespaces: string[],
    scope: TokenScope,
): Promise<CreateTokenResponse> {
    return await apiJson<CreateTokenResponse>('/auth/tokens', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ label, namespaces, scope }),
    });
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
            const namespaces = listField('default');
            const reads = listField('* for everything');
            const writes = listField('* for everything');

            // The button turns its own label into Error and hides the reason in
            // a tooltip. Minting a credential is not a place for that.
            const refusal = document.createElement('div');
            refusal.className = 'tokens-refusal';
            refusal.style.color = 'var(--color-error)';
            refusal.style.wordBreak = 'break-word';
            refusal.style.overflowWrap = 'break-word';

            const mint = createPrimaryButton('Mint token', async () => {
                refusal.textContent = '';
                try {
                    const named = label.value.trim();
                    if (!named) {
                        throw new Error('no label');
                    }
                    const scope: TokenScope = {
                        read: asList(reads.value),
                        write: asList(writes.value),
                    };
                    const resp = await createToken(named, asList(namespaces.value), scope);
                    label.value = '';
                    // The token that now exists is where the raw value belongs:
                    // one place that is about this token and nothing else.
                    openTokenGlyph(resp.id, resp.label, resp.token);
                } catch (e) {
                    refusal.textContent = e instanceof Error ? e.message : String(e);
                    throw e;
                }
            });

            content.append(
                labelled('Label', label),
                labelled('Namespaces', namespaces),
                labelled('Predicates it may read', reads),
                labelled('Predicates it may write', writes),
                mint.element,
                refusal,
            );
            return content;
        },
    };
}

/**
 * Opens the mint glyph, from the Access Tokens glyph. It is built on the way in
 * and removed on close rather than registered at boot: minting is somewhere you
 * go from the list, not a thing standing in the tray beside Database and Pulse.
 */
export function openTokenMintGlyph(): void {
    if (!glyphRun.has(GLYPH_ID)) {
        glyphRun.add(mintGlyph());
    }
    glyphRun.openGlyph(GLYPH_ID);
}
