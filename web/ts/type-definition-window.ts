/**
 * Type Definition Glyph — configures type field metadata as a window glyph.
 *
 * Spawned from search view "+" button. Opens as a window manifestation.
 * Two modes: "create new type" form, then "edit type fields" after creation.
 */

import { apiFetch } from './api.js';
import { log, SEG } from './logger.ts';
import { escapeHtml } from './html-utils.js';
import { glyphRun } from './components/glyph/run.ts';
import type { Glyph } from './components/glyph/glyph.ts';

const GLYPH_ID = 'type-definition';

const HEX_CHARS = '0123456789abcdefABCDEF';

function isLetter(ch: string): boolean {
    return (ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z');
}

function isDigit(ch: string): boolean {
    return ch >= '0' && ch <= '9';
}

/** Validate hex color: # followed by 3-8 hex digits */
function isHexColor(value: string): boolean {
    if (!value || !value.startsWith('#') || value.length < 4 || value.length > 9) return false;
    for (let i = 1; i < value.length; i++) {
        if (!HEX_CHARS.includes(value[i])) return false;
    }
    return true;
}

function capitalizeFirst(str: string): string {
    if (!str) return '';
    return str.charAt(0).toUpperCase() + str.slice(1);
}

function validateFieldName(name: string): { valid: boolean; error?: string } {
    if (!name) {
        return { valid: false, error: 'Field name cannot be empty' };
    }
    if (!isLetter(name[0])) {
        return { valid: false, error: 'Field name must start with a letter' };
    }
    for (let i = 1; i < name.length; i++) {
        const ch = name[i];
        if (!isLetter(ch) && !isDigit(ch) && ch !== '_') {
            return { valid: false, error: 'Field name can only contain letters, numbers, and underscores' };
        }
    }
    if (name.length > 64) {
        return { valid: false, error: 'Field name must be 64 characters or less' };
    }
    return { valid: true };
}

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

// ── Module state ──────────────────────────────────────────────────────

let currentType: TypeDefinition | null = null;
let fields: Map<string, FieldInfo> = new Map();
let selectedField: string | null = null;

function resetState(): void {
    currentType = null;
    fields.clear();
    selectedField = null;
}

// ── Spawn entry points ───────────────────────────────────────────────

/**
 * Open type definition glyph for an existing type.
 */
export function openTypeDefinition(typeName: string, typeInfo?: TypeDefinition): void {
    currentType = typeInfo || {
        name: typeName,
        label: capitalizeFirst(typeName),
        color: '#888888',
        rich_string_fields: [],
        array_fields: []
    };

    fields.clear();
    selectedField = null;
    populateFieldsFromType(currentType);
    try {
        spawnGlyph('Type: ' + currentType.name, renderEditContent);
    } catch (e) {
        log.error(SEG.GLYPH, '[TypeDefGlyph] Failed to spawn glyph:', e);
    }
}

/**
 * Open type definition glyph in "create new type" mode.
 */
export function createNewType(): void {
    currentType = {
        name: '',
        label: '',
        color: '#888888',
        rich_string_fields: [],
        array_fields: []
    };
    fields.clear();
    selectedField = null;
    spawnGlyph('Create New Type', renderCreateContent);
}

function spawnGlyph(title: string, renderContent: () => HTMLElement): void {
    // Remove existing instance to re-render with new state
    if (glyphRun.has(GLYPH_ID)) {
        glyphRun.remove(GLYPH_ID);
    }

    const glyph: Glyph = {
        id: GLYPH_ID,
        title,
        renderContent,
        initialWidth: '500px',
        onClose: () => {
            resetState();
            log.debug(SEG.GLYPH, '[TypeDefGlyph] Closed');
        },
    };

    glyphRun.add(glyph);
    glyphRun.openGlyph(GLYPH_ID);
}

// ── Field population ─────────────────────────────────────────────────

function populateFieldsFromType(type: TypeDefinition): void {
    if (type.rich_string_fields) {
        for (const name of type.rich_string_fields) {
            fields.set(name, { name, value: null, isRichString: true, isArray: false });
        }
    }
    if (type.array_fields) {
        for (const name of type.array_fields) {
            const existing = fields.get(name);
            if (existing) {
                existing.isArray = true;
            } else {
                fields.set(name, { name, value: null, isRichString: false, isArray: true });
            }
        }
    }
}

// ── Edit mode content ────────────────────────────────────────────────

function renderEditContent(): HTMLElement {
    const container = document.createElement('div');
    container.className = 'glyph-content type-definition-content';
    buildEditUI(container);
    discoverFields(container);
    return container;
}

function buildEditUI(container: HTMLElement): void {
    container.innerHTML = '';
    if (!currentType) return;

    // Back button
    const backBtn = document.createElement('button');
    backBtn.textContent = '← Back';
    backBtn.style.cssText = 'background:none;border:none;color:var(--text-secondary);cursor:pointer;padding:4px 0;font-size:12px;margin-bottom:4px;';
    backBtn.addEventListener('mouseenter', () => { backBtn.style.color = 'var(--text-primary)'; });
    backBtn.addEventListener('mouseleave', () => { backBtn.style.color = 'var(--text-secondary)'; });
    backBtn.addEventListener('click', () => {
        resetState();
        createNewType();
    });
    container.appendChild(backBtn);

    // Type header
    const header = document.createElement('div');
    header.className = 'type-def-header';
    const safeColor = isHexColor(currentType.color) ? currentType.color : '#888888';
    header.innerHTML = `
        <div class="type-def-title" style="display:flex;align-items:center;gap:8px;">
            <span class="type-def-name" style="color:var(--text-primary);font-weight:600;">${escapeHtml(currentType.name)}</span>
            <span class="type-def-color" style="width:14px;height:14px;border-radius:3px;background:${safeColor};display:inline-block;"></span>
        </div>
    `;
    container.appendChild(header);

    // Fields section
    const fieldsSection = document.createElement('div');
    fieldsSection.className = 'type-def-fields';
    fieldsSection.innerHTML = '<h3 style="color:var(--text-primary);font-size:13px;">Fields</h3>';

    const fieldList = document.createElement('div');
    fieldList.className = 'field-list';

    fields.forEach((field) => {
        fieldList.appendChild(createFieldElement(field, container));
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
    container.appendChild(fieldsSection);

    // Field configuration controls (hidden until selection)
    const controlsSection = document.createElement('div');
    controlsSection.className = 'field-controls';
    controlsSection.style.display = 'none';
    controlsSection.innerHTML = `
        <h4>Field Configuration</h4>
        <div class="field-control-options">
            <label class="field-option">
                <input type="checkbox" id="rich-string-toggle">
                <span>Searchable</span>
                <span class="field-option-desc">Enable text search on this field</span>
            </label>
            <label class="field-option">
                <input type="checkbox" id="array-toggle">
                <span>Array Field</span>
                <span class="field-option-desc">Display as interactive tag badges</span>
            </label>
        </div>
    `;
    container.appendChild(controlsSection);

    // Save button section
    const saveSection = document.createElement('div');
    saveSection.className = 'save-section';
    saveSection.style.cssText = `
        padding: 12px;
        border-top: 1px solid var(--panel-border-color, #333);
        display: flex;
        justify-content: space-between;
        align-items: center;
    `;
    saveSection.innerHTML = `
        <button class="btn btn-primary" id="save-type-btn">Attest Type Definition</button>
        <div class="save-status" id="save-status" style="font-size: 11px; color: var(--text-secondary);"></div>
    `;
    container.appendChild(saveSection);

    attachEditListeners(container);
}

function createFieldElement(field: FieldInfo, container: HTMLElement): HTMLElement {
    const fieldEl = document.createElement('div');
    fieldEl.className = 'field-item';
    fieldEl.dataset.field = field.name;

    if (selectedField === field.name) fieldEl.classList.add('selected');
    if (field.isRichString) fieldEl.classList.add('searchable-field');
    if (field.isArray) fieldEl.classList.add('array-field');

    fieldEl.innerHTML = `
        <span class="field-name">${escapeHtml(field.name)}</span>
        <div class="field-indicators">
            ${field.isRichString ? '<span class="indicator search-indicator" title="Searchable">🔍</span>' : ''}
            ${field.isArray ? '<span class="indicator array-indicator" title="Array Field">📦</span>' : ''}
        </div>
    `;

    fieldEl.addEventListener('click', () => selectFieldUI(field.name, container));
    return fieldEl;
}

function attachEditListeners(container: HTMLElement): void {
    const addInput = container.querySelector('.add-field-input') as HTMLInputElement;
    const addBtn = container.querySelector('.add-field-btn') as HTMLButtonElement;

    const addField = () => {
        const fieldName = addInput.value.trim();

        if (fields.size >= 50) {
            showStatus(container, 'Maximum 50 fields allowed per type', '#e74c3c');
            return;
        }

        const validation = validateFieldName(fieldName);
        if (!validation.valid) {
            showStatus(container, validation.error || 'Invalid field name', '#e74c3c');
            addInput.focus();
            return;
        }

        if (fieldName && !fields.has(fieldName)) {
            fields.set(fieldName, { name: fieldName, value: null, isRichString: false, isArray: false });
            buildEditUI(container);
        }
        addInput.value = '';
    };

    addBtn?.addEventListener('click', addField);
    addInput?.addEventListener('keypress', (e) => {
        if (e.key === 'Enter') addField();
    });

    // Toggle controls
    const richToggle = container.querySelector('#rich-string-toggle') as HTMLInputElement;
    const arrayToggle = container.querySelector('#array-toggle') as HTMLInputElement;

    richToggle?.addEventListener('change', () => {
        if (selectedField) toggleFieldOption(selectedField, 'rich_string_fields', richToggle.checked, container);
    });
    arrayToggle?.addEventListener('change', () => {
        if (selectedField) toggleFieldOption(selectedField, 'array_fields', arrayToggle.checked, container);
    });

    // Save button
    const saveBtn = container.querySelector('#save-type-btn') as HTMLButtonElement;

    saveBtn?.addEventListener('click', async () => {
        saveBtn.disabled = true;
        saveBtn.textContent = 'Attesting...';
        try {
            await persistTypeDefinition();
            showStatus(container, '✓ Attested', 'var(--success-color, #2ecc71)');
        } catch (error) {
            showStatus(container, '✗ Failed to attest', 'var(--error-color, #e74c3c)', 0);
            log.error(SEG.ERROR, 'Failed to attest type definition:', error);
        } finally {
            saveBtn.disabled = false;
            saveBtn.textContent = 'Attest Type Definition';
        }
    });
}

function selectFieldUI(fieldName: string, container: HTMLElement): void {
    selectedField = fieldName;
    const field = fields.get(fieldName);
    if (!field) return;

    container.querySelectorAll('.field-item').forEach(item => {
        item.classList.toggle('selected', item.getAttribute('data-field') === fieldName);
    });

    const controls = container.querySelector('.field-controls') as HTMLElement;
    if (controls) {
        controls.style.display = 'block';
        const richToggle = container.querySelector('#rich-string-toggle') as HTMLInputElement;
        const arrayToggle = container.querySelector('#array-toggle') as HTMLInputElement;
        if (richToggle) richToggle.checked = field.isRichString;
        if (arrayToggle) arrayToggle.checked = field.isArray;
    }
}

function toggleFieldOption(fieldName: string, listKey: 'rich_string_fields' | 'array_fields', enabled: boolean, container: HTMLElement): void {
    const field = fields.get(fieldName);
    if (!field || !currentType) return;

    if (listKey === 'rich_string_fields') {
        field.isRichString = enabled;
    } else {
        field.isArray = enabled;
    }

    if (!currentType[listKey]) currentType[listKey] = [];
    const list = currentType[listKey]!;

    if (enabled) {
        if (!list.includes(fieldName)) list.push(fieldName);
    } else {
        currentType[listKey] = list.filter(f => f !== fieldName);
    }

    buildEditUI(container);
    selectFieldUI(fieldName, container);
}

function showStatus(container: HTMLElement, text: string, color: string, clearMs: number = 3000): void {
    const el = container.querySelector('#save-status') as HTMLElement;
    if (!el) return;
    el.textContent = text;
    el.style.color = color;
    if (clearMs > 0) {
        setTimeout(() => { el.textContent = ''; }, clearMs);
    }
}

// ── Field discovery ──────────────────────────────────────────────────

async function discoverFields(container: HTMLElement): Promise<void> {
    if (!currentType) return;
    try {
        const response = await apiFetch(`/api/types/${currentType.name}/fields`);
        if (response.ok) {
            const data = await response.json();
            processFieldData(data);
            buildEditUI(container);
        }
    } catch (error) {
        log.error(SEG.ERROR, 'Failed to discover fields:', error);
        showStatus(container, 'Could not auto-discover fields - add them manually', '#f39c12', 4000);
    }
}

function processFieldData(data: any): void {
    if (!currentType || !data.fields) return;
    fields.clear();
    for (const name of Object.keys(data.fields)) {
        fields.set(name, {
            name,
            value: data.fields[name],
            isRichString: currentType.rich_string_fields?.includes(name) || false,
            isArray: currentType.array_fields?.includes(name) || false,
        });
    }
}

// ── Persistence ──────────────────────────────────────────────────────

async function persistTypeDefinition(): Promise<void> {
    if (!currentType) return;

    const allFields = [
        ...(currentType.rich_string_fields || []),
        ...(currentType.array_fields || [])
    ];

    if (allFields.length > 50) {
        throw new Error(`Too many fields: ${allFields.length}. Maximum 50 fields allowed per type.`);
    }

    for (const name of allFields) {
        const validation = validateFieldName(name);
        if (!validation.valid) {
            throw new Error(`Invalid field name '${name}': ${validation.error}`);
        }
    }

    const response = await apiFetch('/api/types', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
            name: currentType.name,
            label: currentType.label,
            color: currentType.color,
            opacity: currentType.opacity || 1.0,
            deprecated: currentType.deprecated || false,
            rich_string_fields: currentType.rich_string_fields || [],
            array_fields: currentType.array_fields || []
        })
    });

    if (!response.ok) {
        throw new Error(`Failed to persist type definition for '${currentType.name}': ${response.statusText}`);
    }
}

// ── Create mode content ──────────────────────────────────────────────

function renderCreateContent(): HTMLElement {
    const container = document.createElement('div');
    container.className = 'glyph-content type-definition-content';

    // Existing types list (loaded async)
    const typesSection = document.createElement('div');
    typesSection.className = 'type-def-existing-types';
    typesSection.innerHTML = '<h4>Existing Types</h4>';
    const typesList = document.createElement('div');
    typesList.className = 'type-def-types-list';
    typesList.style.cssText = 'font-size:12px;color:var(--text-secondary);max-height:200px;overflow-y:auto;';
    typesList.textContent = 'Loading...';
    typesSection.appendChild(typesList);
    container.appendChild(typesSection);

    // New type form
    const form = document.createElement('div');
    form.className = 'new-type-form';
    form.innerHTML = `
        <h4 style="margin:12px 0 8px;">Create New Type</h4>
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
                <input type="text" id="type-color-text" class="form-input" value="#888888">
            </div>
        </div>
        <div class="form-actions">
            <button class="btn btn-primary" id="create-type-btn">Create Type</button>
            <button class="btn btn-secondary" id="cancel-btn">Cancel</button>
        </div>
    `;
    container.appendChild(form);

    attachCreateListeners(container);
    loadExistingTypes(typesList, container);
    return container;
}

/**
 * Fetch and render existing types as clickable items.
 * Clicking a type switches to edit mode for that type.
 */
async function loadExistingTypes(typesList: HTMLElement, container: HTMLElement): Promise<void> {
    try {
        const response = await apiFetch('/api/types');
        if (!response.ok) {
            typesList.textContent = 'Failed to load types';
            return;
        }
        const types: TypeDefinition[] = await response.json();

        if (types.length === 0) {
            typesList.textContent = 'No types defined yet';
            return;
        }

        typesList.innerHTML = '';
        for (const type of types) {
            const item = document.createElement('div');
            item.className = 'type-def-type-item';
            item.style.cssText = 'display:flex;align-items:center;gap:8px;padding:4px 8px;cursor:pointer;border-radius:4px;';

            const colorDot = document.createElement('span');
            const safeColor = isHexColor(type.color) ? type.color : '#888888';
            colorDot.style.cssText = `width:10px;height:10px;border-radius:50%;background:${safeColor};flex-shrink:0;`;

            const name = document.createElement('span');
            name.style.cssText = 'font-size:12px;font-weight:500;';
            name.textContent = type.label || type.name;

            const fieldCount = document.createElement('span');
            fieldCount.style.cssText = 'font-size:11px;color:var(--text-secondary);margin-left:auto;';
            const rCount = type.rich_string_fields?.length || 0;
            const aCount = type.array_fields?.length || 0;
            const total = rCount + aCount;
            fieldCount.textContent = total > 0 ? `${total} field${total > 1 ? 's' : ''}` : '';

            if (type.deprecated) {
                name.style.textDecoration = 'line-through';
                name.style.opacity = '0.6';
            }

            item.appendChild(colorDot);
            item.appendChild(name);
            item.appendChild(fieldCount);

            item.addEventListener('mouseenter', () => { item.style.background = 'var(--hover-bg, rgba(255,255,255,0.05))'; });
            item.addEventListener('mouseleave', () => { item.style.background = ''; });

            item.addEventListener('click', () => {
                // Switch to edit mode for this type
                currentType = type;
                fields.clear();
                selectedField = null;
                populateFieldsFromType(type);
                buildEditUI(container);
                discoverFields(container);
            });

            typesList.appendChild(item);
        }
    } catch (error) {
        log.error(SEG.ERROR, 'Failed to load existing types:', error);
        typesList.textContent = 'Failed to load types';
    }
}

function attachCreateListeners(container: HTMLElement): void {
    const nameInput = container.querySelector('#type-name') as HTMLInputElement;
    const labelInput = container.querySelector('#type-label') as HTMLInputElement;
    const colorInput = container.querySelector('#type-color') as HTMLInputElement;
    const colorTextInput = container.querySelector('#type-color-text') as HTMLInputElement;
    const createBtn = container.querySelector('#create-type-btn') as HTMLButtonElement;
    const cancelBtn = container.querySelector('#cancel-btn') as HTMLButtonElement;

    // Auto-generate label from name
    nameInput?.addEventListener('input', () => {
        const labelFromName = capitalizeFirst(nameInput.value.split('_').join(' '));
        if (!labelInput.value || labelInput.value === labelFromName) {
            labelInput.value = labelFromName;
        }
    });

    // Sync color inputs
    colorInput?.addEventListener('input', () => {
        colorTextInput.value = colorInput.value;
    });
    colorTextInput?.addEventListener('input', () => {
        if (isHexColor(colorTextInput.value) && colorTextInput.value.length === 7) {
            colorInput.value = colorTextInput.value;
        }
    });

    // Create button — transitions to edit mode in-place
    createBtn?.addEventListener('click', () => {
        const name = nameInput.value.trim();
        const label = labelInput.value.trim() || capitalizeFirst(name);
        const color = colorInput.value;

        const validation = validateFieldName(name);
        if (!validation.valid) {
            const errorEl = document.createElement('div');
            errorEl.className = 'field-error';
            errorEl.style.cssText = 'color: #e74c3c; font-size: 12px; margin-top: 4px;';
            errorEl.textContent = validation.error || 'Invalid type name';

            nameInput.parentElement?.querySelector('.field-error')?.remove();
            nameInput.parentElement?.appendChild(errorEl);
            nameInput.focus();
            setTimeout(() => errorEl.remove(), 3000);
            return;
        }

        // Transition to edit mode: update state and rebuild content in-place
        currentType = { name, label, color, rich_string_fields: [], array_fields: [] };
        fields.clear();
        selectedField = null;
        buildEditUI(container);
        discoverFields(container);
    });

    // Cancel button
    cancelBtn?.addEventListener('click', () => {
        if (glyphRun.has(GLYPH_ID)) {
            glyphRun.remove(GLYPH_ID);
        }
    });

    // Enter key in inputs
    [nameInput, labelInput].forEach(input => {
        input?.addEventListener('keypress', (e) => {
            if (e.key === 'Enter') createBtn.click();
        });
    });
}

// ── Backwards-compatible singleton ───────────────────────────────────

/** @deprecated Use createNewType() and openTypeDefinition() directly. */
export const typeDefinitionWindow = {
    createNewType,
    open: openTypeDefinition,
    getFieldInfo: (name: string): FieldInfo | undefined => fields.get(name),
};
