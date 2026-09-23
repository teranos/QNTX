/**
 * Mint Token Element — creating one access token (ADR-025, TOKATTEST).
 * Split out of the Access Tokens element, which stays the place you see every
 * token at once. The raw value is shown once, here and nowhere else.
 */

import type { Element } from '@teranos/elements';
import { tray } from '@teranos/elements';
import { apiJson } from './client/http';
import { createPrimaryButton } from './components/button';
import { ordered, type Namespace } from './namespaces-view';
import { person } from './self-person';
import { openTokenElement } from './token-element';

interface CreateTokenResponse {
    id: string;
    label: string;
    token: string;
    created_at: string;
    expires_at?: string;
}

const ELEMENT_ID = 'token-mint-element';

// What to tell when a token is minted. The list that opened this element is the
// thing that goes out of date the moment minting works, so it hears about it
// rather than carrying a button that asks you to notice.
let onMinted: (() => void) | undefined;

// What a token may read and write is not asked here: the roles its DID holds
// say, through their WRITE and READ lines (ADR-034).
async function createToken(
    label: string,
    level: string,
    namespaces: string[],
    returnAddress: string,
): Promise<CreateTokenResponse> {
    // A client's, and only a client's: the node refuses one on any other kind.
    const body: Record<string, unknown> = { label, level, namespaces };
    if (returnAddress) body.return_address = returnAddress;
    return await apiJson<CreateTokenResponse>('/auth/tokens', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(body),
    });
}

/**
 * The three kinds a token is minted as, named the way the node names them
 * (server/auth/admission.go). The generated AccessLevel is a User's ladder and
 * numbers its members, so it does not answer for what a mint sends.
 */
export const SUPER = 'SUPER';
export const ATTESTOR = 'ATTESTOR';
export const OAUTH = 'OAUTH';

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

/** What each kind says of itself, in the order the rows are drawn. */
export const KINDS: ReadonlyArray<readonly [string, string]> = [
    [SUPER, 'does pretty much everything'],
    [ATTESTOR, 'attests what the roles its DID holds say'],
    [OAUTH, 'is a door: an app let in on your say-so'],
];

/** Which kind is being minted: the pressed row, or none while none is. */
export interface KindRows {
    element: HTMLElement;
    /** The pressed kind, or '' until somebody presses one. */
    readonly value: string;
    onChange(listener: () => void): void;
}

/**
 * One row per kind, none pressed until somebody presses one.
 *
 * "making a selection between two mutually exclusive options and it being
 * instantiated having neither selected"
 *
 * A select opened on its first option, which was SUPER, and a mint that
 * never touched it minted the widest kind. Naming a kind is still not
 * optional: the mint refuses until a row is pressed, and so does the node.
 */
export function kindRows(): KindRows {
    const group = document.createElement('div');
    group.setAttribute('role', 'radiogroup');
    group.style.display = 'flex';
    group.style.flexDirection = 'column';
    group.style.gap = '4px';

    let pressed = '';
    const listeners: Array<() => void> = [];
    const rows: HTMLButtonElement[] = [];

    for (const [kind, says] of KINDS) {
        const row = document.createElement('button');
        row.type = 'button';
        row.setAttribute('role', 'radio');
        row.setAttribute('aria-checked', 'false');
        row.dataset.kind = kind;
        row.textContent = `${kind} — ${says}`;
        row.style.textAlign = 'left';
        row.style.padding = '6px 8px';
        row.style.fontFamily = 'var(--font-mono)';
        row.style.color = 'var(--text-on-dark)';
        row.style.background = 'var(--bg-dark-light)';
        row.style.border = '1px solid var(--border-on-dark)';
        row.style.borderRadius = 'var(--border-radius)';
        row.style.cursor = 'pointer';
        row.addEventListener('click', () => {
            pressed = kind;
            for (const other of rows) {
                const isThis = other === row;
                other.setAttribute('aria-checked', isThis ? 'true' : 'false');
                other.style.borderColor = isThis ? 'var(--text-on-dark)' : 'var(--border-on-dark)';
            }
            for (const listener of listeners) listener();
        });
        rows.push(row);
        group.appendChild(row);
    }

    return {
        element: group,
        get value() { return pressed; },
        onChange(listener) { listeners.push(listener); },
    };
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

function mintElement(): Element {
    return {
        id: ELEMENT_ID,
        title: 'Mint Token',
        symbol: '⚿',
        onClose: () => { tray.remove(ELEMENT_ID); },
        renderContent: () => {
            const content = document.createElement('div');
            renderMint(content);
            return content;
        },
    };
}

/** Exported for tests: the mint form, drawn into a container. */
export function renderMint(content: HTMLElement): void {
    content.className = 'token-mint-content';
    content.style.display = 'flex';
    content.style.flexDirection = 'column';
    content.style.gap = '10px';
    content.style.padding = '12px';
    content.style.fontFamily = 'var(--font-mono)';

    const label = labelField();
    const kind = kindRows();
    const namespace = namespaceField();
    // Where a client's codes go. Written here by the same hand that
    // writes a door's origin in am.toml (ADR-025).
    const returnAddress = styled(document.createElement('input'));
    returnAddress.type = 'text';
    returnAddress.placeholder = 'https://app.example/callback';
    returnAddress.size = 28;

    // A SUPER token is not narrowed, so the field that narrows one is
    // not asked for when that is what is being minted. A client's
    // connector acts in the namespace picked here and nowhere else
    // (ADR-038), and a client is also asked where its codes go.
    const narrowing: HTMLElement[] = [];
    const returning: HTMLElement[] = [];
    const showNarrowing = () => {
        const narrowed = kind.value === ATTESTOR || kind.value === OAUTH;
        for (const row of narrowing) row.hidden = !narrowed;
        const client = kind.value === OAUTH;
        for (const row of returning) row.hidden = !client;
    };
    kind.onChange(showNarrowing);

    // Beside the button, not on top of it: selectable, and a press
    // copies it (same acknowledgement as tokens-element.ts didCell()).
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
            if (!kind.value) {
                throw new Error('no kind');
            }
            const narrowed = kind.value === ATTESTOR || kind.value === OAUTH;
            if (narrowed && namespace.value === '') {
                throw new Error('no namespace picked');
            }
            const client = kind.value === OAUTH;
            const resp = await createToken(
                named, kind.value, narrowed ? [namespace.value] : [],
                client ? returnAddress.value.trim() : '');
            label.value = '';
            onMinted?.();
            // The token that now exists is where the raw value belongs:
            // one place that is about this token and nothing else.
            openTokenElement(resp.id, resp.label, resp.token);
        } catch (e) {
            refusal.textContent = e instanceof Error ? e.message : String(e);
            throw e;
        }
    });

    narrowing.push(labelled('Namespace', namespace));
    returning.push(labelled('Return address', returnAddress));
    content.append(
        labelled('Label', label),
        labelled('Kind', kind.element),
        ...narrowing,
        ...returning,
        mint.element,
        refusal,
    );
    showNarrowing();
}

/**
 * Opens the mint element, from the Access Tokens element. It is built on the way in
 * and removed on close rather than registered at boot: minting is somewhere you
 * go from the list, not a thing standing in the tray beside Database and Pulse.
 */
export function openTokenMintElement(minted?: () => void): void {
    onMinted = minted;
    if (!tray.has(ELEMENT_ID)) {
        tray.add(mintElement());
    }
    tray.open(ELEMENT_ID);
}
