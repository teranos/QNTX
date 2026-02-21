package id

import (
	"crypto/sha256"
	"fmt"
	"strings"

	"github.com/google/uuid"
)

// GenerateJDASID generates a Job Description ASID
// Format: JD + random(2) + company(5) + random(2) + role(7) + random(2) + location(5) + random(4) + type(3)
// Returns an ID like "JD47ACMEX22ENGINEE33SFBAY5E7AJOB"
//
// The components provide semantic hints while maintaining uniqueness:
//   - company: Helps identify the organization
//   - role: The role title/type
//   - location: Geographic location
//   - type: Job type indicator (default: "JOB")
//
// This format enables:
//   - Quick visual identification of JD source
//   - Deduplication by comparing semantic components
//   - Versioning of the same JD over time (different random parts)
func GenerateJDASID(company, roleTitle, location string) (string, error) {
	// Generate a new UUID for the random components
	u, err := uuid.NewRandom()
	if err != nil {
		return "", fmt.Errorf("failed to generate UUID: %w", err)
	}

	// Get random hex characters from UUID (remove dashes)
	uuidStr := strings.ToUpper(strings.ReplaceAll(u.String(), "-", ""))

	// Extract random segments: 2 + 2 + 2 + 4 = 10 chars total
	rand1 := uuidStr[0:2]  // First 2 chars
	rand2 := uuidStr[2:4]  // Next 2 chars
	rand3 := uuidStr[4:6]  // Next 2 chars
	rand4 := uuidStr[6:10] // Next 4 chars

	// Extract vanity components
	companyPart := extractVanityComponent(company, 5)
	rolePart := extractVanityComponent(roleTitle, 7)
	locationPart := extractVanityComponent(location, 5)
	typePart := "JOB" // Default type indicator for job descriptions

	// Construct the JD ASID - 32 characters total
	// Format: JD(2) + random(2) + company(5) + random(2) + role(7) + random(2) + location(5) + random(4) + type(3) = 32
	jdID := fmt.Sprintf("JD%s%s%s%s%s%s%s%s",
		rand1,
		companyPart,
		rand2,
		rolePart,
		rand3,
		locationPart,
		rand4,
		typePart,
	)

	return jdID, nil
}

// GenerateJDASIDWithRetry generates a JD ASID with collision detection
// The checkExists function should return true if the ID already exists
func GenerateJDASIDWithRetry(company, roleTitle, location string, checkExists func(string) bool) (string, error) {
	const maxRetries = 10

	for i := 0; i < maxRetries; i++ {
		jdID, err := GenerateJDASID(company, roleTitle, location)
		if err != nil {
			return "", err
		}

		if checkExists != nil && checkExists(jdID) {
			continue // Collision detected, retry
		}

		return jdID, nil
	}

	return "", fmt.Errorf("failed to generate unique JD ASID after %d attempts", maxRetries)
}

// extractVanityComponent extracts the specified number of characters from a string, using hash-based padding
func extractVanityComponent(s string, length int) string {
	if len(s) == 0 {
		// For empty strings, generate from hash of empty string
		return generateHashBasedPadding("", length)
	}

	// Clean and uppercase the string, keeping only alphanumeric characters
	var cleaned strings.Builder
	for _, r := range strings.ToUpper(s) {
		if (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			cleaned.WriteRune(r)
		}
	}

	cleanedStr := cleaned.String()
	if len(cleanedStr) == 0 {
		// If no valid characters, generate from hash of original string
		return generateHashBasedPadding(s, length)
	}

	// Take first N characters, pad with hash-based characters if needed
	if len(cleanedStr) >= length {
		return cleanedStr[:length]
	}

	// Need padding - use hash-based approach
	needed := length - len(cleanedStr)
	padding := generateHashBasedPadding(s, needed)
	return cleanedStr + padding
}

// generateHashBasedPadding generates deterministic padding characters from string hash
func generateHashBasedPadding(s string, length int) string {
	if length <= 0 {
		return ""
	}

	// Valid characters for vanity components (alphanumeric, excluding confusing ones)
	validChars := "ABCDEFGHJKLMNPQRSTUVWXYZ23456789" // No I,O,0,1 for clarity

	// Generate hash of the original string
	hash := sha256.Sum256([]byte(s))

	var result strings.Builder
	for i := 0; i < length; i++ {
		// Use different bytes from the hash for each position
		byteIndex := i % len(hash)
		charIndex := int(hash[byteIndex]) % len(validChars)
		result.WriteByte(validChars[charIndex])
	}

	return result.String()
}

// GenerateASIDWithPrefix generates a new ASID with custom prefix and vanity components
// Format: prefix(2) + random(2) + subject(5) + random(2) + predicate(7) + random(2) + context(5) + random(4) + actor(3)
// Returns an ID like "AS47ALICE22FOLLOWS33BOBXY5E7ACLA" or "JD47ALICE22FOLLOWS33BOBXY5E7ACLA"
// The prefix must be exactly 2 characters and will be uppercased
func GenerateASIDWithPrefix(prefix, subject, predicate, context, actor string) (string, error) {
	// Validate and normalize prefix
	prefix = strings.ToUpper(prefix)
	if len(prefix) != 2 {
		return "", fmt.Errorf("prefix must be exactly 2 characters, got %d", len(prefix))
	}

	// Generate a new UUID for the random components
	u, err := uuid.NewRandom()
	if err != nil {
		return "", err
	}

	// Extract vanity components with appropriate lengths
	subjectVanity := extractVanityComponent(subject, 5)
	predicateVanity := extractVanityComponent(predicate, 7)
	contextVanity := extractVanityComponent(context, 5)
	actorVanity := extractVanityComponent(actor, 3)

	// Get random hex characters from UUID (remove dashes)
	uuidStr := strings.ToUpper(strings.ReplaceAll(u.String(), "-", ""))

	// Extract random segments: 2 + 2 + 2 + 4 = 10 chars total
	random1 := uuidStr[0:2]  // First 2 chars
	random2 := uuidStr[2:4]  // Next 2 chars
	random3 := uuidStr[4:6]  // Next 2 chars
	random4 := uuidStr[6:10] // Next 4 chars

	// Construct ASID: prefix(2) + random(2) + subject(5) + random(2) + predicate(7) + random(2) + context(5) + random(4) + actor(3) = 32 chars
	asid := fmt.Sprintf("%s%s%s%s%s%s%s%s%s", prefix, random1, subjectVanity, random2, predicateVanity, random3, contextVanity, random4, actorVanity)

	return asid, nil
}

// GenerateASIDWithVanity generates a new ASID with vanity components from subject, predicate, context, and actor
// Format: AS + random(2) + subject(5) + random(2) + predicate(7) + random(2) + context(5) + random(4) + actor(3)
// Returns an ID like "AS47ALICE22FOLLOWS33BOBXY5E7ACLA"
func GenerateASIDWithVanity(subject, predicate, context, actor string) (string, error) {
	return GenerateASIDWithPrefix("AS", subject, predicate, context, actor)
}

// GenerateASID generates a new ASID with vanity components from subject, predicate, context, and actor
// Format: AS + random(2) + subject(5) + random(2) + predicate(7) + random(2) + context(5) + random(4) + actor(3)
// Returns an ID like "AS47ALICE22FOLLOWS33BOBXY5E7ACLA"
func GenerateASID(subject, predicate, context, actor string) (string, error) {
	return GenerateASIDWithVanity(subject, predicate, context, actor)
}

// GenerateASIDWithPrefixAndRetry generates an ASID with custom prefix, vanity components, and collision detection
// The checkExists function should return true if the ID already exists
func GenerateASIDWithPrefixAndRetry(prefix, subject, predicate, context, actor string, checkExists func(string) bool) (string, error) {
	const maxRetries = 10

	for i := 0; i < maxRetries; i++ {
		asid, err := GenerateASIDWithPrefix(prefix, subject, predicate, context, actor)
		if err != nil {
			return "", err
		}

		// Check if this ID already exists
		if !checkExists(asid) {
			return asid, nil
		}
	}

	// If we get here, we've had too many collisions
	// This is extremely unlikely with UUIDs, so just return the last attempt
	return GenerateASIDWithPrefix(prefix, subject, predicate, context, actor)
}

// GenerateASIDWithVanityAndRetry generates an ASID with vanity components and collision detection
// The checkExists function should return true if the ID already exists
func GenerateASIDWithVanityAndRetry(subject, predicate, context, actor string, checkExists func(string) bool) (string, error) {
	return GenerateASIDWithPrefixAndRetry("AS", subject, predicate, context, actor, checkExists)
}

// GenerateASIDWithRetry generates an ASID with collision detection
// The checkExists function should return true if the ID already exists
func GenerateASIDWithRetry(subject, predicate, context, actor string, checkExists func(string) bool) (string, error) {
	return GenerateASIDWithVanityAndRetry(subject, predicate, context, actor, checkExists)
}

// GenerateJobASID generates an async job ASID
// Format: JB + random(2) + jobType(5) + random(2) + process(7) + random(2) + source(5) + random(4) + actor(3)
// Returns an ID like "JB47JDING22PROCESS33URLXY5E7AGRU"
//
// The components provide semantic information:
//   - jobType: The type of job (jd_ingestion, candidate_scoring, batch_rescore)
//   - process: Fixed "process" predicate
//   - source: The source URL/file identifier
//   - actor: Who requested the job
//
// This format enables:
//   - Quick visual identification of job type
//   - Traceability to source and requester
//   - Consistency with ASID format throughout system
func GenerateJobASID(jobType, source, actor string) (string, error) {
	return GenerateASIDWithPrefix("JB", jobType, "process", source, actor)
}

// GenerateJobASIDWithRetry generates a Job ASID with collision detection
// The checkExists function should return true if the ID already exists
func GenerateJobASIDWithRetry(jobType, source, actor string, checkExists func(string) bool) (string, error) {
	return GenerateASIDWithPrefixAndRetry("JB", jobType, "process", source, actor, checkExists)
}

// GenerateExecutionID generates a Pulse Execution ID
// Format: PX (Pulse Execution) + timestamp + random
func GenerateExecutionID() string {
	return GenerateASIDSimple("PX", "execution", "pulse")
}

// GenerateASIDSimple generates a simple ASID without retry logic (for non-critical IDs)
func GenerateASIDSimple(prefix, subject, context string) string {
	id, _ := GenerateASIDWithPrefix(prefix, subject, "id", context, "")
	return id
}

// IsValidASID checks if a string is a valid ASID format
// Supports any 2-letter prefix (AS, JD, JB, etc.), legacy format (prefix + 30 hex chars),
// and new vanity format with mixed segments
func IsValidASID(id string) bool {
	// Must be 32 characters total
	if len(id) != 32 {
		return false
	}

	// Prefix must be 2 uppercase letters or digits
	for i := 0; i < 2; i++ {
		r := rune(id[i])
		if !((r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9')) {
			return false
		}
	}

	// Check if it's the legacy format (all remaining chars are hex)
	isLegacyFormat := true
	for _, r := range id[2:] {
		if !((r >= '0' && r <= '9') || (r >= 'A' && r <= 'F')) {
			isLegacyFormat = false
			break
		}
	}

	if isLegacyFormat {
		return true
	}

	// If not legacy format, check if it's valid new vanity format
	// Format: prefix(2) + random(2) + subject(5) + random(2) + predicate(7) + random(2) + context(5) + random(4) + actor(3)

	// Check random segments (positions: 2-3, 9-10, 18-19, 25-28) - must be hex
	randomSegments := [][]int{{2, 4}, {9, 11}, {18, 20}, {25, 29}}
	for _, segment := range randomSegments {
		for i := segment[0]; i < segment[1]; i++ {
			r := rune(id[i])
			if !((r >= '0' && r <= '9') || (r >= 'A' && r <= 'F')) {
				return false
			}
		}
	}

	// Check vanity segments (positions: 4-8, 11-17, 20-24, 29-31) - must be alphanumeric
	vanitySegments := [][]int{{4, 9}, {11, 18}, {20, 25}, {29, 32}}
	for _, segment := range vanitySegments {
		for i := segment[0]; i < segment[1]; i++ {
			r := rune(id[i])
			if !((r >= '0' && r <= '9') || (r >= 'A' && r <= 'Z')) {
				return false
			}
		}
	}

	return true
}

// IsVanityASID checks if an ASID uses the new vanity format
func IsVanityASID(id string) bool {
	if !IsValidASID(id) {
		return false
	}

	// Check if it's the legacy format (all remaining chars are hex)
	for _, r := range id[2:] {
		if !((r >= '0' && r <= '9') || (r >= 'A' && r <= 'F')) {
			return true // Contains non-hex, so it's vanity format
		}
	}

	return false // All chars are hex, so it's legacy format
}
