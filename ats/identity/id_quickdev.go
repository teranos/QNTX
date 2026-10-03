//go:build !qntxwasm && quickdev

package identity

func generateASUID(prefix, subject, predicate, context string) (string, error) {
	return quickDevID(prefix)
}

func generateCompactASUID(prefix, name string) (string, error) {
	return quickDevID(prefix)
}

func generateRandomID(length int) (string, error) {
	return quickDevHex(length)
}
