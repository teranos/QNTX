package sacred

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/teranos/errors"
)

// What the Rust side writes decodes into the same fields, and the value is
// the error: nothing on the way turned it into a sentence.
func TestTheShapeCrossesWhole(t *testing.T) {
	wire := `{"id":"err-ats-duckdb-1","severity":"error","context":{"surface":"ats-duckdb","region":"write"},` +
		`"title":"failed to write an access token object s3://bucket/x.json","why":"HTTP Error: Bad Request (HTTP code 400)",` +
		`"raw":"Write { .. }","at":"1","source":"duckdb","ffi_call":"duckdb_tokens_put","location":"s3://bucket/x.json"}`

	err := Decode(wire)
	var sacred *Error
	require.True(t, errors.As(err, &sacred), "decoded into %T", err)
	assert.Equal(t, "ats-duckdb", sacred.Context.Surface)
	assert.Equal(t, "write", sacred.Context.Region)
	assert.Equal(t, "duckdb", sacred.Source)
	assert.Equal(t, "duckdb_tokens_put", sacred.FFICall)
	assert.Equal(t, "HTTP Error: Bad Request (HTTP code 400)", sacred.Why)
	assert.Equal(t, "failed to write an access token object s3://bucket/x.json (duckdb said: HTTP Error: Bad Request (HTTP code 400)) via duckdb_tokens_put", err.Error())
}

// A severity outside the closed set is a contract violation, not Info. The
// text is kept whole as Unshaped, with why it did not decode.
func TestAnUnknownSeverityFailsToDecode(t *testing.T) {
	wire := `{"id":"x","severity":"mild","context":{"surface":"s"},"title":"t","why":"","at":"1"}`

	err := Decode(wire)
	var unshaped *Unshaped
	require.True(t, errors.As(err, &unshaped), "decoded into %T", err)
	assert.Equal(t, wire, unshaped.Raw)
	assert.Contains(t, unshaped.Decode.Error(), "mild")
}

// A panic message or a static string is not the shape. It arrives whole.
func TestTextThatIsNotTheShapeIsKeptWhole(t *testing.T) {
	err := Decode("panic reached the duckdb_storage_put FFI boundary: index out of range")
	var unshaped *Unshaped
	require.True(t, errors.As(err, &unshaped))
	assert.Equal(t, "panic reached the duckdb_storage_put FFI boundary: index out of range", err.Error())
}

// Wrapping for context keeps the value reachable underneath.
func TestContextWrapsAndTheValueStays(t *testing.T) {
	wire := `{"id":"x","severity":"error","context":{"surface":"ats-duckdb","region":"read"},"title":"failed to read the Users under p","why":"","at":"1"}`
	err := errors.Wrapf(Decode(wire), "failed to resolve the User reached by %q", "google:1")

	var sacred *Error
	require.True(t, errors.As(err, &sacred))
	assert.Equal(t, "read", sacred.Context.Region)
	assert.Contains(t, err.Error(), "google:1")
	assert.Contains(t, err.Error(), "failed to read the Users under p")
}
