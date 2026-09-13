package auth

import "testing"

// Where a person is standing has one reading, and three things read it: the
// universe a request acts in, what GET /i/ answers, and what POST /i/standing
// answers back. A second reading is a rectangle drawn on one namespace while
// the writes land in another.
func TestWhereAPersonIsStanding(t *testing.T) {
	for _, c := range []struct {
		what     string
		admitted Admission
		stepped  string
		want     string
	}{
		{
			what:     "an admission reaching one namespace is in that one",
			admitted: Admitted(LevelAttestor, "pond"),
			stepped:  "",
			want:     "pond",
		},
		{
			what:     "and stepping does not move it",
			admitted: Admitted(LevelAttestor, "pond"),
			stepped:  "playground",
			want:     "pond",
		},
		{
			what:     "reaching every namespace, a person stands where they stepped",
			admitted: Admission{},
			stepped:  "playground",
			want:     "playground",
		},
		{
			what:     "having stepped nowhere, in the default project",
			admitted: Admission{},
			stepped:  "",
			want:     NamespaceDefault,
		},
		{
			what:     "reaching several, a person stands where they stepped",
			admitted: Admission{Namespaces: []string{"pond", "playground"}},
			stepped:  "playground",
			want:     "playground",
		},
	} {
		if got := StandingIn(c.admitted, c.stepped); got != c.want {
			t.Errorf("%s: stands in %q, not %q", c.what, got, c.want)
		}
	}
}

// The rectangle is never on nothing, so the field it is drawn from is never
// empty — a person who has never stepped anywhere included.
func TestThePersonAlwaysStandsSomewhere(t *testing.T) {
	p := personOf(User{ID: "u1"}, Admission{})
	if p.Standing == "" {
		t.Fatal("a person who has not stepped stands nowhere at all")
	}
	if p.Standing != NamespaceDefault {
		t.Fatalf("a person who has not stepped stands in %q", p.Standing)
	}
}

// Door is where they came in and does not move; Standing is where they are.
// Reading one for the other puts the rectangle on the door forever.
func TestTheDoorIsNotWhereYouStand(t *testing.T) {
	p := personOf(User{ID: "u1", Namespace: "pond", Standing: "playground"}, Admission{})
	if p.Door != "pond" {
		t.Fatalf("the door moved to %q", p.Door)
	}
	if p.Standing != "playground" {
		t.Fatalf("the person stands at their door %q rather than where they stepped", p.Standing)
	}
}
