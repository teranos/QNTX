/**
 * File API Client
 *
 * Upload files to the backend and get serve URLs.
 */

import type { FileUploadResult } from '../generated/proto/glyph/proto/files';
import { apiJson, backendUrl } from '../client';
import { log, SEG } from '../logger';

export type { FileUploadResult };

/**
 * Upload a file to the backend.
 * Returns the stored file metadata including the ID used to retrieve it.
 */
export async function uploadFile(file: File): Promise<FileUploadResult> {
    const form = new FormData();
    form.append('file', file);

    const result = await apiJson<FileUploadResult>('/api/files', {
        method: 'POST',
        body: form,
    });
    log.info(SEG.GLYPH, `[FileAPI] Uploaded ${result.filename} (${result.size} bytes) → ${result.id}`);
    return result;
}

/**
 * Returns the URL to serve a stored file by ID (with extension).
 */
export function fileUrl(id: string, ext: string): string {
    return `${backendUrl()}/api/files/${id}${ext}`;
}
