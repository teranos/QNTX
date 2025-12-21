package ax

import (
	"testing"
)

// TestSemanticMatcher_RoleMatching tests semantic matching for role queries
// This is what we need for temporal experience queries to work properly
func TestSemanticMatcher_RoleMatching(t *testing.T) {
	testCases := []struct {
		query          string
		roleIdentity   string
		expectedMatch  bool
		similarityNote string
	}{
		// Backend variations - should all match "backend" query
		{"backend", "backend_engineer", true, "exact domain match"},
		{"backend", "api_engineer", true, "APIs are backend work"},
		{"backend", "server_engineer", true, "server is backend"},
		{"backend", "microservices_engineer", true, "microservices often backend"},
		{"backend", "backend_developer", true, "engineer ≈ developer"},

		// Backend should NOT match frontend
		{"backend", "frontend_engineer", false, "completely different domain"},
		{"backend", "ui_engineer", false, "UI is frontend"},
		{"backend", "react_specialist", false, "React is frontend"},

		// DevOps variations - infrastructure/platform/operations are similar
		{"devops", "devops_engineer", true, "exact match"},
		{"devops", "platform_engineer", true, "platform ≈ devops"},
		{"devops", "infrastructure_architect", true, "infrastructure ≈ devops"},
		{"devops", "sre", true, "SRE ≈ devops"},
		{"devops", "site_reliability_engineer", true, "SRE full form"},
		{"devops", "cloud_engineer", true, "cloud ops ≈ devops"},

		// Security variations - cybersecurity, infosec are synonyms
		{"security", "security_engineer", true, "exact match"},
		{"security", "cybersecurity_specialist", true, "cybersecurity ≈ security"},
		{"security", "infosec_analyst", true, "infosec ≈ security"},
		{"security", "penetration_tester", true, "pentesting is security work"},

		// Abbreviations and expansions
		{"ml", "machine_learning_engineer", true, "ML abbreviation"},
		{"ai", "machine_learning_engineer", true, "AI ≈ ML"},
		{"k8s", "kubernetes_administrator", true, "k8s ≈ kubernetes"},

		// Full-stack edge cases
		{"fullstack", "fullstack_engineer", true, "exact match"},
		{"backend", "fullstack_engineer", false, "full-stack is both, not just backend"},
		{"frontend", "fullstack_engineer", false, "full-stack is both, not just frontend"},

		// Data roles
		{"data", "data_engineer", true, "exact domain"},
		{"data", "data_scientist", true, "data domain"},
		{"data", "data_analyst", true, "data domain"},
		{"data", "analytics_engineer", true, "analytics ≈ data"},

		// Mobile variations
		{"mobile", "mobile_engineer", true, "exact match"},
		{"mobile", "ios_developer", true, "iOS is mobile"},
		{"mobile", "android_developer", true, "Android is mobile"},
		{"mobile", "react_native_developer", true, "React Native is mobile"},

		// Negative cases - should NOT match
		{"backend", "qa_engineer", false, "QA is different domain"},
		{"backend", "designer", false, "design is different domain"},
		{"devops", "data_scientist", false, "data science is different"},
		{"security", "frontend_developer", false, "frontend not security"},
	}

	matcher := NewSemanticMatcher()

	for _, tc := range testCases {
		t.Run(tc.query+"_vs_"+tc.roleIdentity, func(t *testing.T) {
			result := matcher.MatchesRole(tc.query, tc.roleIdentity)

			if result != tc.expectedMatch {
				if tc.expectedMatch {
					t.Errorf("Expected %q to match %q (%s), but it didn't",
						tc.query, tc.roleIdentity, tc.similarityNote)
				} else {
					t.Errorf("Expected %q NOT to match %q (%s), but it did",
						tc.query, tc.roleIdentity, tc.similarityNote)
				}
			}
		})
	}
}

// TestSemanticMatcher_RoleHierarchy tests matching against SemanticRole hierarchy
// When query is "backend", it should check all levels of the hierarchy
func TestSemanticMatcher_RoleHierarchy(t *testing.T) {
	testCases := []struct {
		query         string
		hierarchy     []string
		expectedMatch bool
		reason        string
	}{
		{
			query:         "backend",
			hierarchy:     []string{"backend_engineer", "engineer", "backend", "technical_role", "professional"},
			expectedMatch: true,
			reason:        "backend appears in hierarchy",
		},
		{
			query:         "engineer",
			hierarchy:     []string{"backend_engineer", "engineer", "backend", "technical_role", "professional"},
			expectedMatch: true,
			reason:        "engineer appears in hierarchy",
		},
		{
			query:         "api",
			hierarchy:     []string{"backend_engineer", "engineer", "backend", "technical_role", "professional"},
			expectedMatch: true,
			reason:        "api is semantically similar to backend",
		},
		{
			query:         "frontend",
			hierarchy:     []string{"backend_engineer", "engineer", "backend", "technical_role", "professional"},
			expectedMatch: false,
			reason:        "frontend not in backend hierarchy",
		},
		{
			query:         "devops",
			hierarchy:     []string{"platform_engineer", "engineer", "devops", "technical_role", "professional"},
			expectedMatch: true,
			reason:        "devops appears in hierarchy",
		},
		{
			query:         "infrastructure",
			hierarchy:     []string{"platform_engineer", "engineer", "devops", "technical_role", "professional"},
			expectedMatch: true,
			reason:        "infrastructure ≈ platform ≈ devops",
		},
	}

	matcher := NewSemanticMatcher()

	for _, tc := range testCases {
		t.Run(tc.query+"_in_hierarchy", func(t *testing.T) {
			result := matcher.MatchesRoleHierarchy(tc.query, tc.hierarchy)

			if result != tc.expectedMatch {
				if tc.expectedMatch {
					t.Errorf("Expected %q to match hierarchy %v (%s), but it didn't",
						tc.query, tc.hierarchy, tc.reason)
				} else {
					t.Errorf("Expected %q NOT to match hierarchy %v (%s), but it did",
						tc.query, tc.hierarchy, tc.reason)
				}
			}
		})
	}
}

// TestSemanticMatcher_SimilarityScore tests similarity scoring
// This helps us understand how "close" two roles are semantically
func TestSemanticMatcher_SimilarityScore(t *testing.T) {
	testCases := []struct {
		role1         string
		role2         string
		minSimilarity float64 // Minimum expected similarity (0.0 to 1.0)
		maxSimilarity float64 // Maximum expected similarity
		reason        string
	}{
		{"backend_engineer", "backend_engineer", 1.0, 1.0, "exact match"},
		{"backend_engineer", "api_engineer", 0.7, 1.0, "high similarity"},
		{"backend_engineer", "server_engineer", 0.7, 1.0, "high similarity"},
		{"devops_engineer", "platform_engineer", 0.7, 1.0, "high similarity"},
		{"devops_engineer", "infrastructure_architect", 0.6, 1.0, "medium-high similarity"},
		{"backend_engineer", "frontend_engineer", 0.0, 0.4, "low similarity - different domains"},
		{"security_engineer", "frontend_engineer", 0.0, 0.3, "very low similarity"},
		{"ml_engineer", "machine_learning_engineer", 0.8, 1.0, "abbreviation match"},
	}

	matcher := NewSemanticMatcher()

	for _, tc := range testCases {
		t.Run(tc.role1+"_vs_"+tc.role2, func(t *testing.T) {
			score := matcher.SimilarityScore(tc.role1, tc.role2)

			if score < tc.minSimilarity || score > tc.maxSimilarity {
				t.Errorf("Similarity between %q and %q = %.2f, expected between %.2f and %.2f (%s)",
					tc.role1, tc.role2, score, tc.minSimilarity, tc.maxSimilarity, tc.reason)
			}
		})
	}
}

// TestSemanticMatcher_CompanyAliases tests organization name matching
// This is part of Issue #32 but lower priority for temporal queries
func TestSemanticMatcher_CompanyAliases(t *testing.T) {
	testCases := []struct {
		query         string
		companyName   string
		expectedMatch bool
		reason        string
	}{
		{"google", "Google", true, "case insensitive"},
		{"google", "Alphabet", true, "parent company"},
		{"google", "Google UK", true, "geographic variation"},
		{"facebook", "Meta", true, "rebranded company"},
		{"facebook", "Facebook", true, "old name still valid"},
		{"twitter", "X", true, "rebranded company"},
		{"google", "Microsoft", false, "different company"},
	}

	matcher := NewSemanticMatcher()

	for _, tc := range testCases {
		t.Run(tc.query+"_vs_"+tc.companyName, func(t *testing.T) {
			result := matcher.MatchesCompany(tc.query, tc.companyName)

			if result != tc.expectedMatch {
				if tc.expectedMatch {
					t.Errorf("Expected %q to match company %q (%s), but it didn't",
						tc.query, tc.companyName, tc.reason)
				} else {
					t.Errorf("Expected %q NOT to match company %q (%s), but it did",
						tc.query, tc.companyName, tc.reason)
				}
			}
		})
	}
}
