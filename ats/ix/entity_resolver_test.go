package ix

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/teranos/vanity-id"
)

// testEntity is a minimal entity type for entity resolution testing
// This demonstrates how ATS works with any entity data structure
type testEntity struct {
	ID             string
	Name           string
	Identifiers    []string // Unique identifiers, contact addresses, handles, etc.
	ProfileURL     string
	Affiliation    string
	AlternativeIDs []id.AlternativeID
}

// BasicEntityResolver demonstrates the concept of unified entity resolution
// This is a minimal implementation to validate the approach before building the full infrastructure
type BasicEntityResolver struct {
	entities     map[string]*testEntity // Simulated entity storage
	attestations map[string][]string    // Simulated attestation system
}

// NewBasicEntityResolver creates a new basic entity resolver for testing
func NewBasicEntityResolver() *BasicEntityResolver {
	return &BasicEntityResolver{
		entities:     make(map[string]*testEntity),
		attestations: make(map[string][]string),
	}
}

// ResolveOrCreateEntity finds or creates a unified entity across both domains
func (r *BasicEntityResolver) ResolveOrCreateEntity(entity *testEntity, attestations []string, source string) (string, error) {
	// 1. Check if this entity already exists by looking for common identifiers
	existingEntityID := r.findExistingEntityByIdentifiers(entity)

	if existingEntityID != "" {
		// Entity exists - merge data and return existing ID
		existingEntity := r.entities[existingEntityID]

		// Add new source as alternative ID
		existingEntity.AlternativeIDs = id.AddAlternativeID(
			existingEntity.AlternativeIDs,
			existingEntity.ID,
			entity.ID,
			source,
			"ix-"+source,
		)

		// Merge entity data (simplified - in real implementation this would be more sophisticated)
		if entity.ProfileURL != "" && existingEntity.ProfileURL == "" {
			existingEntity.ProfileURL = entity.ProfileURL
		}
		if entity.Affiliation != "" && existingEntity.Affiliation == "" {
			existingEntity.Affiliation = entity.Affiliation
		}

		// Store attestations under unified ID
		r.attestations[existingEntityID] = append(r.attestations[existingEntityID], attestations...)

		return existingEntityID, nil
	}

	// 2. New entity - create in both domains
	entity.AlternativeIDs = id.AddAlternativeID(
		entity.AlternativeIDs,
		entity.ID,
		entity.ID+"-"+source,
		source,
		"ix-"+source,
	)

	r.entities[entity.ID] = entity
	r.attestations[entity.ID] = attestations

	return entity.ID, nil
}

// findExistingEntityByIdentifiers looks for existing entities by common identifiers
func (r *BasicEntityResolver) findExistingEntityByIdentifiers(newEntity *testEntity) string {
	for entityID, existingEntity := range r.entities {
		// Check identifier overlap (addresses, handles, unique IDs, etc.)
		if r.hasCommonIdentifiers(newEntity, existingEntity) {
			return entityID
		}

		// Check profile URL match
		if newEntity.ProfileURL != "" && newEntity.ProfileURL == existingEntity.ProfileURL {
			return entityID
		}

		// Check if newEntity ID is in existing alternative IDs
		for _, altID := range existingEntity.AlternativeIDs {
			if altID.ID == newEntity.ID {
				return entityID
			}
		}
	}

	return ""
}

// hasCommonIdentifiers checks if two entities share any identifiers
func (r *BasicEntityResolver) hasCommonIdentifiers(entity1, entity2 *testEntity) bool {
	if len(entity1.Identifiers) == 0 || len(entity2.Identifiers) == 0 {
		return false
	}

	identifier1Set := make(map[string]bool)
	for _, identifier := range entity1.Identifiers {
		identifier1Set[identifier] = true
	}

	for _, identifier := range entity2.Identifiers {
		if identifier1Set[identifier] {
			return true
		}
	}

	return false
}

// GetUnifiedEntity returns the complete unified entity data
func (r *BasicEntityResolver) GetUnifiedEntity(entityID string) (*testEntity, []string, bool) {
	entity, exists := r.entities[entityID]
	if !exists {
		return nil, nil, false
	}

	attestations := r.attestations[entityID]
	return entity, attestations, true
}

func TestUnifiedEntityResolution_Matrix(t *testing.T) {
	resolver := NewBasicEntityResolver()

	t.Run("Bruce Wayne Identity - Public Records to Crime Database Unified Deduplication", func(t *testing.T) {
		// Step 1: Bruce Wayne from public records
		brucePublic := &testEntity{
			ID:          "BRUC1",
			Name:        "Bruce Wayne",
			Identifiers: []string{"bruce@wayneenterprises.com", "+1-555-WAYNE"},
			Affiliation: "Wayne Enterprises",
		}

		publicAttestations := []string{
			"BRUC1 is ceo_of Wayne-Enterprises",
			"BRUC1 resides_in Wayne-Manor",
			"BRUC1 has_identifier bruce@wayneenterprises.com",
		}

		// Process public records data
		entityID1, err := resolver.ResolveOrCreateEntity(brucePublic, publicAttestations, "public-records")
		require.NoError(t, err)
		assert.Equal(t, "BRUC1", entityID1)

		// Verify public entity created
		entity1, attestations1, exists1 := resolver.GetUnifiedEntity(entityID1)
		require.True(t, exists1)
		assert.Equal(t, "Bruce Wayne", entity1.Name)
		assert.Equal(t, "Wayne Enterprises", entity1.Affiliation)
		assert.Len(t, attestations1, 3)
		assert.Contains(t, attestations1, "BRUC1 is ceo_of Wayne-Enterprises")

		// Step 2: Batman from crime database (same person!)
		batmanCrime := &testEntity{
			ID:          "BATM2",
			Name:        "Batman",
			Identifiers: []string{"bruce@wayneenterprises.com"}, // Same identifier - key for deduplication!
			Affiliation: "Justice League",
			ProfileURL:  "https://gcpd.gov/vigilantes/batman",
		}

		crimeAttestations := []string{
			"BATM2 operates_in Gotham-City",
			"BATM2 has_profile https://gcpd.gov/vigilantes/batman",
			"BATM2 affiliated_with Justice-League since 2015-03-20",
		}

		// Process crime database data - should resolve to same entity!
		entityID2, err := resolver.ResolveOrCreateEntity(batmanCrime, crimeAttestations, "crime-database")
		require.NoError(t, err)

		// KEY ASSERTION: Both identities resolve to the same entity ID
		assert.Equal(t, entityID1, entityID2, "Bruce Wayne and Batman should resolve to the same entity")

		// Step 3: Verify unified entity has data from both sources
		unifiedEntity, allAttestations, exists := resolver.GetUnifiedEntity(entityID1)
		require.True(t, exists)

		// Entity data should be merged
		assert.Equal(t, "Bruce Wayne", unifiedEntity.Name) // Kept original name
		assert.Contains(t, unifiedEntity.Identifiers, "bruce@wayneenterprises.com")
		assert.Equal(t, "https://gcpd.gov/vigilantes/batman", unifiedEntity.ProfileURL) // Added from crime database

		// Alternative IDs should track both sources
		require.Len(t, unifiedEntity.AlternativeIDs, 2)

		// First alternative ID should be from public records (added when entity was created)
		publicAltID := unifiedEntity.AlternativeIDs[0]
		assert.Equal(t, "BRUC1-public-records", publicAltID.ID)
		assert.Equal(t, "public-records", publicAltID.Source)
		assert.Equal(t, "ix-public-records", publicAltID.Attestor)

		// Second alternative ID should be from crime database (added when merged)
		crimeAltID := unifiedEntity.AlternativeIDs[1]
		assert.Equal(t, "BATM2", crimeAltID.ID)
		assert.Equal(t, "crime-database", crimeAltID.Source)
		assert.Equal(t, "ix-crime-database", crimeAltID.Attestor)

		// Attestations should include data from both sources
		assert.Len(t, allAttestations, 6)                                                            // 3 from public + 3 from crime
		assert.Contains(t, allAttestations, "BRUC1 resides_in Wayne-Manor")                          // From public records
		assert.Contains(t, allAttestations, "BATM2 operates_in Gotham-City")                         // From crime database
		assert.Contains(t, allAttestations, "BATM2 affiliated_with Justice-League since 2015-03-20") // Crime database relationship

		t.Logf("✓ Unified Bruce Wayne/Batman Entity:")
		t.Logf("  Primary ID: %s", unifiedEntity.ID)
		t.Logf("  Alternative IDs: %v", unifiedEntity.AlternativeIDs)
		t.Logf("  Total Attestations: %d", len(allAttestations))
		t.Logf("  Entity Data: Public=%s + Crime=%s", "Wayne Enterprises", unifiedEntity.ProfileURL)
	})

	t.Run("Diana Prince - Separate Entity (No Identifier Overlap)", func(t *testing.T) {
		// Diana Prince from news reports - different person, no common identifiers
		diana := &testEntity{
			ID:          "DIAN3",
			Name:        "Diana Prince",
			Identifiers: []string{"diana@smithsonian.museum"},
			Affiliation: "Smithsonian Institution",
		}

		dianaAttestations := []string{
			"DIAN3 is curator_at Smithsonian",
			"DIAN3 works_with artifacts",
		}

		// Process Diana - should create separate entity
		entityID3, err := resolver.ResolveOrCreateEntity(diana, dianaAttestations, "news-reports")
		require.NoError(t, err)
		assert.Equal(t, "DIAN3", entityID3)

		// Verify Diana is separate from Bruce/Batman
		assert.NotEqual(t, "BRUC1", entityID3, "Diana should be a separate entity from Bruce/Batman")

		// Verify Diana's data
		dianaEntity, dianaAtts, exists := resolver.GetUnifiedEntity(entityID3)
		require.True(t, exists)
		assert.Equal(t, "Diana Prince", dianaEntity.Name)
		assert.Len(t, dianaAtts, 2)
		assert.Contains(t, dianaAtts, "DIAN3 is curator_at Smithsonian")
	})

	t.Run("Cross-Entity Attestation Queries", func(t *testing.T) {
		// Simulate querying attestations across all entities
		allAttestations := make(map[string][]string)
		for entityID, attestations := range resolver.attestations {
			allAttestations[entityID] = attestations
		}

		// Should find attestations for 2 distinct entities
		assert.Len(t, allAttestations, 2, "Should have exactly 2 distinct entities")

		// Bruce/Batman entity should have 6 attestations (public + crime)
		bruceAttestations := allAttestations["BRUC1"]
		assert.Len(t, bruceAttestations, 6)

		// Diana's entity should have 2 attestations
		dianaAttestations := allAttestations["DIAN3"]
		assert.Len(t, dianaAttestations, 2)

		t.Logf("✓ Cross-Entity Query Results:")
		t.Logf("  Bruce Wayne/Batman (BRUC1): %d attestations from public + crime", len(bruceAttestations))
		t.Logf("  Diana Prince (DIAN3): %d attestations from news reports", len(dianaAttestations))
	})
}

func TestAlternativeIDIntegration(t *testing.T) {
	t.Run("Alternative ID Bridge Between Domains", func(t *testing.T) {
		// Test the Alternative ID system as a bridge between entity storage and attestation system
		entity := &testEntity{
			ID:          "PETE4",
			Name:        "Peter Parker",
			Identifiers: []string{"peter@dailybugle.com"},
		}

		// Add alternative IDs from different sources
		entity.AlternativeIDs = id.AddAlternativeID(
			entity.AlternativeIDs,
			entity.ID,
			"peter-parker-student",
			"school-records",
			"ix-school-records",
		)

		entity.AlternativeIDs = id.AddAlternativeID(
			entity.AlternativeIDs,
			entity.ID,
			"spider-man-vigilante",
			"crime-database",
			"ix-crime-database",
		)

		// Verify alternative IDs are properly structured
		require.Len(t, entity.AlternativeIDs, 2)

		schoolAltID := entity.AlternativeIDs[0]
		assert.Equal(t, "peter-parker-student", schoolAltID.ID)
		assert.Equal(t, "school-records", schoolAltID.Source)
		assert.Equal(t, "ix-school-records", schoolAltID.Attestor)
		assert.WithinDuration(t, time.Now(), schoolAltID.AddedAt, time.Second)

		crimeAltID := entity.AlternativeIDs[1]
		assert.Equal(t, "spider-man-vigilante", crimeAltID.ID)
		assert.Equal(t, "crime-database", crimeAltID.Source)
		assert.Equal(t, "ix-crime-database", crimeAltID.Attestor)

		t.Logf("✓ Alternative ID Bridge Established:")
		t.Logf("  Primary ID: %s", entity.ID)
		t.Logf("  School Alternative: %s", schoolAltID.ID)
		t.Logf("  Crime Database Alternative: %s", crimeAltID.ID)
		t.Logf("  Bridge enables cross-domain entity resolution")
	})
}
