//go:build !qntxwasm

package storage

import (
	"context"

	"github.com/teranos/QNTX/errors"
)

func (bs *BoundedStore) searchFuzzyWithEngine(_ context.Context, query string, _ int) ([]RichSearchMatch, error) {
	return nil, errors.Newf("WASM fuzzy engine unavailable (build without qntxwasm tag), query=%q", query)
}
