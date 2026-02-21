package id

import (
	"regexp"
	"strings"
)

// prefixRule defines a prefix pattern and its replacement
type prefixRule struct {
	prefix      string
	minLen      int
	replacement string
}

// orgPrefixRules defines prefix patterns for organization name processing
// These are checked in order, so more specific prefixes should come first
var orgPrefixRules = []prefixRule{
	{"ELECTRO", 7, "EL"},
	{"QUANTUM", 7, "Q"},
	{"CRYPTO", 6, "CRYP"},
	{"PHARMA", 5, "PH"},
	{"NEURO", 5, "NEUR"},
	{"MICRO", 5, "M"},
	{"CYBER", 5, "CY"},
	{"GENO", 4, "GEN"},
	{"GENE", 4, "GEN"},
	{"NANO", 4, "N"},
	{"TELE", 4, "TEL"},
	{"AUTO", 4, "AU"},
	{"AERO", 4, "AIR"},
	{"BIO", 3, "BIO"},
	{"ECO", 3, "ECO"},
}

// orgExcludeWords are common words to exclude from organization names (case-insensitive)
var orgExcludeWords = map[string]bool{
	// Articles
	"THE": true, "A": true, "AN": true,
	// Prepositions (various languages)
	"DE": true, "DU": true, "LA": true, "LE": true, "LES": true, "DES": true,
	"VAN": true, "VON": true, "DER": true, "DIE": true, "DAS": true, "OF": true,
	"EL": true, "LOS": true, "LAS": true, "DEL": true, "AL": true,
	"DA": true, "DO": true, "DOS": true,
	// Common business suffixes that might appear at start
	"AND": true, "&": true,
	// Generic qualifiers to ignore
	"NEW": true, "FIRST": true, "REAL": true, "GENERAL": true, "PROFESSIONAL": true,
}

// orgAcronymWords are common long words to convert to acronyms (case-insensitive)
var orgAcronymWords = map[string]string{
	// Institutions
	"UNIVERSITY": "U", "COLLEGE": "C", "INSTITUTE": "I", "ACADEMY": "A",
	"SCHOOL": "S", "FOUNDATION": "F", "ASSOCIATION": "A",
	// Geographic/Political
	"INTERNATIONAL": "I", "NATIONAL": "N", "FEDERAL": "F", "MINISTERIE": "MI", "MINISTRY": "MI",
	"EUROPEAN": "E", "AMERICAN": "A", "BRITISH": "B", "CANADIAN": "C",
	"AUSTRALIAN": "A", "GLOBAL": "G", "WORLDWIDE": "G", "DUTCH": "D", "UNITED": "UN",
	"CENTRAL": "C", "ADVANCED": "A", "INNOVATION": "IN",
	// Cities and locations
	"AMSTERDAM": "AMS", "LONDON": "LON", "CALIFORNIA": "CA", "CITY": "C",
	// Geographic regions
	"ASIAN": "AS", "AFRICAN": "AF", "LATIN": "LA", "PACIFIC": "PAC", "ATLANTIC": "ATL",
	"SCANDINAVIAN": "SC", "MEDITERRANEAN": "MED", "CARIBBEAN": "CAR", "MIDDLE": "MID",
	"EASTERN": "E", "WESTERN": "W", "NORTHERN": "N", "SOUTHERN": "S", "ARCTIC": "ARC",
	// Business types
	"CORPORATION": "C", "CORP": "C", "COMPANY": "C", "LIMITED": "L", "VENTURE": "V",
	"CAPITAL": "C", "INCORPORATED": "I", "INC": "I", "ENTERPRISES": "E",
	"INDUSTRIES": "I", "COLLECTIVE": "C", "LOBBY": "L", "GROUP": "G",
	"PARTNERS": "P", "AGENCY": "A",
	"TECHNOLOGIES": "T", "TECHNOLOGY": "T", "SYSTEMS": "S",
	"SOLUTIONS": "S", "SERVICES": "S", "SERVICE": "S", "CONSULTING": "C",
	"MANAGEMENT": "M", "DEVELOPMENT": "D", "RESEARCH": "R", "MEDIA": "M",
	"NETWORK": "N", "DIGITAL": "D", "BUSINESS": "B", "WORLD": "G", "SECURITY": "S",
	"OFFICE": "O", "STUDIO": "S", "DESIGN": "D", "ONLINE": "O", "MOBILE": "M",
	"CLOUD": "C", "FINANCE": "F", "MARKET": "M", "SCIENCE": "S", "HEALTH": "H",
	"LEGAL": "L", "SOCIAL": "S", "SUPPORT": "S", "TRAINING": "T", "LEARNING": "L",
	"MARKETING": "M", "PUBLISHING": "P", "HOLDINGS": "H", "OPERATIONS": "O",
	// Industry verticals
	"AUTOMOTIVE": "AU", "RETAIL": "RT", "HOSPITALITY": "HO", "AGRICULTURE": "AG",
	"TEXTILE": "TX", "FURNITURE": "FU", "JEWELRY": "JW", "FASHION": "FS",
	"LOGISTICS": "LG", "SHIPPING": "SH", "AVIATION": "AV", "MARITIME": "MAR",
	"ENERGY": "EN", "RENEWABLE": "RN", "PETROLEUM": "PT", "MINING": "MN",
	"ENTERTAINMENT": "ET", "GAMING": "GM", "SPORTS": "SP", "FITNESS": "FT",
	"TOURISM": "TM", "RESTAURANT": "RS", "CATERING": "CT",
	// Specialized terms
	"PROJECT": "P", "INTELLIGENCE": "I",
	"DATA": "D", "BLOCKCHAIN": "B", "DEFENSE": "DEF", "DEFENSIE": "DEF",
	"CROSS": "X", "SPACE": "S",
	// Financial/Business terms
	"INVESTMENT": "INV", "BANKING": "BK", "INSURANCE": "INS", "WEALTH": "WL",
	"PENSION": "PN", "MORTGAGE": "MG", "CREDIT": "CR", "EQUITY": "EQ",
	"ASSET": "AS", "PORTFOLIO": "PF", "FUND": "FD", "TREASURY": "TR",
	"ACCOUNTING": "AC", "AUDIT": "AD", "COMPLIANCE": "CP", "RISK": "RK",
	// Academic/Research terms
	"LABORATORY": "LAB", "OBSERVATORY": "OBS", "LIBRARY": "LIB", "MUSEUM": "MUS",
	"ARCHIVE": "ARC", "STUDY": "ST", "EXPERIMENT": "EXP", "ANALYSIS": "AN",
	"THEORY": "TH", "METHODOLOGY": "MET",
	// Modern business models
	"PLATFORM": "PL", "MARKETPLACE": "MK", "SUBSCRIPTION": "SUB", "STREAMING": "ST",
	"SHARING": "SH", "CROWDSOURCING": "CS", "FREELANCE": "FL", "REMOTE": "RM",
	"VIRTUAL": "VR", "HYBRID": "HY", "ECOSYSTEM": "ECO",
	"ACCELERATOR": "ACC", "INCUBATOR": "INC", "COWORKING": "CW", "STARTUP": "SU",
	// Other long common words
	"ENVIRONMENTAL": "EN", "PHARMACEUTICAL": "PH", "PHARMACEUTICALS": "PH", "MANUFACTURING": "MF",
	"COMMUNICATIONS": "CM", "TRANSPORTATION": "TR", "CONSTRUCTION": "CN",
	"ENGINEERING": "EG", "FINANCIAL": "FN", "HEALTHCARE": "HC",
}

// orgPhraseReplacements are multi-word phrases to replace (case-insensitive)
var orgPhraseReplacements = map[string]string{
	"NEW YORK":                "NY",
	"CYBER SECURITY":          "CS",
	"UNITED STATES":           "US",
	"UNITED KINGDOM":          "UK",
	"MACHINE LEARNING":        "ML",
	"ARTIFICIAL INTELLIGENCE": "AI",
	"VENTURE CAPITAL":         "VC",
	"PRIVATE EQUITY":          "PE",
	"REAL ESTATE":             "RE",
	"HUMAN RESOURCES":         "HR",
	"CUSTOMER SERVICE":        "CS",
	"SUPPLY CHAIN":            "SC",
	"CLOUD COMPUTING":         "CC",
	"DATA SCIENCE":            "DS",
}

// GenerateOrganizationID generates a vanity ID for an organization using its name
func GenerateOrganizationID(name string, checker ReservedWordsChecker, put func(id string) error) (string, error) {
	seed := BuildOrganizationSeed(name)
	return AssignID(Organization, seed, checker, put)
}

// BuildOrganizationSeed builds a seed string from organization name
func BuildOrganizationSeed(name string) string {
	if name == "" {
		return ""
	}
	// Handle multi-word phrases first (case-insensitive)
	processedName := strings.TrimSpace(name)
	// Apply phrase replacements while preserving case for other words
	for phrase, replacement := range orgPhraseReplacements {
		// Case-insensitive replacement but preserve original case for non-matched parts
		re := regexp.MustCompile("(?i)" + regexp.QuoteMeta(phrase))
		processedName = re.ReplaceAllString(processedName, replacement)
	}
	// Split into words and process
	words := strings.Fields(processedName)
	var processedWords []string
	for _, word := range words {
		upperWord := strings.ToUpper(word)
		// Skip excluded words
		if orgExcludeWords[upperWord] {
			continue
		}
		// Check prefix patterns first (priority over exact matches)
		matched := false
		for _, rule := range orgPrefixRules {
			if len(upperWord) >= rule.minLen && strings.HasPrefix(upperWord, rule.prefix) {
				processedWords = append(processedWords, rule.replacement)
				matched = true
				break
			}
		}
		if matched {
			continue
		}
		// Check if word should be converted to acronym
		if acronym, exists := orgAcronymWords[upperWord]; exists {
			processedWords = append(processedWords, acronym)
		} else {
			processedWords = append(processedWords, word)
		}
	}
	// If all words were filtered out, use original name
	if len(processedWords) == 0 {
		return strings.TrimSpace(name)
	}
	return strings.Join(processedWords, " ")
}
