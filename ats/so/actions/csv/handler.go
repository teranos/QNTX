package csv

import (
	"context"
	"database/sql"
	"encoding/csv"
	"fmt"
	"os"
	"strings"

	"github.com/teranos/QNTX/ats/storage"
	"github.com/teranos/QNTX/ats/types"
	"github.com/teranos/QNTX/errors"
)

// Execute generates a CSV file from attestations matching the filter
func Execute(ctx context.Context, db *sql.DB, payload Payload) error {
	// Execute the query
	executor := storage.NewExecutor(db)
	result, err := executor.ExecuteAsk(ctx, payload.AxFilter)
	if err != nil {
		return errors.Wrap(err, "failed to execute query")
	}

	if len(result.Attestations) == 0 {
		return errors.New("no attestations found matching query")
	}

	// Create output file
	file, err := os.Create(payload.Filename)
	if err != nil {
		return errors.Wrap(err, "failed to create CSV file")
	}
	defer file.Close()

	writer := csv.NewWriter(file)
	if payload.Delimiter != "" && payload.Delimiter != "," {
		// Only set delimiter if it's different from default
		if len(payload.Delimiter) > 1 {
			return errors.New("csv delimiter must be a single character")
		}
		writer.Comma = rune(payload.Delimiter[0])
	}
	defer writer.Flush()

	// Determine headers
	headers := payload.Headers
	if len(headers) == 0 {
		// Default headers: all fields
		headers = []string{"id", "subjects", "predicates", "contexts", "actors", "timestamp", "source"}
	}

	// Write headers
	if err := writer.Write(headers); err != nil {
		return errors.Wrap(err, "failed to write headers")
	}

	// Write attestations
	for _, attest := range result.Attestations {
		row := make([]string, len(headers))
		for i, header := range headers {
			row[i] = getFieldValue(&attest, header)
		}
		if err := writer.Write(row); err != nil {
			return errors.Wrap(err, "failed to write row")
		}
	}

	return nil
}

// getFieldValue extracts a field value from an attestation
func getFieldValue(attest *types.As, field string) string {
	switch field {
	case "id":
		return attest.ID
	case "subjects", "subject":
		return joinSlice(attest.Subjects)
	case "predicates", "predicate":
		return joinSlice(attest.Predicates)
	case "contexts", "context":
		return joinSlice(attest.Contexts)
	case "actors", "actor":
		return joinSlice(attest.Actors)
	case "timestamp":
		return attest.Timestamp.Format("2006-01-02T15:04:05Z07:00")
	case "source":
		return attest.Source
	default:
		// Try attributes
		if attest.Attributes != nil {
			if val, ok := attest.Attributes[field]; ok {
				return fmt.Sprintf("%v", val)
			}
		}
		return ""
	}
}

// joinSlice joins a slice of strings with semicolon.
// Per RFC 4180, the encoding/csv writer will automatically quote fields
// that contain the delimiter character, so this is safe even when delimiter is ";".
// Example: ["ALICE", "BOB"] with delimiter ";" becomes quoted: "ALICE;BOB"
//
// Individual array elements are escaped per CSV spec (quotes doubled) before joining
// to prevent ambiguity. Example: ["ALICE \"Crypto\"", "BOB"] becomes "ALICE ""Crypto"";BOB"
func joinSlice(slice []string) string {
	if len(slice) == 0 {
		return ""
	}
	// Escape quotes in individual elements per CSV spec
	result := escapeCSVElement(slice[0])
	for i := 1; i < len(slice); i++ {
		result += ";" + escapeCSVElement(slice[i])
	}
	return result
}

// escapeCSVElement escapes special characters in a single array element.
// Per RFC 4180, quotes are escaped by doubling them.
func escapeCSVElement(s string) string {
	// Replace " with "" per CSV spec
	return strings.ReplaceAll(s, `"`, `""`)
}
