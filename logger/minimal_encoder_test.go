package logger

import (
	"regexp"
	"strings"
	"testing"
	"time"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

// stripANSI removes ANSI color codes from a string for testing
func stripANSI(str string) string {
	ansiRegex := regexp.MustCompile(`\x1b\[[0-9;]*m`)
	return ansiRegex.ReplaceAllString(str, "")
}

// TestMinimalEncoderNeverDiscarsFields is a CRITICAL test that ensures
// the minimal encoder NEVER silently discards log fields.
// This test MUST pass to prevent loss of debugging information.
func TestMinimalEncoderNeverDiscardsFields(t *testing.T) {
	// Create a minimal encoder
	encoder := newMinimalEncoder()

	// Create an entry with MANY different field types and names
	// to ensure nothing gets silently dropped
	entry := zapcore.Entry{
		Level:      zapcore.InfoLevel,
		Time:       time.Now(),
		LoggerName: "test",
		Message:    "Testing field preservation",
	}

	// Test fields that MUST appear in output
	testFields := []struct {
		field    zapcore.Field
		mustFind string // What we must find in the output
	}{
		// Critical business fields that were being silently dropped
		{zap.String("type", "UserType"), "type=UserType"},
		{zap.String("label", "User Label"), "label=User Label"},
		{zap.String("color", "#FF0000"), "color=#FF0000"},
		{zap.Bool("deprecated", true), "deprecated=true"},
		{zap.Float64("opacity", 0.8), "opacity=0.8"},
		{zap.Strings("rich_string_fields", []string{"field1", "field2"}), "rich_string_fields"},
		{zap.Strings("array_fields", []string{"arr1", "arr2"}), "array_fields"},

		// Random field names that should NEVER be dropped
		{zap.String("random_field_xyz", "important_data"), "random_field_xyz=important_data"},
		{zap.Int("critical_count", 999), "critical_count=999"},
		{zap.String("user_action", "delete_database"), "user_action=delete_database"},
		{zap.String("error_details", "null pointer exception"), "error_details=null pointer exception"},

		// Fields with underscores, hyphens, dots (edge cases)
		{zap.String("field_with_underscores", "test"), "field_with_underscores=test"},
		{zap.String("field.with.dots", "test2"), "field.with.dots=test2"},

		// Numeric fields
		{zap.Int32("int32_field", 42), "int32_field=42"},
		{zap.Int64("int64_field", 9999999), "int64_field=9999999"},
		{zap.Float32("float32_field", 3.14), "float32_field=3.14"},

		// Boolean fields
		{zap.Bool("success", false), "success=false"},

		// Error fields (critical for debugging!)
		{zap.Error(nil), ""}, // nil error shouldn't crash
		{zap.String("error", "something went wrong"), "error=something went wrong"},

		// Fields that were previously special-cased (should still work)
		{zap.String("query_id", "q123"), "q123"}, // Special formatting
		{zap.Int("nodes", 10), "10"},             // Graph stats
		{zap.Int("links", 5), "5"},               // Graph stats
	}

	// Encode all fields at once
	var allFields []zapcore.Field
	for _, tf := range testFields {
		allFields = append(allFields, tf.field)
	}

	buf, err := encoder.EncodeEntry(entry, allFields)
	if err != nil {
		t.Fatalf("Failed to encode entry: %v", err)
	}

	output := buf.String()
	// Strip ANSI color codes for testing
	cleanOutput := stripANSI(output)

	// CRITICAL: Check that EVERY field appears in the output
	missingFields := []string{}
	for _, tf := range testFields {
		if tf.mustFind != "" && !strings.Contains(cleanOutput, tf.mustFind) {
			missingFields = append(missingFields, tf.mustFind)
			t.Errorf("CRITICAL: Field was silently discarded from log output: %s", tf.mustFind)
		}
	}

	if len(missingFields) > 0 {
		t.Fatalf("CRITICAL BUG: Logger is silently discarding %d fields! Missing: %v\nClean output was: %s\nRaw output was: %s",
			len(missingFields), missingFields, cleanOutput, output)
	}
}

// TestMinimalEncoderFieldCount ensures that the NUMBER of fields in equals
// the number of fields that appear in the output (minus special formatting)
func TestMinimalEncoderFieldCount(t *testing.T) {
	encoder := newMinimalEncoder()

	entry := zapcore.Entry{
		Level:      zapcore.InfoLevel,
		Time:       time.Now(),
		LoggerName: "test",
		Message:    "Field count test",
	}

	// Add exactly 10 unique fields
	fields := []zapcore.Field{
		zap.String("field1", "value1"),
		zap.String("field2", "value2"),
		zap.String("field3", "value3"),
		zap.String("field4", "value4"),
		zap.String("field5", "value5"),
		zap.Int("field6", 6),
		zap.Int("field7", 7),
		zap.Bool("field8", true),
		zap.Float64("field9", 9.9),
		zap.String("field10", "value10"),
	}

	buf, err := encoder.EncodeEntry(entry, fields)
	if err != nil {
		t.Fatalf("Failed to encode: %v", err)
	}

	output := buf.String()

	// Count how many field assignments appear (looking for = sign)
	// Each field should produce a "key=value" pattern
	fieldCount := strings.Count(output, "field1=") +
		strings.Count(output, "field2=") +
		strings.Count(output, "field3=") +
		strings.Count(output, "field4=") +
		strings.Count(output, "field5=") +
		strings.Count(output, "field6=") +
		strings.Count(output, "field7=") +
		strings.Count(output, "field8=") +
		strings.Count(output, "field9=") +
		strings.Count(output, "field10=")

	if fieldCount != 10 {
		t.Errorf("Expected 10 fields in output, but found %d. Output: %s", fieldCount, output)
	}
}

// TestTypeAttestationLogging specifically tests the exact scenario that was failing:
// Type attestation logs with rich_string_fields and array_fields
func TestTypeAttestationLogging(t *testing.T) {
	encoder := newMinimalEncoder()

	entry := zapcore.Entry{
		Level:      zapcore.InfoLevel,
		Time:       time.Now(),
		LoggerName: "server",
		Message:    "Type attestation created",
	}

	// These are the EXACT fields that were being silently dropped
	fields := []zapcore.Field{
		zap.String("type", "UserProfile"),
		zap.String("label", "User Profile"),
		zap.String("color", "#888888"),
		zap.Strings("rich_string_fields", []string{"bio", "description"}),
		zap.Strings("array_fields", []string{"skills", "interests"}),
		zap.Bool("deprecated", false),
		zap.String("client", "127.0.0.1:63318"),
	}

	buf, err := encoder.EncodeEntry(entry, fields)
	if err != nil {
		t.Fatalf("Failed to encode type attestation log: %v", err)
	}

	output := buf.String()
	cleanOutput := stripANSI(output)

	// CRITICAL: Verify each field is in the output
	requiredFields := []string{
		"type=UserProfile",
		"label=User Profile",
		"color=#888888",
		"rich_string_fields=[bio description]",
		"array_fields=[skills interests]",
		"deprecated=false",
		"client=127.0.0.1:63318",
	}

	for _, required := range requiredFields {
		if !strings.Contains(cleanOutput, required) {
			t.Errorf("Type attestation field missing from log: %s\nFull output: %s", required, cleanOutput)
		}
	}
}

// TestUnknownFieldTypes tests that the encoder handles all possible field types
// without crashing or silently dropping them
func TestUnknownFieldTypes(t *testing.T) {
	encoder := newMinimalEncoder()

	entry := zapcore.Entry{
		Level:      zapcore.InfoLevel,
		Time:       time.Now(),
		LoggerName: "test",
		Message:    "Testing unknown field types",
	}

	// Test various field types including complex ones
	fields := []zapcore.Field{
		zap.Complex128("complex", complex(1.0, 2.0)),
		zap.Complex64("complex64", complex64(complex(3.0, 4.0))),
		zap.Duration("duration", 5*time.Second),
		zap.Time("timestamp", time.Now()),
		zap.Uint("uint", 100),
		zap.Uint8("uint8", 200),
		zap.Uint16("uint16", 30000),
		zap.Uint32("uint32", 4000000),
		zap.Uint64("uint64", 5000000000),
		zap.Uintptr("uintptr", 0xDEADBEEF),
		zap.ByteString("bytes", []byte("hello world")),
		zap.Binary("binary", []byte{0x01, 0x02, 0x03}),
	}

	buf, err := encoder.EncodeEntry(entry, fields)
	if err != nil {
		t.Fatalf("Failed to encode complex types: %v", err)
	}

	output := buf.String()
	cleanOutput := stripANSI(output)

	// Verify that SOME representation of each field appears
	// We don't care about exact formatting, just that it's not silently dropped
	expectedSubstrings := []string{
		"complex",
		"complex64",
		"duration",
		"timestamp",
		"uint",
		"bytes",
		"binary",
	}

	for _, expected := range expectedSubstrings {
		if !strings.Contains(cleanOutput, expected) {
			t.Errorf("Field with key '%s' was completely dropped from output: %s", expected, cleanOutput)
		}
	}
}