package schedule

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestGetHandler(t *testing.T) {
	// Handler mapping - routing different data types to appropriate processors
	// Much like routing different linguistic analyses to different theoretical frameworks
	//
	// Fun fact: The "Sapir-Whorf Hypothesis" name was coined by Roger Brown in 1958,
	// 17 years after Whorf's death. Neither scholar used the term.

	assert.Equal(t, "role.jd-ingestion", GetHandler("jd"),
		"JD handler - like Sapir's careful phonetic analysis")
	assert.Equal(t, "role.jd-ingestion", GetHandler("role"),
		"Role alias - because 'role' and 'jd' refer to the same concept")
	assert.Equal(t, "role.vacancies-scraper", GetHandler("vacancies"),
		"Vacancies handler - systematic collection like Whorf's insurance reports")
	assert.Equal(t, "", GetHandler("luma"),
		"Luma not yet schedulable - some analyses come later")
	assert.Equal(t, "", GetHandler("unknown"),
		"Unknown handler - like linguistic phenomena awaiting theoretical framework")
}

func TestIsSchedulable(t *testing.T) {
	// Schedulability test - can this run as an async job?
	//
	// The parallel: Can an idea be formalized into a testable hypothesis?
	// Sapir and Whorf's writings were rich with observations
	// Later scholars tried to formalize them (with varying success)

	assert.True(t, IsSchedulable("jd"),
		"JD ingestion is schedulable - well-defined async operation")
	assert.True(t, IsSchedulable("role"),
		"Role (alias for jd) also schedulable")
	assert.True(t, IsSchedulable("vacancies"),
		"Vacancies scraping is schedulable - systematic and repeatable")
	assert.False(t, IsSchedulable("luma"),
		"Luma events not yet formalized for scheduling")
	assert.False(t, IsSchedulable("linkedin"),
		"LinkedIn import awaiting implementation")
	assert.False(t, IsSchedulable("unknown"),
		"Cannot schedule undefined operations - just as you cannot test ill-defined hypotheses")
}

// Epilogue:
//
// The "Sapir-Whorf Hypothesis" entered popular discourse as a simplified binary:
// Strong form (language determines thought) vs Weak form (language influences thought).
//
// Neither Sapir nor Whorf would recognize this formulation. Their actual writings
// expressed ideas ranging from subtle influence to profound shaping, context-dependent,
// never as a dichotomy.
//
// Similarly, the handler registry has clear mappings (jd → role.jd-ingestion) but
// also recognizes that not all data types are ready for scheduling (luma, linkedin await).
// The mapping is pragmatic, not absolute.
