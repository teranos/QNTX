// Package id generates human-readable, memorable vanity identifiers from names and text.
//
// Unlike UUIDs or auto-incrementing integers, vanity IDs are short, professional-looking,
// and easy to communicate verbally (e.g., "SBVH", "JDOE", "ACME").
//
// # Key Concepts
//
// The package supports three types of ID generation:
//
//  1. Vanity IDs: Generated from entity attributes (names, titles, etc.)
//     - Contact IDs: "SBVH", "JDOE" from human names
//     - Organization IDs: "ACME", "NASA" from company names
//     - Role IDs: "SWE", "PM" from job titles
//
//  2. ASIDs (Application-Scoped IDs): Domain-specific structured identifiers
//     - Job Description: "JD-ACME-SWE-NYC-A3B7"
//     - Custom formats with prefix and suffix components
//
//  3. Random IDs: Cryptographically secure random identifiers when no seed is available
//
// # Design Philosophy
//
// Character exclusions: Excludes confusing characters (0/O, 1/I) for clear communication.
// Collision handling: Automatically appends random suffixes when vanity IDs collide.
// Entity-specific constraints: Different min/max lengths per entity type (contacts, orgs, roles).
// Normalization: Converts unicode to ASCII, removes invalid characters, handles typos via NormalizeForLookup.
//
// # Thread Safety
//
// All exported functions are safe for concurrent use. Config changes via SetConfig
// are protected by a mutex and affect subsequent ID generation calls.
//
// # Basic Usage
//
//	import "github.com/teranos/vanity-id"
//
//	// Generate contact ID from name
//	contactID, err := id.GenerateContactID(
//	    id.HumanContact{FirstName: "Jane", LastName: "Doe"},
//	    reservedChecker,
//	    putFunc,
//	)
//	// Result: "JDOE" or "JDOE2K" if collision occurs
//
//	// Generate organization ID
//	orgID, err := id.GenerateOrganizationID("Acme Corp", reservedChecker, putFunc)
//	// Result: "ACME" or similar
//
//	// Normalize user input for lookups (handles typos, case)
//	normalized := id.NormalizeForLookup("jd0e")  // Returns "JDOE"
package id

import (
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"fmt"
	"regexp"
	"strings"
	"sync"
	"unicode"

	"golang.org/x/text/runes"
	"golang.org/x/text/transform"
	"golang.org/x/text/unicode/norm"
)

const (
	// Custom alphabet that's human-readable and avoids confusing characters
	// Excludes: 1 (looks like I), 0 (looks like O)
	// Includes: I, L, O (user preference), all other clear letters and numbers
	customAlphabet = "23456789ABCDEFGHIJKLMNOPQRSTUVWXYZ"
)

// EntityKind represents different types of entities that can have vanity IDs
type EntityKind int

const (
	HumanContact EntityKind = iota
	Organization
	Role
)

// EntityLengthConfig defines min/max length constraints for an entity type
type EntityLengthConfig struct {
	MinLength int
	MaxLength int
}

// Config holds ID generation configuration.
// Use SetConfig to override defaults, or rely on built-in defaults.
type Config struct {
	MaxRetries   int
	Contact      EntityLengthConfig
	Organization EntityLengthConfig
	Role         EntityLengthConfig
}

// DefaultConfig returns the default ID generation configuration.
// These defaults match the original hardcoded values.
func DefaultConfig() *Config {
	return &Config{
		MaxRetries:   128,
		Contact:      EntityLengthConfig{MinLength: 4, MaxLength: 9},
		Organization: EntityLengthConfig{MinLength: 3, MaxLength: 9},
		Role:         EntityLengthConfig{MinLength: 2, MaxLength: 10},
	}
}

var (
	pkgConfig   *Config
	pkgConfigMu sync.RWMutex
)

// SetConfig sets the package-level ID generation configuration.
// Pass nil to reset to defaults. This is safe for concurrent use.
func SetConfig(cfg *Config) {
	pkgConfigMu.Lock()
	defer pkgConfigMu.Unlock()
	pkgConfig = cfg
}

// getConfig returns the current configuration, using defaults if not set.
func getConfig() *Config {
	pkgConfigMu.RLock()
	defer pkgConfigMu.RUnlock()
	if pkgConfig != nil {
		return pkgConfig
	}
	return DefaultConfig()
}

// String returns the string representation of EntityKind
func (k EntityKind) String() string {
	switch k {
	case HumanContact:
		return "contact"
	case Organization:
		return "organization"
	case Role:
		return "role"
	default:
		return "unknown"
	}
}

// GetMinLength returns the minimum vanity ID length for this entity kind
func (k EntityKind) GetMinLength() int {
	cfg := getConfig()
	switch k {
	case HumanContact:
		return cfg.Contact.MinLength
	case Organization:
		return cfg.Organization.MinLength
	case Role:
		return cfg.Role.MinLength
	default:
		return cfg.Contact.MinLength // fallback to contact default
	}
}

// GetMaxLength returns the maximum vanity ID length for this entity kind
func (k EntityKind) GetMaxLength() int {
	cfg := getConfig()
	switch k {
	case HumanContact:
		return cfg.Contact.MaxLength
	case Organization:
		return cfg.Organization.MaxLength
	case Role:
		return cfg.Role.MaxLength
	default:
		return cfg.Contact.MaxLength // fallback to contact default
	}
}

// getMaxRetries returns the maximum number of collision retry attempts
func getMaxRetries() int {
	return getConfig().MaxRetries
}

// ReservedWordsChecker checks if a seed matches reserved words
type ReservedWordsChecker interface {
	IsReserved(seed string) (bool, error)
}

// DatabaseReservedChecker implements ReservedWordsChecker using a database
type DatabaseReservedChecker struct {
	db *sql.DB
}

// NewDatabaseReservedChecker creates a new database-backed reserved words checker
func NewDatabaseReservedChecker(db *sql.DB) *DatabaseReservedChecker {
	return &DatabaseReservedChecker{db: db}
}

// HardcodedReservedChecker implements ReservedWordsChecker using hardcoded reserved words
type HardcodedReservedChecker struct {
	reservedWords map[string]bool
}

// NewHardcodedReservedChecker creates a new hardcoded reserved words checker
func NewHardcodedReservedChecker() *HardcodedReservedChecker {
	reservedWords := []string{
		// System words
		"ADMIN", "ROOT", "TEST", "NULL",

		// Common system terms
		"USER", "API", "CONFIG", "DATA", "SYSTEM", "PUBLIC", "PRIVATE",
		"GUEST", "DEMO", "TEMP",

		// Database terms
		"SELECT", "INSERT", "UPDATE", "DELETE", "CREATE", "DROP", "TABLE", "INDEX",

		// HTTP/Web terms
		"GET", "POST", "PUT", "PATCH", "HEAD", "OPTIONS",

		// Common reserved terms
		"ABOUT", "HELP", "CONTACT", "SUPPORT", "FAQ", "LOGIN", "LOGOUT",
		"SIGNUP", "REGISTER", "PROFILE", "SETTINGS", "SEARCH",

		// Technical terms
		"ERROR", "SUCCESS", "FAILED", "PENDING", "ACTIVE", "INACTIVE",
		"DELETED", "ARCHIVE",
	}

	wordMap := make(map[string]bool, len(reservedWords))
	for _, word := range reservedWords {
		wordMap[strings.ToUpper(word)] = true
	}

	return &HardcodedReservedChecker{reservedWords: wordMap}
}

// IsReserved checks if a seed is in the hardcoded reserved words list
func (c *HardcodedReservedChecker) IsReserved(seed string) (bool, error) {
	return c.reservedWords[strings.ToUpper(seed)], nil
}

// IsReserved checks if a seed is in the reserved words table
func (c *DatabaseReservedChecker) IsReserved(seed string) (bool, error) {
	var exists bool
	err := c.db.QueryRow("SELECT EXISTS(SELECT 1 FROM reserved_ids WHERE word = ?)", strings.ToUpper(seed)).Scan(&exists)
	return exists, err
}

// GenerateVanityID generates a vanity ID from a seed string using legacy constraints
func GenerateVanityID(seed string) (string, error) {
	return GenerateVanityIDForEntity(seed, HumanContact) // Default to contact for backward compatibility
}

// GenerateVanityIDForEntity generates a vanity ID from a seed string with entity-specific constraints
func GenerateVanityIDForEntity(seed string, kind EntityKind) (string, error) {
	if seed == "" {
		return "", fmt.Errorf("seed cannot be empty")
	}

	minLength := kind.GetMinLength()
	maxLength := kind.GetMaxLength()

	// Step 1: Normalize and transliterate
	normalized, err := normalizeToASCII(seed)
	if err != nil {
		return "", fmt.Errorf("failed to normalize seed: %w", err)
	}

	// Step 2: Clean and filter
	cleaned := cleanSeed(normalized)
	if len(cleaned) < 3 {
		// Step 3: Pad with consonants from hash if too short
		cleaned = padWithConsonants(cleaned, seed)
	}

	// Ensure minimum length for this entity type
	if len(cleaned) < minLength {
		cleaned = padToMinLengthForEntity(cleaned, minLength)
	}

	// Ensure maximum length for this entity type
	if len(cleaned) > maxLength {
		cleaned = cleaned[:maxLength]
	}

	return cleaned, nil
}

// GenerateRandomID generates a random Base32 Crockford ID of specified length
func GenerateRandomID(n int) (string, error) {
	if n < 1 {
		return "", fmt.Errorf("length must be positive")
	}

	bytes := make([]byte, (n*5+7)/8) // Calculate bytes needed for n base32 chars
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}

	// Convert bytes to base32 crockford
	result := make([]byte, n)
	bitBuffer := uint64(0)
	bitCount := 0

	byteIdx := 0
	for i := 0; i < n; i++ {
		// Fill buffer if needed
		for bitCount < 5 && byteIdx < len(bytes) {
			bitBuffer = (bitBuffer << 8) | uint64(bytes[byteIdx])
			bitCount += 8
			byteIdx++
		}

		if bitCount < 5 {
			// Not enough bits, pad with zeros
			bitBuffer <<= (5 - bitCount)
			bitCount = 5
		}

		// Extract 5 bits
		result[i] = customAlphabet[(bitBuffer>>(bitCount-5))&31]
		bitCount -= 5
	}

	return string(result), nil
}

// AssignID tries to assign an ID based on entity kind and seed
func AssignID(kind EntityKind, seed string, checker ReservedWordsChecker, put func(id string) error) (string, error) {
	// For human contacts, organizations, and roles, try vanity ID first
	if (kind == HumanContact || kind == Organization || kind == Role) && seed != "" {
		vanityID, err := tryVanityIDForEntity(seed, kind, checker, put)
		if err == nil {
			return vanityID, nil
		}
		// Log vanity failure but continue to random fallback
	}

	// Fallback to random ID using entity-specific minimum length
	minLen := kind.GetMinLength()
	for i := 0; i < getMaxRetries(); i++ {
		randomID, err := GenerateRandomID(minLen)
		if err != nil {
			return "", fmt.Errorf("failed to generate random ID: %w", err)
		}

		if err := put(randomID); err != nil {
			// If it's a conflict error, retry
			continue
		}

		return randomID, nil
	}

	return "", fmt.Errorf("failed to assign ID after %d retries", getMaxRetries())
}

// normalizeToASCII normalizes Unicode and transliterates to ASCII
func normalizeToASCII(s string) (string, error) {
	// NFKD normalization
	t := transform.Chain(norm.NFKD, runes.Remove(runes.In(unicode.Mn)), norm.NFC)
	result, _, err := transform.String(t, s)
	if err != nil {
		return "", err
	}

	// Simple ASCII transliteration for common cases
	result = strings.ReplaceAll(result, "ß", "ss")
	result = strings.ReplaceAll(result, "æ", "ae")
	result = strings.ReplaceAll(result, "œ", "oe")

	return result, nil
}

// cleanSeed cleans the seed according to the algorithm

func cleanSeed(s string) string {
	// Convert to uppercase
	s = strings.ToUpper(s)

	// Keep only alphanumeric characters
	var result strings.Builder
	for _, r := range s {
		if (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			result.WriteRune(r)
		}
	}

	cleaned := result.String()

	// Collapse consecutive repeats
	if len(cleaned) > 1 {
		var collapsed strings.Builder
		prev := rune(0)
		for _, r := range cleaned {
			if r != prev {
				collapsed.WriteRune(r)
				prev = r
			}
		}
		cleaned = collapsed.String()
	}

	// Strip leading digits
	cleaned = regexp.MustCompile(`^[0-9]+`).ReplaceAllString(cleaned, "")

	// Now convert to custom alphabet by mapping excluded chars
	cleaned = ConvertToCustomAlphabet(cleaned)

	return cleaned
}

// ConvertToCustomAlphabet converts characters to valid custom alphabet
// Exported for use by domain-specific ID generation
func ConvertToCustomAlphabet(s string) string {
	var result strings.Builder
	for _, r := range s {
		switch r {
		case '0':
			result.WriteRune('O') // 0 -> O (since we exclude 0 but include O)
		case '1':
			result.WriteRune('I') // 1 -> I (since we exclude 1 but include I)
		default:
			if strings.ContainsRune(customAlphabet, r) {
				result.WriteRune(r)
			}
			// Skip characters not in alphabet
		}
	}
	return result.String()
}

// NormalizeForLookup normalizes user input for ID lookups.
// Converts to uppercase, maps confusing characters (0→O, 1→I),
// and strips invalid characters. Useful for case-insensitive
// and typo-tolerant ID searches.
func NormalizeForLookup(input string) string {
	return ConvertToCustomAlphabet(strings.ToUpper(input))
}

// padWithConsonants pads the cleaned seed with consonants from the original seed hash
func padWithConsonants(cleaned, original string) string {
	consonants := "BCDFGHJKMNPQRSTVWXYZ"

	// Generate deterministic consonants from hash
	hash := sha256.Sum256([]byte(original))

	result := cleaned
	for i := 0; len(result) < 3 && i < len(hash); i++ {
		consonantIdx := int(hash[i]) % len(consonants)
		result += string(consonants[consonantIdx])
	}

	return result
}

// padToMinLength pads to minimum vanity length (legacy function)
func padToMinLength(s string) string {
	return padToMinLengthForEntity(s, HumanContact.GetMinLength())
}

// padToMinLengthForEntity pads to specified minimum length
func padToMinLengthForEntity(s string, minLength int) string {
	if len(s) >= minLength {
		return s
	}

	// Pad with pattern from the string itself or default pattern
	padding := "ABCD"
	if len(s) > 0 {
		// Use last character as padding pattern
		lastChar := string(s[len(s)-1])
		padding = strings.Repeat(lastChar, minLength-len(s))
	}

	return s + padding[:minLength-len(s)]
}

// padToMinLengthWithNames intelligently pads using letters from original names
func padToMinLengthWithNames(s string, minLength int, firstName, lastName string) string {
	if len(s) >= minLength {
		return s
	}

	needed := minLength - len(s)

	// Create a pool of available characters from the original names
	var namePool []rune

	// Add characters from both names, removing what's already used in the seed
	allNames := strings.ToUpper(firstName + lastName)
	usedInSeed := strings.ToUpper(s)

	for _, r := range allNames {
		// Only add valid Crockford Base32 characters that aren't already heavily used in seed
		if strings.ContainsRune(customAlphabet, r) {
			// Count how many times this char appears in seed vs available in names
			countInSeed := strings.Count(usedInSeed, string(r))
			countInNames := strings.Count(allNames, string(r))

			// Add character if it's not over-represented in the seed
			if countInSeed < countInNames {
				namePool = append(namePool, r)
			}
		}
	}

	// If no valid characters from names, fall back to original logic
	if len(namePool) == 0 {
		fallbackPadding := "ABCD"
		if len(s) > 0 {
			lastChar := string(s[len(s)-1])
			fallbackPadding = strings.Repeat(lastChar, needed)
		}
		return s + fallbackPadding[:needed]
	}

	// If we have unused characters from names, use them for padding
	var padding strings.Builder
	poolIndex := 0

	for i := 0; i < needed; i++ {
		if poolIndex < len(namePool) {
			padding.WriteRune(namePool[poolIndex])
			poolIndex++
		} else {
			// Cycle through available characters (safe since we know len(namePool) > 0)
			padding.WriteRune(namePool[poolIndex%len(namePool)])
			poolIndex++
		}
	}

	return s + padding.String()
}

// tryVanityID attempts to assign a vanity ID with collision handling (legacy function)
// Tries progressively longer IDs starting from 5 characters
func tryVanityID(seed string, checker ReservedWordsChecker, put func(id string) error) (string, error) {
	return tryVanityIDForEntity(seed, HumanContact, checker, put) // Default to contact for backward compatibility
}

// tryVanityIDForEntity attempts to assign a vanity ID with entity-specific collision handling
// Uses ALI2/ALI3 pattern for collisions as documented in retrospective
func tryVanityIDForEntity(seed string, kind EntityKind, checker ReservedWordsChecker, put func(id string) error) (string, error) {
	baseID, err := GenerateVanityIDForEntity(seed, kind)
	if err != nil {
		return "", err
	}

	minLength := kind.GetMinLength()
	maxLength := kind.GetMaxLength()

	// For organizations and roles, try progressively longer lengths first, then numeric suffixes
	if kind == Organization || kind == Role {
		// Track attempted IDs to prevent duplicates within this collision session
		attempted := make(map[string]bool)

		// Phase 1: Try all lengths first without numeric suffixes (CYB → CYBER → CYBERD)
		for length := minLength; length <= maxLength; length++ {
			candidateBase := baseID
			if len(candidateBase) < length {
				candidateBase = padToMinLengthForEntity(candidateBase, length)
			}
			if len(candidateBase) > length {
				candidateBase = candidateBase[:length]
			}

			// Skip if we've already attempted this ID in this session
			if attempted[candidateBase] {
				continue
			}
			attempted[candidateBase] = true

			// Check reserved words
			if checker != nil {
				if reserved, err := checker.IsReserved(candidateBase); err != nil {
					return "", fmt.Errorf("failed to check reserved words: %w", err)
				} else if reserved {
					continue // Try next length
				}
			}

			// Try the base candidate
			if err := put(candidateBase); err == nil {
				return candidateBase, nil
			}
		}

		// Phase 2: If all base lengths fail, try numeric suffixes starting from longest possible
		for length := maxLength; length >= minLength; length-- {
			candidateBase := baseID
			if len(candidateBase) < length {
				candidateBase = padToMinLengthForEntity(candidateBase, length)
			}
			if len(candidateBase) > length {
				candidateBase = candidateBase[:length]
			}

			// Try numeric suffixes (reduce base to accommodate suffix)
			if len(candidateBase) >= 2 {
				baseForSuffix := candidateBase[:len(candidateBase)-1] // Remove last char for suffix
				for i := 2; i <= 9; i++ {
					candidateID := baseForSuffix + fmt.Sprintf("%d", i)

					// Skip if we've already attempted this ID in this session
					if attempted[candidateID] {
						continue
					}
					attempted[candidateID] = true

					// Check reserved words for suffixed version
					if checker != nil {
						if reserved, err := checker.IsReserved(candidateID); err != nil {
							return "", fmt.Errorf("failed to check reserved words: %w", err)
						} else if reserved {
							continue // Try next suffix
						}
					}

					if err := put(candidateID); err == nil {
						return candidateID, nil
					}
				}
			}
		}

		return "", fmt.Errorf("failed to assign %s vanity ID within length range %d-%d", kind, minLength, maxLength)
	}

	// For contacts, try progressively longer lengths (5-6 characters)
	for length := minLength; length <= len(baseID) && length <= maxLength; length++ {
		candidateBase := baseID
		if len(candidateBase) > length {
			candidateBase = candidateBase[:length]
		}

		// Check reserved words
		if checker != nil {
			if reserved, err := checker.IsReserved(candidateBase); err != nil {
				return "", fmt.Errorf("failed to check reserved words: %w", err)
			} else if reserved {
				continue // Try next length
			}
		}

		// Try the base candidate at this length
		if err := put(candidateBase); err == nil {
			return candidateBase, nil
		}

		// Try with vowel-based collision resolution at this length
		vowelVariations := generateVowelVariations(candidateBase, maxLength)
		for _, candidateID := range vowelVariations {
			// Check reserved words for vowel variation
			if checker != nil {
				if reserved, err := checker.IsReserved(candidateID); err != nil {
					return "", fmt.Errorf("failed to check reserved words: %w", err)
				} else if reserved {
					continue // Try next variation
				}
			}

			if err := put(candidateID); err == nil {
				return candidateID, nil
			}
		}
	}

	return "", fmt.Errorf("failed to assign vanity ID after trying all lengths up to %d", maxLength)
}

// generateVowelVariations creates name-aware variations by reconstructing the original name
// Uses vowels from the original name to create more readable and authentic variations
func generateVowelVariations(baseID string, maxLength int) []string {
	if baseID == "" {
		return []string{}
	}

	var variations []string

	// Strategy 1: Progressive vowel insertion to reconstruct name-like forms
	// This creates variations like MKWZW → MIKWZ → MIKEW for "Mike Wazowski"
	nameReconstructionVariations := generateNameReconstructionVariations(baseID, maxLength)
	variations = append(variations, nameReconstructionVariations...)

	// Strategy 2: Fallback to generic vowel patterns if needed
	if len(variations) < 10 { // Ensure we have enough variations
		genericVariations := generateGenericVowelVariations(baseID, maxLength)
		variations = append(variations, genericVariations...)
	}

	return variations
}

// generateNameReconstructionVariations creates variations that progressively add vowels
// to reconstruct a more name-like appearance, prioritizing shorter, readable forms
func generateNameReconstructionVariations(baseID string, maxLength int) []string {
	var variations []string

	// Common vowels in order of natural appearance in names
	vowels := []string{"I", "A", "E", "O", "U"} // I first as it's common in names

	// Strategy 1: Smart insertion at natural positions (early in the ID for readability)
	for _, vowel := range vowels {
		// Insert at position 1 (after first consonant) - most natural
		if len(baseID) >= 1 && len(baseID)+1 <= maxLength {
			variation := baseID[:1] + vowel + baseID[1:]
			if len(variation) <= maxLength {
				variations = append(variations, variation)
			}
		}

		// Insert at position 2 (after second consonant) - second most natural
		if len(baseID) >= 2 && len(baseID)+1 <= maxLength {
			variation := baseID[:2] + vowel + baseID[2:]
			if len(variation) <= maxLength {
				variations = append(variations, variation)
			}
		}
	}

	// Strategy 2: Replace consonants with vowels for shorter forms
	if len(baseID) >= 3 {
		for i, vowel := range vowels {
			// Replace 3rd character with vowel (keeps length same)
			if i < 3 && len(baseID) > 2 { // Only first 3 vowels for replacements
				variation := baseID[:2] + vowel + baseID[3:]
				if len(variation) <= maxLength {
					variations = append(variations, variation)
				}
			}
		}
	}

	// Strategy 3: Create truncated + vowel versions (shorter forms)
	if len(baseID) > 4 {
		truncated := baseID[:4]            // Take first 4 consonants
		for _, vowel := range vowels[:3] { // Only use I, A, E for short forms
			variation := truncated + vowel
			if len(variation) <= maxLength && len(variation) >= 5 {
				variations = append(variations, variation)
			}
		}
	}

	return variations
}

// generateGenericVowelVariations provides fallback vowel patterns
func generateGenericVowelVariations(baseID string, maxLength int) []string {
	var variations []string
	primaryVowels := []string{"A", "E", "I", "O", "U"}

	// Append vowels
	for _, vowel := range primaryVowels {
		if len(baseID)+1 <= maxLength {
			variations = append(variations, baseID+vowel)
		}
	}

	// Insert vowels between consonant clusters
	for _, vowel := range primaryVowels {
		insertVariations := insertVowelAtNaturalPositions(baseID, vowel, maxLength)
		variations = append(variations, insertVariations...)
	}

	return variations
}

// insertVowelAtNaturalPositions finds good places to insert vowels
func insertVowelAtNaturalPositions(baseID, vowel string, maxLength int) []string {
	if len(baseID)+1 > maxLength {
		return []string{}
	}

	var variations []string

	// Insert vowel between consonant clusters (makes it more pronounceable)
	for i := 1; i < len(baseID); i++ {
		// Check if we have two consonants next to each other
		if !isVowel(string(baseID[i-1])) && !isVowel(string(baseID[i])) {
			// Insert vowel between them
			newID := baseID[:i] + vowel + baseID[i:]
			if len(newID) <= maxLength {
				variations = append(variations, newID)
			}
		}
	}

	return variations
}

// isVowel checks if a character is a vowel
func isVowel(char string) bool {
	return isVowelRune(rune(strings.ToUpper(char)[0]))
}

// isVowelRune checks if a rune is a vowel (more efficient for rune-based operations)
func isVowelRune(r rune) bool {
	switch r {
	case 'A', 'E', 'I', 'O', 'U', 'Y', 'a', 'e', 'i', 'o', 'u', 'y':
		return true
	}
	return false
}

// appendUnique appends a string to a slice only if it doesn't already exist
func appendUnique(slice []string, item string) []string {
	for _, existing := range slice {
		if existing == item {
			return slice
		}
	}
	return append(slice, item)
}
