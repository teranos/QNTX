// File upload and import handling

import { sendMessage } from './websocket.ts';
import { toast } from './components/toast';
import type { ImportProgressData, ImportStatsData, ImportCompleteData } from '../types/websocket';

// Handle import progress updates
export function handleImportProgress(data: ImportProgressData): void {
    const progress = document.getElementById('import-progress');
    const fill = document.getElementById('progress-fill');
    const status = document.getElementById('import-status');
    const details = document.getElementById('import-details');

    if (!progress || !fill || !status || !details) return;

    progress.classList.add('active');

    if (data.current && data.total) {
        const percent = Math.round((data.current / data.total) * 100);
        // Inline style required here for dynamic percentage calculation
        fill.style.width = percent + '%';
        details.textContent = 'Processing ' + data.current + ' of ' + data.total;
    }

    if (data.message) {
        status.textContent = data.message;
    }
}

// Handle import stats updates
export function handleImportStats(data: ImportStatsData): void {
    if (data.contacts) {
        const contactsEl = document.getElementById('stat-contacts');
        if (contactsEl) contactsEl.textContent = data.contacts.toString();
    }
    if (data.attestations) {
        const attestationsEl = document.getElementById('stat-attestations');
        if (attestationsEl) attestationsEl.textContent = data.attestations.toString();
    }
    if (data.companies) {
        const companiesEl = document.getElementById('stat-companies');
        if (companiesEl) companiesEl.textContent = data.companies.toString();
    }
}

// Handle import completion
export function handleImportComplete(data: ImportCompleteData): void {
    const status = document.getElementById('import-status');
    const fill = document.getElementById('progress-fill');

    if (fill) {
        fill.classList.remove('u-w-0', 'u-w-25', 'u-w-50', 'u-w-75');
        fill.classList.add('u-w-100');
    }
    if (status) {
        status.textContent = data.message || 'Import complete!';
    }

    // Show toast notification
    if (data.success) {
        const statsMsg = data.stats
            ? ` (${data.stats.imported} imported, ${data.stats.skipped} skipped)`
            : '';
        toast.success(`Import complete${statsMsg}`);
    } else {
        toast.error(data.message || 'Import failed');
    }

    // Auto-hide progress after 3 seconds
    setTimeout(() => {
        const progress = document.getElementById('import-progress');
        if (progress) {
            progress.classList.remove('active');
        }
    }, 3000);
}

// Handle file upload
function handleFileUpload(files: FileList | File[]): void {
    if (!files || files.length === 0) return;

    for (let file of files) {
        const reader = new FileReader();

        reader.onload = function(e: ProgressEvent<FileReader>): void {
            if (!e.target?.result) return;

            const content = e.target.result as string;
            const filename = file.name;
            let fileType = 'unknown';

            // Detect file type
            if (filename.toLowerCase().includes('connection') && filename.endsWith('.csv')) {
                fileType = 'linkedin';
            } else if (filename.endsWith('.vcf')) {
                fileType = 'vcf';
            } else if (filename.endsWith('.csv')) {
                fileType = 'csv';
            } else if (filename.toLowerCase().endsWith('.html') || filename.toLowerCase().endsWith('.htm')) {
                fileType = 'html';
            }

            // Send via WebSocket
            const message = {
                type: 'upload',
                filename: filename,
                fileType: fileType,
                data: btoa(content)  // Base64 encode
            };

            sendMessage(message);

            // Show progress UI
            const importProgress = document.getElementById('import-progress');
            const importStatus = document.getElementById('import-status');
            const progressFill = document.getElementById('progress-fill');

            if (importProgress) importProgress.classList.add('active');
            if (importStatus) importStatus.textContent = 'Uploading ' + filename + '...';
            if (progressFill) {
                progressFill.classList.remove('u-w-25', 'u-w-50', 'u-w-75', 'u-w-100');
                progressFill.classList.add('u-w-0');
            }
        };

        reader.readAsText(file);
    }
}

// Initialize file drop zone on query area
export function initQueryFileDrop(): void {
    const dropZone = document.getElementById('query-drop-zone');
    const dropIndicator = document.getElementById('drop-indicator');
    const queryInput = document.getElementById('ats-editor') as HTMLTextAreaElement | null;

    if (!dropZone || !dropIndicator || !queryInput) return;

    // Drag over
    dropZone.addEventListener('dragover', (e: DragEvent) => {
        e.preventDefault();
        dropZone.classList.add('dragging');
        dropIndicator.classList.remove('u-hidden');
        dropIndicator.classList.add('u-flex');
    });

    // Drag leave
    dropZone.addEventListener('dragleave', (e: DragEvent) => {
        // Only remove if we're really leaving the drop zone
        if (e.target === dropZone) {
            dropZone.classList.remove('dragging');
            dropIndicator.classList.add('u-hidden');
        }
    });

    // Drop
    dropZone.addEventListener('drop', (e: DragEvent) => {
        e.preventDefault();
        dropZone.classList.remove('dragging');
        dropIndicator.classList.add('u-hidden');
        dropIndicator.classList.remove('u-flex');

        const files = e.dataTransfer?.files;
        if (!files || files.length === 0) return;

        const file = files[0];
        const filename = file.name.toLowerCase();

        // For HTML files, upload and process directly
        if (filename.endsWith('.html') || filename.endsWith('.htm')) {
            handleFileUpload([file]);
            return;
        }

        // For other recognized file types, also upload directly
        if ((filename.includes('connection') && filename.endsWith('.csv')) ||
            filename.endsWith('.vcf') ||
            filename.endsWith('.csv')) {
            handleFileUpload([file]);
            return;
        }

        // For unknown file types, create ix command
        queryInput.value = 'ix ' + file.name;
        queryInput.dispatchEvent(new Event('input'));
    });
}

export {};
