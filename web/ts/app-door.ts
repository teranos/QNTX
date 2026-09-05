/**
 * The app is a door. Its page lives at a scheme, and a scheme has no Safari
 * session, no autofill and no passkey, so a provider's page opened inside it
 * asks for a password nobody types on that phone. The ceremony runs in Safari
 * instead, where the person's accounts already are, and Safari hands the
 * ticket back through qntx://, which am.toml names as this door.
 *
 * "security is a server concern"
 */

import { invoke } from '@tauri-apps/api/core';
import { listen } from '@tauri-apps/api/event';
import { isTauri } from './tauri-notifications';
import { log, SEG } from './logger';

/** The origin am.toml lists in rp_origins for the app. One string, both sides. */
export const APP_DOOR = 'qntx://door';

/** Whether this page is the app's WebView rather than a browser. */
export function inApp(): boolean {
    return isTauri();
}

/**
 * The ticket carried by a deep link, or null. Only the app's own door counts:
 * the node sends a ticket nowhere else, so one arriving elsewhere is not one.
 */
export function ticketIn(urls: string[]): string | null {
    for (const raw of urls) {
        const cut = raw.indexOf('?');
        if (cut === -1) continue;
        if (raw.slice(0, cut) !== APP_DOOR) continue;
        const ticket = new URLSearchParams(raw.slice(cut + 1)).get('ceremony');
        if (ticket) return ticket;
    }
    return null;
}

/** Opens the URL in Safari. The shell plugin is what the app allows for it. */
export async function openInSafari(url: string): Promise<void> {
    await invoke('plugin:shell|open', { path: url });
}

/**
 * The ticket a deep link already delivered. An app launched by the link,
 * because the person closed it while Safari had the ceremony, finds it here
 * rather than in an event that fired before anyone listened.
 */
export async function ticketWaiting(): Promise<string | null> {
    const urls = await invoke<string[] | null>('plugin:deep-link|get_current');
    return urls ? ticketIn(urls) : null;
}

/**
 * The next ticket to arrive by deep link. Resolves once, with the ticket; the
 * listener is released either way.
 */
export function nextTicket(signal: AbortSignal): Promise<string> {
    return new Promise((resolve, reject) => {
        let release: (() => void) | null = null;
        const stop = () => { release?.(); release = null; };
        signal.addEventListener('abort', () => { stop(); reject(new Error('the ceremony was abandoned before Safari came back')); });
        listen<string[]>('deep-link://new-url', (event) => {
            const ticket = ticketIn(event.payload);
            if (!ticket) {
                log.warn(SEG.UI, '[Door] a deep link arrived carrying no ticket:', event.payload);
                return;
            }
            stop();
            resolve(ticket);
        }).then((unlisten) => {
            release = unlisten;
            if (signal.aborted) stop();
        }, (err: unknown) => {
            reject(new Error(`could not listen for the deep link: ${err}`));
        });
    });
}
