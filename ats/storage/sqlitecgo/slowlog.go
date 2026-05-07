//go:build cgo

package sqlitecgo

import (
	"log"
	"time"
)

// slowQueryThreshold is the duration above which FFI calls are logged.
const slowQueryThreshold = 100 * time.Millisecond

// logSlow logs an FFI operation if it exceeded the slow query threshold.
// Call as: defer logSlow(time.Now(), "operation_name")
func logSlow(start time.Time, op string) {
	elapsed := time.Since(start)
	if elapsed >= slowQueryThreshold {
		log.Printf("[slow-query] %s took %s", op, elapsed)
	}
}
