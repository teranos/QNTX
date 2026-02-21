package id

import (
	"fmt"
	"strings"
)

// Name processing thresholds
const (
	// Name processing thresholds
	veryShortNameLength = 3 // Names ≤3 chars get special handling
	shortNameLength     = 4 // Names ≤4 chars use different strategies
	mediumNameLength    = 6 // Names ≤6 chars use optimal segment extraction

	// Segment extraction constants
	minSegmentLength = 2 // Minimum meaningful segment length
	fallbackLength   = 3 // Fallback segment length for short names
)

// filterNameParticles removes common name particles/articles from a name
func filterNameParticles(name string) string {
	if name == "" {
		return ""
	}

	// Common name particles to filter (case-insensitive)
	particles := map[string]bool{
		// Dutch/Germanic
		"VAN": true, "VON": true, "VAN DER": true, "VAN DEN": true, "VON DER": true,
		// Romance languages
		"DA": true, "DE": true, "DI": true, "DEL": true, "DELLA": true, "DELLE": true,
		"DU": true, "DES": true, "LA": true, "LE": true, "LOS": true, "LAS": true,
		// Arabic/Spanish articles
		"AL": true, "EL": true,
		// Religious/Geographic
		"SAINT": true, "ST": true, "SAN": true, "SANTA": true,
	}

	// Handle multi-word particles first (case-insensitive removal)
	result := name
	for particle := range particles {
		if strings.Contains(particle, " ") {
			// Case-insensitive replacement
			re := strings.NewReplacer(particle, "", strings.ToLower(particle), "", strings.Title(strings.ToLower(particle)), "")
			result = re.Replace(result)
		}
	}

	// Split into words and filter single-word particles
	words := strings.Fields(result)
	var filteredWords []string
	for _, word := range words {
		upperWord := strings.ToUpper(word)
		if !particles[upperWord] && strings.TrimSpace(word) != "" {
			filteredWords = append(filteredWords, word)
		}
	}

	return strings.Join(filteredWords, " ")
}

// BuildContactSeed builds a seed string from contact first and last name
// Uses bigram/trigram approach for more intuitive name-like IDs
func BuildContactSeed(firstName, lastName string) string {
	if lastName == "" && firstName == "" {
		return ""
	}

	// Filter particles from names before processing
	filteredFirstName := filterNameParticles(firstName)
	filteredLastName := filterNameParticles(lastName)

	var seed strings.Builder

	// Extract meaningful segments from first name
	if filteredFirstName != "" {
		hasLastName := filteredLastName != ""
		firstSegments := extractNameSegments(filteredFirstName, true, hasLastName) // true = prioritize full short names
		seed.WriteString(firstSegments)
	}

	// Extract meaningful segments from last name
	if filteredLastName != "" {
		lastSegments := extractNameSegments(filteredLastName, false, false) // false = use bigrams/trigrams, no need for special handling
		seed.WriteString(lastSegments)
	}

	// Special case: if we only have a first name and the seed is too short, use the full name
	contactMinLength := HumanContact.GetMinLength()
	if filteredLastName == "" && len(seed.String()) < contactMinLength && len(filteredFirstName) >= contactMinLength {
		return strings.ToUpper(filteredFirstName)
	}

	return seed.String()
}

// extractNameSegments extracts meaningful bigrams/trigrams from a name for intuitive IDs
func extractNameSegments(name string, prioritizeFullName bool, hasLastName bool) string {
	if name == "" {
		return ""
	}

	nameUpper := strings.ToUpper(name)

	contactMinLength := HumanContact.GetMinLength()

	// SPECIAL STRATEGY: For long first names (>contactMinLength chars) with a last name, use first+last letter
	// This creates compact representations: "Maarten" → "MN", "Trinity" → "TY"
	if prioritizeFullName && hasLastName && len(nameUpper) > contactMinLength {
		firstLetter := string(nameUpper[0])
		lastLetter := string(nameUpper[len(nameUpper)-1])
		return firstLetter + lastLetter
	}

	// For short-to-medium names (≤contactMinLength chars), try to keep the full name if possible
	if prioritizeFullName && len(nameUpper) <= contactMinLength {
		return nameUpper
	}

	// For longer names, extract meaningful consonant-vowel patterns
	if len(nameUpper) <= mediumNameLength {
		// Medium names: extract first few chars + strategic ending
		if len(nameUpper) <= veryShortNameLength {
			return nameUpper
		}
		// Take first few chars + last consonant for names like "Anderson" → "AND"
		return extractOptimalSegment(nameUpper)
	}

	// For long names, extract the most meaningful bigrams/trigrams
	return extractBigramsTrigramsStrategy(nameUpper)
}

// extractOptimalSegment extracts the most meaningful segment from a medium-length name
func extractOptimalSegment(name string) string {
	if len(name) <= veryShortNameLength {
		return name
	}

	// Strategy: Try to capture the start + a meaningful ending
	// For "Anderson": AN + D → "AND" (better than "ndr")
	// For "Johnson": JO + H → "JOH" or "Johnson" → "JHN"

	// Extract meaningful consonant patterns
	segments := extractMeaningfulBigrams(name)
	if len(segments) > 0 {
		// Prefer segments that start at the beginning of the name
		for _, segment := range segments {
			if strings.HasPrefix(name, segment) && len(segment) >= minSegmentLength {
				return segment
			}
		}
		// Fallback to first segment
		return segments[0]
	}

	// Fallback: first few characters
	if len(name) >= fallbackLength {
		return name[:fallbackLength]
	}
	return name
}

// extractMeaningfulBigrams finds consonant-vowel or consonant-consonant bigrams that preserve name structure
func extractMeaningfulBigrams(name string) []string {
	var segments []string
	runes := []rune(name)

	// Extract bigrams that maintain phonetic structure
	for i := 0; i < len(runes)-1; i++ {
		bigram := string(runes[i : i+2])

		// Prefer consonant-vowel or vowel-consonant patterns
		isCV := !isVowelRune(runes[i]) && isVowelRune(runes[i+1])  // Consonant-Vowel
		isVC := isVowelRune(runes[i]) && !isVowelRune(runes[i+1])  // Vowel-Consonant
		isCC := !isVowelRune(runes[i]) && !isVowelRune(runes[i+1]) // Consonant-Consonant

		if isCV || isVC || (isCC && i == 0) { // CC only at start for names like "Christian"
			segments = append(segments, bigram)
		}
	}

	// Also try trigrams for better representation
	for i := 0; i < len(runes)-2; i++ {
		trigram := string(runes[i : i+3])
		segments = append(segments, trigram)
	}

	return segments
}

// extractBigramsTrigramsStrategy handles long names with systematic bigram/trigram extraction
func extractBigramsTrigramsStrategy(name string) string {
	// For names like "Alexander", "Christopher", "Elizabeth"
	// Extract the most representative segments

	runes := []rune(name)
	if len(runes) <= shortNameLength {
		return name
	}

	// Strategy 1: Start + meaningful middle/end segments
	start := string(runes[:2]) // First 2 chars

	// Find a good ending segment
	var end string
	if len(runes) >= shortNameLength {
		// Look for good consonant combinations in the latter half
		segments := extractMeaningfulBigrams(name[2:]) // Skip first 2 chars
		if len(segments) > 0 {
			end = segments[0]
			if len(end) > 2 {
				end = end[:2] // Limit to 2 chars
			}
		} else {
			// Fallback: last consonant + previous char
			end = string(runes[len(runes)-2:])
		}
	}

	result := start + end

	// Ensure we don't exceed reasonable length for seed (leave room for extensions)
	contactMinLength := HumanContact.GetMinLength()
	maxTruncateLength := contactMinLength - 1 // Maximum length before truncation (leaves room for extensions)
	if len(result) > maxTruncateLength {
		result = result[:maxTruncateLength]
	}

	return result
}

// ExtractConsonants extracts consonants from a name for more name-like IDs
// Exported for use by domain-specific ID generation
func ExtractConsonants(name string) string {
	consonants := "bcdfghjklmnpqrstvwxyzBCDFGHJKLMNPQRSTVWXYZ"
	var result strings.Builder

	for _, r := range name {
		if strings.ContainsRune(consonants, r) {
			result.WriteRune(r)
		}
	}

	return result.String()
}

// AssignContactID assigns a vanity ID for a contact with name-aware collision handling
func AssignContactID(kind EntityKind, seed, firstName, lastName string, checker ReservedWordsChecker, put func(id string) error) (string, error) {
	// Delegate to configurable version using entity kind constraints
	minLength := kind.GetMinLength()
	maxLength := kind.GetMaxLength()
	return AssignContactIDWithLengthConstraints(kind, seed, firstName, lastName, minLength, maxLength, checker, put)
}

// AssignContactIDWithLengthConstraints is like AssignContactID but with custom length constraints
// This function is primarily for testing different length configurations
func AssignContactIDWithLengthConstraints(kind EntityKind, seed, firstName, lastName string, minLength, maxLength int, checker ReservedWordsChecker, put func(id string) error) (string, error) {
	// Clean and validate the seed
	cleanedSeed := cleanSeed(seed)
	if cleanedSeed == "" {
		// Fallback to random ID if no valid seed
		for i := 0; i < getMaxRetries(); i++ {
			randomID, err := GenerateRandomID(minLength)
			if err != nil {
				return "", err
			}
			if err := put(randomID); err == nil {
				return randomID, nil
			}
		}
		return "", fmt.Errorf("failed to assign random ID after %d retries", getMaxRetries())
	}

	// Track attempted IDs to prevent duplicates within this collision session
	attempted := make(map[string]bool)

	// VOWEL-FIRST STRATEGY: Try vowel-rich reconstructions first, then fall back to consonant-only
	for length := minLength; length <= maxLength; length++ {
		// Pad to target length using intelligent padding from original names
		candidateBase := padToMinLengthWithNames(cleanedSeed, length, firstName, lastName)

		// Truncate if too long for this attempt
		if len(candidateBase) > length {
			candidateBase = candidateBase[:length]
		}

		// PRIORITY 1: Try vowel-rich name reconstructions FIRST
		vowelVariations := generateNameAwareVowelVariations(candidateBase, firstName, lastName, maxLength)
		for _, candidateID := range vowelVariations {
			// Skip if doesn't match current length attempt
			if len(candidateID) != length {
				continue
			}

			// Skip if we've already attempted this ID in this session
			if attempted[candidateID] {
				continue
			}
			attempted[candidateID] = true

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

		// PRIORITY 2: Fall back to consonant-only base if vowel-rich forms are taken
		// Skip if we've already attempted this base ID
		if !attempted[candidateBase] {
			attempted[candidateBase] = true

			// Check reserved words for base candidate
			if checker != nil {
				if reserved, err := checker.IsReserved(candidateBase); err != nil {
					return "", fmt.Errorf("failed to check reserved words: %w", err)
				} else if reserved {
					continue // Try next length
				}
			}

			// Try the base candidate at this length as fallback
			if err := put(candidateBase); err == nil {
				return candidateBase, nil
			}
		}
	}

	return "", fmt.Errorf("failed to assign vanity ID after trying all lengths up to %d", maxLength)
}

// generateNameAwareVowelVariations creates variations using reverse vowel strategy:
// Start with vowel-rich reconstructed names, progressively remove vowels on collision
func generateNameAwareVowelVariations(baseID, firstName, lastName string, maxLength int) []string {
	if baseID == "" {
		return []string{}
	}

	// NEW STRATEGY: Start with vowel-inserted names, work backwards to consonant-only
	// For "Voice Mail": VOICE → VOIC → VOC → VC
	// For "Mike Wazowski": MIKE → MIKW → MKW → MKWZ

	// Extract vowels from the original names
	originalVowels := extractVowelsFromNames(firstName, lastName)

	// Generate the full vowel-inserted reconstruction first (highest priority)
	vocalVariations := generateVowelFirstReconstruction(baseID, originalVowels, firstName, lastName, maxLength)

	return vocalVariations
}

// extractVowelsFromNames extracts vowels from first and last name in order of appearance
func extractVowelsFromNames(firstName, lastName string) []string {
	var vowels []string
	seen := make(map[string]bool)

	// Process full name (first + last) to maintain vowel order from the actual name
	fullName := firstName + lastName
	for _, r := range fullName {
		if isVowelRune(r) && r != 'Y' && r != 'y' { // Exclude Y for name vowel extraction
			vowel := strings.ToUpper(string(r))
			if !seen[vowel] {
				vowels = append(vowels, vowel)
				seen[vowel] = true
			}
		}
	}

	// If no vowels found in names, use common English vowels in frequency order
	if len(vowels) == 0 {
		vowels = []string{"E", "A", "I", "O", "U"}
	}

	return vowels
}

// generateVowelFirstReconstruction creates variations starting with full vowel reconstruction
// and progressively removing vowels: VOICE → VOIC → VOC → VC
func generateVowelFirstReconstruction(baseID string, originalVowels []string, firstName, lastName string, maxLength int) []string {
	var variations []string

	if len(originalVowels) == 0 {
		// Fallback to the baseID if no vowels
		return []string{baseID}
	}

	// Cache min length to avoid repeated calls
	minLen := HumanContact.GetMinLength()

	// PRIORITY 0: Use the baseID first if it's meaningful (like BOBJO)
	if len(baseID) >= minLen && len(baseID) <= maxLength {
		// The baseID already combines meaningful parts from both names
		variations = append(variations, baseID)
	}

	// PRIORITY 1: Create the most vowel-rich reconstruction from combined seed
	fullNameReconstruction := createFullNameReconstruction(baseID, originalVowels, firstName, lastName, maxLength)
	if fullNameReconstruction != "" && fullNameReconstruction != baseID {
		variations = appendUnique(variations, fullNameReconstruction)
	}

	// PRIORITY 2: For very short names, add bigram variations
	if len(firstName) == veryShortNameLength {
		firstNameUpper := strings.ToUpper(firstName)
		validName := ConvertToCustomAlphabet(firstNameUpper)

		if len(validName) >= minSegmentLength {
			// Create bigrams: BOB → BO + suffix, OB + suffix
			bigrams := []string{validName[:2], validName[1:]}

			for _, bigram := range bigrams {
				extended := bigram
				// Add suffix to reach minimum length
				suffix := ""
				if lastName != "" && len(baseID) > 2 {
					remaining := baseID[2:]
					neededChars := minLen - len(extended)
					if len(remaining) >= neededChars {
						suffix = remaining[:neededChars]
					} else {
						suffix = remaining
					}
				}
				extended += suffix

				// Pad if still needed
				for len(extended) < minLen && len(originalVowels) > 0 {
					extended += originalVowels[0]
				}
				for len(extended) < minLen {
					extended += "G"
				}

				if len(extended) >= minLen && len(extended) <= maxLength {
					variations = appendUnique(variations, extended)
				}
			}
		}
	}

	// PRIORITY 3: Create progressively vowel-reduced variations
	progressiveVariations := createProgressiveVowelReduction(baseID, originalVowels, firstName, lastName, maxLength)
	for _, variant := range progressiveVariations {
		variations = appendUnique(variations, variant)
	}

	// PRIORITY 4: Ensure the consonant-only baseID is included as final fallback
	variations = appendUnique(variations, baseID)

	return variations
}

// createFullNameReconstruction attempts to create the most readable name form
// For "Mike Wazowski" consonants "MKWZW" + vowels ["I","E","A","O"] → "MIKE" or "MIKEO"
func createFullNameReconstruction(baseID string, originalVowels []string, firstName, lastName string, maxLength int) string {
	// Strategy: Create the most name-like form by intelligently placing vowels

	// Cache min length to avoid repeated calls
	minLen := HumanContact.GetMinLength()

	// PRIORITY 1: For short names (≤minLen chars), try meaningful extensions
	if len(firstName) >= veryShortNameLength && len(firstName) <= minLen {
		firstNameUpper := strings.ToUpper(firstName)
		validName := ConvertToCustomAlphabet(firstNameUpper)

		if len(validName) >= minLen && len(validName) <= maxLength {
			// Perfect! The name meets length requirements as-is
			return validName
		} else if len(validName) >= veryShortNameLength && len(validName) < minLen {
			// Name is valid but too short - extend with vowels instead of padding
			extended := validName

			// Strategy A: Add vowels from the original names
			if len(originalVowels) > 0 {
				vowelIndex := 0
				for len(extended) < minLen && vowelIndex < len(originalVowels) {
					extended += originalVowels[vowelIndex]
					vowelIndex++
				}
			}

			// Strategy B: Minimal fallback if still too short
			for len(extended) < minLen {
				extended += "E" // Use common vowel instead of repetitive padding
			}

			if len(extended) >= minLen && len(extended) <= maxLength {
				return extended
			}
		}
	}

	// PRIORITY 2: For very short names, try bigram combinations
	if len(firstName) == veryShortNameLength {
		firstNameUpper := strings.ToUpper(firstName)
		validName := ConvertToCustomAlphabet(firstNameUpper)

		// Try bigram combinations: BOB → BO, OB, etc.
		if len(validName) >= minSegmentLength {
			bigrams := []string{
				validName[:2], // First 2 chars: "BO" from "BOB"
				validName[1:], // Last 2 chars: "OB" from "BOB"
			}

			for _, bigram := range bigrams {
				if len(bigram) >= 2 {
					// Extend bigram to minimum length with lastName or vowels
					extended := bigram
					if lastName != "" && len(baseID) > 2 {
						// Add from lastName portion of baseID
						remaining := baseID[2:]
						neededChars := minLen - len(extended)
						if len(remaining) >= neededChars {
							extended += remaining[:neededChars]
						} else {
							extended += remaining
						}
					}

					// Pad to minimum if still needed
					for len(extended) < minLen && len(originalVowels) > 0 {
						extended += originalVowels[0]
					}
					for len(extended) < minLen {
						extended += "G"
					}

					if len(extended) >= minLen && len(extended) <= maxLength {
						return extended
					}
				}
			}
		}
	}

	// Approach 2: Create systematic vowel insertion based on consonant structure
	if len(baseID) <= 3 && len(originalVowels) >= 2 {
		// For short consonant strings, create readable forms
		// "MK" + ["I","E"] → "MIKE"
		consonants := []rune(baseID)
		result := string(consonants[0]) // Start with first consonant

		// Insert vowels between consonants
		for i := 1; i < len(consonants) && i <= len(originalVowels); i++ {
			result += originalVowels[i-1]   // Add vowel
			result += string(consonants[i]) // Add next consonant
		}

		// Add final vowel if we have more vowels and space
		if len(result) < maxLength && len(originalVowels) > len(consonants)-1 {
			result += originalVowels[len(consonants)-1]
		}

		if len(result) <= maxLength {
			return result
		}
	}

	// Approach 3: For longer consonant strings, strategic vowel insertion
	if len(baseID) >= shortNameLength {
		runes := []rune(baseID)

		// Insert primary vowel after first consonant: MKWZW → MIKWZW
		if len(originalVowels) > 0 {
			result := string(runes[0]) + originalVowels[0] + string(runes[1:])
			if len(result) <= maxLength {
				// Now try to insert secondary vowel: MIKWZW → MIKEWZW or better placement
				if len(originalVowels) > 1 && len(result)+1 <= maxLength {
					// Strategic placement of second vowel
					midPoint := len(result) / 2
					if midPoint > 1 && midPoint < len(result) {
						final := result[:midPoint] + originalVowels[1] + result[midPoint:]
						if len(final) <= maxLength {
							return final
						}
					}
				}
				return result
			}
		}
	}

	return ""
}

// createProgressiveVowelReduction creates variations by progressively removing vowels
// Starting from a vowel-rich form and working down to consonant-only
func createProgressiveVowelReduction(baseID string, originalVowels []string, firstName, lastName string, maxLength int) []string {
	var variations []string

	// If we have vowels, create intermediate forms
	if len(originalVowels) > 0 {
		// Variation 1: Insert primary vowel only
		primaryVowel := originalVowels[0]
		if len(baseID) >= 1 {
			variation1 := baseID[:1] + primaryVowel + baseID[1:]
			if len(variation1) <= maxLength && variation1 != baseID {
				variations = append(variations, variation1)
			}
		}

		// Variation 2: Replace last consonant with primary vowel
		if len(baseID) >= 2 {
			runes := []rune(baseID)
			runes[len(runes)-1] = rune(primaryVowel[0])
			variation2 := string(runes)
			if len(variation2) <= maxLength && variation2 != baseID {
				variations = append(variations, variation2)
			}
		}

		// Variation 3: If we have multiple vowels, try inserting the second vowel
		if len(originalVowels) > 1 && len(baseID) >= 2 {
			secondVowel := originalVowels[1]
			variation3 := baseID[:2] + secondVowel + baseID[2:]
			if len(variation3) <= maxLength && variation3 != baseID {
				variations = append(variations, variation3)
			}
		}
	}

	return variations
}

// Legacy function kept for compatibility but now redirects to new vowel-first strategy
func generateSmartNameReconstruction(baseID string, originalVowels []string, firstName, lastName string, maxLength int) []string {
	var variations []string

	if len(originalVowels) == 0 {
		return variations
	}

	// Strategy 1: HIGHEST PRIORITY - Create shorter, more name-like forms first
	// For "Mike Wazowski" (MKWZW): create MIKE, MIKW by strategic replacement
	// For "James Sullivan" (JMSLL): create JAMES, JAMSL by strategic replacement
	primaryVowel := originalVowels[0]

	// Replace consonants with primary vowel to create recognizable name patterns
	if len(baseID) >= shortNameLength {
		// Try replacing the last 1-2 consonants to create name-like endings
		// MKWZW → MKWZE → MIKE (if we replace W->I, Z->E)
		runes := []rune(baseID)

		// Replace last consonant with primary vowel: MKWZW → MKWZE
		runes[len(runes)-1] = rune(primaryVowel[0])
		variation := string(runes)
		if variation != baseID && len(variation) <= maxLength {
			variations = append(variations, variation)
		}

		// Replace second-to-last consonant: MKWZW → MKWEW
		if len(runes) >= 2 {
			runes2 := []rune(baseID)
			runes2[len(runes2)-2] = rune(primaryVowel[0])
			variation2 := string(runes2)
			if variation2 != baseID && len(variation2) <= maxLength {
				variations = append(variations, variation2)
			}
		}

		// Create more aggressive name reconstruction
		if len(originalVowels) >= 2 && len(runes) >= 4 {
			// For MKWZW with vowels [I,E], create MIKE: M-I-K-E-W
			nameRunes := []rune(baseID)
			nameRunes[1] = rune(originalVowels[0][0]) // Position 1: I
			nameRunes[3] = rune(originalVowels[1][0]) // Position 3: E
			nameVariation := string(nameRunes)
			if nameVariation != baseID && len(nameVariation) <= maxLength {
				variations = append(variations, nameVariation)
			}
		}
	}

	// Strategy 2: MEDIUM PRIORITY - Progressive vowel insertion at natural positions
	// For "Voice Mail" (VCMLL): VOCMLL → VOICMLL → VOICELL
	for _, vowel := range originalVowels {
		// Insert at position 1 (after first consonant)
		if len(baseID) >= 1 && len(baseID)+1 <= maxLength {
			variation := baseID[:1] + vowel + baseID[1:]
			if len(variation) <= maxLength && variation != baseID {
				variations = append(variations, variation)
			}
		}

		// Insert at position 2 (after second consonant)
		if len(baseID) >= 2 && len(baseID)+1 <= maxLength {
			variation := baseID[:2] + vowel + baseID[2:]
			if len(variation) <= maxLength && variation != baseID {
				variations = append(variations, variation)
			}
		}
	}

	// Strategy 3: LOW PRIORITY - Multiple vowel insertions for progressive reconstruction
	// Only do this if we have room and multiple vowels
	if len(originalVowels) >= 2 && len(baseID)+2 <= maxLength {
		firstVowel := originalVowels[0]
		secondVowel := originalVowels[1]

		// Insert first vowel at pos 1, second vowel at pos 3
		if len(baseID) >= 3 {
			variation := baseID[:1] + firstVowel + baseID[1:2] + secondVowel + baseID[2:]
			if len(variation) <= maxLength && variation != baseID {
				variations = append(variations, variation)
			}
		}

		// For longer IDs, try more complex reconstructions
		if len(baseID) >= 4 && len(originalVowels) >= 3 {
			thirdVowel := originalVowels[2]
			variation := baseID[:1] + firstVowel + baseID[1:2] + secondVowel + baseID[2:3] + thirdVowel + baseID[3:]
			if len(variation) <= maxLength && variation != baseID {
				variations = append(variations, variation)
			}
		}
	}

	return variations
}

// GenerateContactID generates a vanity ID for a contact using their name
func GenerateContactID(firstName, lastName string, checker ReservedWordsChecker, put func(id string) error) (string, error) {
	// Delegate to configurable version using current config constraints
	minLength := HumanContact.GetMinLength()
	maxLength := HumanContact.GetMaxLength()
	return GenerateContactIDWithLengthConstraints(firstName, lastName, minLength, maxLength, checker, put)
}

// GenerateContactIDWithLengthConstraints generates a vanity ID for a contact with custom length constraints
// This function is primarily for testing different length configurations
func GenerateContactIDWithLengthConstraints(firstName, lastName string, minLength, maxLength int, checker ReservedWordsChecker, put func(id string) error) (string, error) {
	seed := BuildContactSeed(firstName, lastName)
	return AssignContactIDWithLengthConstraints(HumanContact, seed, firstName, lastName, minLength, maxLength, checker, put)
}
