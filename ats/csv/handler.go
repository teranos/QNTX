package csv

import (
	"context"
	"database/sql"
	"encoding/csv"
	"fmt"
	"os"

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
		if len(payload.Delimiter) > 0 {
			writer.Comma = rune(payload.Delimiter[0])
		}
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

// joinSlice joins a slice of strings with semicolon
func joinSlice(slice []string) string {
	if len(slice) == 0 {
		return ""
	}
	result := slice[0]
	for i := 1; i < len(slice); i++ {
		result += ";" + slice[i]
	}
	return result
}
