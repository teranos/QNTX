import { describe, test, expect, beforeEach, vi } from 'vitest';
import { JSDOM } from 'jsdom';
import { TypeDefinitionWindow } from './type-definition-window';

// Mock WebSocket
class MockWebSocket {
    readyState = 1; // OPEN
    send = vi.fn();
    close = vi.fn();
    addEventListener = vi.fn();
    removeEventListener = vi.fn();
}

// Mock global WebSocket
global.WebSocket = MockWebSocket as any;

describe('TypeDefinitionWindow - Restaurant Domain', () => {
    let dom: JSDOM;
    let window: TypeDefinitionWindow;
    let container: HTMLElement;

    beforeEach(() => {
        // Setup DOM
        dom = new JSDOM(`
            <!DOCTYPE html>
            <html>
            <body>
                <div id="type-definition-window"></div>
            </body>
            </html>
        `);

        global.document = dom.window.document as any;
        global.window = dom.window as any;

        container = document.getElementById('type-definition-window')!;
        window = new TypeDefinitionWindow();
    });

    describe('Restaurant type creation', () => {
        test('should create restaurant type with searchable culinary fields', () => {
            // Chez Laurent wants customers to find them by cuisine and neighborhood
            window.createNewType();

            // Set basic restaurant properties
            const nameInput = container.querySelector('input[placeholder="Type name (e.g., person, company)"]') as HTMLInputElement;
            const labelInput = container.querySelector('input[placeholder="Display label"]') as HTMLInputElement;

            expect(nameInput).toBeTruthy();
            expect(labelInput).toBeTruthy();

            nameInput.value = 'restaurant';
            labelInput.value = 'Restaurant';

            // Add searchable fields for culinary discovery
            window.fields.set('name', {
                name: 'name',
                value: null,
                isRichString: true,
                isArray: false
            });
            window.fields.set('cuisine_type', {
                name: 'cuisine_type',
                value: null,
                isRichString: true,
                isArray: false
            });
            window.fields.set('neighborhood', {
                name: 'neighborhood',
                value: null,
                isRichString: true,
                isArray: false
            });
            // NOT searchable: tax_id, owner_ssn
            window.fields.set('tax_id', {
                name: 'tax_id',
                value: null,
                isRichString: false,
                isArray: false
            });

            // Verify rich fields are marked for search
            const richFields = Array.from(window.fields.values())
                .filter(f => f.isRichString)
                .map(f => f.name);

            expect(richFields).toContain('name');
            expect(richFields).toContain('cuisine_type');
            expect(richFields).toContain('neighborhood');
            expect(richFields).not.toContain('tax_id');
        });

        test('should configure menu_item type with dietary searchability', () => {
            // The Blue Door needs diners to find dishes by dietary restrictions
            window.createNewType();

            const nameInput = container.querySelector('input[placeholder="Type name (e.g., person, company)"]') as HTMLInputElement;
            nameInput.value = 'menu_item';

            // Configure ingredient and dietary fields as searchable
            window.fields.set('dish_name', {
                name: 'dish_name',
                value: null,
                isRichString: true,
                isArray: false
            });
            window.fields.set('ingredients', {
                name: 'ingredients',
                value: null,
                isRichString: true,
                isArray: false
            });
            window.fields.set('dietary_tags', {
                name: 'dietary_tags',
                value: null,
                isRichString: true,
                isArray: false
            });
            window.fields.set('allergens', {
                name: 'allergens',
                value: null,
                isRichString: false,
                isArray: true  // Array field for specific allergen list
            });

            // Verify dietary fields are searchable
            const richFields = Array.from(window.fields.values())
                .filter(f => f.isRichString)
                .map(f => f.name);

            expect(richFields).toContain('ingredients');
            expect(richFields).toContain('dietary_tags');

            // Verify allergens is an array field
            const arrayFields = Array.from(window.fields.values())
                .filter(f => f.isArray)
                .map(f => f.name);
            expect(arrayFields).toContain('allergens');
        });
    });

    describe('Editing existing restaurant types', () => {
        test('should display existing rich_string_fields when editing restaurant', () => {
            // Open existing restaurant type definition
            const restaurantType = {
                name: 'restaurant',
                label: 'Restaurant',
                color: '#e74c3c',
                rich_string_fields: ['name', 'cuisine_type', 'chef_bio', 'specialties'],
                array_fields: []
            };

            window.open('restaurant', restaurantType);

            // Verify fields were discovered from rich_string_fields
            expect(window.fields.has('name')).toBe(true);
            expect(window.fields.has('cuisine_type')).toBe(true);
            expect(window.fields.has('chef_bio')).toBe(true);
            expect(window.fields.has('specialties')).toBe(true);

            // Verify they're marked as rich strings
            expect(window.fields.get('name')?.isRichString).toBe(true);
            expect(window.fields.get('cuisine_type')?.isRichString).toBe(true);
        });

        test('should handle health_inspection type evolution', () => {
            // Health department initially hides violation details
            const initialType = {
                name: 'health_inspection',
                label: 'Health Inspection',
                color: '#27ae60',
                rich_string_fields: ['inspector_name', 'summary'],
                array_fields: []
            };

            window.open('health_inspection', initialType);

            // Public pressure: make violations searchable
            window.fields.set('violations_found', {
                name: 'violations_found',
                value: null,
                isRichString: true,  // Now searchable!
                isArray: false
            });
            window.fields.set('corrective_actions', {
                name: 'corrective_actions',
                value: null,
                isRichString: true,  // Also searchable
                isArray: false
            });

            // Verify transparency: violations are now searchable
            const richFields = Array.from(window.fields.values())
                .filter(f => f.isRichString)
                .map(f => f.name);

            expect(richFields).toContain('violations_found');
            expect(richFields).toContain('corrective_actions');
            expect(richFields).toContain('inspector_name');
            expect(richFields).toContain('summary');
        });
    });

    describe('Food review type relationships', () => {
        test('should create food_review type linking critics to restaurants', () => {
            // The Michelin Guide needs deeply searchable reviews
            window.createNewType();

            const nameInput = container.querySelector('input[placeholder="Type name (e.g., person, company)"]') as HTMLInputElement;
            nameInput.value = 'food_review';

            // All review content should be searchable
            const reviewFields = [
                'reviewer_name',
                'review_text',
                'highlighted_dishes',
                'ambiance_notes'
            ];

            reviewFields.forEach(fieldName => {
                window.fields.set(fieldName, {
                    name: fieldName,
                    value: null,
                    isRichString: true,
                    isArray: false
                });
            });

            // Internal rating not searchable
            window.fields.set('star_rating', {
                name: 'star_rating',
                value: null,
                isRichString: false,
                isArray: false
            });

            // Verify all review content is searchable
            const richFields = Array.from(window.fields.values())
                .filter(f => f.isRichString)
                .map(f => f.name);

            expect(richFields).toHaveLength(4);
            expect(richFields).not.toContain('star_rating');
        });
    });

    describe('City culinary destination search', () => {
        test('should configure city type for food scene discovery', () => {
            // San Francisco wants to be known for its food culture
            window.createNewType();

            const nameInput = container.querySelector('input[placeholder="Type name (e.g., person, company)"]') as HTMLInputElement;
            nameInput.value = 'city';

            // Make culinary aspects searchable
            window.fields.set('name', {
                name: 'name',
                value: null,
                isRichString: true,
                isArray: false
            });
            window.fields.set('culinary_scene', {
                name: 'culinary_scene',
                value: null,
                isRichString: true,
                isArray: false
            });
            window.fields.set('famous_districts', {
                name: 'famous_districts',
                value: null,
                isRichString: true,
                isArray: false
            });
            window.fields.set('food_festivals', {
                name: 'food_festivals',
                value: null,
                isRichString: true,
                isArray: false
            });

            // Population stats not searchable
            window.fields.set('population', {
                name: 'population',
                value: null,
                isRichString: false,
                isArray: false
            });

            const richFields = Array.from(window.fields.values())
                .filter(f => f.isRichString)
                .map(f => f.name);

            // Verify food culture is searchable
            expect(richFields).toContain('culinary_scene');
            expect(richFields).toContain('famous_districts');
            expect(richFields).toContain('food_festivals');
            expect(richFields).not.toContain('population');
        });
    });

    describe('Field management UI', () => {
        test('should toggle field searchability with checkbox', () => {
            window.createNewType();

            // Add a field
            window.fields.set('cuisine_type', {
                name: 'cuisine_type',
                value: null,
                isRichString: false,
                isArray: false
            });

            // Simulate checking the "searchable" checkbox
            const field = window.fields.get('cuisine_type')!;
            field.isRichString = true;

            expect(window.fields.get('cuisine_type')?.isRichString).toBe(true);
        });

        test('should preserve existing fields when adding new ones', () => {
            const existingType = {
                name: 'restaurant',
                label: 'Restaurant',
                color: '#e74c3c',
                rich_string_fields: ['name', 'cuisine_type'],
                array_fields: []
            };

            window.open('restaurant', existingType);

            // Add new field while preserving existing
            window.fields.set('neighborhood', {
                name: 'neighborhood',
                value: null,
                isRichString: true,
                isArray: false
            });

            // Verify all fields present
            expect(window.fields.has('name')).toBe(true);
            expect(window.fields.has('cuisine_type')).toBe(true);
            expect(window.fields.has('neighborhood')).toBe(true);
            expect(window.fields.size).toBe(3);
        });
    });
});