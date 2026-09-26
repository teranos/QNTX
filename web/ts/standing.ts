/**
 * The namespace this page acts in, and the storage that is its.
 *
 * "A canvas lives in one namespace and only that one" (ADR-026). The browser
 * keeps one canvas per namespace, so what the page stores is keyed by where
 * the person is standing, set once at boot from what the node answers.
 * Stepping to another namespace reloads the page (namespaces-bar.ts), so
 * this never changes while the page is up.
 */

let standing = '';

/** The namespace set at boot. Empty is a node with none, or nobody in. */
export function standingNamespace(): string {
    return standing;
}

/** Where the node said this person stands. Called once, before any state loads. */
export function setStanding(namespace: string): void {
    standing = namespace;
}

/**
 * The key a namespace's copy of some browser state is kept under. Default and
 * nowhere share the key the browser always used, so what it holds today is
 * default's, and a node with one namespace never sees the difference.
 */
export function keyFor(base: string, namespace: string = standing): string {
    if (namespace === '' || namespace === 'default') return base;
    return `${base}:${namespace}`;
}
