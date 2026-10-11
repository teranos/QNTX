package named

import "github.com/teranos/errors"

// Items is a list that holds at least one thing.
type Items[T any] struct {
	items []T
}

// ItemsOf makes Items of items, or refuses them for holding none. what names
// the field, so the refusal says which one arrived empty.
func ItemsOf[T any](what string, items []T) (Items[T], error) {
	if len(items) == 0 {
		return Items[T]{}, errors.Newf("%s names nothing: at least one is needed", what)
	}
	return Items[T]{items: items}, nil
}

// All is every item, at least one.
func (i Items[T]) All() []T {
	return i.items
}

// First is the first item, which there always is.
func (i Items[T]) First() T {
	return i.items[0]
}

// ItemOf is Items of the one item: one is never none.
func ItemOf[T any](item T) Items[T] {
	return Items[T]{items: []T{item}}
}
