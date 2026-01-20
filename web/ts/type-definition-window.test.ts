import { describe, test, expect, beforeEach } from 'vitest';
import { TypeDefinitionWindow } from './type-definition-window';

describe('TypeDefinitionWindow', () => {
    let window: TypeDefinitionWindow;

    beforeEach(() => {
        window = new TypeDefinitionWindow();
    });

    test('marks fields from rich_string_fields as searchable', () => {
        // Open a type with searchable fields
        window.open('restaurant', {
            name: 'restaurant',
            label: 'Restaurant',
            color: '#e74c3c',
            rich_string_fields: ['name', 'cuisine_type'],
            array_fields: []
        });

        // Verify the fields are marked searchable
        expect(window.getFieldInfo('name')?.isRichString).toBe(true);
        expect(window.getFieldInfo('cuisine_type')?.isRichString).toBe(true);
    });

    test('marks fields from array_fields as arrays', () => {
        // Open a type with array fields
        window.open('menu_item', {
            name: 'menu_item',
            label: 'Menu Item',
            color: '#f39c12',
            rich_string_fields: [],
            array_fields: ['allergens']
        });

        // Verify the field is marked as array
        expect(window.getFieldInfo('allergens')?.isArray).toBe(true);
    });

    test('preserves both rich and array field types', () => {
        // Open a type with both types of fields
        window.open('food_review', {
            name: 'food_review',
            label: 'Food Review',
            color: '#9b59b6',
            rich_string_fields: ['review_text', 'reviewer_name'],
            array_fields: ['tags']
        });

        // Verify rich fields
        expect(window.getFieldInfo('review_text')?.isRichString).toBe(true);
        expect(window.getFieldInfo('reviewer_name')?.isRichString).toBe(true);

        // Verify array field
        expect(window.getFieldInfo('tags')?.isArray).toBe(true);
    });
});