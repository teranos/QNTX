package parity

import "testing"

// A subsystem compiled in says what its own messages and fields say of
// themselves (ADR-056). Read where ours are read, by full name; what ours say
// is never overridden.
func TestASubsystemsSaysAreReadWhereOursAre(t *testing.T) {
	t.Cleanup(func() { also = map[string]string{} })
	AlsoSays(map[string]string{
		"quotes.Quote.content": "The words, verbatim.",
		"protocol.Sigil.does":  "not what the .proto says",
	})
	if got, err := OursSay("quotes.Quote.content"); err != nil || got != "The words, verbatim." {
		t.Fatalf("a subsystem's field says %q, %v", got, err)
	}
	if got, err := OursSay("protocol.Sigil.does"); err != nil || got != "What it is for, in words, for somebody who has never seen the code." {
		t.Fatalf("our own field says %q, %v; a subsystem said it again and ours stands", got, err)
	}
	if got, err := OursSay("nobody.Says.this"); err != nil || got != "" {
		t.Fatalf("a name nobody says anything of: %q, %v", got, err)
	}
}
