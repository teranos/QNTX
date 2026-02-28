package httputil

import "testing"

func TestEscapeHTML(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"hello", "hello"},
		{"<script>alert('xss')</script>", "&lt;script&gt;alert(&#39;xss&#39;)&lt;/script&gt;"},
		{`a & b "c"`, "a &amp; b &quot;c&quot;"},
		{"", ""},
	}

	for _, tt := range tests {
		got := EscapeHTML(tt.input)
		if got != tt.want {
			t.Errorf("EscapeHTML(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestEscapeHTMLAttr(t *testing.T) {
	got := EscapeHTMLAttr(`val"ue`)
	want := "val&quot;ue"
	if got != want {
		t.Errorf("EscapeHTMLAttr(%q) = %q, want %q", `val"ue`, got, want)
	}
}
