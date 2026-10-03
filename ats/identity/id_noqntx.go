//go:build !qntxwasm

package identity

import "github.com/teranos/errors"

func generateASUID(prefix, subject, predicate, context string) (string, error) {
	return "", errors.Wrap(ErrNoWASM, "ASUID generation")
}

func generateCompactASUID(prefix, name string) (string, error) {
	return "", errors.Wrap(ErrNoWASM, "compact ASUID generation")
}

func generateRandomID(length int) (string, error) {
	return "", errors.Wrap(ErrNoWASM, "random ID generation")
}
