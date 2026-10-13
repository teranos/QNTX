import { describe, test, expect, beforeEach } from 'vitest';
import { endsOf, getFieldInfo, openTypeDefinition } from './type-definition-window';

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

        expect(getFieldInfo('name')?.isRichString).toBe(true);
        expect(getFieldInfo('cuisine_type')?.isRichString).toBe(true);
    });

    test('marks fields from array_fields as arrays', () => {
        openTypeDefinition('menu_item', {
            name: 'menu_item',
            label: 'Menu Item',
            color: '#f39c12',
            rich_string_fields: [],
            array_fields: ['allergens']
        });

        expect(getFieldInfo('allergens')?.isArray).toBe(true);
    });

    test('preserves both rich and array field types', () => {
        openTypeDefinition('food_review', {
            name: 'food_review',
            label: 'Food Review',
            color: '#9b59b6',
            rich_string_fields: ['review_text', 'reviewer_name'],
            array_fields: ['tags']
        });

        expect(getFieldInfo('review_text')?.isRichString).toBe(true);
        expect(getFieldInfo('reviewer_name')?.isRichString).toBe(true);
        expect(getFieldInfo('tags')?.isArray).toBe(true);
    });

    test('a type that names where its attestations start and end shows both as fields', () => {
        openTypeDefinition('employed', {
            name: 'employed',
            label: 'Employed',
            color: '#2ecc71',
            rich_string_fields: [],
            array_fields: [],
            start_field: 'from',
            end_field: 'until',
        });

        expect(getFieldInfo('from')?.name).toBe('from');
        expect(getFieldInfo('until')?.name).toBe('until');
    });

    test('ends are posted both or neither', () => {
        const type = { name: 'employed', label: 'Employed', color: '#2ecc71' };
        expect(endsOf({ ...type, start_field: 'from', end_field: 'until' })).toEqual({ start_field: 'from', end_field: 'until' });
        expect(endsOf({ ...type, start_field: null, end_field: null })).toEqual({});
        expect(endsOf(type)).toEqual({});
        expect(() => endsOf({ ...type, start_field: 'from' })).toThrow('both or neither');
        expect(() => endsOf({ ...type, end_field: 'until' })).toThrow('both or neither');
    });
});
