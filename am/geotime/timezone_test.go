package geotime

import "testing"

func TestNormalizeTimezone(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"Europe/Amsterdam", "Europe/Amsterdam"},
		{"europe/berlin", "Europe/Berlin"},
		{"PST", "America/Los_Angeles"},
		{"Amsterdam", "Europe/Amsterdam"},
		{"San Francisco", "America/Los_Angeles"},
		{"NL", "Europe/Amsterdam"},
		// Test case for the fixed bug - valid IANA names with lowercase articles
		{"America/Port_of_Spain", "America/Port_of_Spain"},
		{"Europe/Isle_of_Man", "Europe/Isle_of_Man"},
		{"Pacific/Port_Moresby", "Pacific/Port_Moresby"},
	}

	for _, tc := range tests {
		actual, err := NormalizeTimezone(tc.input)
		if err != nil {
			t.Fatalf("expected timezone for %q, got error: %v", tc.input, err)
		}
		if actual != tc.expected {
			t.Fatalf("expected %s for input %q, got %s", tc.expected, tc.input, actual)
		}
	}
}

func TestGuessTimezoneHelpers(t *testing.T) {
	if tz := GuessTimezoneFromLocation("Based in Berlin, Germany"); tz != "Europe/Berlin" {
		t.Fatalf("expected Berlin to map to Europe/Berlin, got %s", tz)
	}

	if tz := GuessTimezoneFromEmail("alex@example.co.uk"); tz != "Europe/London" {
		t.Fatalf("expected .co.uk email to map to Europe/London, got %s", tz)
	}

	if tz := GuessTimezoneFromLinkedInHost("de.linkedin.com"); tz != "Europe/Berlin" {
		t.Fatalf("expected LinkedIn host to map to Europe/Berlin, got %s", tz)
	}

	if tz := GuessTimezoneFromLinkedInQuery("originalSubdomain=nl"); tz != "Europe/Amsterdam" {
		t.Fatalf("expected LinkedIn query to map to Europe/Amsterdam, got %s", tz)
	}
}
