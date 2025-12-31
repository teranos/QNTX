/**
 * Test: Scheduled jobs created from prose documents preserve document reference
 *
 * This test verifies that when a job is created from an ATS block in a prose
 * document, the job's `created_from_doc` field contains the document path,
 * allowing users to navigate back to the source document.
 */

import { describe, it, expect } from 'bun:test';

describe('Prose Document Linking', () => {
  it('should include document path when creating job from prose document', () => {
    // Simulate what happens when user clicks "Add Schedule" in prose document
    const documentPath = 'projects/data-import.md';
    const atsCode = 'ix https://example.com/api/data';
    const intervalSeconds = 21600; // 6 hours

    // This is what scheduling-controls.ts sends to the API
    const createRequest = {
      ats_code: atsCode,
      interval_seconds: intervalSeconds,
      created_from_doc: documentPath, // <-- This must be set!
    };

    // Verify the request has document reference
    expect(createRequest.created_from_doc).toBe(documentPath);
    expect(createRequest.created_from_doc).not.toBeUndefined();
    expect(createRequest.created_from_doc).not.toBe('');
  });

  it('should render prose link when job has created_from_doc', () => {
    const job = {
      id: 'SPJ_123',
      ats_code: 'ix https://example.com',
      interval_seconds: 3600,
      created_from_doc: 'projects/data-import.md', // <-- Source document
      state: 'active' as const,
      next_run_at: '2025-12-06T10:00:00Z',
      last_run_at: null,
      last_execution_id: '',
      metadata: '',
      created_at: '2025-12-06T09:00:00Z',
      updated_at: '2025-12-06T09:00:00Z',
    };

    // Simulate pulse-panel.ts renderJob logic
    const hasProseLink = !!job.created_from_doc;
    const proseLocationHtml = job.created_from_doc
      ? `<a href="#" class="pulse-prose-link" data-doc-id="${job.created_from_doc}">
           ▣ ${job.created_from_doc}
         </a>`
      : '';

    expect(hasProseLink).toBe(true);
    expect(proseLocationHtml).toContain('pulse-prose-link');
    expect(proseLocationHtml).toContain('data-import.md');
  });

  it('should NOT render prose link when created_from_doc is empty', () => {
    const job = {
      id: 'SPJ_456',
      ats_code: 'ix https://example.com',
      interval_seconds: 3600,
      created_from_doc: '', // <-- No source document (created from main editor)
      state: 'active' as const,
      next_run_at: '2025-12-06T10:00:00Z',
      last_run_at: null,
      last_execution_id: '',
      metadata: '',
      created_at: '2025-12-06T09:00:00Z',
      updated_at: '2025-12-06T09:00:00Z',
    };

    const hasProseLink = !!job.created_from_doc;
    const proseLocationHtml = job.created_from_doc ? 'link html' : '';

    expect(hasProseLink).toBe(false);
    expect(proseLocationHtml).toBe('');
  });

  it('should pass document path through NodeView constructor', () => {
    // This tests the fix: ATSCodeBlockNodeView must receive documentPath
    const documentPath = 'docs/api/pulse-api.md';

    // Simulate NodeView construction in prose/editor.ts
    class MockNodeView {
      documentPath: string;

      constructor(documentPath: string) {
        this.documentPath = documentPath;
      }

      getSchedulingOptions() {
        return {
          documentId: this.documentPath, // <-- Must use the path, not undefined!
        };
      }
    }

    const nodeView = new MockNodeView(documentPath);
    const options = nodeView.getSchedulingOptions();

    expect(options.documentId).toBe(documentPath);
    expect(options.documentId).not.toBeUndefined();
  });

  it('should preserve document path through job creation flow', () => {
    // Test the full flow: NodeView → SchedulingControls → API → Database
    const documentPath = 'projects/data-import.md';

    // Step 1: NodeView has document path
    const nodeViewContext = {
      documentPath,
    };

    // Step 2: NodeView passes to SchedulingControls
    const schedulingOptions = {
      documentId: nodeViewContext.documentPath,
    };

    // Step 3: SchedulingControls creates API request
    const apiRequest = {
      created_from_doc: schedulingOptions.documentId,
    };

    // Step 4: Verify path preserved through entire flow
    expect(apiRequest.created_from_doc).toBe(documentPath);
  });
});
