/**
 * The session an app holds. A door on another site gets no cookie back from
 * the node, so the session the node hands it is held here and presented as a
 * bearer on every fetch and on the socket. Empty is none held: the node's own
 * web never holds one, its cookie does that.
 *
 * Leaf module: nothing from client/ is imported here.
 */

const KEY = 'qntx.session';

/** The session held, or empty. Storage that throws is no session. */
export function heldSession(): string {
    try {
        return localStorage.getItem(KEY) ?? '';
    } catch (err: unknown) {
        void err;
        return '';
    }
}

/** Holds the session the node handed over. */
export function holdSession(token: string): void {
    try {
        localStorage.setItem(KEY, token);
    } catch (err: unknown) {
        void err;
    }
}

/** Lets go of it: logging out, or the node saying it is not honoured. */
export function dropSession(): void {
    try {
        localStorage.removeItem(KEY);
    } catch (err: unknown) {
        void err;
    }
}

/** The headers a request carries when a session is held. */
export function bearing(init?: HeadersInit): Headers {
    const headers = new Headers(init);
    const held = heldSession();
    if (held && !headers.has('Authorization')) headers.set('Authorization', `Bearer ${held}`);
    return headers;
}
