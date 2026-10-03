//go:build cgo && rustduckdb && !atsless

package server

import (
	"github.com/teranos/QNTX/ats/storage/duckdbcgo"
)

// asksItsLocation is a store keeping a tally of what it has asked its location
// for. Every parquet-backed store holding an Objects answers this.
type asksItsLocation interface {
	Requests() ([]duckdbcgo.Asked, error)
}

// recordSpendOf reports what a store has cost against the record, when the
// store is one that reaches a location at all.
//
// The access token and User stores are opened where the auth routes are rather
// than by the backend, so the backend's reporter never sees them.
func recordSpendOf(store any) (RecordReporter, bool) {
	asks, ok := store.(asksItsLocation)
	if !ok {
		return nil, false
	}
	return spendOf{asks}, true
}

// spendOf turns one store's tally into the rows the panel draws. Of already
// carries the name make parity uses, so a row here and a row there are about
// one subject.
type spendOf struct{ asks asksItsLocation }

func (s spendOf) RecordSpend() ([]Spend, error) {
	asked, err := s.asks.Requests()
	if err != nil {
		return nil, err
	}
	spend := make([]Spend, 0, len(asked))
	for _, one := range asked {
		spend = append(spend, Spend{
			Of:      one.Of,
			Request: one.Request,
			Count:   one.Count,
		})
	}
	return spend, nil
}
