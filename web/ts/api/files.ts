/**
 * File API Client
 *
 * Upload files to the backend and get serve URLs.
 */

import type { FileUploadResult } from '../generated/proto/glyph/proto/files';
import { apiFetch, backendUrl } from '../client';
import { assertOk } from '../http-utils';
import { log, SEG } from '../logger';

export type { FileUploadResult };

/**
 * Upload a file to the backend.
 * Returns the stored file metadata including the ID used to retrieve it.
 */
export async function uploadFile(file: File): Promise<FileUploadResult> {
    const form = new FormData();
    form.append('file', file);

    const response = await apiFetch('/api/files', {
        method: 'POST',
        body: form,
    });

    await assertOk(response, 'File upload failed');

    const result: FileUploadResult = await response.json();
    log.info(SEG.GLYPH, `[FileAPI] Uploaded ${result.filename} (${result.size} bytes) → ${result.id}`);
    return result;
}

/**
 * Returns the URL to serve a stored file by ID (with extension).
 */
export function fileUrl(id: string, ext: string): string {
    return `${backendUrl()}/api/files/${id}${ext}`;
}
