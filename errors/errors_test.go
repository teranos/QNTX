package errors

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNew(t *testing.T) {
	err := New("test error")
	require.NotNil(t, err)
	assert.Equal(t, "test error", err.Error())
}

func TestNewf(t *testing.T) {
	err := Newf("error: %s %d", "test", 42)
	require.NotNil(t, err)
	assert.Equal(t, "error: test 42", err.Error())
}

func TestWrap(t *testing.T) {
	original := New("original")
	wrapped := Wrap(original, "wrapped")

	assert.Contains(t, wrapped.Error(), "wrapped")
	assert.Contains(t, wrapped.Error(), "original")
	assert.True(t, Is(wrapped, original))
}

func TestWrapf(t *testing.T) {
	original := New("original")
	wrapped := Wrapf(original, "wrapped: %d", 42)

	assert.Contains(t, wrapped.Error(), "wrapped: 42")
	assert.Contains(t, wrapped.Error(), "original")
}

func TestIs(t *testing.T) {
	err1 := New("error 1")
	err2 := New("error 2")
	wrapped := Wrap(err1, "wrapped")

	assert.True(t, Is(wrapped, err1))
	assert.False(t, Is(wrapped, err2))
	assert.False(t, Is(nil, err1))
}

type customError struct {
	msg string
}

func (e *customError) Error() string {
	return e.msg
}

func TestAs(t *testing.T) {
	original := &customError{msg: "custom"}
	wrapped := Wrap(original, "wrapped")

	var target *customError
	require.True(t, As(wrapped, &target))
	assert.Equal(t, "custom", target.msg)
}

func TestWithHint(t *testing.T) {
	err := New("error")
	withHint := WithHint(err, "try this fix")

	hints := GetAllHints(withHint)
	require.Len(t, hints, 1)
	assert.Equal(t, "try this fix", hints[0])
}

func TestWithDetail(t *testing.T) {
	err := New("error")
	withDetail := WithDetail(err, "detailed information")

	details := GetAllDetails(withDetail)
	require.Len(t, details, 1)
	assert.Equal(t, "detailed information", details[0])
}

func TestWithHintf(t *testing.T) {
	err := New("error")
	withHint := WithHintf(err, "try setting value to %d", 42)

	hints := GetAllHints(withHint)
	require.Len(t, hints, 1)
	assert.Equal(t, "try setting value to 42", hints[0])
}

func TestStackTrace(t *testing.T) {
	err := New("with stack")

	// Format with stack trace
	detailed := fmt.Sprintf("%+v", err)
	assert.Contains(t, detailed, "errors_test.go")
}

func TestUnwrap(t *testing.T) {
	original := New("original")
	wrapped := Wrap(original, "wrapped")

	unwrapped := Unwrap(wrapped)
	assert.NotNil(t, unwrapped)
}

func TestUnwrapAll(t *testing.T) {
	err1 := New("base")
	err2 := Wrap(err1, "middle")
	err3 := Wrap(err2, "top")

	all := UnwrapAll(err3)
	assert.NotEmpty(t, all)
}

func TestNilHandling(t *testing.T) {
	assert.Nil(t, Wrap(nil, "context"))
	assert.Nil(t, Wrapf(nil, "context %d", 1))
	assert.Nil(t, WithStack(nil))
	assert.Nil(t, WithHint(nil, "hint"))
	assert.Nil(t, WithDetail(nil, "detail"))
}

func TestErrorChaining(t *testing.T) {
	base := New("base error")

	err := Wrap(base, "layer 1")
	err = WithHint(err, "helpful hint")
	err = WithDetail(err, "detailed info")
	err = Wrap(err, "layer 2")

	// Should preserve all context
	assert.True(t, Is(err, base))
	assert.Contains(t, err.Error(), "layer 2")
	assert.Contains(t, err.Error(), "layer 1")
	assert.Contains(t, err.Error(), "base error")

	// Hints and details should be accessible
	hints := GetAllHints(err)
	assert.Contains(t, hints, "helpful hint")

	details := GetAllDetails(err)
	assert.Contains(t, details, "detailed info")
}

func ExampleNew() {
	err := New("something went wrong")
	fmt.Println(err)
	// Output: something went wrong
}

func ExampleWrap() {
	baseErr := New("connection failed")
	err := Wrap(baseErr, "failed to connect to database")
	fmt.Println(err)
	// Output: failed to connect to database: connection failed
}

func ExampleWithHint() {
	err := New("timeout")
	err = WithHint(err, "try increasing the timeout value")

	hints := GetAllHints(err)
	fmt.Println(hints[0])
	// Output: try increasing the timeout value
}
