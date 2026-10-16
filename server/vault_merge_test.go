package server

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// "I need to be able to work with it and trust my word always wins, no discussion"

const mergeBase = "# ADR\n\none\ntwo\nthree\nfour\nfive\n"

func TestOnlyTheVaultChangedKeepsTheVault(t *testing.T) {
	vault := "# ADR\n\none\nTWO, mine\nthree\nfour\nfive\n"
	assert.Equal(t, vault, string(mergeVaultWins([]byte(mergeBase), []byte(vault), []byte(mergeBase))))
}

func TestOnlyMainChangedTakesMain(t *testing.T) {
	main := "# ADR\n\none\ntwo\nthree\nFOUR, main's\nfive\n"
	assert.Equal(t, main, string(mergeVaultWins([]byte(mergeBase), []byte(mergeBase), []byte(main))))
}

func TestBothChangedApartKeepsBoth(t *testing.T) {
	vault := "# ADR\n\nONE, mine\ntwo\nthree\nfour\nfive\n"
	main := "# ADR\n\none\ntwo\nthree\nfour\nFIVE, main's\n"
	assert.Equal(t, "# ADR\n\nONE, mine\ntwo\nthree\nfour\nFIVE, main's\n",
		string(mergeVaultWins([]byte(mergeBase), []byte(vault), []byte(main))))
}

func TestBothChangedTheSameLinesKeepsTheVault(t *testing.T) {
	vault := "# ADR\n\none\nTWO, mine\nthree\nfour\nfive\n"
	main := "# ADR\n\none\nTWO, main's\nthree\nfour\nfive\n"
	assert.Equal(t, vault, string(mergeVaultWins([]byte(mergeBase), []byte(vault), []byte(main))))
}

// A change of main's that touches a line the vault changed is not taken: next to
// the vault's words is the vault's to say.
func TestMainTouchingTheVaultsChangeKeepsTheVault(t *testing.T) {
	vault := "# ADR\n\none\nTWO, mine\nthree\nfour\nfive\n"
	main := "# ADR\n\none\ntwo\nTHREE, main's\nfour\nfive\n"
	assert.Equal(t, vault, string(mergeVaultWins([]byte(mergeBase), []byte(vault), []byte(main))))
}

func TestBothAddingAtOnePlaceKeepsTheVault(t *testing.T) {
	vault := "# ADR\n\none\ntwo\nmine, added\nthree\nfour\nfive\n"
	main := "# ADR\n\none\ntwo\nmain's, added\nthree\nfour\nfive\n"
	assert.Equal(t, vault, string(mergeVaultWins([]byte(mergeBase), []byte(vault), []byte(main))))
}

func TestMainRemovingALineTheVaultChangedKeepsTheVault(t *testing.T) {
	vault := "# ADR\n\none\nTWO, mine\nthree\nfour\nfive\n"
	main := "# ADR\n\none\nthree\nfour\nfive\n"
	assert.Equal(t, vault, string(mergeVaultWins([]byte(mergeBase), []byte(vault), []byte(main))))
}

func TestMainRemovingALineElsewhereIsTaken(t *testing.T) {
	vault := "# ADR\n\none\nTWO, mine\nthree\nfour\nfive\n"
	main := "# ADR\n\none\ntwo\nthree\nfour\n"
	assert.Equal(t, "# ADR\n\none\nTWO, mine\nthree\nfour\n",
		string(mergeVaultWins([]byte(mergeBase), []byte(vault), []byte(main))))
}

// A last line with no newline is a line like the others.
func TestALastLineWithoutANewline(t *testing.T) {
	base := "a\nb\nc"
	vault := "A\nb\nc"
	main := "a\nb\nC"
	assert.Equal(t, "A\nb\nC", string(mergeVaultWins([]byte(base), []byte(vault), []byte(main))))
}

func TestNothingChangedIsTheSame(t *testing.T) {
	assert.Equal(t, mergeBase, string(mergeVaultWins([]byte(mergeBase), []byte(mergeBase), []byte(mergeBase))))
}
