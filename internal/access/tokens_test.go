package access

import "testing"

// "nil is nil"

// A token says when it ends. One that says nothing has not said it never
// ends, and is not usable; one that does not end says NeverExpires.
func TestATokenThatSaysNoExpiryIsNotUsable(t *testing.T) {
	now := int64(1_800_000_000_000)
	past, never := now-1, NeverExpires

	if (TokenRecord{}).Usable(now) {
		t.Error("a token saying nothing of when it ends was usable")
	}
	if (TokenRecord{ExpiresAt: &past}).Usable(now) {
		t.Error("an expired token was usable")
	}
	if !(TokenRecord{ExpiresAt: &never}).Usable(now) {
		t.Error("a token that says it never ends was not usable")
	}
}
