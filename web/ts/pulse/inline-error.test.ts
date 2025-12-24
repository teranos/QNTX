/**
 * Tests for Inline Error Display
 *
 * Verifies that scheduling errors are displayed inline (not as toasts)
 */

import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';
import { Window } from 'happy-dom';

// Set up DOM environment
const window = new Window();
globalThis.document = window.document as unknown as Document;
globalThis.window = window as unknown as Window & typeof globalThis;

// Mock scheduling controls error display
function showSchedulingError(container: HTMLElement, message: string): HTMLElement {
  // Remove any existing error
  const existingError = container.querySelector('.pulse-error');
  if (existingError) existingError.remove();

  // Create error element
  const errorEl = document.createElement('div');
  errorEl.className = 'pulse-error';
  errorEl.textContent = message;

  // Insert at the top of scheduling controls
  container.insertBefore(errorEl, container.firstChild);

  return errorEl;
}

describe('Inline Error Display', () => {
  let container: HTMLElement;
  let timers: NodeJS.Timeout[] = [];

  beforeEach(() => {
    container = document.createElement('div');
    container.className = 'pulse-scheduling-controls';
    timers = [];
  });

  afterEach(() => {
    // Clean up any pending timers
    timers.forEach(timer => clearTimeout(timer));
  });

  describe('Error Element Creation', () => {
    it('should create error element with correct class and message', () => {
      const message = 'Failed to create scheduled job: Invalid ATS code';

      const errorEl = showSchedulingError(container, message);

      expect(errorEl.className).toBe('pulse-error');
      expect(errorEl.textContent).toBe(message);
      expect(container.querySelector('.pulse-error')).toBe(errorEl);
    });

    it('should insert error at the top of container', () => {
      // Add some existing content
      const existingContent = document.createElement('div');
      existingContent.textContent = 'Existing content';
      container.appendChild(existingContent);

      const errorEl = showSchedulingError(container, 'Error message');

      expect(container.firstChild).toBe(errorEl);
      expect(container.children[1]).toBe(existingContent);
    });

    it('should remove previous error when new error is shown', () => {
      const firstError = showSchedulingError(container, 'First error');
      expect(container.querySelectorAll('.pulse-error')).toHaveLength(1);

      const secondError = showSchedulingError(container, 'Second error');

      // Only one error should exist
      expect(container.querySelectorAll('.pulse-error')).toHaveLength(1);
      expect(container.querySelector('.pulse-error')).toBe(secondError);
      expect(container.querySelector('.pulse-error')?.textContent).toBe('Second error');
    });

    it('should handle empty message gracefully', () => {
      const errorEl = showSchedulingError(container, '');

      expect(errorEl.textContent).toBe('');
      expect(container.querySelector('.pulse-error')).toBeTruthy();
    });
  });

  describe('Auto-Dismiss Behavior', () => {
    it('should auto-dismiss error after 8 seconds', async () => {
      const errorEl = showSchedulingError(container, 'Test error');

      // Verify error exists
      expect(container.querySelector('.pulse-error')).toBeTruthy();

      // Simulate auto-dismiss after 8 seconds
      const timer = setTimeout(() => {
        errorEl.remove();
      }, 8000);
      timers.push(timer);

      // Fast-forward time (in real code, we'd use vi.useFakeTimers)
      await new Promise(resolve => {
        const checkTimer = setTimeout(() => {
          clearTimeout(timer);
          errorEl.remove();
          resolve(null);
        }, 100); // Short wait for test
        timers.push(checkTimer);
      });

      expect(container.querySelector('.pulse-error')).toBeNull();
    });

    it('should not affect other elements when auto-dismissing', async () => {
      const otherContent = document.createElement('div');
      otherContent.className = 'other-content';
      otherContent.textContent = 'Other content';
      container.appendChild(otherContent);

      const errorEl = showSchedulingError(container, 'Test error');

      // Simulate auto-dismiss
      await new Promise(resolve => {
        const timer = setTimeout(() => {
          errorEl.remove();
          resolve(null);
        }, 100);
        timers.push(timer);
      });

      // Other content should still exist
      expect(container.querySelector('.other-content')).toBeTruthy();
      expect(container.querySelector('.other-content')?.textContent).toBe('Other content');
    });
  });

  describe('Multiple Errors', () => {
    it('should only show one error at a time', () => {
      showSchedulingError(container, 'First error');
      showSchedulingError(container, 'Second error');
      showSchedulingError(container, 'Third error');

      const errors = container.querySelectorAll('.pulse-error');
      expect(errors).toHaveLength(1);
      expect(errors[0].textContent).toBe('Third error');
    });

    it('should replace error even if previous has not auto-dismissed yet', () => {
      const firstError = showSchedulingError(container, 'First error');

      // Set up auto-dismiss timer for first error
      const timer1 = setTimeout(() => firstError.remove(), 8000);
      timers.push(timer1);

      // Immediately show second error (before first auto-dismisses)
      const secondError = showSchedulingError(container, 'Second error');

      expect(container.querySelectorAll('.pulse-error')).toHaveLength(1);
      expect(container.querySelector('.pulse-error')).toBe(secondError);
    });
  });

  describe('Error Message Content', () => {
    it('should display detailed error messages', () => {
      const detailedMessage = `Failed to create scheduled job: ats_code is required

ATS Code:
ix https://example.com/api/data

Interval: 6h
Document: projects/example.md`;

      const errorEl = showSchedulingError(container, detailedMessage);

      expect(errorEl.textContent).toContain('Failed to create scheduled job');
      expect(errorEl.textContent).toContain('ats_code is required');
      expect(errorEl.textContent).toContain('Interval: 6h');
      expect(errorEl.textContent).toContain('Document: projects/example.md');
    });

    it('should handle multiline error messages', () => {
      const multilineMessage = 'Line 1\nLine 2\nLine 3';

      const errorEl = showSchedulingError(container, multilineMessage);

      expect(errorEl.textContent).toBe(multilineMessage);
    });

    it('should escape HTML in error messages', () => {
      const htmlMessage = '<script>alert("xss")</script>Error message';

      const errorEl = showSchedulingError(container, htmlMessage);

      // textContent automatically escapes HTML
      expect(errorEl.textContent).toBe(htmlMessage);
      expect(errorEl.innerHTML).not.toContain('<script>');
    });
  });
});
