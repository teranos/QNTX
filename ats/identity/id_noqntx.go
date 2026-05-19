//go:build !qntxwasm

package identity

import "github.com/teranos/errors"

func generateASUID(prefix, subject, predicate, context string) (string, error) {
	return "", errors.New("ASUID generation requires qntxwasm build tag")
}

func generateCompactASUID(prefix, name string) (string, error) {
	return "", errors.New("compact ASUID generation requires qntxwasm build tag")
}

func generateRandomID(length int) (string, error) {
	return "", errors.New("random ID generation requires qntxwasm build tag")
}
