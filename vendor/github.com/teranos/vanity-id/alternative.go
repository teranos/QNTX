package id

import (
	"time"
)

// AlternativeID represents an alternative ID with timestamp and attestation tracking
type AlternativeID struct {
	ID       string    `json:"id"`       // The alternative ID (CardDAV UID, vanity ID, etc.)
	AddedAt  time.Time `json:"added_at"` // When this ID was added to the entity
	Source   string    `json:"source"`   // Source of the ID (merge, import, carddav-sync, etc.)
	Attestor string    `json:"attestor"` // Who/what is claiming this ID belongs to the entity
}

// AddAlternativeID adds an ID to the alternative IDs list if it doesn't already exist
func AddAlternativeID(existingIDs []AlternativeID, primaryID, id, source, attestor string) []AlternativeID {
	// Don't add the primary ID as an alternative
	if id == primaryID {
		return existingIDs
	}

	// Check if ID already exists in alternatives
	for _, existingAltID := range existingIDs {
		if existingAltID.ID == id {
			return existingIDs // Already exists
		}
	}

	// Add the ID with timestamp and attestation
	altID := AlternativeID{
		ID:       id,
		AddedAt:  time.Now(),
		Source:   source,
		Attestor: attestor,
	}
	return append(existingIDs, altID)
}

// HasAlternativeID checks if the given ID exists in the alternative IDs list
func HasAlternativeID(existingIDs []AlternativeID, id string) bool {
	for _, existingAltID := range existingIDs {
		if existingAltID.ID == id {
			return true
		}
	}
	return false
}

// GetAllIDs returns all IDs for an entity (primary ID + alternative IDs)
func GetAllIDs(primaryID string, alternativeIDs []AlternativeID) []string {
	allIDs := []string{primaryID}
	for _, altID := range alternativeIDs {
		allIDs = append(allIDs, altID.ID)
	}
	return allIDs
}
