/**
 * Tests for breadcrumb stack management and bar rendering
 *
 * Personas:
 * - Tim: Happy path user, navigating nested subcanvases
 * - Spike: Edge cases, empty state, deep nesting
 */

import { describe, test, expect, beforeEach, mock } from 'bun:test';
import {
    pushBreadcrumb,
    popBreadcrumb,
    getBreadcrumbStack,
    jumpToBreadcrumb,
    buildBreadcrumbBar,
    _resetBreadcrumbs,
} from './breadcrumb';

beforeEach(() => {
    _resetBreadcrumbs();
});

describe('Breadcrumb Stack - Tim (Happy Path)', () => {
    test('Tim expands one subcanvas, stack has one entry', () => {
        pushBreadcrumb({ canvasId: 'sub-1', name: 'Notes', minimize: mock(() => {}) });

        const stack = getBreadcrumbStack();
        expect(stack).toHaveLength(1);
        expect(stack[0].canvasId).toBe('sub-1');
        expect(stack[0].name).toBe('Notes');
    });

    test('Tim expands nested subcanvas, stack grows to 2', () => {
        pushBreadcrumb({ canvasId: 'sub-1', name: 'Notes', minimize: mock(() => {}) });
        pushBreadcrumb({ canvasId: 'sub-2', name: 'Details', minimize: mock(() => {}) });

        expect(getBreadcrumbStack()).toHaveLength(2);
        expect(getBreadcrumbStack()[1].name).toBe('Details');
    });

    test('Tim minimizes current level, stack shrinks by one', () => {
        pushBreadcrumb({ canvasId: 'sub-1', name: 'Notes', minimize: mock(() => {}) });
        pushBreadcrumb({ canvasId: 'sub-2', name: 'Details', minimize: mock(() => {}) });

        popBreadcrumb();
        expect(getBreadcrumbStack()).toHaveLength(1);
        expect(getBreadcrumbStack()[0].name).toBe('Notes');
    });
});

describe('jumpToBreadcrumb - Tim (Happy Path)', () => {
    test('Tim clicks root crumb, all levels cascade-minimize', () => {
        const min1 = mock(() => {});
        const min2 = mock(() => {});
        const min3 = mock(() => {});

        pushBreadcrumb({ canvasId: 'sub-1', name: 'A', minimize: min1 });
        pushBreadcrumb({ canvasId: 'sub-2', name: 'B', minimize: min2 });
        pushBreadcrumb({ canvasId: 'sub-3', name: 'C', minimize: min3 });

        jumpToBreadcrumb(-1);

        // All three called with instant=true, innermost first
        expect(min3).toHaveBeenCalledWith(true);
        expect(min2).toHaveBeenCalledWith(true);
        expect(min1).toHaveBeenCalledWith(true);
        expect(getBreadcrumbStack()).toHaveLength(0);
    });

    test('Tim clicks middle crumb, only levels above it collapse', () => {
        const min1 = mock(() => {});
        const min2 = mock(() => {});
        const min3 = mock(() => {});

        pushBreadcrumb({ canvasId: 'sub-1', name: 'A', minimize: min1 });
        pushBreadcrumb({ canvasId: 'sub-2', name: 'B', minimize: min2 });
        pushBreadcrumb({ canvasId: 'sub-3', name: 'C', minimize: min3 });

        jumpToBreadcrumb(0);

        expect(min3).toHaveBeenCalledWith(true);
        expect(min2).toHaveBeenCalledWith(true);
        expect(min1).not.toHaveBeenCalled();
        expect(getBreadcrumbStack()).toHaveLength(1);
    });
});

describe('buildBreadcrumbBar - Tim (Happy Path)', () => {
    test('Tim sees root crumb when no subcanvas is expanded', () => {
        const bar = buildBreadcrumbBar();
        expect(bar.className).toBe('canvas-breadcrumb-bar');

        const items = bar.querySelectorAll('.breadcrumb-item');
        expect(items).toHaveLength(1);
        expect(items[0].textContent).toBe('Canvas');
        expect(items[0].classList.contains('breadcrumb-current')).toBe(true);
    });

    test('Tim sees "Canvas › Notes" with one level expanded', () => {
        pushBreadcrumb({ canvasId: 'sub-1', name: 'Notes', minimize: mock(() => {}) });

        const bar = buildBreadcrumbBar();
        const items = bar.querySelectorAll('.breadcrumb-item');
        expect(items).toHaveLength(2);

        // Root is clickable
        expect(items[0].textContent).toBe('Canvas');
        expect(items[0].classList.contains('breadcrumb-clickable')).toBe(true);

        // Current level
        expect(items[1].textContent).toBe('Notes');
        expect(items[1].classList.contains('breadcrumb-current')).toBe(true);

        // Separator present
        expect(bar.querySelectorAll('.breadcrumb-separator')).toHaveLength(1);
    });

    test('Tim sees 3-level trail with correct clickability', () => {
        pushBreadcrumb({ canvasId: 'sub-1', name: 'A', minimize: mock(() => {}) });
        pushBreadcrumb({ canvasId: 'sub-2', name: 'B', minimize: mock(() => {}) });
        pushBreadcrumb({ canvasId: 'sub-3', name: 'C', minimize: mock(() => {}) });

        const bar = buildBreadcrumbBar();
        const items = bar.querySelectorAll('.breadcrumb-item');
        expect(items).toHaveLength(4); // Canvas, A, B, C

        // Root + A + B are clickable
        expect(items[0].classList.contains('breadcrumb-clickable')).toBe(true);
        expect(items[1].classList.contains('breadcrumb-clickable')).toBe(true);
        expect(items[2].classList.contains('breadcrumb-clickable')).toBe(true);

        // C is current
        expect(items[3].classList.contains('breadcrumb-current')).toBe(true);
    });
});

describe('Breadcrumb Stack - Spike (Edge Cases)', () => {
    test('Spike pops from empty stack (no crash)', () => {
        popBreadcrumb();
        expect(getBreadcrumbStack()).toHaveLength(0);
    });

    test('Spike jumps when stack is already at target level', () => {
        pushBreadcrumb({ canvasId: 'sub-1', name: 'A', minimize: mock(() => {}) });

        jumpToBreadcrumb(0);
        expect(getBreadcrumbStack()).toHaveLength(1);
    });

    test('Spike sees fallback name for unnamed subcanvas', () => {
        pushBreadcrumb({ canvasId: 'sub-1', name: '', minimize: mock(() => {}) });

        const bar = buildBreadcrumbBar();
        const items = bar.querySelectorAll('.breadcrumb-item');
        expect(items[1].textContent).toBe('subcanvas');
    });
});
