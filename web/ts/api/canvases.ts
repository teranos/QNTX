/**
 * The canvases of the namespace stood in: listed, created, disabled,
 * enabled, owned, granted, invited to.
 */

import { apiFetch } from '../client';
import { jsonBody } from '../http-utils';
import { refusal } from '../self-person';

export interface PersonView {
    id: string;
    name: string;
    picture: string;
}

export interface CanvasRow {
    id: string;
    name: string;
    kind: 'namespace' | 'user';
    created_by: string;
    created_at: string;
    disabled_by: string;
    owners: string[];
    access: string[];
    owner_views: PersonView[];
    mine: boolean;
}

async function answered<T>(response: Response): Promise<T> {
    if (!response.ok) {
        throw new Error(await refusal(response));
    }
    return await response.json() as T;
}

/** Every canvas this person may act on where they stand. */
export async function listCanvases(): Promise<CanvasRow[]> {
    return answered<CanvasRow[]>(await apiFetch('/api/canvases'));
}

/** Creates a canvas: the namespace's own (ROOT and SUPER), or this person's. */
export async function createCanvas(name: string, kind: 'namespace' | 'user'): Promise<CanvasRow> {
    return answered<CanvasRow>(await apiFetch('/api/canvases', jsonBody('POST', { name, kind })));
}

/** Delete is disable. */
export async function disableCanvas(id: string): Promise<CanvasRow> {
    return answered<CanvasRow>(await apiFetch(`/api/canvases/${encodeURIComponent(id)}/disable`, { method: 'POST' }));
}

export async function enableCanvas(id: string): Promise<CanvasRow> {
    return answered<CanvasRow>(await apiFetch(`/api/canvases/${encodeURIComponent(id)}/enable`, { method: 'POST' }));
}

/** ROOT and SUPER: a disabled canvas leaves, whole. */
export async function nukeCanvas(id: string): Promise<void> {
    const response = await apiFetch(`/api/canvases/${encodeURIComponent(id)}/nuke`, { method: 'POST' });
    if (!response.ok) {
        throw new Error(await refusal(response));
    }
}

/** ROOT and SUPER make a User an owner outright. */
export async function addOwner(id: string, user: string): Promise<CanvasRow> {
    return answered<CanvasRow>(await apiFetch(`/api/canvases/${encodeURIComponent(id)}/owners`, jsonBody('POST', { user })));
}

export async function removeOwner(id: string, user: string): Promise<CanvasRow> {
    return answered<CanvasRow>(await apiFetch(`/api/canvases/${encodeURIComponent(id)}/owners/${encodeURIComponent(user)}`, { method: 'DELETE' }));
}

/** "made unowned" */
export async function disownCanvas(id: string): Promise<CanvasRow> {
    return answered<CanvasRow>(await apiFetch(`/api/canvases/${encodeURIComponent(id)}/owners`, { method: 'DELETE' }));
}

export async function grantAccess(id: string, user: string): Promise<CanvasRow> {
    return answered<CanvasRow>(await apiFetch(`/api/canvases/${encodeURIComponent(id)}/access`, jsonBody('POST', { user })));
}

export async function revokeAccess(id: string, user: string): Promise<CanvasRow> {
    return answered<CanvasRow>(await apiFetch(`/api/canvases/${encodeURIComponent(id)}/access/${encodeURIComponent(user)}`, { method: 'DELETE' }));
}

/** An owner invites a User by mail to own this canvas with them. */
export async function inviteOwner(id: string, email: string): Promise<void> {
    await answered<unknown>(await apiFetch(`/api/canvases/${encodeURIComponent(id)}/invite`, jsonBody('POST', { email })));
}

/** The invitee says yes, with the token the mail carried. */
export async function acceptInvitation(token: string): Promise<CanvasRow> {
    return answered<CanvasRow>(await apiFetch('/api/canvases/accept', jsonBody('POST', { token })));
}
