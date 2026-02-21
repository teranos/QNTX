package id

import (
	"fmt"
	"strings"
)

// roleExcludeWords are common words to exclude from role titles (case-insensitive)
var roleExcludeWords = map[string]bool{
	// Articles and prepositions
	"THE": true, "A": true, "AN": true, "OF": true, "AND": true,
	// Common connectors
	"FOR": true, "TO": true, "IN": true, "AT": true, "BY": true,
}

// roleAbbreviations are common role abbreviations and mappings (case-insensitive)
// These are industry-standard abbreviations
var roleAbbreviations = map[string]string{
	// Exact role matches (highest priority)
	"SOFTWARE ENGINEER":         "SWE",
	"PRODUCT MANAGER":           "PM",
	"DATA SCIENTIST":            "DS",
	"ENGINEERING MANAGER":       "EM",
	"TECHNICAL PROGRAM MANAGER": "TPM",
	"PROGRAM MANAGER":           "PGM",
	"BACKEND DEVELOPER":         "BACKEND",
	"FRONTEND DEVELOPER":        "FRONTEND",
	"FULL STACK DEVELOPER":      "FULLSTACK",
	"DEVOPS ENGINEER":           "DEVOPS",
	"SITE RELIABILITY ENGINEER": "SRE",
	"MACHINE LEARNING ENGINEER": "MLE",
	"QUALITY ASSURANCE":         "QA",
	"BUSINESS ANALYST":          "BA",
	"SYSTEMS ADMINISTRATOR":     "SYSADMIN",
	"DATABASE ADMINISTRATOR":    "DBA",
	"TECHNICAL WRITER":          "TECHWRITER",

	// Function/role components
	"ENGINEER":      "ENG",
	"MANAGER":       "MGR",
	"DIRECTOR":      "DIR",
	"DEVELOPER":     "DEV",
	"DESIGNER":      "DSGN",
	"ARCHITECT":     "ARCH",
	"ANALYST":       "ANLST",
	"SPECIALIST":    "SPEC",
	"COORDINATOR":   "COORD",
	"CONSULTANT":    "CONSULT",
	"ADMINISTRATOR": "ADMIN",
	"TECHNICIAN":    "TECH",
	"ASSOCIATE":     "ASSOC",
	"ASSISTANT":     "ASST",

	// Domain/technology areas
	"SOFTWARE":       "SW",
	"HARDWARE":       "HW",
	"PRODUCT":        "PROD",
	"ENGINEERING":    "ENG",
	"TECHNOLOGY":     "TECH",
	"INFORMATION":    "INFO",
	"OPERATIONS":     "OPS",
	"MARKETING":      "MKT",
	"SALES":          "SALES",
	"CUSTOMER":       "CUST",
	"BUSINESS":       "BIZ",
	"DEVELOPMENT":    "DEV",
	"RESEARCH":       "RES",
	"DESIGN":         "DSGN",
	"QUALITY":        "QA",
	"SECURITY":       "SEC",
	"NETWORK":        "NET",
	"SYSTEMS":        "SYS",
	"DATABASE":       "DB",
	"ANALYTICS":      "ANALYTICS",
	"DATA":           "DATA",
	"SCIENCE":        "SCI",
	"MACHINE":        "ML",
	"LEARNING":       "ML",
	"ARTIFICIAL":     "AI",
	"INTELLIGENCE":   "AI",
	"CLOUD":          "CLOUD",
	"INFRASTRUCTURE": "INFRA",
	"PLATFORM":       "PLATFORM",
	"MOBILE":         "MOBILE",
	"WEB":            "WEB",
	"BACKEND":        "BACKEND",
	"FRONTEND":       "FRONTEND",
	"FULLSTACK":      "FULLSTACK",
	"FULL":           "FULL",
	"STACK":          "STACK",

	// Experience levels
	"SENIOR":    "SR",
	"JUNIOR":    "JR",
	"STAFF":     "STF",
	"PRINCIPAL": "PRIN",
	"LEAD":      "LEAD",
	"CHIEF":     "CHIEF",
	"HEAD":      "HEAD",
	"VICE":      "VP",
	"PRESIDENT": "P",
	"EXECUTIVE": "EXEC",
	"OFFICER":   "O",
}

// rolePatterns are common role patterns that should be kept together
var rolePatterns = []string{
	"SOFTWARE ENGINEER", "PRODUCT MANAGER", "DATA SCIENTIST",
	"ENGINEERING MANAGER", "BACKEND DEVELOPER", "FRONTEND DEVELOPER",
	"FULL STACK DEVELOPER", "DEVOPS ENGINEER", "SITE RELIABILITY ENGINEER",
	"MACHINE LEARNING ENGINEER", "QUALITY ASSURANCE", "BUSINESS ANALYST",
	"TECHNICAL PROGRAM MANAGER", "PROGRAM MANAGER",
	"SYSTEMS ADMINISTRATOR", "DATABASE ADMINISTRATOR",
}

// GenerateRoleID generates a vanity ID for a role using its title
func GenerateRoleID(title string, checker ReservedWordsChecker, put func(id string) error) (string, error) {
	seed := BuildRoleSeed(title)
	return AssignID(Role, seed, checker, put)
}

// BuildRoleSeed builds an optimized seed string from a role title
// Filters common words and applies role-specific abbreviations
func BuildRoleSeed(title string) string {
	if title == "" {
		return ""
	}

	// Step 1: Check if entire title matches a common role
	upperTitle := strings.ToUpper(strings.TrimSpace(title))

	// Handle empty or whitespace-only input
	if upperTitle == "" {
		return ""
	}

	if abbr, exists := roleAbbreviations[upperTitle]; exists {
		return abbr
	}

	// Step 2: Handle multi-word role phrases within the title

	processedTitle := upperTitle
	replacements := make(map[string]string)
	placeholderCounter := 0

	// Replace multi-word patterns with single-word placeholders
	for _, pattern := range rolePatterns {
		if strings.Contains(processedTitle, pattern) {
			if abbr, exists := roleAbbreviations[pattern]; exists {
				// Use a single-word placeholder (no spaces)
				placeholder := fmt.Sprintf("PLACEHOLDER%d", placeholderCounter)
				placeholderCounter++
				replacements[placeholder] = abbr
				processedTitle = strings.ReplaceAll(processedTitle, pattern, placeholder)
			}
		}
	}

	// Step 3: Split into words and process remaining words
	words := strings.Fields(processedTitle)
	var result []string

	for _, word := range words {
		// Check if this is a placeholder
		if strings.HasPrefix(word, "PLACEHOLDER") {
			if abbr, exists := replacements[word]; exists {
				result = append(result, abbr)
				continue
			}
		}

		// Skip excluded words
		if roleExcludeWords[word] {
			continue
		}

		// Check if word has an abbreviation
		if abbr, exists := roleAbbreviations[word]; exists {
			result = append(result, abbr)
		} else {
			// Keep the word as-is
			result = append(result, word)
		}
	}

	// Join the processed words
	if len(result) == 0 {
		// If everything was filtered, return original (will be cleaned by cleanSeed)
		return title
	}

	return strings.Join(result, " ")
}
