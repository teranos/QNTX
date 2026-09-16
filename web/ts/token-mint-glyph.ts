/**
 * Mint Token Glyph — creating one access token (ADR-025, TOKATTEST).
 * Split out of the Access Tokens glyph, which stays the place you see every
 * token at once. The raw value is shown once, here and nowhere else.
 */

import type { Glyph } from '@qntx/glyphs';
import { glyphRun } from '@qntx/glyphs';
import { apiJson } from './client/http';
import { createPrimaryButton } from './components/button';
import { ordered, type Namespace } from './namespaces-view';
import { person } from './self-person';
import { openTokenGlyph } from './token-glyph';

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

// What a token may read and write is not asked here: the roles its DID holds
// say, through their WRITE and READ lines (ADR-034).
async function createToken(
    label: string,
    level: string,
    namespaces: string[],
): Promise<CreateTokenResponse> {
    return await apiJson<CreateTokenResponse>('/auth/tokens', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ label, level, namespaces }),
    });
}

/**
 * The two kinds a token is minted as, named the way the node names them
 * (server/auth/admission.go). The generated AccessLevel is a User's ladder and
 * numbers its members, so it does not answer for what a mint sends.
 */
export const SUPER = 'SUPER';
export const ATTESTOR = 'ATTESTOR';

function styled<T extends HTMLElement>(field: T): T {
    field.style.padding = '6px 8px';
    field.style.fontFamily = 'var(--font-mono)';
    field.style.color = 'var(--text-on-dark)';
    field.style.background = 'var(--bg-dark-light)';
    field.style.border = '1px solid var(--border-on-dark)';
    field.style.borderRadius = 'var(--border-radius)';
    return field;
}

function option(value: string, text: string): HTMLOptionElement {
    const made = document.createElement('option');
    made.value = value;
    made.textContent = text;
    return made;
}

/** Which of the two kinds is being minted. Naming neither is not an option. */
function kindField(): HTMLSelectElement {
    const select = styled(document.createElement('select'));
    for (const [kind, says] of [
        [SUPER, 'does pretty much everything'],
        [ATTESTOR, 'attests what the roles its DID holds say'],
    ]) {
        select.appendChild(option(kind, `${kind} — ${says}`));
    }
    return select;
}

/** A name is the one thing a person types here. */
function labelField(): HTMLInputElement {
    const input = styled(document.createElement('input'));
    input.type = 'text';
    input.placeholder = 'what this token is for';
    input.size = 28;
    return input;
}

/** Every namespace the node lists, in the bar's order, set to where you stand.
 *  Standing nowhere is default; a standing the list lacks starts on nothing,
 *  rather than on a namespace you are not in. */
export function namespacePick(namespaces: Namespace[], standing: string): { names: string[]; chosen: string } {
    const names = ordered(namespaces).map(ns => ns.name);
    const here = standing === '' ? 'default' : standing;
    return { names, chosen: names.includes(here) ? here : '' };
}

// "I SHOULD NOT HAVE TO TYPE THE NAMESPACE NAME"
// "NO, TYPING IS NEVER"
function namespaceField(): HTMLSelectElement {
    const select = styled(document.createElement('select'));
    select.appendChild(option('', 'reading namespaces…'));
    select.disabled = true;

    Promise.all([
        apiJson<{ namespaces: Namespace[] }>('/api/namespaces'),
        person(),
    ]).then(([listed, who]) => {
        const { names, chosen } = namespacePick(listed.namespaces || [], who.standing);
        select.innerHTML = '';
        select.appendChild(option('', 'pick a namespace'));
        for (const name of names) select.appendChild(option(name, name));
        select.value = chosen;
        select.disabled = false;
    }).catch((err: unknown) => {
        // The pick is where the failure is met, so the whole answer stands in it.
        select.innerHTML = '';
        select.appendChild(option('', err instanceof Error ? err.message : String(err)));
    });
    return select;
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

            const label = labelField();
            const kind = kindField();
            const namespace = namespaceField();

            // A SUPER token is not narrowed, so the field that narrows one is
            // not asked for when that is what is being minted.
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
                    if (narrowed && namespace.value === '') {
                        throw new Error('no namespace picked');
                    }
                    const resp = await createToken(
                        named, kind.value, narrowed ? [namespace.value] : []);
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

            narrowing.push(labelled('Namespace', namespace));
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
