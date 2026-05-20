package storage

import "testing"

func TestLooksLikeID(t *testing.T) {
	cases := []struct {
		subject string
		want    string
	}{
		// UUID
		{"550e8400-e29b-41d4-a716-446655440000", "UUID shape"},
		{"550E8400-E29B-41D4-A716-446655440000", "UUID shape"},

		// Long hex run (≥16)
		{"abcdef0123456789", "≥16-char hex run"},
		{"deadbeefdeadbeefdeadbeef", "≥16-char hex run"},
		{"prefix-abcdef0123456789-suffix", "≥16-char hex run"},

		// Trailing _<digits> or -<digits>
		{"user_123", "trailing _<digits> or -<digits>"},
		{"item-42", "trailing _<digits> or -<digits>"},
		{"2026-05-20", "trailing _<digits> or -<digits>"},

		// All-numeric
		{"12345", "all-numeric"},
		{"2026", "all-numeric"},

		// Clean subjects — no warn
		{"alice", ""},
		{"vacancies", ""},
		{"contact", ""},
		{"embeddings", ""},
		{"pulse", ""},
		{"alice@example.com", ""},
		{"v0.16.14", ""},
		{"user:alice", ""},
		{"abcdef012345", ""},
		{"", ""},
		{"_", ""},
		{"-", ""},
		{"foo-", ""},
		{"foo_bar", ""},
		{"-42", ""},
	}

	for _, tc := range cases {
		t.Run(tc.subject, func(t *testing.T) {
			got := looksLikeID(tc.subject)
			if got != tc.want {
				t.Errorf("looksLikeID(%q) = %q, want %q", tc.subject, got, tc.want)
			}
		})
	}
}
