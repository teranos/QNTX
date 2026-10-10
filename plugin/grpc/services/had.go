package services

// Had is something the node hands a service, or why the node runs without it.
// The node boots without it; what needs it is refused with why.
type Had[T any] struct {
	it  T
	why error
}

// Having is the thing the node has.
func Having[T any](it T) Had[T] {
	return Had[T]{it: it}
}

// HavingNot is the node running without the thing, and why.
func HavingNot[T any](why error) Had[T] {
	return Had[T]{why: why}
}

// Get is the thing, or why the node runs without it.
func (h Had[T]) Get() (T, error) {
	return h.it, h.why
}
