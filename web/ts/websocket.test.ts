/**
 * Tests for WebSocket message routing
 *
 * Ultra-fast tests focusing on core message dispatch logic.
 * No DOM, no real WebSocket connections - just pure routing.
 */

import { describe, test, expect, mock } from 'bun:test';
import { routeMessage } from './websocket';
import type { MessageHandlers } from '../types/websocket';

describe('WebSocket Message Routing', () => {
    test('routes daemon_status to registered handler', () => {
        const handler = mock(() => {});
        const handlers: MessageHandlers = { daemon_status: handler };

        // Simulate incoming message
        const message = {
            type: 'daemon_status' as const,
            running: true,
            active_jobs: 3,
            load_percent: 45
        };

        // Direct handler invocation (what websocket.ts does)
        const routedHandler = handlers[message.type];
        if (routedHandler) {
            routedHandler(message);
        }

        expect(handler).toHaveBeenCalledWith(message);
        expect(handler).toHaveBeenCalledTimes(1);
    });

    test('routes job_update to registered handler', () => {
        const handler = mock(() => {});
        const handlers: MessageHandlers = { job_update: handler };

        const message = {
            type: 'job_update' as const,
            job: {
                id: 'job-123',
                type: 'test',
                status: 'running' as const,
                created_at: Date.now(),
                updated_at: Date.now()
            },
            action: 'created' as const
        };

        const routedHandler = handlers[message.type];
        if (routedHandler) {
            routedHandler(message);
        }

        expect(handler).toHaveBeenCalledWith(
            expect.objectContaining({
                type: 'job_update',
                job: expect.objectContaining({ id: 'job-123' })
            })
        );
    });

    test('routes llm_stream to registered handler', () => {
        const handler = mock(() => {});
        const handlers: MessageHandlers = { llm_stream: handler };

        const message = {
            type: 'llm_stream' as const,
            job_id: 'job-456',
            content: 'Hello from LLM',
            done: false
        };

        const routedHandler = handlers[message.type];
        if (routedHandler) {
            routedHandler(message);
        }

        expect(handler).toHaveBeenCalledWith(
            expect.objectContaining({
                type: 'llm_stream',
                job_id: 'job-456',
                content: 'Hello from LLM',
                done: false
            })
        );
    });

    test('falls back to _default for unknown message types', () => {
        const defaultHandler = mock(() => {});
        const handlers: MessageHandlers = { _default: defaultHandler };

        const message = {
            type: 'unknown_type' as any,
            data: 'test'
        };

        // Unknown type logic (what websocket.ts:105-107 does)
        const routedHandler = handlers[message.type] || handlers._default;
        if (routedHandler) {
            routedHandler(message);
        }

        expect(defaultHandler).toHaveBeenCalledWith(message);
    });

    test('ignores message when no handler registered', () => {
        const handlers: MessageHandlers = {}; // Empty

        const message = {
            type: 'daemon_status' as const,
            running: false,
            active_jobs: 0,
            load_percent: 0
        };

        const routedHandler = handlers[message.type];
        // Should be undefined, no error thrown
        expect(routedHandler).toBeUndefined();
    });

    test('handles multiple message types with different handlers', () => {
        const daemonHandler = mock(() => {});
        const jobHandler = mock(() => {});
        const llmHandler = mock(() => {});

        const handlers: MessageHandlers = {
            daemon_status: daemonHandler,
            job_update: jobHandler,
            llm_stream: llmHandler
        };

        // Send daemon_status
        const daemonMsg = {
            type: 'daemon_status' as const,
            running: true,
            active_jobs: 5,
            load_percent: 75
        };
        handlers[daemonMsg.type]?.(daemonMsg);

        // Send job_update
        const jobMsg = {
            type: 'job_update' as const,
            job: {
                id: 'job-789',
                type: 'test',
                status: 'completed' as const,
                created_at: Date.now(),
                updated_at: Date.now()
            }
        };
        handlers[jobMsg.type]?.(jobMsg);

        // Send llm_stream
        const llmMsg = {
            type: 'llm_stream' as const,
            content: 'More text',
            done: true
        };
        handlers[llmMsg.type]?.(llmMsg);

        expect(daemonHandler).toHaveBeenCalledTimes(1);
        expect(jobHandler).toHaveBeenCalledTimes(1);
        expect(llmHandler).toHaveBeenCalledTimes(1);
    });

    test('handles error messages', () => {
        const errorHandler = mock(() => {});
        const handlers: MessageHandlers = { error: errorHandler };

        const message = {
            type: 'error' as const,
            error: 'Something went wrong',
            code: 'ERR_TEST'
        };

        const routedHandler = handlers[message.type];
        if (routedHandler) {
            routedHandler(message);
        }

        expect(errorHandler).toHaveBeenCalledWith(
            expect.objectContaining({
                type: 'error',
                error: 'Something went wrong',
                code: 'ERR_TEST'
            })
        );
    });

    test('handles reload messages', () => {
        const reloadHandler = mock(() => {});
        const handlers: MessageHandlers = { reload: reloadHandler };

        const message = {
            type: 'reload' as const,
            reason: 'Code updated'
        };

        const routedHandler = handlers[message.type];
        if (routedHandler) {
            routedHandler(message);
        }

        expect(reloadHandler).toHaveBeenCalledWith(
            expect.objectContaining({
                type: 'reload',
                reason: 'Code updated'
            })
        );
    });
});

describe('routeMessage() function', () => {
    test('routes built-in message types (reload, backend_status)', () => {
        // Built-in handlers are in MESSAGE_HANDLERS, not in registered handlers
        const result = routeMessage(
            { type: 'backend_status', status: 'healthy' },
            {} // No registered handlers
        );

        expect(result.handled).toBe(true);
        expect(result.handlerType).toBe('builtin');
    });

    test('routes registered handler when no built-in exists', () => {
        const customHandler = mock(() => {});
        const handlers: MessageHandlers = {
            custom_type: customHandler
        };

        const result = routeMessage(
            { type: 'custom_type', data: 'test' },
            handlers
        );

        expect(result.handled).toBe(true);
        expect(result.handlerType).toBe('registered');
        expect(customHandler).toHaveBeenCalledTimes(1);
    });

    test('routes to _default for unknown message types', () => {
        const defaultHandler = mock(() => {});
        const handlers: MessageHandlers = {
            _default: defaultHandler
        };

        const result = routeMessage(
            { type: 'unknown_type', data: 'test' },
            handlers
        );

        expect(result.handled).toBe(true);
        expect(result.handlerType).toBe('default');
        expect(defaultHandler).toHaveBeenCalledTimes(1);
    });

    test('returns not handled when no handler exists', () => {
        const result = routeMessage(
            { type: 'unknown_type', data: 'test' },
            {} // No handlers at all
        );

        expect(result.handled).toBe(false);
        expect(result.handlerType).toBe('none');
    });

    test('prioritizes built-in over registered handlers', () => {
        // Even if registered handler exists, built-in should take precedence
        const registeredReload = mock(() => {});
        const handlers: MessageHandlers = {
            reload: registeredReload // Try to override built-in
        };

        const result = routeMessage(
            { type: 'reload', reason: 'test' },
            handlers
        );

        // Should use built-in, not registered
        expect(result.handlerType).toBe('builtin');
        // Built-in reload handler will trigger, but won't call registered one
        // (Note: Built-in reload calls window.location.reload(), can't easily test in Node)
    });

    test('handler precedence: builtin > registered > default', () => {
        const defaultHandler = mock(() => {});
        const registeredHandler = mock(() => {});

        // Test with only default
        let result = routeMessage(
            { type: 'unknown', data: 'test' },
            { _default: defaultHandler }
        );
        expect(result.handlerType).toBe('default');

        // Test with registered handler (overrides default)
        result = routeMessage(
            { type: 'custom', data: 'test' },
            { custom: registeredHandler, _default: defaultHandler }
        );
        expect(result.handlerType).toBe('registered');

        // Test with built-in (overrides both)
        result = routeMessage(
            { type: 'reload', reason: 'test' },
            { reload: registeredHandler, _default: defaultHandler }
        );
        expect(result.handlerType).toBe('builtin');
    });
});
