/**
 * Type Definition Window - Floating window for configuring type field metadata
 * Allows marking fields as rich/fuzzy searchable with immediate persistence
 */

import { Window } from './components/window.js';
import { apiFetch } from './api.js';

interface TypeDefinition {
    name: string;
    label: string;
    color: string;
    opacity?: number;
    deprecated?: boolean;
    rich_string_fields?: string[];
    array_fields?: string[];
}

interface FieldInfo {
    name: string;
    value: any;
    isRichString: boolean;
    isArray: boolean;
}

export class TypeDefinitionWindow {
    private window: Window;
    private currentType: TypeDefinition | null = null;
    private fields: Map<string, FieldInfo> = new Map();
    private selectedField: string | null = null;

    constructor() {
        this.window = new Window({
            id: 'type-definition-window',
            title: 'Type Definition',
            width: '500px',
            height: 'auto',
            onClose: () => this.onClose()
        });
    }

    /**
     * Open the window for a specific type
     */
    public open(typeName: string, typeInfo?: TypeDefinition): void {
        this.currentType = typeInfo || {
            name: typeName,
            label: typeName.charAt(0).toUpperCase() + typeName.slice(1),
            color: '#888888',
            rich_string_fields: [],
            array_fields: []
        };

        // Clear and populate fields from the type definition
        this.fields.clear();

        // Add fields from rich_string_fields
        if (this.currentType.rich_string_fields) {
            for (const fieldName of this.currentType.rich_string_fields) {
                this.fields.set(fieldName, {
                    name: fieldName,
                    value: null,
                    isRichString: true,
                    isArray: false
                });
            }
        }

        // Add fields from array_fields (might overlap with rich_string_fields)
        if (this.currentType.array_fields) {
            for (const fieldName of this.currentType.array_fields) {
                const existing = this.fields.get(fieldName);
                if (existing) {
                    existing.isArray = true;
                } else {
                    this.fields.set(fieldName, {
                        name: fieldName,
                        value: null,
                        isRichString: false,
                        isArray: true
                    });
                }
            }
        }

        this.discoverFields();
        this.render();
        this.window.show();
    }

    /**
     * Discover fields from attestations for this type
     */
    private async discoverFields(): Promise<void> {
        if (!this.currentType) return;

        // Query attestations to discover fields
        try {
            const response = await apiFetch(`/api/types/${this.currentType.name}/fields`);
            if (response.ok) {
                const data = await response.json();
                this.processFieldData(data);
            }
        } catch (error) {
            console.error('Failed to discover fields:', error);
            // Continue with manual field entry
        }
    }

    /**
     * Process field data from API response
     */
    private processFieldData(data: any): void {
        if (!this.currentType) return;

        // Clear existing fields
        this.fields.clear();

        // Process discovered fields
        if (data.fields) {
            for (const fieldName of Object.keys(data.fields)) {
                const isRichString = this.currentType.rich_string_fields?.includes(fieldName) || false;
                const isArray = this.currentType.array_fields?.includes(fieldName) || false;

                this.fields.set(fieldName, {
                    name: fieldName,
                    value: data.fields[fieldName],
                    isRichString,
                    isArray
                });
            }
        }
    }

    /**
     * Render the window content
     */
    private render(): void {
        if (!this.currentType) return;

        const content = document.createElement('div');
        content.className = 'type-definition-content';

        // Type header
        const header = document.createElement('div');
        header.className = 'type-def-header';
        header.innerHTML = `
            <div class="type-def-title">
                <span class="type-def-name">${this.currentType.name}</span>
                <span class="type-def-label">${this.currentType.label}</span>
                <span class="type-def-color" style="background-color: ${this.currentType.color}"></span>
            </div>
        `;
        content.appendChild(header);

        // Fields section
        const fieldsSection = document.createElement('div');
        fieldsSection.className = 'type-def-fields';
        fieldsSection.innerHTML = '<h3>Fields</h3>';

        // Field list
        const fieldList = document.createElement('div');
        fieldList.className = 'field-list';

        // Add existing fields
        this.fields.forEach((field, fieldName) => {
            const fieldEl = this.createFieldElement(field);
            fieldList.appendChild(fieldEl);
        });

        // Add new field input
        const addFieldEl = document.createElement('div');
        addFieldEl.className = 'add-field';
        addFieldEl.innerHTML = `
            <input type="text" class="add-field-input" placeholder="Add new field name...">
            <button class="add-field-btn">+</button>
        `;
        fieldList.appendChild(addFieldEl);

        fieldsSection.appendChild(fieldList);
        content.appendChild(fieldsSection);

        // Selected field controls
        const controlsSection = document.createElement('div');
        controlsSection.className = 'field-controls';
        controlsSection.style.display = 'none';
        controlsSection.innerHTML = `
            <h4>Field Configuration</h4>
            <div class="field-control-options">
                <label class="field-option">
                    <input type="checkbox" id="rich-string-toggle">
                    <span>Fuzzy Searchable</span>
                    <span class="field-option-desc">Enable fuzzy text search on this field</span>
                </label>
                <label class="field-option">
                    <input type="checkbox" id="array-toggle">
                    <span>Array Field</span>
                    <span class="field-option-desc">Display as interactive tag badges</span>
                </label>
            </div>
        `;
        content.appendChild(controlsSection);

        // Save button section
        const saveSection = document.createElement('div');
        saveSection.className = 'save-section';
        saveSection.style.cssText = `
            padding: 12px;
            border-top: 1px solid var(--panel-border-color, #e0e0e0);
            display: flex;
            justify-content: space-between;
            align-items: center;
        `;
        saveSection.innerHTML = `
            <button class="btn btn-primary" id="save-type-btn">Attest Type Definition</button>
            <div class="save-status" id="save-status" style="font-size: 11px; color: var(--panel-text-secondary, #666);"></div>
        `;
        content.appendChild(saveSection);

        this.window.setContent(content);
        this.attachEventListeners();
    }

    /**
     * Create a field element for the list
     */
    private createFieldElement(field: FieldInfo): HTMLElement {
        const fieldEl = document.createElement('div');
        fieldEl.className = 'field-item';
        fieldEl.dataset.field = field.name;

        // Add selected state
        if (this.selectedField === field.name) {
            fieldEl.classList.add('selected');
        }

        // Add rich string highlight
        if (field.isRichString) {
            fieldEl.classList.add('rich-string-field');
        }

        // Add array indicator
        if (field.isArray) {
            fieldEl.classList.add('array-field');
        }

        fieldEl.innerHTML = `
            <span class="field-name">${field.name}</span>
            <div class="field-indicators">
                ${field.isRichString ? '<span class="indicator fuzzy-indicator" title="Fuzzy Searchable">🔍</span>' : ''}
                ${field.isArray ? '<span class="indicator array-indicator" title="Array Field">📦</span>' : ''}
            </div>
        `;

        return fieldEl;
    }

    /**
     * Attach event listeners
     */
    private attachEventListeners(): void {
        const container = this.window.getContentElement();

        // Field selection
        container.querySelectorAll('.field-item').forEach(item => {
            item.addEventListener('click', (e) => {
                const fieldName = (e.currentTarget as HTMLElement).dataset.field;
                if (fieldName) {
                    this.selectField(fieldName);
                }
            });
        });

        // Add new field
        const addInput = container.querySelector('.add-field-input') as HTMLInputElement;
        const addBtn = container.querySelector('.add-field-btn') as HTMLButtonElement;

        const addField = () => {
            const fieldName = addInput.value.trim();
            if (fieldName && !this.fields.has(fieldName)) {
                this.fields.set(fieldName, {
                    name: fieldName,
                    value: null,
                    isRichString: false,
                    isArray: false
                });
                this.render();
            }
            addInput.value = '';
        };

        addBtn?.addEventListener('click', addField);
        addInput?.addEventListener('keypress', (e) => {
            if (e.key === 'Enter') {
                addField();
            }
        });

        // Toggle controls
        const richStringToggle = container.querySelector('#rich-string-toggle') as HTMLInputElement;
        const arrayToggle = container.querySelector('#array-toggle') as HTMLInputElement;

        richStringToggle?.addEventListener('change', () => {
            if (this.selectedField) {
                this.toggleRichString(this.selectedField, richStringToggle.checked);
            }
        });

        arrayToggle?.addEventListener('change', () => {
            if (this.selectedField) {
                this.toggleArrayField(this.selectedField, arrayToggle.checked);
            }
        });

        // Save button handler
        const saveBtn = container.querySelector('#save-type-btn') as HTMLButtonElement;
        const saveStatus = container.querySelector('#save-status') as HTMLElement;

        saveBtn?.addEventListener('click', async () => {
            saveBtn.disabled = true;
            saveBtn.textContent = 'Attesting...';

            try {
                await this.persistTypeDefinition();

                // Show success message
                if (saveStatus) {
                    saveStatus.textContent = '✓ Attested';
                    saveStatus.style.color = 'var(--success-color, #2ecc71)';
                    setTimeout(() => {
                        saveStatus.textContent = '';
                    }, 3000);
                }
            } catch (error) {
                // Show error message
                if (saveStatus) {
                    saveStatus.textContent = '✗ Failed to attest';
                    saveStatus.style.color = 'var(--error-color, #e74c3c)';
                }
                console.error('Failed to attest type definition:', error);
            } finally {
                saveBtn.disabled = false;
                saveBtn.textContent = 'Attest Type Definition';
            }
        });
    }

    /**
     * Select a field for configuration
     */
    private selectField(fieldName: string): void {
        this.selectedField = fieldName;
        const field = this.fields.get(fieldName);

        if (!field) return;

        // Update UI
        const container = this.window.getContentElement();

        // Update field selection
        container.querySelectorAll('.field-item').forEach(item => {
            item.classList.toggle('selected', item.getAttribute('data-field') === fieldName);
        });

        // Show controls
        const controlsSection = container.querySelector('.field-controls') as HTMLElement;
        if (controlsSection) {
            controlsSection.style.display = 'block';

            // Update toggles
            const richStringToggle = container.querySelector('#rich-string-toggle') as HTMLInputElement;
            const arrayToggle = container.querySelector('#array-toggle') as HTMLInputElement;

            if (richStringToggle) richStringToggle.checked = field.isRichString;
            if (arrayToggle) arrayToggle.checked = field.isArray;
        }
    }

    /**
     * Toggle rich string field status
     */
    private toggleRichString(fieldName: string, enabled: boolean): void {
        const field = this.fields.get(fieldName);
        if (!field || !this.currentType) return;

        field.isRichString = enabled;

        // Update type definition
        if (!this.currentType.rich_string_fields) {
            this.currentType.rich_string_fields = [];
        }

        if (enabled) {
            if (!this.currentType.rich_string_fields.includes(fieldName)) {
                this.currentType.rich_string_fields.push(fieldName);
            }
        } else {
            this.currentType.rich_string_fields = this.currentType.rich_string_fields.filter(f => f !== fieldName);
        }

        // Update UI (don't persist - wait for Attest button)
        this.render();
        this.selectField(fieldName);
    }

    /**
     * Toggle array field status
     */
    private toggleArrayField(fieldName: string, enabled: boolean): void {
        const field = this.fields.get(fieldName);
        if (!field || !this.currentType) return;

        field.isArray = enabled;

        // Update type definition
        if (!this.currentType.array_fields) {
            this.currentType.array_fields = [];
        }

        if (enabled) {
            if (!this.currentType.array_fields.includes(fieldName)) {
                this.currentType.array_fields.push(fieldName);
            }
        } else {
            this.currentType.array_fields = this.currentType.array_fields.filter(f => f !== fieldName);
        }

        // Update UI (don't persist - wait for Attest button)
        this.render();
        this.selectField(fieldName);
    }

    /**
     * Persist type definition as attestation
     */
    private async persistTypeDefinition(): Promise<void> {
        if (!this.currentType) return;

        // Create type definition payload
        const typePayload = {
            name: this.currentType.name,
            label: this.currentType.label,
            color: this.currentType.color,
            opacity: this.currentType.opacity || 1.0,
            deprecated: this.currentType.deprecated || false,
            rich_string_fields: this.currentType.rich_string_fields || [],
            array_fields: this.currentType.array_fields || []
        };

        const response = await apiFetch('/api/types', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify(typePayload)
        });

        if (!response.ok) {
            throw new Error(`Failed to persist type definition: ${response.statusText}`);
        }
    }

    /**
     * Handle window close
     */
    private onClose(): void {
        this.currentType = null;
        this.fields.clear();
        this.selectedField = null;
    }

    /**
     * Create a new type definition
     */
    public createNewType(): void {
        // Open the window in "create new type" mode
        this.currentType = {
            name: '',
            label: '',
            color: '#888888',
            rich_string_fields: [],
            array_fields: []
        };

        this.fields.clear();
        this.renderNewTypeForm();
        this.window.setTitle('Create New Type');
        this.window.show();
    }

    /**
     * Render the new type creation form
     */
    private renderNewTypeForm(): void {
        const content = document.createElement('div');
        content.className = 'type-definition-content';

        // New type form
        const form = document.createElement('div');
        form.className = 'new-type-form';
        form.innerHTML = `
            <div class="form-group">
                <label for="type-name">Type Name</label>
                <input type="text" id="type-name" class="form-input" placeholder="e.g., document, user, task" autofocus>
            </div>
            <div class="form-group">
                <label for="type-label">Display Label</label>
                <input type="text" id="type-label" class="form-input" placeholder="e.g., Document, User, Task">
            </div>
            <div class="form-group">
                <label for="type-color">Color</label>
                <div class="color-input-wrapper">
                    <input type="color" id="type-color" class="form-color" value="#888888">
                    <input type="text" id="type-color-text" class="form-input" value="#888888" pattern="^#[0-9A-Fa-f]{6}$">
                </div>
            </div>
            <div class="form-actions">
                <button class="btn btn-primary" id="create-type-btn">Create Type</button>
                <button class="btn btn-secondary" id="cancel-btn">Cancel</button>
            </div>
        `;
        content.appendChild(form);

        this.window.setContent(content);
        this.attachNewTypeEventListeners();
    }

    /**
     * Attach event listeners for new type form
     */
    private attachNewTypeEventListeners(): void {
        const container = this.window.getContentElement();

        const nameInput = container.querySelector('#type-name') as HTMLInputElement;
        const labelInput = container.querySelector('#type-label') as HTMLInputElement;
        const colorInput = container.querySelector('#type-color') as HTMLInputElement;
        const colorTextInput = container.querySelector('#type-color-text') as HTMLInputElement;
        const createBtn = container.querySelector('#create-type-btn') as HTMLButtonElement;
        const cancelBtn = container.querySelector('#cancel-btn') as HTMLButtonElement;

        // Auto-generate label from name
        nameInput?.addEventListener('input', () => {
            if (!labelInput.value || labelInput.value === this.capitalizeFirst(nameInput.value.replace(/_/g, ' '))) {
                labelInput.value = this.capitalizeFirst(nameInput.value.replace(/_/g, ' '));
            }
        });

        // Sync color inputs
        colorInput?.addEventListener('input', () => {
            colorTextInput.value = colorInput.value;
        });

        colorTextInput?.addEventListener('input', () => {
            if (/^#[0-9A-Fa-f]{6}$/.test(colorTextInput.value)) {
                colorInput.value = colorTextInput.value;
            }
        });

        // Create button
        createBtn?.addEventListener('click', () => {
            const name = nameInput.value.trim();
            const label = labelInput.value.trim() || this.capitalizeFirst(name);
            const color = colorInput.value;

            if (!name) {
                nameInput.focus();
                return;
            }

            // Now open the regular editor with the new type
            this.open(name, {
                name,
                label,
                color,
                rich_string_fields: [],
                array_fields: []
            });
        });

        // Cancel button
        cancelBtn?.addEventListener('click', () => {
            this.window.hide();
        });

        // Enter key in inputs
        [nameInput, labelInput].forEach(input => {
            input?.addEventListener('keypress', (e) => {
                if (e.key === 'Enter') {
                    createBtn.click();
                }
            });
        });
    }

    /**
     * Helper to capitalize first letter
     */
    private capitalizeFirst(str: string): string {
        if (!str) return '';
        return str.charAt(0).toUpperCase() + str.slice(1);
    }
}

// Export singleton instance
export const typeDefinitionWindow = new TypeDefinitionWindow();