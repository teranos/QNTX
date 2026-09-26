/**
 * The namespace this page acts in, the canvas open in it, and the storage
 * that is theirs.
 *
 * "A canvas lives in one namespace and only that one" (ADR-026), and a
 * namespace holds many. The browser keeps one copy of the canvas state per
 * (namespace, canvas), keyed by where the person stands and which canvas they
 * opened, both set once at boot. Stepping to another namespace or opening
 * another canvas reloads the page, so neither changes while it is up.
 */

let standing = '';
let open = '';

/** The namespace set at boot. Empty is a node with none, or nobody in. */
export function standingNamespace(): string {
    return standing;
}

/** Where the node said this person stands. Called once, before any state loads. */
export function setStanding(namespace: string): void {
    standing = namespace;
}

/**
 * The canvas open where this person stands. Empty is the namespace's own
 * canvas, which keeps the key the browser always used.
 */
export function openCanvas(): string {
    return open;
}

/** Which canvas the page was built for. Called once, before any state loads. */
export function setOpenCanvas(id: string): void {
    open = id;
}

/** The key the browser remembers which canvas is open under, per namespace. */
export function openCanvasKey(namespace: string = standing): string {
    return keyForNamespace('qntx-open-canvas', namespace);
}

/**
 * The key under which one deliberate opening of a disabled canvas waits: a
 * disabled canvas is "seen by opening it", never opened on its own.
 */
export function openOnceKey(namespace: string = standing): string {
    return keyForNamespace('qntx-open-canvas-once', namespace);
}

/**
 * The key a namespace's copy of some browser state is kept under. Default and
 * nowhere share the key the browser always used, so what it holds today is
 * default's, and a node with one namespace never sees the difference.
 */
export function keyForNamespace(base: string, namespace: string = standing): string {
    if (namespace === '' || namespace === 'default') return base;
    return `${base}:${namespace}`;
}

/**
 * The key for the canvas open in the namespace stood in: the namespace's own
 * canvas keeps the namespace's key, and a User's canvas has its own.
 */
export function keyFor(base: string, namespace: string = standing, canvas: string = open): string {
    const key = keyForNamespace(base, namespace);
    if (canvas === '') return key;
    return `${key}:${canvas}`;
}

/** What every canvas route is asked with: the canvas open, when it is not the namespace's. */
export function canvasQuery(): string {
    return open === '' ? '' : `?canvas=${encodeURIComponent(open)}`;
}
