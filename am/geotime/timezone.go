package geotime

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/teranos/QNTX/errors"
)

// Timezone compatibility scoring constants for evaluating timezone proximity
const (
	// Timezone hour difference thresholds for scoring bands
	TimezoneExcellentThreshold = 0 // Same timezone
	TimezoneGoodThreshold      = 3 // 1-3 hours difference
	TimezoneFairThreshold      = 6 // 4-6 hours difference
	TimezonePoorThreshold      = 9 // 7-9 hours difference
	// 10+ hours = challenging

	// Timezone compatibility scores
	TimezoneScoreExcellent   = 1.0 // Same timezone: perfect compatibility
	TimezoneScoreGood        = 0.9 // 1-3 hours: very good for meetings/calls
	TimezoneScoreFair        = 0.7 // 4-6 hours: manageable with planning
	TimezoneScorePoor        = 0.4 // 7-9 hours: challenging but possible
	TimezoneScoreChallenging = 0.2 // 10+ hours: very difficult coordination

	// Default scores for edge cases
	TimezoneScoreUnknown = 0.7 // Neutral score when timezone cannot be determined
)

var locationKeywordTimezones = map[string]string{
	"amsterdam":      "Europe/Amsterdam",
	"netherlands":    "Europe/Amsterdam",
	"rotterdam":      "Europe/Amsterdam",
	"eindhoven":      "Europe/Amsterdam",
	"dutch":          "Europe/Amsterdam",
	"berlin":         "Europe/Berlin",
	"germany":        "Europe/Berlin",
	"munich":         "Europe/Berlin",
	"hamburg":        "Europe/Berlin",
	"frankfurt":      "Europe/Berlin",
	"cologne":        "Europe/Berlin",
	"london":         "Europe/London",
	"united kingdom": "Europe/London",
	"great britain":  "Europe/London",
	"england":        "Europe/London",
	"manchester":     "Europe/London",
	"edinburgh":      "Europe/London",
	"belfast":        "Europe/London",
	"new york":       "America/New_York",
	"boston":         "America/New_York",
	"washington":     "America/New_York",
	"new jersey":     "America/New_York",
	"usa":            "America/New_York",
	"united states":  "America/New_York",
	"san francisco":  "America/Los_Angeles",
	"los angeles":    "America/Los_Angeles",
	"seattle":        "America/Los_Angeles",
	"california":     "America/Los_Angeles",
	"bay area":       "America/Los_Angeles",
	"silicon valley": "America/Los_Angeles",
	"vancouver":      "America/Vancouver",
	"canada":         "America/Toronto",
	"toronto":        "America/Toronto",
	"montreal":       "America/Toronto",
	"quebec":         "America/Toronto",
	"mexico":         "America/Mexico_City",
	"mexico city":    "America/Mexico_City",
	"brazil":         "America/Sao_Paulo",
	"sao paulo":      "America/Sao_Paulo",
	"buenos aires":   "America/Argentina/Buenos_Aires",
	"sydney":         "Australia/Sydney",
	"melbourne":      "Australia/Sydney",
	"australia":      "Australia/Sydney",
	"brisbane":       "Australia/Brisbane",
	"singapore":      "Asia/Singapore",
	"hong kong":      "Asia/Hong_Kong",
	"tokyo":          "Asia/Tokyo",
	"japan":          "Asia/Tokyo",
	"india":          "Asia/Kolkata",
	"delhi":          "Asia/Kolkata",
	"mumbai":         "Asia/Kolkata",
	"bangalore":      "Asia/Kolkata",
	"israel":         "Asia/Jerusalem",
	"tel aviv":       "Asia/Jerusalem",
	"uae":            "Asia/Dubai",
	"dubai":          "Asia/Dubai",
	"stockholm":      "Europe/Stockholm",
	"sweden":         "Europe/Stockholm",
	"oslo":           "Europe/Oslo",
	"norway":         "Europe/Oslo",
	"copenhagen":     "Europe/Copenhagen",
	"denmark":        "Europe/Copenhagen",
	"helsinki":       "Europe/Helsinki",
	"finland":        "Europe/Helsinki",
	"dublin":         "Europe/Dublin",
	"ireland":        "Europe/Dublin",
	"paris":          "Europe/Paris",
	"france":         "Europe/Paris",
	"madrid":         "Europe/Madrid",
	"spain":          "Europe/Madrid",
	"rome":           "Europe/Rome",
	"italy":          "Europe/Rome",
}

var countryCodeTimezones = map[string]string{
	"nl": "Europe/Amsterdam",
	"de": "Europe/Berlin",
	"be": "Europe/Brussels",
	"fr": "Europe/Paris",
	"it": "Europe/Rome",
	"es": "Europe/Madrid",
	"gb": "Europe/London",
	"uk": "Europe/London",
	"ie": "Europe/Dublin",
	"ca": "America/Toronto",
	"us": "America/New_York",
	"mx": "America/Mexico_City",
	"br": "America/Sao_Paulo",
	"ar": "America/Argentina/Buenos_Aires",
	"au": "Australia/Sydney",
	"nz": "Pacific/Auckland",
	"sg": "Asia/Singapore",
	"hk": "Asia/Hong_Kong",
	"jp": "Asia/Tokyo",
	"kr": "Asia/Seoul",
	"in": "Asia/Kolkata",
	"il": "Asia/Jerusalem",
	"ae": "Asia/Dubai",
	"se": "Europe/Stockholm",
	"no": "Europe/Oslo",
	"dk": "Europe/Copenhagen",
	"fi": "Europe/Helsinki",
}

var timezoneByAbbreviation = map[string]string{
	"pst":   "America/Los_Angeles",
	"pdt":   "America/Los_Angeles",
	"est":   "America/New_York",
	"edt":   "America/New_York",
	"cst":   "America/Chicago",
	"cdt":   "America/Chicago",
	"mst":   "America/Denver",
	"mdt":   "America/Denver",
	"bst":   "Europe/London",
	"cet":   "Europe/Berlin",
	"cest":  "Europe/Berlin",
	"ist":   "Asia/Kolkata",
	"sgt":   "Asia/Singapore",
	"hkt":   "Asia/Hong_Kong",
	"aest":  "Australia/Sydney",
	"aedst": "Australia/Sydney",
}

var timezoneByTLD = map[string]string{
	".nl":     "Europe/Amsterdam",
	".de":     "Europe/Berlin",
	".be":     "Europe/Brussels",
	".fr":     "Europe/Paris",
	".it":     "Europe/Rome",
	".es":     "Europe/Madrid",
	".co.uk":  "Europe/London",
	".uk":     "Europe/London",
	".ie":     "Europe/Dublin",
	".ca":     "America/Toronto",
	".us":     "America/New_York",
	".mx":     "America/Mexico_City",
	".br":     "America/Sao_Paulo",
	".ar":     "America/Argentina/Buenos_Aires",
	".com.au": "Australia/Sydney",
	".au":     "Australia/Sydney",
	".sg":     "Asia/Singapore",
	".hk":     "Asia/Hong_Kong",
	".jp":     "Asia/Tokyo",
	".kr":     "Asia/Seoul",
	".in":     "Asia/Kolkata",
	".il":     "Asia/Jerusalem",
	".ae":     "Asia/Dubai",
	".se":     "Europe/Stockholm",
	".no":     "Europe/Oslo",
	".dk":     "Europe/Copenhagen",
	".fi":     "Europe/Helsinki",
}

// NormalizeTimezone attempts to resolve user input into a valid IANA timezone.
func NormalizeTimezone(input string) (string, error) {
	trimmed := strings.TrimSpace(input)
	if trimmed == "" {
		return "", errors.New("timezone cannot be empty")
	}

	// First, check if the input is already a valid timezone
	if isValidTimezone(trimmed) {
		// For valid timezones, always canonicalize to ensure proper IANA format
		// This handles cases like "america/New_york" which may be parseable but not canonical
		canonicalized := canonicalizeValidTimezone(trimmed)
		if canonicalized != "" {
			return canonicalized, nil
		}
		// If canonicalization fails but timezone is valid, return as-is
		// This preserves properly formatted names like "America/Port_of_Spain"
		return trimmed, nil
	}

	// Try sanitizing only if the raw input isn't valid
	candidate := sanitizeTimezone(trimmed)
	if isValidTimezone(candidate) {
		return candidate, nil
	}

	lower := strings.ToLower(trimmed)
	if tz, ok := timezoneByAbbreviation[lower]; ok {
		return tz, nil
	}

	if tz := GuessTimezoneFromLocation(lower); tz != "" {
		return tz, nil
	}

	if tz, ok := countryCodeTimezones[lower]; ok {
		return tz, nil
	}

	return "", errors.Newf("unknown timezone: %s", input)
}

// GuessTimezoneFromLocation uses keyword heuristics to derive a timezone.
func GuessTimezoneFromLocation(location string) string {
	lower := strings.ToLower(strings.TrimSpace(location))
	for keyword, timezone := range locationKeywordTimezones {
		if strings.Contains(lower, keyword) {
			return timezone
		}
	}
	return ""
}

// GuessTimezoneFromCountryCode maps ISO-like country codes to timezones.
func GuessTimezoneFromCountryCode(code string) string {
	lower := strings.ToLower(strings.TrimSpace(code))
	if tz, ok := countryCodeTimezones[lower]; ok {
		return tz
	}
	return ""
}

// GuessTimezoneFromEmail derives timezone using email domain TLDs.
func GuessTimezoneFromEmail(email string) string {
	parts := strings.Split(email, "@")
	if len(parts) != 2 {
		return ""
	}
	domain := strings.ToLower(strings.TrimSpace(parts[1]))

	if strings.HasSuffix(domain, ".co.uk") {
		return "Europe/London"
	}
	if strings.HasSuffix(domain, ".com.au") {
		return "Australia/Sydney"
	}

	for tld, tz := range timezoneByTLD {
		if strings.HasSuffix(domain, tld) {
			return tz
		}
	}
	return ""
}

// GuessTimezoneFromLinkedInHost infers timezone from LinkedIn locale hints.
func GuessTimezoneFromLinkedInHost(host string) string {
	lower := strings.ToLower(strings.TrimSpace(host))
	parts := strings.Split(lower, ".")
	if len(parts) >= 2 {
		if tz := GuessTimezoneFromCountryCode(parts[0]); tz != "" {
			return tz
		}
	}
	if len(parts) >= 3 {
		if tz := GuessTimezoneFromCountryCode(parts[len(parts)-3]); tz != "" {
			return tz
		}
	}
	return ""
}

// GuessTimezoneFromLinkedInQuery inspects query parameters like originalSubdomain.
func GuessTimezoneFromLinkedInQuery(rawQuery string) string {
	if rawQuery == "" {
		return ""
	}
	values, err := url.ParseQuery(rawQuery)
	if err != nil {
		return ""
	}
	for _, vals := range values {
		for _, v := range vals {
			if tz := GuessTimezoneFromCountryCode(v); tz != "" {
				return tz
			}
			if tz := GuessTimezoneFromLocation(v); tz != "" {
				return tz
			}
		}
	}
	return ""
}

// DetectLocalTimezone attempts to determine the host operating system timezone.
func DetectLocalTimezone() (string, error) {
	if tz := os.Getenv("TZ"); tz != "" {
		if isValidTimezone(tz) {
			return tz, nil
		}
	}

	if name := time.Now().Location().String(); name != "" && name != "Local" {
		if isValidTimezone(name) {
			return name, nil
		}
	}

	if data, err := os.ReadFile("/etc/timezone"); err == nil {
		tz := sanitizeTimezone(string(data))
		if isValidTimezone(tz) {
			return tz, nil
		}
	}

	if tz, err := readZoneinfoSymlink("/etc/localtime"); err == nil && tz != "" {
		return tz, nil
	}
	if tz, err := readZoneinfoSymlink("/var/db/timezone/zoneinfo/localtime"); err == nil && tz != "" {
		return tz, nil
	}

	return "", errors.New("could not detect local timezone: tried TZ env var, time.Now().Location(), /etc/timezone, /etc/localtime, /var/db/timezone/zoneinfo/localtime")
}

func readZoneinfoSymlink(path string) (string, error) {
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return "", err
	}
	idx := strings.Index(resolved, "zoneinfo")
	if idx == -1 {
		return "", errors.New("zoneinfo segment not found")
	}
	candidate := strings.TrimPrefix(resolved[idx+len("zoneinfo"):], string(filepath.Separator))
	candidate = strings.ReplaceAll(candidate, string(os.PathSeparator), "/")
	candidate = sanitizeTimezone(candidate)
	if isValidTimezone(candidate) {
		return candidate, nil
	}
	return "", errors.Newf("invalid timezone: %q (from %s)", candidate, path)
}

func sanitizeTimezone(tz string) string {
	trimmed := strings.TrimSpace(tz)
	trimmed = strings.Trim(trimmed, "\"'")
	trimmed = strings.ReplaceAll(trimmed, " ", "_")
	if strings.Contains(trimmed, "/") {
		parts := strings.Split(trimmed, "/")
		for i, part := range parts {
			parts[i] = title(part)
		}
		return strings.Join(parts, "/")
	}
	return title(trimmed)
}

func title(s string) string {
	if s == "" {
		return s
	}
	lower := strings.ToLower(s)
	return strings.ToUpper(lower[:1]) + lower[1:]
}

func isValidTimezone(tz string) bool {
	if tz == "" {
		return false
	}
	_, err := time.LoadLocation(tz)
	return err == nil
}

// canonicalizeValidTimezone attempts to return the canonical IANA name for a valid timezone
// This ensures proper formatting for cases like "america/New_york" -> "America/New_York"
// but preserves properly formatted names like "America/Port_of_Spain"
func canonicalizeValidTimezone(tz string) string {
	// Only canonicalize if the timezone appears to have incorrect capitalization
	// Check if it's all lowercase or has clear formatting issues
	if strings.ToLower(tz) == tz || hasIncorrectCapitalization(tz) {
		candidate := sanitizeTimezone(tz)
		if isValidTimezone(candidate) && candidate != tz {
			return candidate
		}
	}
	// For properly formatted timezones, return empty to preserve original
	return ""
}

// hasIncorrectCapitalization detects timezones that need case correction
// but aren't already properly formatted IANA names
func hasIncorrectCapitalization(tz string) bool {
	// If it's all lowercase, it needs correction
	if strings.ToLower(tz) == tz {
		return true
	}

	// If it starts with lowercase after a slash, it needs correction
	// e.g., "america/New_York" should be "America/New_York"
	if strings.Contains(tz, "/") {
		parts := strings.Split(tz, "/")
		for _, part := range parts {
			if len(part) > 0 && part[0] >= 'a' && part[0] <= 'z' {
				return true
			}
		}
	}

	return false
}

// ValidateTimezone ensures the timezone string maps to a valid IANA entry.
func ValidateTimezone(tz string) error {
	if !isValidTimezone(tz) {
		return errors.Newf("invalid timezone: %s", tz)
	}
	return nil
}
