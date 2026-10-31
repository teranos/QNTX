//go:build !qntxquote

package quote

import errors "github.com/teranos/sacred-error"

// Carried is whether this build holds the Rust that judges quotes. It does
// not: the node leaves the quote subsystem out rather than keep a second
// judge in Go.
const Carried = false

// Judge is Rust's (quote_qntx.go), and a build without qntxquote carries no
// Rust to ask.
func Judge(spans, said []string) ([]Judgement, error) {
	return nil, errors.Newf("%d quoted spans were judged by no one: quotes are judged in Rust, which this build carries without the qntxquote tag", len(spans))
}
