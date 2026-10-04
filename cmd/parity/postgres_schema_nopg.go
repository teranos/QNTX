//go:build !rustpostgres

package main

import "github.com/teranos/errors"

// PostgresSchema needs ats-postgres, which this build does not link without
// the rustpostgres tag.
func PostgresSchema() (map[string]bool, string, error) {
	return nil, "", errors.New("the postgres schema is read from ats-postgres: build with -tags rustpostgres, as make parity does")
}
