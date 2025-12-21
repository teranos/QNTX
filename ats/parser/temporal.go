package parser

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// timeNow is a variable that can be mocked for testing
var timeNow = time.Now

// temporalDateLayouts defines supported date/time formats for parsing
// Ordered from most specific to least specific for accurate parsing
var temporalDateLayouts = []string{
	time.RFC3339,           // "2006-01-02T15:04:05Z07:00"
	time.RFC3339Nano,       // "2006-01-02T15:04:05.999999999Z07:00"
	"2006-01-02T15:04:05Z", // "2025-01-15T14:30:00Z"
	"2006-01-02T15:04:05",  // "2025-01-15T14:30:00"
	"2006-01-02 15:04:05",  // "2025-01-15 14:30:00"
	"2006-01-02T15:04Z",    // "2025-01-15T14:30Z"
	"2006-01-02T15:04",     // "2025-01-15T14:30"
	"2006-01-02 15:04",     // "2025-01-15 14:30"
	"2006-01-02",           // "2025-01-15"
	"01/02/2006",           // "01/15/2025" (US format)
	"01-02-2006",           // "01-15-2025" (US format)
	"2006/01/02",           // "2025/01/15" (ISO-ish)
}

// temporalWordsMap provides O(1) lookup for temporal word validation
var temporalWordsMap = map[string]bool{
	// Relative time indicators
	"ago": true, "and": true, "week": true, "month": true, "day": true, "days": true, "weeks": true, "months": true, "years": true,
	"last": true, "next": true, "yesterday": true, "today": true, "tomorrow": true, "in": true,

	// Month names
	"january": true, "february": true, "march": true, "april": true, "may": true, "june": true,
	"july": true, "august": true, "september": true, "october": true, "november": true, "december": true,
	"jan": true, "feb": true, "mar": true, "apr": true, "jun": true, "jul": true, "aug": true, "sep": true, "oct": true, "nov": true, "dec": true,

	// Day names
	"monday": true, "tuesday": true, "wednesday": true, "thursday": true, "friday": true, "saturday": true, "sunday": true,
	"mon": true, "tue": true, "wed": true, "thu": true, "fri": true, "sat": true, "sun": true,

	// Time units
	"second": true, "seconds": true, "minute": true, "minutes": true, "hour": true, "hours": true,
	"sec": true, "secs": true, "min": true, "mins": true, "hr": true, "hrs": true,
}

// dayNameMap maps day names (full and abbreviated) to weekday numbers
var dayNameMap = map[string]time.Weekday{
	"monday":    time.Monday,
	"tuesday":   time.Tuesday,
	"wednesday": time.Wednesday,
	"thursday":  time.Thursday,
	"friday":    time.Friday,
	"saturday":  time.Saturday,
	"sunday":    time.Sunday,
	"mon":       time.Monday,
	"tue":       time.Tuesday,
	"wed":       time.Wednesday,
	"thu":       time.Thursday,
	"fri":       time.Friday,
	"sat":       time.Saturday,
	"sun":       time.Sunday,
}

// ParseTemporalExpression parses natural language and ISO temporal expressions
func ParseTemporalExpression(expr string) (*time.Time, error) {
	expr = strings.TrimSpace(expr)
	if expr == "" {
		return nil, fmt.Errorf("empty temporal expression")
	}

	now := timeNow()

	// Natural language expressions
	switch strings.ToLower(expr) {
	case "now", "today":
		return &now, nil
	case "yesterday":
		yesterday := now.AddDate(0, 0, -1)
		return &yesterday, nil
	case "tomorrow":
		tomorrow := now.AddDate(0, 0, 1)
		return &tomorrow, nil
	case "last week":
		lastWeek := now.AddDate(0, 0, -7)
		return &lastWeek, nil
	case "last month":
		lastMonth := now.AddDate(0, -1, 0)
		return &lastMonth, nil
	case "last year":
		lastYear := now.AddDate(-1, 0, 0)
		return &lastYear, nil
	case "next week":
		nextWeek := now.AddDate(0, 0, 7)
		return &nextWeek, nil
	case "next month":
		nextMonth := now.AddDate(0, 1, 0)
		return &nextMonth, nil
	case "next year":
		nextYear := now.AddDate(1, 0, 0)
		return &nextYear, nil
	}

	// Relative expressions: "3 days ago", "2 weeks ago"
	if strings.HasSuffix(expr, " ago") {
		relativeExpr := strings.TrimSuffix(expr, " ago")
		duration, err := ParseRelativeDuration(relativeExpr)
		if err == nil {
			past := now.Add(-duration)
			return &past, nil
		}
	}

	// Future relative expressions: "in 3 days", "in 2 weeks"
	if strings.HasPrefix(expr, "in ") {
		relativeExpr := strings.TrimPrefix(expr, "in ")
		duration, err := ParseRelativeDuration(relativeExpr)
		if err == nil {
			future := now.Add(duration)
			return &future, nil
		}
	}

	// Named day expressions: "last friday", "next monday"
	if result := parseNamedDay(expr, now); result != nil {
		return result, nil
	}

	// ISO date formats (most specific to least specific)
	for _, layout := range temporalDateLayouts {
		if t, err := time.Parse(layout, expr); err == nil {
			return &t, nil
		}
	}

	return nil, fmt.Errorf("unable to parse temporal expression: %s", expr)
}

// ParseRelativeDuration parses relative duration expressions like "3 days", "2 weeks"
func ParseRelativeDuration(expr string) (time.Duration, error) {
	parts := strings.Fields(strings.TrimSpace(expr))
	if len(parts) != 2 {
		return 0, fmt.Errorf("invalid duration format: %s (expected 'NUMBER UNIT')", expr)
	}

	num, err := strconv.Atoi(parts[0])
	if err != nil {
		return 0, fmt.Errorf("invalid duration format: invalid number '%s'", parts[0])
	}

	if num < 0 {
		return 0, fmt.Errorf("invalid duration format: negative duration not supported")
	}

	unit := strings.ToLower(parts[1])
	switch unit {
	case "second", "seconds", "sec", "secs":
		return time.Duration(num) * time.Second, nil
	case "minute", "minutes", "min", "mins":
		return time.Duration(num) * time.Minute, nil
	case "hour", "hours", "hr", "hrs":
		return time.Duration(num) * time.Hour, nil
	case "day", "days":
		return time.Duration(num) * 24 * time.Hour, nil
	case "week", "weeks":
		return time.Duration(num) * 7 * 24 * time.Hour, nil
	case "month", "months":
		// Approximate month as 30 days
		return time.Duration(num) * 30 * 24 * time.Hour, nil
	case "year", "years":
		// Approximate year as 365 days
		return time.Duration(num) * 365 * 24 * time.Hour, nil
	}

	return 0, fmt.Errorf("unsupported duration unit: %s", unit)
}

// IsTemporalContinuation checks if a token should be considered part of a temporal expression
func IsTemporalContinuation(value string) bool {
	// Use map lookup for O(1) temporal word validation
	lower := strings.ToLower(value)
	if temporalWordsMap[lower] {
		return true
	}

	// Check if it's a number (for "3 days ago")
	if _, err := strconv.Atoi(value); err == nil {
		return true
	}

	// Check if it's a date-like format (contains separators)
	if strings.Contains(value, "-") || strings.Contains(value, "/") || strings.Contains(value, ":") {
		return true
	}

	return false
}

// parseNamedDay handles expressions like "last friday", "next monday"
func parseNamedDay(expr string, baseTime time.Time) *time.Time {
	parts := strings.Fields(strings.ToLower(expr))
	if len(parts) != 2 {
		return nil
	}

	direction := parts[0]
	dayName := parts[1]

	// Use package-level map to avoid recreating on each call
	targetDay, exists := dayNameMap[dayName]
	if !exists {
		return nil
	}

	currentDay := baseTime.Weekday()

	var result time.Time
	switch direction {
	case "last":
		// Find the most recent occurrence of targetDay in the past
		daysBack := int(currentDay - targetDay)
		if daysBack <= 0 {
			daysBack += 7 // Go back to previous week
		}
		result = baseTime.AddDate(0, 0, -daysBack)
	case "next":
		// Find the next occurrence of targetDay in the future
		daysForward := int(targetDay - currentDay)
		if daysForward <= 0 {
			daysForward += 7 // Go forward to next week
		}
		result = baseTime.AddDate(0, 0, daysForward)
	case "this":
		// Find targetDay in current week
		daysOffset := int(targetDay - currentDay)
		result = baseTime.AddDate(0, 0, daysOffset)
	default:
		return nil
	}

	return &result
}
