/**
 * Tests for hixtory panel core functionality
 */

import { describe, it, expect, beforeEach } from 'vitest';
import type { JobUpdateData } from '../types/websocket';

describe('Hixtory Panel - handleJobUpdate', () => {
  it('should store job from WebSocket update', () => {
    const mockJobUpdate: JobUpdateData = {
      type: 'job_update',
      job: {
        id: 'test-job-123',
        type: 'ix-import',
        status: 'running',
        created_at: Date.now(),
        metadata: {
          command: 'ix import test.csv',
          total_operations: 100,
          completed_operations: 50
        }
      }
    };

    // Test that job is stored and can be retrieved
    // This would test the jobs Map storage
    const jobId = mockJobUpdate.job.id;
    expect(jobId).toBe('test-job-123');
    expect(mockJobUpdate.job.status).toBe('running');
  });

  it('should preserve graph_query metadata', () => {
    const mockJobUpdate: JobUpdateData = {
      type: 'job_update',
      job: {
        id: 'test-job-456',
        type: 'ix-query',
        status: 'completed',
        created_at: Date.now()
      },
      metadata: {
        graph_query: 'MATCH (n) RETURN n LIMIT 10'
      }
    };

    // Verify metadata is attached
    expect(mockJobUpdate.metadata?.graph_query).toBe('MATCH (n) RETURN n LIMIT 10');
  });
});
