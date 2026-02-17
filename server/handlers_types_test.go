package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	qntxtest "github.com/teranos/QNTX/internal/testing"
)

// TypeRequest represents the JSON request for creating/updating types
type TypeRequest struct {
	Name             string   `json:"name"`
	Label            string   `json:"label"`
	Color            string   `json:"color"`
	Opacity          *float64 `json:"opacity,omitempty"`
	Deprecated       bool     `json:"deprecated"`
	RichStringFields []string `json:"rich_string_fields"`
	ArrayFields      []string `json:"array_fields"`
}

// TestRichStringFieldsForRestaurantDomain tests that restaurant ecosystem types
// can configure which fields are fuzzy-searchable, enabling discovery of
// restaurants by cuisine, menu items by ingredients, and cities by neighborhoods
func TestRichStringFieldsForRestaurantDomain(t *testing.T) {
	// Create test database with real migrations
	db := qntxtest.CreateTestDB(t)

	// Create server instance
	srv, err := NewQNTXServer(db, ":memory:", 0, "")
	require.NoError(t, err)
	defer srv.Stop()

	// Helper to verify type fields via GET
	verifyTypeFields := func(t *testing.T, srv *QNTXServer, typeName string, expectedRichFields []string, expectedArrayFields []string) {
		req := httptest.NewRequest("GET", "/api/types/"+typeName, nil)
		w := httptest.NewRecorder()
		srv.HandleTypes(w, req)

		require.Equal(t, http.StatusOK, w.Code) // GET requests return 200

		var typeResp map[string]interface{}
		err := json.Unmarshal(w.Body.Bytes(), &typeResp)
		require.NoError(t, err)

		// Check rich_string_fields
		if expectedRichFields != nil {
			richFields, ok := typeResp["rich_string_fields"].([]interface{})
			require.True(t, ok, "rich_string_fields should be an array")

			var richStrings []string
			for _, field := range richFields {
				if str, ok := field.(string); ok {
					richStrings = append(richStrings, str)
				}
			}
			assert.ElementsMatch(t, expectedRichFields, richStrings)
		}

		// Check array_fields
		if expectedArrayFields != nil {
			arrayFields, ok := typeResp["array_fields"].([]interface{})
			require.True(t, ok, "array_fields should be an array")

			var arrayStrings []string
			for _, field := range arrayFields {
				if str, ok := field.(string); ok {
					arrayStrings = append(arrayStrings, str)
				}
			}
			assert.ElementsMatch(t, expectedArrayFields, arrayStrings)
		}
	}

	t.Run("restaurant type with searchable fields", func(t *testing.T) {
		// Chez Laurent wants to be findable by cuisine type, chef bio, and specialties
		// but NOT by internal fields like tax_id or owner_ssn
		payload := TypeRequest{
			Name:  "restaurant",
			Label: "Restaurant",
			Color: "#e74c3c", // Appetizing red
			RichStringFields: []string{
				"name",           // "Chez Laurent"
				"cuisine_type",   // "French Bistro"
				"chef_bio",       // "Trained at Le Cordon Bleu..."
				"specialties",    // "Duck confit, Bouillabaisse"
				"neighborhood",   // "Mission District"
			},
			// NOT searchable: tax_id, license_number, owner_ssn
		}

		body, _ := json.Marshal(payload)
		req := httptest.NewRequest("POST", "/api/types", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		srv.HandleTypes(w, req)

		require.Equal(t, http.StatusCreated, w.Code)

		// Verify Chez Laurent can be found by cuisine and specialties
		verifyTypeFields(t, srv, "restaurant",
			[]string{"name", "cuisine_type", "chef_bio", "specialties", "neighborhood"},
			nil)
	})

	t.Run("menu_item type with ingredient searchability", func(t *testing.T) {
		// Menu items at The Blue Door need to be searchable for dietary restrictions
		payload := TypeRequest{
			Name:  "menu_item",
			Label: "Menu Item",
			Color: "#f39c12", // Golden like a croissant
			RichStringFields: []string{
				"dish_name",        // "Coq au Vin"
				"description",      // "Braised chicken in wine sauce..."
				"ingredients",      // "chicken, red wine, mushrooms, pearl onions"
				"dietary_tags",     // "gluten-free, dairy-free"
				"wine_pairing",     // "Pairs well with Burgundy"
			},
			ArrayFields: []string{
				"allergens", // ["nuts", "shellfish"]
			},
			// NOT searchable: cost_to_make, supplier_id, prep_time_minutes
		}

		body, _ := json.Marshal(payload)
		req := httptest.NewRequest("POST", "/api/types", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		srv.HandleTypes(w, req)

		require.Equal(t, http.StatusCreated, w.Code)

		// Verify diners can search for "vegetarian" or "truffle" in ingredients
		verifyTypeFields(t, srv, "menu_item",
			[]string{"dish_name", "description", "ingredients", "dietary_tags", "wine_pairing"},
			[]string{"allergens"})
	})

	t.Run("city type for culinary destination search", func(t *testing.T) {
		// San Francisco wants to be known for its food scene
		payload := TypeRequest{
			Name:  "city",
			Label: "City",
			Color: "#3498db", // Ocean blue for SF
			RichStringFields: []string{
				"name",              // "San Francisco"
				"culinary_scene",    // "Famous for sourdough, Dungeness crab..."
				"famous_districts",  // "Mission for burritos, Chinatown for dim sum"
				"food_festivals",    // "Eat Drink SF, SF Street Food Festival"
			},
			// Hints at future relationships:
			// - city has_many restaurants
			// - city hosts_events food_festivals
		}

		body, _ := json.Marshal(payload)
		req := httptest.NewRequest("POST", "/api/types", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		srv.HandleTypes(w, req)

		require.Equal(t, http.StatusCreated, w.Code)

		verifyTypeFields(t, srv, "city",
			[]string{"name", "culinary_scene", "famous_districts", "food_festivals"},
			nil)
	})

	t.Run("food_review type links critics to restaurants", func(t *testing.T) {
		// The Michelin Guide needs reviews to be deeply searchable
		payload := TypeRequest{
			Name:  "food_review",
			Label: "Food Review",
			Color: "#9b59b6", // Sophisticated purple
			RichStringFields: []string{
				"reviewer_name",     // "Ruth Reichl"
				"review_text",       // "The Duck à l'Orange transported me..."
				"highlighted_dishes", // "Don't miss the soufflé"
				"ambiance_notes",    // "Romantic lighting, jazz trio on Fridays"
			},
			// Relationships hinted at:
			// - food_review reviews restaurant
			// - food_review mentions menu_item
			// - food_review written_by critic (future type)
		}

		body, _ := json.Marshal(payload)
		req := httptest.NewRequest("POST", "/api/types", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		srv.HandleTypes(w, req)

		require.Equal(t, http.StatusCreated, w.Code)

		verifyTypeFields(t, srv, "food_review",
			[]string{"reviewer_name", "review_text", "highlighted_dishes", "ambiance_notes"},
			nil)
	})

	t.Run("health_inspection type for safety transparency", func(t *testing.T) {
		// Health department wants violations to be searchable by the public
		// First create with minimal fields
		initialPayload := TypeRequest{
			Name:             "health_inspection",
			Label:            "Health Inspection",
			Color:            "#27ae60", // Green for healthy
			RichStringFields: []string{"inspector_name", "summary"},
		}

		body, _ := json.Marshal(initialPayload)
		req := httptest.NewRequest("POST", "/api/types", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		srv.HandleTypes(w, req)

		require.Equal(t, http.StatusCreated, w.Code)

		// Public pressure: make violation details searchable too
		updatePayload := TypeRequest{
			Name:  "health_inspection",
			Label: "Health Inspection",
			Color: "#27ae60",
			RichStringFields: []string{
				"inspector_name",     // Keep original
				"summary",            // Keep original
				"violations_found",   // NEW: "Improper food storage at 38°F"
				"corrective_actions", // NEW: "Installed new refrigeration thermometer"
			},
			// Relationships:
			// - health_inspection inspects restaurant
			// - health_inspection performed_by inspector
			// - health_inspection affects permit
		}

		body, _ = json.Marshal(updatePayload)
		req = httptest.NewRequest("POST", "/api/types", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w = httptest.NewRecorder()
		srv.HandleTypes(w, req)

		require.Equal(t, http.StatusCreated, w.Code) // Currently always returns 201

		// Verify transparency: violations are now searchable
		verifyTypeFields(t, srv, "health_inspection",
			[]string{"inspector_name", "summary", "violations_found", "corrective_actions"},
			nil)
	})

	t.Run("GET returns complete restaurant ecosystem types", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/types", nil)
		w := httptest.NewRecorder()
		srv.HandleTypes(w, req)

		assert.Equal(t, http.StatusOK, w.Code)

		var types []map[string]interface{}
		err = json.Unmarshal(w.Body.Bytes(), &types)
		require.NoError(t, err)

		// Should have complete restaurant domain model
		// Note: health_inspection appears twice (initial + update)
		require.Len(t, types, 7, "Should have restaurant, menu_item, city, food_review, health_inspection (2 versions), and prompt-result")

		// Map for easy verification
		typeMap := make(map[string]map[string]interface{})
		for _, t := range types {
			if name, ok := t["name"].(string); ok {
				typeMap[name] = t
			}
		}

		// Verify the ecosystem is complete
		assert.Contains(t, typeMap, "restaurant", "Core type: where people eat")
		assert.Contains(t, typeMap, "menu_item", "What restaurants serve")
		assert.Contains(t, typeMap, "city", "Where restaurants are located")
		assert.Contains(t, typeMap, "food_review", "Critics evaluate restaurants")
		assert.Contains(t, typeMap, "health_inspection", "Safety and compliance")

		// The graph would show relationships like:
		// San Francisco --[has_many]--> Chez Laurent
		// Chez Laurent --[serves]--> Coq au Vin
		// Ruth Reichl --[reviewed]--> Chez Laurent
		// Health Dept --[inspected]--> Chez Laurent
	})
}