package slug

import "testing"

// The slug is case and nothing else. A sanitizer here would let a door reach a
// namespace whose name it does not share, which is a universe nobody chose.
func TestASlugIsCaseAndNothingElse(t *testing.T) {
	if got := Of("Clean"); got != "clean" {
		t.Fatalf("the namespace Clean is reached by %q", got)
	}
	for _, name := range []string{"clean amsterdam", "clean_amsterdam", "clean-amsterdam", "clean.amsterdam"} {
		if got := Of(name); got != name {
			t.Errorf("%q was changed into %q by something other than case", name, got)
		}
	}
}
