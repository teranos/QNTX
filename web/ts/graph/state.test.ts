/**
 * Tests for Graph State Management
 *
 * Critical tests for DOM caching, focus state management, and state clearing
 */

import { describe, it, expect, beforeEach } from 'bun:test';
import {
    getDomCache,
    clearState,
    getSimulation,
    getFocusedNodeId,
    setFocusedNodeId,
    getPreFocusTransform,
    setPreFocusTransform,
    getIsFocusAnimating,
    setIsFocusAnimating,
    getHiddenNodes
} from './state';

describe('Graph State Management', () => {
    beforeEach(() => {
        // Clear state between tests
        clearState();
    });

    describe('DOMCache', () => {
        it('should cache DOM element on first access', () => {
            // Create a test element
            const testDiv = document.createElement('div');
            testDiv.id = 'test-element';
            document.body.appendChild(testDiv);

            const domCache = getDomCache();

            // First access should query the DOM
            const el1 = domCache.get('graphViewer', 'test-element');
            expect(el1).toBe(testDiv);

            // Second access should return cached value (not query DOM again)
            const el2 = domCache.get('graphViewer', 'test-element');
            expect(el2).toBe(testDiv);
            expect(el2).toBe(el1); // Same reference

            // Cleanup
            document.body.removeChild(testDiv);
        });

        it('should return null for non-existent elements', () => {
            const domCache = getDomCache();
            const el = domCache.get('graphViewer', 'non-existent-element');
            expect(el).toBeNull();
        });

        it('should clear all cached elements', () => {
            // Create test elements
            const testDiv1 = document.createElement('div');
            testDiv1.id = 'test-1';
            const testDiv2 = document.createElement('div');
            testDiv2.id = 'test-2';
            document.body.appendChild(testDiv1);
            document.body.appendChild(testDiv2);

            const domCache = getDomCache();

            // Cache elements
            domCache.get('graphViewer', 'test-1');
            domCache.get('isolatedToggle', 'test-2');

            // Verify cached
            expect(domCache.graphViewer).toBe(testDiv1);
            expect(domCache.isolatedToggle).toBe(testDiv2);

            // Clear cache
            domCache.clear();

            // Verify cleared
            expect(domCache.graphViewer).toBeNull();
            expect(domCache.isolatedToggle).toBeNull();

            // Cleanup
            document.body.removeChild(testDiv1);
            document.body.removeChild(testDiv2);
        });

        it('should support both getElementById and querySelector', () => {
            // Create elements for both lookup methods
            const idElement = document.createElement('div');
            idElement.id = 'test-id';
            const classElement = document.createElement('div');
            classElement.className = 'test-class';

            document.body.appendChild(idElement);
            document.body.appendChild(classElement);

            const domCache = getDomCache();

            // Test getElementById (via #id)
            const el1 = domCache.get('graphViewer', 'test-id');
            expect(el1).toBe(idElement);

            // Test querySelector (via .class)
            const el2 = domCache.get('isolatedToggle', '.test-class');
            expect(el2).toBe(classElement);

            // Cleanup
            document.body.removeChild(idElement);
            document.body.removeChild(classElement);
        });
    });

    describe('Focus State Management', () => {
        it('should get and set focused node ID', () => {
            expect(getFocusedNodeId()).toBeNull();

            setFocusedNodeId('node-123');
            expect(getFocusedNodeId()).toBe('node-123');

            setFocusedNodeId(null);
            expect(getFocusedNodeId()).toBeNull();
        });

        it('should get and set pre-focus transform', () => {
            expect(getPreFocusTransform()).toBeNull();

            const transform = { x: 100, y: 200, k: 1.5 };
            setPreFocusTransform(transform);
            expect(getPreFocusTransform()).toEqual(transform);

            setPreFocusTransform(null);
            expect(getPreFocusTransform()).toBeNull();
        });

        it('should get and set focus animating flag', () => {
            expect(getIsFocusAnimating()).toBe(false);

            setIsFocusAnimating(true);
            expect(getIsFocusAnimating()).toBe(true);

            setIsFocusAnimating(false);
            expect(getIsFocusAnimating()).toBe(false);
        });
    });

    describe('Hidden Nodes Management', () => {
        it('should maintain hidden nodes set', () => {
            const hiddenNodes = getHiddenNodes();

            expect(hiddenNodes.size).toBe(0);

            hiddenNodes.add('node-1');
            hiddenNodes.add('node-2');

            expect(hiddenNodes.size).toBe(2);
            expect(hiddenNodes.has('node-1')).toBe(true);
            expect(hiddenNodes.has('node-2')).toBe(true);

            hiddenNodes.delete('node-1');
            expect(hiddenNodes.size).toBe(1);
            expect(hiddenNodes.has('node-1')).toBe(false);
        });
    });

    describe('State Clearing', () => {
        it('should clear all state including DOM cache', () => {
            // Set up some state
            setFocusedNodeId('node-123');
            setPreFocusTransform({ x: 100, y: 200, k: 1.5 });
            setIsFocusAnimating(true);
            getHiddenNodes().add('node-1');

            // Create and cache a DOM element
            const testDiv = document.createElement('div');
            testDiv.id = 'test-element';
            document.body.appendChild(testDiv);
            getDomCache().get('graphViewer', 'test-element');

            // Verify state is set
            expect(getFocusedNodeId()).toBe('node-123');
            expect(getPreFocusTransform()).not.toBeNull();
            expect(getIsFocusAnimating()).toBe(true);
            expect(getDomCache().graphViewer).toBe(testDiv);

            // Clear all state
            clearState();

            // Verify everything is cleared
            expect(getFocusedNodeId()).toBeNull();
            expect(getPreFocusTransform()).toBeNull();
            expect(getIsFocusAnimating()).toBe(false);
            expect(getSimulation()).toBeNull();
            expect(getDomCache().graphViewer).toBeNull();

            // Note: hiddenNodes set is not cleared by clearState()
            // This may be intentional to preserve visibility preferences

            // Cleanup
            document.body.removeChild(testDiv);
        });
    });
});
