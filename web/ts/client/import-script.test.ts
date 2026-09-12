/**
 * Where a page may import a module from.
 *
 * backendPath answers where the node is, and is right for a fetch, an
 * EventSource, an <img src>. A script is judged under `script-src` rather than
 * `connect-src`, so the same origin can be permitted for data and refused for
 * code — and reaching for backendPath here is what put every published glyph
 * behind a CSP refusal on the web after a fix for the app.
 *
 * importScript is the one that knows the difference. A test can arrange
 * neither a CSP refusal nor an app scheme, so what it arranges is the loader:
 * the rule under test is the real one, and only the browser is stood in for.
 */

import { describe, test, expect, beforeEach, afterEach } from 'bun:test';
import { importScript, scriptsComeFrom, forgetWhereScriptsComeFrom } from './url';

const NODE = 'https://api.example.nl';
const MODULE = { glyphDef: {}, render: () => null };

/** A loader that records what it was asked for, and answers per url. */
function loaderThat(answers: (url: string) => boolean) {
    const asked: string[] = [];
    return {
        asked,
        loads: async (url: string) => {
            asked.push(url);
            if (!answers(url)) throw new Error(`refused ${url}`);
            return MODULE;
        },
    };
}

const fromNode = (url: string) => url.startsWith(NODE);
const fromPage = (url: string) => !url.startsWith(NODE);

beforeEach(() => {
    (window as unknown as { __BACKEND_URL__?: string }).__BACKEND_URL__ = NODE;
    forgetWhereScriptsComeFrom();
});

afterEach(() => {
    delete (window as unknown as { __BACKEND_URL__?: string }).__BACKEND_URL__;
    forgetWhereScriptsComeFrom();
});

describe('a page behind an edge that forwards the path', () => {
    test('imports same-origin, which is what script-src permits', async () => {
        const loader = loaderThat(fromPage);

        await importScript('/g/crier.js', loader.loads);

        expect(scriptsComeFrom()).toBe('page');
        expect(loader.asked).toEqual(['/g/crier.js']);
    });

    test('every module after the first goes straight there', async () => {
        const loader = loaderThat(fromPage);

        await importScript('/g/crier.js', loader.loads);
        await importScript('/g/hello.js', loader.loads);

        expect(loader.asked).toEqual(['/g/crier.js', '/g/hello.js']);
    });
});

describe('an app at its own scheme, with no edge in front of it', () => {
    test('falls back to the node when the page answers with its own html', async () => {
        const loader = loaderThat(fromNode);

        await importScript('/g/crier.js', loader.loads);

        expect(scriptsComeFrom()).toBe('node');
        expect(loader.asked).toEqual(['/g/crier.js', `${NODE}/g/crier.js`]);
    });

    test('what it found out costs the next module nothing', async () => {
        const loader = loaderThat(fromNode);

        await importScript('/g/crier.js', loader.loads);
        loader.asked.length = 0;
        await importScript('/g/hello.js', loader.loads);

        expect(loader.asked).toEqual([`${NODE}/g/hello.js`]);
    });
});

describe('when neither answers', () => {
    test('the failure names both, because each is silent in its own way', async () => {
        const loader = loaderThat(() => false);

        const failed = await importScript('/g/crier.js', loader.loads).catch((err: unknown) => err);

        expect(String(failed)).toContain('/g/crier.js');
        expect(String(failed)).toContain(`${NODE}/g/crier.js`);
    });

    test('nothing is remembered, so the next module asks again', async () => {
        const loader = loaderThat(() => false);

        // The rejection is the point of the case above; here it is expected,
        // and named rather than dropped.
        const failed = await importScript('/g/crier.js', loader.loads).catch((err: unknown) => err);

        expect(failed).toBeInstanceOf(Error);
        expect(scriptsComeFrom()).toBeNull();
    });
});

describe('a page the node itself serves', () => {
    test('has nothing to choose between and nothing to remember', async () => {
        delete (window as unknown as { __BACKEND_URL__?: string }).__BACKEND_URL__;
        const loader = loaderThat(() => true);

        await importScript('/g/crier.js', loader.loads);

        // window.location.origin + path, asked once and only once.
        expect(loader.asked).toHaveLength(1);
        expect(scriptsComeFrom()).toBeNull();
    });
});
