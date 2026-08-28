/**
 * Relaying the dev server to a backend that is not on this machine.
 * Pure rules only — dev-server.ts owns the sockets.
 */

export interface Backend {
    url: string;
    isRemote: boolean;
}

/** QNTX_BACKEND names a whole backend; without it, a port beside us. */
export function resolveBackend(env: Record<string, string | undefined>, backendPort: number): Backend {
    const named = env.QNTX_BACKEND;
    if (!named) {
        return { url: `http://localhost:${backendPort}`, isRemote: false };
    }
    const url = named.endsWith('/') ? named.slice(0, -1) : named;
    const isRemote = url.indexOf('localhost') === -1 && url.indexOf('127.0.0.1') === -1;
    return { url, isRemote };
}

/** What the relay presents as itself. A token says whose it is; a session is borrowed. */
export interface Credential {
    token?: string;
    session?: string;
}

/** A token is preferred: it is minted, scoped and revocable on its own (ADR-025). */
export function resolveCredential(env: Record<string, string | undefined>): Credential {
    if (env.QNTX_TOKEN) {
        return { token: env.QNTX_TOKEN };
    }
    if (env.QNTX_SESSION) {
        return { session: env.QNTX_SESSION };
    }
    return {};
}

/**
 * allowed_origins names where browsers come from, and localhost is in the
 * defaults. Empty leaves the browser's own origin, which is such a browser.
 */
export function resolveOrigin(env: Record<string, string | undefined>): string {
    return env.QNTX_ORIGIN || '';
}

/**
 * The node checks Origin against allowed_origins and refuses a socket carrying
 * none, so the browser's is forwarded. Host describes the hop, not the message.
 */
export function backendHeaders(incoming: Headers, credential: Credential, origin: string): Headers {
    const h = new Headers(incoming);
    if (origin) {
        h.set('Origin', origin);
    }
    h.delete('host');

    // The relay authenticates as itself. Whatever the browser held is for the
    // dev server's own origin and means nothing to the node.
    h.delete('cookie');
    h.delete('authorization');

    if (credential.token) {
        h.set('Authorization', `Bearer ${credential.token}`);
    } else if (credential.session) {
        h.set('Cookie', `qntx_session=${credential.session}`);
    }
    return h;
}

/**
 * The browser is never handed the node's cookie. Minted by an https node it
 * arrives Secure, and the session travels on the relay's own hop anyway.
 */
export function dropSetCookie(response: Response): Response {
    if (response.headers.getSetCookie().length === 0) {
        return response;
    }
    const headers = new Headers(response.headers);
    headers.delete('set-cookie');
    return new Response(response.body, { status: response.status, statusText: response.statusText, headers });
}

// What the node answers, as opposed to the bundle this server is serving.
// /auth belongs here or the page reads as logged out against a live session.
const BACKEND_PREFIXES = ["/api", "/ws", "/lsp", "/auth", "/setup", "/health", "/.well-known", "/logs"];

/** A prefix matches a whole segment, so /authors is not /auth. */
export function isBackendPath(pathname: string): boolean {
    return BACKEND_PREFIXES.some(prefix => {
        if (!pathname.startsWith(prefix)) {
            return false;
        }
        const rest = pathname.slice(prefix.length);
        return rest === "" || rest.startsWith("/");
    });
}

/** The socket follows the scheme the backend answers on. */
export function backendWsUrl(backendUrl: string, pathWithQuery: string): string {
    const base = backendUrl.startsWith('https://')
        ? `wss://${backendUrl.slice('https://'.length)}`
        : `ws://${backendUrl.slice('http://'.length)}`;
    return base + pathWithQuery;
}
