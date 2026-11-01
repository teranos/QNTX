package parity

import (
	"strings"
	"testing"
)

// What TEI P5 4.12.0 says of q, of interp and of their attributes is read
// from the ODD pinned for it, with each attribute's class named as where it
// was read.
func TestReference_TEISays(t *testing.T) {
	schema, refused := Reference("tei")
	if refused != nil {
		t.Fatal(refused)
	}
	q := modelNamed(t, schema, "q")
	if q.Says != "contains material which is distinguished from the surrounding text using quotation marks or a similar method, for any one of a variety of reasons including, but not limited to: direct speech or thought, technical terms or jargon, authorial distance, quotations from elsewhere, and passages that are mentioned but not used." {
		t.Errorf("q says %q", q.Says)
	}
	for _, want := range []Column{
		{Name: "content", Type: "macro.specialPara"},
		{Name: "type", Type: "teidata.enumerated", Says: "may be used to indicate whether the offset passage is spoken or thought, or to characterize it more finely."},
		{Name: "who", Type: "teidata.pointer", List: true, Says: "indicates the person, or group of people, to whom the element content is ascribed.", SaysFrom: "Specs/att.ascribed.xml · att.ascribed.who"},
		{Name: "ana", Type: "teidata.pointer", List: true, Says: "indicates one or more elements containing interpretations of the element on which the ana attribute appears.", SaysFrom: "Specs/att.global.analytic.xml · att.global.analytic.ana"},
		{Name: "rend", Type: "teidata.word", List: true, Says: "indicates how the element in question was rendered or presented in the source text.", SaysFrom: "Specs/att.global.rendition.xml · att.global.rendition.rend"},
	} {
		got := columnNamed(t, q, want.Name)
		if got.Type != want.Type || got.List != want.List || got.Says != want.Says || got.SaysFrom != want.SaysFrom || got.Required {
			t.Errorf("q.%s is %+v, want %+v", want.Name, got, want)
		}
	}
	// q's own type replaces att.typed's: one column, the element's.
	if n := strings.Count(" "+names(q)+" ", " type "); n != 1 {
		t.Errorf("q has type %d times: %s", n, names(q))
	}

	interp := modelNamed(t, schema, "interp")
	if interp.Says != "summarizes a specific interpretative annotation which can be linked to a span of text." {
		t.Errorf("interp says %q", interp.Says)
	}
	for _, want := range []Column{
		{Name: "content", Type: "text"},
		{Name: "inst", Type: "teidata.pointer", List: true, Says: "points to instances of the analysis or interpretation represented by the current element.", SaysFrom: "Specs/att.interpLike.xml · att.interpLike.inst"},
		{Name: "resp", Type: "teidata.pointer", List: true, Says: columnNamed(t, q, "resp").Says, SaysFrom: "Specs/att.global.responsibility.xml · att.global.responsibility.resp"},
	} {
		got := columnNamed(t, interp, want.Name)
		if got.Type != want.Type || got.List != want.List || got.Says != want.Says || got.SaysFrom != want.SaysFrom {
			t.Errorf("interp.%s is %+v, want %+v", want.Name, got, want)
		}
	}
	if columnNamed(t, q, "resp").Says == "" {
		t.Error("resp says nothing")
	}
}

// A class an element is a member of and the pin lacks is named, not skipped.
func TestParseODD_RefusesAnUnpinnedClass(t *testing.T) {
	raw, err := pinned.ReadFile("tei_4.12.0_113e933/Specs/q.xml")
	if err != nil {
		t.Fatal(err)
	}
	_, err = ParseODD(map[string][]byte{"Specs/q.xml": raw})
	if err == nil || !strings.Contains(err.Error(), "q is a member of att.global, which is not pinned") {
		t.Errorf("err is %v", err)
	}
}

func columnNamed(t *testing.T, model Model, name string) Column {
	t.Helper()
	for _, c := range model.Columns {
		if c.Name == name {
			return c
		}
	}
	t.Fatalf("%s has no %s; it has %s", model.Name, name, names(model))
	return Column{}
}

func names(model Model) string {
	var all []string
	for _, c := range model.Columns {
		all = append(all, c.Name)
	}
	return strings.Join(all, " ")
}
