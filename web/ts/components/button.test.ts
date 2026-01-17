/**
 * Button Component Tests
 */

import { describe, it, expect, beforeEach, afterEach, mock } from 'bun:test';
import { Button, createButton, createPrimaryButton, createDangerButton } from './button';

describe('Button', () => {
    let container: HTMLElement;

    beforeEach(() => {
        container = document.createElement('div');
        document.body.appendChild(container);
    });

    afterEach(() => {
        container.remove();
    });

    describe('creation', () => {
        it('creates a button element with correct attributes', () => {
            const btn = new Button({
                label: 'Test Button',
                onClick: () => {}
            });

            expect(btn.element.tagName).toBe('BUTTON');
            expect(btn.element.type).toBe('button');
            expect(btn.element.textContent).toContain('Test Button');
            expect(btn.element.getAttribute('aria-label')).toBe('Test Button');
        });

        it('applies variant class', () => {
            const btn = new Button({
                label: 'Primary',
                onClick: () => {},
                variant: 'primary'
            });

            expect(btn.element.classList.contains('qntx-btn-primary')).toBe(true);
        });

        it('applies size class', () => {
            const btn = new Button({
                label: 'Small',
                onClick: () => {},
                size: 'small'
            });

            expect(btn.element.classList.contains('qntx-btn-small')).toBe(true);
        });

        it('applies custom className', () => {
            const btn = new Button({
                label: 'Custom',
                onClick: () => {},
                className: 'my-custom-class'
            });

            expect(btn.element.classList.contains('my-custom-class')).toBe(true);
        });

        it('sets aria-label when provided', () => {
            const btn = new Button({
                label: 'Save',
                onClick: () => {},
                ariaLabel: 'Save document'
            });

            expect(btn.element.getAttribute('aria-label')).toBe('Save document');
        });
    });

    describe('disabled state', () => {
        it('starts disabled when configured', () => {
            const btn = new Button({
                label: 'Disabled',
                onClick: () => {},
                disabled: true
            });

            expect(btn.element.disabled).toBe(true);
            expect(btn.element.getAttribute('aria-disabled')).toBe('true');
        });

        it('can be disabled after creation', () => {
            const btn = new Button({
                label: 'Test',
                onClick: () => {}
            });

            btn.setDisabled(true, 'Feature not available');

            expect(btn.element.disabled).toBe(true);
            expect(btn.element.title).toBe('Feature not available');
        });

        it('can be re-enabled', () => {
            const btn = new Button({
                label: 'Test',
                onClick: () => {},
                disabled: true
            });

            btn.setDisabled(false);

            expect(btn.element.disabled).toBe(false);
        });

        it('ignores clicks when disabled', async () => {
            const onClick = mock(() => {});
            const btn = new Button({
                label: 'Test',
                onClick,
                disabled: true
            });

            btn.element.click();

            expect(onClick).not.toHaveBeenCalled();
        });
    });

    describe('loading state', () => {
        it('shows loading state', () => {
            const btn = new Button({
                label: 'Save',
                onClick: () => {}
            });

            btn.setLoading(true);

            expect(btn.element.classList.contains('qntx-btn-loading')).toBe(true);
            expect(btn.element.getAttribute('aria-busy')).toBe('true');
            expect(btn.element.disabled).toBe(true);
            expect(btn.element.textContent).toContain('Save...');
        });

        it('removes loading state', () => {
            const btn = new Button({
                label: 'Save',
                onClick: () => {}
            });

            btn.setLoading(true);
            btn.setLoading(false);

            expect(btn.element.classList.contains('qntx-btn-loading')).toBe(false);
            expect(btn.element.getAttribute('aria-busy')).toBe('false');
        });

        it('ignores clicks when loading', async () => {
            const onClick = mock(() => {});
            const btn = new Button({
                label: 'Test',
                onClick
            });

            btn.setLoading(true);
            btn.element.click();

            expect(onClick).not.toHaveBeenCalled();
        });
    });

    describe('error state', () => {
        it('shows error display', () => {
            const btn = new Button({
                label: 'Submit',
                onClick: () => {}
            });
            container.appendChild(btn.element);

            btn.setError(new Error('Network error'));

            const errorDisplay = container.querySelector('.qntx-btn-error-display');
            expect(errorDisplay).not.toBeNull();
            expect(errorDisplay?.textContent).toContain('Network error');
        });

        it('clears error display', () => {
            const btn = new Button({
                label: 'Submit',
                onClick: () => {}
            });
            container.appendChild(btn.element);

            btn.setError(new Error('Network error'));
            btn.clearError();

            // Error should be removed (may have animation delay)
            const state = btn.getState();
            expect(state.error).toBeNull();
        });

        it('error is cleared before new click', async () => {
            let callCount = 0;
            const btn = new Button({
                label: 'Submit',
                onClick: async () => {
                    callCount++;
                    if (callCount === 1) {
                        throw new Error('First error');
                    }
                }
            });
            container.appendChild(btn.element);

            // First click - will fail
            btn.element.click();
            await new Promise(resolve => setTimeout(resolve, 10));

            expect(btn.getState().error).not.toBeNull();

            // Second click - error should be cleared first
            btn.element.click();
            await new Promise(resolve => setTimeout(resolve, 10));

            // Error should be cleared (second call didn't throw)
            expect(btn.getState().error).toBeNull();
        });
    });

    describe('confirmation', () => {
        it('enters confirmation state on first click', () => {
            const onClick = mock(() => {});
            const btn = new Button({
                label: 'Delete',
                onClick,
                confirmation: {
                    label: 'Confirm Delete',
                    timeout: 5000
                }
            });

            btn.element.click();

            expect(btn.element.classList.contains('qntx-btn-confirming')).toBe(true);
            expect(btn.element.textContent).toContain('Confirm Delete');
            expect(onClick).not.toHaveBeenCalled();
        });

        it('executes action on second click', async () => {
            const onClick = mock(() => {});
            const btn = new Button({
                label: 'Delete',
                onClick,
                confirmation: {
                    label: 'Confirm Delete',
                    timeout: 5000
                }
            });

            btn.element.click(); // Enter confirmation
            btn.element.click(); // Confirm

            await new Promise(resolve => setTimeout(resolve, 10));

            expect(onClick).toHaveBeenCalledTimes(1);
            expect(btn.element.classList.contains('qntx-btn-confirming')).toBe(false);
        });

        it('reverts after timeout', async () => {
            const btn = new Button({
                label: 'Delete',
                onClick: () => {},
                confirmation: {
                    label: 'Confirm Delete',
                    timeout: 50 // Short timeout for test
                }
            });

            btn.element.click();
            expect(btn.element.classList.contains('qntx-btn-confirming')).toBe(true);

            await new Promise(resolve => setTimeout(resolve, 100));

            expect(btn.element.classList.contains('qntx-btn-confirming')).toBe(false);
            expect(btn.element.textContent).toContain('Delete');
        });
    });

    describe('click handler', () => {
        it('calls onClick', async () => {
            const onClick = mock(() => {});
            const btn = new Button({
                label: 'Click Me',
                onClick
            });

            btn.element.click();
            await new Promise(resolve => setTimeout(resolve, 10));

            expect(onClick).toHaveBeenCalledTimes(1);
        });

        it('handles async onClick', async () => {
            let completed = false;
            const btn = new Button({
                label: 'Async',
                onClick: async () => {
                    await new Promise(resolve => setTimeout(resolve, 20));
                    completed = true;
                }
            });

            btn.element.click();

            // Should be loading
            expect(btn.getState().loading).toBe(true);

            await new Promise(resolve => setTimeout(resolve, 50));

            expect(completed).toBe(true);
            expect(btn.getState().loading).toBe(false);
        });

        it('catches and displays errors', async () => {
            const btn = new Button({
                label: 'Fail',
                onClick: async () => {
                    throw new Error('Async error');
                }
            });
            container.appendChild(btn.element);

            btn.element.click();
            await new Promise(resolve => setTimeout(resolve, 10));

            expect(btn.getState().error?.message).toBe('Async error');
        });
    });

    describe('factory functions', () => {
        it('createButton creates a button', () => {
            const btn = createButton({
                label: 'Factory',
                onClick: () => {}
            });

            expect(btn.element.textContent).toContain('Factory');
        });

        it('createPrimaryButton creates primary variant', () => {
            const btn = createPrimaryButton('Primary', () => {});

            expect(btn.element.classList.contains('qntx-btn-primary')).toBe(true);
        });

        it('createDangerButton creates danger variant with confirmation', () => {
            const btn = createDangerButton('Delete', 'Confirm', () => {});

            expect(btn.element.classList.contains('qntx-btn-danger')).toBe(true);

            // Click to enter confirmation
            btn.element.click();
            expect(btn.element.textContent).toContain('Confirm');
        });
    });

    describe('cleanup', () => {
        it('destroy removes element', () => {
            const btn = new Button({
                label: 'Test',
                onClick: () => {}
            });
            container.appendChild(btn.element);

            expect(container.contains(btn.element)).toBe(true);

            btn.destroy();

            expect(container.contains(btn.element)).toBe(false);
        });

        it('destroy clears confirmation timeout', () => {
            const btn = new Button({
                label: 'Delete',
                onClick: () => {},
                confirmation: {
                    label: 'Confirm',
                    timeout: 5000
                }
            });

            btn.element.click(); // Start confirmation timeout
            btn.destroy(); // Should clear timeout without error
        });
    });
});

describe('hydrateButtons', () => {
    let container: HTMLElement;

    beforeEach(() => {
        container = document.createElement('div');
        document.body.appendChild(container);
    });

    afterEach(() => {
        container.remove();
    });

    it('replaces placeholder buttons with Button instances', async () => {
        const { hydrateButtons } = await import('./button');

        container.innerHTML = `
            <div class="actions">
                <button class="qntx-btn-placeholder" data-button-id="save">Save</button>
                <button class="qntx-btn-placeholder" data-button-id="cancel">Cancel</button>
            </div>
        `;

        const saveClicked = mock(() => {});
        const cancelClicked = mock(() => {});

        const buttons = hydrateButtons(container, {
            save: { label: 'Save Changes', onClick: saveClicked, variant: 'primary' },
            cancel: { label: 'Cancel', onClick: cancelClicked, variant: 'ghost' }
        });

        // Buttons should exist
        expect(buttons.save).toBeDefined();
        expect(buttons.cancel).toBeDefined();

        // Placeholders should be replaced
        expect(container.querySelectorAll('.qntx-btn-placeholder').length).toBe(0);

        // Button elements should have correct classes
        expect(buttons.save.element.classList.contains('qntx-btn-primary')).toBe(true);
        expect(buttons.cancel.element.classList.contains('qntx-btn-ghost')).toBe(true);

        // Click should work
        buttons.save.element.click();
        await new Promise(resolve => setTimeout(resolve, 10));
        expect(saveClicked).toHaveBeenCalledTimes(1);
    });

    it('preserves extra classes from placeholder', async () => {
        const { hydrateButtons } = await import('./button');

        container.innerHTML = `
            <button class="qntx-btn-placeholder my-custom-class" data-button-id="test">Test</button>
        `;

        const buttons = hydrateButtons(container, {
            test: { label: 'Test', onClick: () => {} }
        });

        expect(buttons.test.element.classList.contains('my-custom-class')).toBe(true);
    });

    it('preserves data attributes from placeholder', async () => {
        const { hydrateButtons } = await import('./button');

        container.innerHTML = `
            <button class="qntx-btn-placeholder" data-button-id="delete" data-item-id="123">Delete</button>
        `;

        const buttons = hydrateButtons(container, {
            delete: { label: 'Delete', onClick: () => {}, variant: 'danger' }
        });

        expect(buttons.delete.element.dataset.itemId).toBe('123');
    });

    it('ignores placeholders without matching config', async () => {
        const { hydrateButtons } = await import('./button');

        container.innerHTML = `
            <button class="qntx-btn-placeholder" data-button-id="missing">Missing</button>
        `;

        const buttons = hydrateButtons(container, {});

        expect(buttons.missing).toBeUndefined();
        // Placeholder should still be there
        expect(container.querySelectorAll('.qntx-btn-placeholder').length).toBe(1);
    });
});

describe('buttonPlaceholder', () => {
    it('generates placeholder HTML', async () => {
        const { buttonPlaceholder } = await import('./button');

        const html = buttonPlaceholder('save', 'Save');
        expect(html).toBe('<button class="qntx-btn-placeholder" data-button-id="save">Save</button>');
    });

    it('includes extra classes', async () => {
        const { buttonPlaceholder } = await import('./button');

        const html = buttonPlaceholder('delete', 'Delete', 'plugin-delete-btn');
        expect(html).toContain('qntx-btn-placeholder');
        expect(html).toContain('plugin-delete-btn');
    });
});

describe('Button Registry', () => {
    beforeEach(async () => {
        const { clearButtonRegistry } = await import('./button');
        clearButtonRegistry();
    });

    it('registers and retrieves buttons by operation ID', async () => {
        const { Button, registerButton, getButton } = await import('./button');

        const btn = new Button({ label: 'Test', onClick: () => {} });
        registerButton('op-123', btn);

        const retrieved = getButton('op-123');
        expect(retrieved).toBe(btn);
    });

    it('returns undefined for unregistered operation IDs', async () => {
        const { getButton } = await import('./button');

        const retrieved = getButton('nonexistent');
        expect(retrieved).toBeUndefined();
    });

    it('unregisters buttons', async () => {
        const { Button, registerButton, getButton, unregisterButton } = await import('./button');

        const btn = new Button({ label: 'Test', onClick: () => {} });
        registerButton('op-456', btn);

        expect(getButton('op-456')).toBe(btn);

        unregisterButton('op-456');

        expect(getButton('op-456')).toBeUndefined();
    });

    it('auto-unregisters button on destroy', async () => {
        const { Button, registerButton, getButton } = await import('./button');

        const btn = new Button({ label: 'Test', onClick: () => {} });
        registerButton('op-789', btn);

        expect(getButton('op-789')).toBe(btn);

        btn.destroy();

        expect(getButton('op-789')).toBeUndefined();
    });

    it('clears all registered buttons', async () => {
        const { Button, registerButton, getButton, clearButtonRegistry, getRegisteredButtonCount } = await import('./button');

        const btn1 = new Button({ label: 'Test 1', onClick: () => {} });
        const btn2 = new Button({ label: 'Test 2', onClick: () => {} });
        registerButton('op-1', btn1);
        registerButton('op-2', btn2);

        expect(getRegisteredButtonCount()).toBe(2);

        clearButtonRegistry();

        expect(getRegisteredButtonCount()).toBe(0);
        expect(getButton('op-1')).toBeUndefined();
        expect(getButton('op-2')).toBeUndefined();
    });
});
