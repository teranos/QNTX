import { describe, test, expect, beforeEach } from 'vitest';
import { typeDefinitionWindow, openTypeDefinition } from './type-definition-window';

describe('TypeDefinitionWindow', () => {
    beforeEach(() => {
        // Reset by opening a blank type (openTypeDefinition clears state)
    });

    test('marks fields from rich_string_fields as searchable', () => {
        openTypeDefinition('restaurant', {
            name: 'restaurant',
            label: 'Restaurant',
            color: '#e74c3c',
            rich_string_fields: ['name', 'cuisine_type'],
            array_fields: []
        });

        expect(typeDefinitionWindow.getFieldInfo('name')?.isRichString).toBe(true);
        expect(typeDefinitionWindow.getFieldInfo('cuisine_type')?.isRichString).toBe(true);
    });

    test('marks fields from array_fields as arrays', () => {
        openTypeDefinition('menu_item', {
            name: 'menu_item',
            label: 'Menu Item',
            color: '#f39c12',
            rich_string_fields: [],
            array_fields: ['allergens']
        });

        expect(typeDefinitionWindow.getFieldInfo('allergens')?.isArray).toBe(true);
    });

    test('preserves both rich and array field types', () => {
        openTypeDefinition('food_review', {
            name: 'food_review',
            label: 'Food Review',
            color: '#9b59b6',
            rich_string_fields: ['review_text', 'reviewer_name'],
            array_fields: ['tags']
        });

        expect(typeDefinitionWindow.getFieldInfo('review_text')?.isRichString).toBe(true);
        expect(typeDefinitionWindow.getFieldInfo('reviewer_name')?.isRichString).toBe(true);
        expect(typeDefinitionWindow.getFieldInfo('tags')?.isArray).toBe(true);
    });
});
