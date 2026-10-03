package identity

import (
	"strings"
	"testing"
)

// A QuickDev id keeps its prefix and says it is QuickDev's; two are not one.
func TestQuickDevIDsKeepTheirPrefixAndAreNotASUIDs(t *testing.T) {
	first, err := quickDevID("US")
	if err != nil {
		t.Fatal(err)
	}
	second, err := quickDevID("US")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(first, "US-quickdev-") || len(first) != len("US-quickdev-")+12 {
		t.Errorf("a QuickDev user id is %q", first)
	}
	if first == second {
		t.Errorf("two QuickDev ids are both %q", first)
	}
	if random, err := quickDevHex(7); err != nil || len(random) != 7 || strings.ToLower(random) != random {
		t.Errorf("a random id of length 7 is %q, %v", random, err)
	}
	if _, err := quickDevHex(0); err == nil {
		t.Error("a random id of length 0 was made")
	}
}
