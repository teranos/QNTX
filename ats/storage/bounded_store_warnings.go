package storage

import (
	"time"

	"github.com/teranos/QNTX/ats/types"
)

// StorageWarning represents a bounded storage warning condition
type StorageWarning struct {
	Actor              string        // Actor approaching limit
	Context            string        // Context approaching limit
	Current            int           // Current attestation count
	Limit              int           // Configured limit
	FillPercent        float64       // Percentage full (0.0-1.0)
	RatePerHour        float64       // Current creation rate (attestations/hour)
	AccelerationFactor float64       // How much faster than normal (1.0 = normal, 2.0 = 2x faster)
	TimeUntilFull      time.Duration // Projected time until hitting limit
}

// CheckStorageStatus checks bounded storage status for an attestation
// Returns warnings for any (actor, context) pairs approaching limits (50-90% full)
func (bs *BoundedStore) CheckStorageStatus(as *types.As) []*StorageWarning {
	// Skip self-certifying attestations (bypass 64-actor limit)
	if len(as.Actors) > 0 && as.Actors[0] == as.ID {
		return nil
	}

	var warnings []*StorageWarning

	// Check each (actor, context) pair
	for _, actor := range as.Actors {
		for _, context := range as.Contexts {
			if warning := bs.checkActorContext(actor, context); warning != nil {
				warnings = append(warnings, warning)
			}
		}
	}

	return warnings
}

// checkActorContext checks a specific (actor, context) pair for approaching limits
func (bs *BoundedStore) checkActorContext(actor, context string) *StorageWarning {
	limit := bs.config.ActorContextLimit

	// Count current attestations
	var count int
	err := bs.db.QueryRow(`
		SELECT COUNT(*)
		FROM attestations,
		json_each(actors) as a,
		json_each(contexts) as c
		WHERE a.value = ? AND c.value = ?
	`, actor, context).Scan(&count)

	if err != nil {
		return nil // Skip on error
	}

	fillPercent := float64(count) / float64(limit)

	// Only warn if 50-90% full
	if fillPercent < 0.5 || fillPercent >= 1.0 {
		return nil
	}

	// Calculate creation rates
	var lastHour, lastDay, lastWeek int
	bs.db.QueryRow(`
		SELECT
			SUM(CASE WHEN timestamp > datetime('now', '-1 hour') THEN 1 ELSE 0 END),
			SUM(CASE WHEN timestamp > datetime('now', '-1 day') THEN 1 ELSE 0 END),
			SUM(CASE WHEN timestamp > datetime('now', '-7 days') THEN 1 ELSE 0 END)
		FROM attestations,
		json_each(actors) as a,
		json_each(contexts) as c
		WHERE a.value = ? AND c.value = ?
	`, actor, context).Scan(&lastHour, &lastDay, &lastWeek)

	// Use day rate for projection (most stable)
	ratePerHour := float64(lastDay) / 24.0
	if ratePerHour < 0.01 {
		return nil // Too slow to matter
	}

	// Calculate time until full
	remaining := limit - count
	hoursUntilFull := float64(remaining) / ratePerHour
	timeUntilFull := time.Duration(hoursUntilFull * float64(time.Hour))

	// Calculate acceleration (compare day to week)
	normalRatePerHour := float64(lastWeek) / (24.0 * 7.0)
	accelerationFactor := 1.0
	if normalRatePerHour > 0.01 {
		accelerationFactor = ratePerHour / normalRatePerHour
	}

	return &StorageWarning{
		Actor:              actor,
		Context:            context,
		Current:            count,
		Limit:              limit,
		FillPercent:        fillPercent,
		RatePerHour:        ratePerHour,
		AccelerationFactor: accelerationFactor,
		TimeUntilFull:      timeUntilFull,
	}
}
