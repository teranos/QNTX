// Package named holds values known to name something. The only way to have one
// is to make it from what arrived, and making one from nothing is refused,
// naming what was empty. Code that holds one never asks whether it is there.
//
// "nil is nil"
package named

import "github.com/teranos/errors"

// Text is text that holds something.
type Text struct {
	text string
}

// TextOf makes a Text of text, or refuses it for holding nothing. what names
// the field, so the refusal says which one arrived empty.
func TextOf(what, text string) (Text, error) {
	if len(text) == 0 {
		return Text{}, errors.Newf("%s names nothing", what)
	}
	return Text{text: text}, nil
}

// String is the text.
func (t Text) String() string {
	return t.text
}

// Count is a number of things that is at least one.
type Count struct {
	n int
}

// CountOf makes a Count of n, or refuses it for being fewer than one. what
// names the field, so the refusal says which one arrived with none.
func CountOf(what string, n int) (Count, error) {
	if n < 1 {
		return Count{}, errors.Newf("%s is %d: at least one is needed", what, n)
	}
	return Count{n: n}, nil
}

// Int is the number.
func (c Count) Int() int {
	return c.n
}
