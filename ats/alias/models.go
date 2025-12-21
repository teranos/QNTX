package alias

import (
	"time"
)

// Alias represents a simple alias mapping
type Alias struct {
	Alias     string    `json:"alias"`      // The alias identifier
	Target    string    `json:"target"`     // What it maps to
	CreatedBy string    `json:"created_by"` // Who created it
	CreatedAt time.Time `json:"created_at"` // When it was created
}
