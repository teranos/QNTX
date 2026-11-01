//go:build !qntxquote

package quote

import errors "github.com/teranos/sacred-error"

// Carried is whether this build holds the Rust that measures quotes. It does
// not: the node leaves the quote subsystem out rather than keep a second
// measure in Go.
const Carried = false

// Measure is Rust's (quote_qntx.go), and a build without qntxquote carries no
// Rust to ask.
func Measure(spans, said []string) ([]Measurement, error) {
	return nil, errors.Newf("%d quoted spans were measured by no one: quotes are measured in Rust, which this build carries without the qntxquote tag", len(spans))
}
