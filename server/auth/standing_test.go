package auth

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// SUPER is never a session: levelOf answers ROOT or PUBLIC_REGISTRATION and
// nothing else, so SUPER arrives on a token it was minted for. Reading a
// session here left the rectangle legible to SUPER and immovable by it.
// anyFooting is a node that serves everything asked for.
func anyFooting(Admission, string) (int, string) { return 0, "" }

func TestSuperStepsWithTheTokenItArrivedOn(t *testing.T) {
	h, store, _ := arrivingHandler(t)
	h.SetFooting(anyFooting)
	held, err := store.List()
	require.NoError(t, err)
	require.Len(t, held, 1)

	admitted := Admitted(LevelSuper)
	admitted.UserID = held[0].ID

	stands, status, err := h.Step(admitted, "pond")
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, status)
	assert.Equal(t, "pond", stands)

	moved, found, err := store.ByRoute(mastodonAccount)
	require.NoError(t, err)
	require.True(t, found)
	assert.Equal(t, "pond", moved.Standing, "the step was answered and not written")
}

// A token minted for one namespace acts there whatever its person stepped to,
// so the answer says where they now stand rather than what they asked for.
func TestAStepAnswersWhereTheCallerNowStands(t *testing.T) {
	h, store, _ := arrivingHandler(t)
	h.SetFooting(anyFooting)
	held, err := store.List()
	require.NoError(t, err)

	admitted := Admitted(LevelAttestor, "pond")
	admitted.UserID = held[0].ID

	stands, _, err := h.Step(admitted, "playground")
	require.NoError(t, err)
	assert.Equal(t, "pond", stands, "the answer moved somewhere the writes do not land")

	read, _, err := h.Standing(admitted)
	require.NoError(t, err)
	assert.Equal(t, "pond", read, "reading where they stand disagrees with what the step answered")
}

// Where a person stands is read without moving them: the reading is the
// person's, as stepping left it.
func TestStandingIsReadWhereTheStepLeftIt(t *testing.T) {
	h, store, _ := arrivingHandler(t)
	h.SetFooting(anyFooting)
	held, err := store.List()
	require.NoError(t, err)

	admitted := Admitted(LevelSuper)
	admitted.UserID = held[0].ID

	before, _, err := h.Standing(admitted)
	require.NoError(t, err)
	assert.Equal(t, NamespaceDefault, before, "a person who has not stepped stands in default")

	_, _, err = h.Step(admitted, "pond")
	require.NoError(t, err)
	after, _, err := h.Standing(admitted)
	require.NoError(t, err)
	assert.Equal(t, "pond", after)
}

// The rectangle cannot land on a disabled namespace, and the UI refusing the
// press is not what makes that true. The step is refused with the stores'
// reason and the person is left where they were.
func TestAStepIntoADisabledNamespaceIsRefused(t *testing.T) {
	h, store, _ := arrivingHandler(t)
	h.SetFooting(func(_ Admission, namespace string) (int, string) {
		if namespace == "pond" {
			return http.StatusConflict, "pond is disabled"
		}
		return 0, ""
	})
	held, err := store.List()
	require.NoError(t, err)

	admitted := Admitted(LevelSuper)
	admitted.UserID = held[0].ID

	_, status, err := h.Step(admitted, "pond")
	require.Error(t, err)
	require.Equal(t, http.StatusConflict, status)
	assert.Contains(t, err.Error(), "pond is disabled", "the stores' reason did not reach the caller")

	stayed, found, err := store.ByRoute(mastodonAccount)
	require.NoError(t, err)
	require.True(t, found)
	assert.Equal(t, "", stayed.Standing, "the person was moved into a namespace that is off")
}

// Nil footing is a node that has not said where anybody may stand. It lets
// nobody step rather than everybody.
func TestANodeThatHasNotSaidWhereYouMayStandLetsNobodyStep(t *testing.T) {
	h, store, _ := arrivingHandler(t)
	held, err := store.List()
	require.NoError(t, err)

	admitted := Admitted(LevelSuper)
	admitted.UserID = held[0].ID

	_, status, err := h.Step(admitted, "pond")
	require.Error(t, err)
	require.Equal(t, http.StatusInternalServerError, status)
}

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
