package util

// Ptr returns a pointer to the given value.
// This is a generic helper for creating pointers to literals.
func Ptr[T any](v T) *T {
	return &v
}
