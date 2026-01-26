package prompt

import (
	"testing"
	"time"

	"github.com/teranos/QNTX/ats/types"
)

// Kitchen inventory attestation examples:
// [subject=ALICE] is inventory of fridge by smartfridge_001 at 2024-06-15
//   ATTRIBUTES{milk:240ml, paprika:1pc, butter:100g, eggs:6pc}
// [subject=ALICE] is inventory of cupboard by manual_entry at 2024-06-15
//   ATTRIBUTES{olive_oil:500ml, rigatoni:250g, canned_tomatoes:2pc, onion:3pc}

func TestParse(t *testing.T) {
	tests := []struct {
		name         string
		template     string
		wantErr      bool
		placeholders []string
	}{
		{
			name:         "literal recipe instruction",
			template:     "Suggest a recipe with the following ingredients",
			wantErr:      false,
			placeholders: nil,
		},
		{
			name:         "single subject - storage owner",
			template:     "{{subject}}'s available ingredients",
			wantErr:      false,
			placeholders: []string{"subject"},
		},
		{
			name:         "inventory context reference",
			template:     "Ingredients from {{context}}: {{attributes}}",
			wantErr:      false,
			placeholders: []string{"context", "attributes"},
		},
		{
			name:         "specific inventory item",
			template:     "Available milk: {{attributes.milk}}",
			wantErr:      false,
			placeholders: []string{"attributes.milk"},
		},
		{
			name:         "nested attribute for complex inventory",
			template:     "Dairy section: {{attributes.dairy.milk}}",
			wantErr:      false,
			placeholders: []string{"attributes.dairy.milk"},
		},
		{
			name:         "full recipe prompt template",
			template:     "For {{subject}}, using inventory from {{context}} recorded by {{actor}} at {{temporal}}, with items: {{attributes}}",
			wantErr:      false,
			placeholders: []string{"subject", "context", "actor", "temporal", "attributes"},
		},
		{
			name:     "empty template",
			template: "",
			wantErr:  true,
		},
		{
			name:     "invalid field - not a valid placeholder",
			template: "{{invalid_field}}",
			wantErr:  true,
		},
		{
			name:     "empty attribute path",
			template: "{{attributes.}}",
			wantErr:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpl, err := Parse(tt.template)
			if (err != nil) != tt.wantErr {
				t.Errorf("Parse() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if err != nil {
				return
			}

			got := tmpl.GetPlaceholders()
			if len(got) != len(tt.placeholders) {
				t.Errorf("GetPlaceholders() = %v, want %v", got, tt.placeholders)
			}
			for i, p := range got {
				if i < len(tt.placeholders) && p != tt.placeholders[i] {
					t.Errorf("placeholder[%d] = %v, want %v", i, p, tt.placeholders[i])
				}
			}
		})
	}
}

func TestExecute_FridgeInventory(t *testing.T) {
	// Simulates: ALICE is inventory of fridge by smartfridge_001 at 2024-06-15
	timestamp := time.Date(2024, 6, 15, 10, 30, 0, 0, time.UTC)

	fridgeInventory := &types.As{
		ID:         "INV-FRIDGE-001",
		Subjects:   []string{"ALICE"},
		Predicates: []string{"inventory"},
		Contexts:   []string{"fridge"},
		Actors:     []string{"smartfridge_001"},
		Timestamp:  timestamp,
		Source:     "smart_home",
		Attributes: map[string]interface{}{
			"milk":    "240ml",
			"paprika": "1pc",
			"butter":  "100g",
			"eggs":    "6pc",
			"cheese":  "200g",
		},
	}

	tests := []struct {
		name     string
		template string
		want     string
	}{
		{
			name:     "storage owner",
			template: "{{subject}}'s fridge contains:",
			want:     "ALICE's fridge contains:",
		},
		{
			name:     "storage location",
			template: "Checking {{context}} inventory",
			want:     "Checking fridge inventory",
		},
		{
			name:     "data source",
			template: "Reported by {{actor}}",
			want:     "Reported by smartfridge_001",
		},
		{
			name:     "specific item quantity",
			template: "Milk available: {{attributes.milk}}",
			want:     "Milk available: 240ml",
		},
		{
			name:     "multiple items",
			template: "Eggs: {{attributes.eggs}}, Butter: {{attributes.butter}}",
			want:     "Eggs: 6pc, Butter: 100g",
		},
		{
			name:     "attestation reference",
			template: "Inventory ID: {{id}} from {{source}}",
			want:     "Inventory ID: INV-FRIDGE-001 from smart_home",
		},
		{
			name:     "timestamp for freshness",
			template: "Last updated: {{temporal}}",
			want:     "Last updated: 2024-06-15T10:30:00Z",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpl, err := Parse(tt.template)
			if err != nil {
				t.Fatalf("Parse() error = %v", err)
			}

			got, err := tmpl.Execute(fridgeInventory)
			if err != nil {
				t.Fatalf("Execute() error = %v", err)
			}

			if got != tt.want {
				t.Errorf("Execute() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestExecute_CupboardInventory(t *testing.T) {
	// Simulates: ALICE is inventory of cupboard by manual_entry at 2024-06-15
	timestamp := time.Date(2024, 6, 15, 9, 0, 0, 0, time.UTC)

	cupboardInventory := &types.As{
		ID:         "INV-CUPBOARD-001",
		Subjects:   []string{"ALICE"},
		Predicates: []string{"inventory"},
		Contexts:   []string{"cupboard", "pantry"},
		Actors:     []string{"manual_entry"},
		Timestamp:  timestamp,
		Source:     "user_input",
		Attributes: map[string]interface{}{
			"olive_oil":       "500ml",
			"rigatoni":        "250g",
			"canned_tomatoes": "2pc",
			"onion":           "3pc",
			"garlic":          "1head",
			"dried_oregano":   "50g",
		},
	}

	tests := []struct {
		name     string
		template string
		want     string
	}{
		{
			name:     "multiple storage contexts",
			template: "Storage locations: {{context}}",
			want:     "Storage locations: cupboard, pantry",
		},
		{
			name:     "contexts as array for LLM",
			template: "Checking: {{contexts}}",
			want:     `Checking: ["cupboard","pantry"]`,
		},
		{
			name:     "pasta ingredients",
			template: "For pasta: {{attributes.rigatoni}} pasta, {{attributes.canned_tomatoes}} tomatoes",
			want:     "For pasta: 250g pasta, 2pc tomatoes",
		},
		{
			name:     "missing item returns empty",
			template: "Flour: {{attributes.flour}}",
			want:     "Flour: ",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpl, err := Parse(tt.template)
			if err != nil {
				t.Fatalf("Parse() error = %v", err)
			}

			got, err := tmpl.Execute(cupboardInventory)
			if err != nil {
				t.Fatalf("Execute() error = %v", err)
			}

			if got != tt.want {
				t.Errorf("Execute() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestExecute_RecipePromptConstruction(t *testing.T) {
	// This test demonstrates the full use case: constructing a recipe prompt
	// from inventory attestations
	timestamp := time.Date(2024, 6, 15, 10, 30, 0, 0, time.UTC)

	inventory := &types.As{
		ID:         "INV-COMBINED-001",
		Subjects:   []string{"ALICE"},
		Predicates: []string{"inventory"},
		Contexts:   []string{"fridge", "cupboard"},
		Actors:     []string{"smartfridge_001", "manual_entry"},
		Timestamp:  timestamp,
		Source:     "inventory_aggregator",
		Attributes: map[string]interface{}{
			"eggs":            "6pc",
			"butter":          "100g",
			"milk":            "240ml",
			"rigatoni":        "250g",
			"canned_tomatoes": "2pc",
			"onion":           "3pc",
			"garlic":          "1head",
			"cheese":          "200g",
		},
	}

	// Full recipe prompt template
	template := `Based on {{subject}}'s kitchen inventory from {{contexts}},
recorded at {{temporal}}, suggest a dinner recipe using these ingredients: {{attributes}}.
Consider that this data comes from {{actors}} sources.`

	tmpl, err := Parse(template)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	result, err := tmpl.Execute(inventory)
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	// Verify key parts are present
	if !contains(result, "ALICE") {
		t.Error("result should contain subject ALICE")
	}
	if !contains(result, `["fridge","cupboard"]`) {
		t.Error("result should contain contexts as JSON array")
	}
	if !contains(result, "2024-06-15T10:30:00Z") {
		t.Error("result should contain ISO timestamp")
	}
	if !contains(result, "eggs") {
		t.Error("result should contain inventory items")
	}
	if !contains(result, `["smartfridge_001","manual_entry"]`) {
		t.Error("result should contain actors as JSON array")
	}
}

func TestExecuteNilAttestation(t *testing.T) {
	tmpl, err := Parse("{{subject}}'s inventory")
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	_, err = tmpl.Execute(nil)
	if err == nil {
		t.Error("Execute(nil) should return error")
	}
}

func TestValidateTemplate(t *testing.T) {
	tests := []struct {
		name     string
		template string
		wantErr  bool
	}{
		{"valid inventory query", "Show {{subject}}'s {{context}} inventory", false},
		{"valid recipe prompt", "Using {{attributes}} from {{context}}, suggest recipes for {{subject}}", false},
		{"invalid field", "{{unknown_field}}", true},
		{"empty", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateTemplate(tt.template)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateTemplate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestRaw(t *testing.T) {
	raw := "Suggest a recipe for {{subject}} using {{attributes}}"
	tmpl, err := Parse(raw)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	if tmpl.Raw() != raw {
		t.Errorf("Raw() = %q, want %q", tmpl.Raw(), raw)
	}
}

// Helper function
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsImpl(s, substr))
}

func containsImpl(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
