package named

import errors "github.com/teranos/sacred-error"

// Pointed is the value a pointer an SDK hands points at, or a refusal naming
// what pointed at nothing. A nil pointer is not the value's zero: it is
// refused, here, once, so code that holds the value never asks.
func Pointed[T any](what string, p *T) (T, error) {
	if p == nil {
		var none T
		return none, errors.Newf("%s points at nothing", what)
	}
	return *p, nil
}
