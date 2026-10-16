package server

// "why cant i have both changes"

// "I need to be able to work with it and trust my word always wins, no discussion"

// Main's change is taken only where the vault did not change, nor beside it; where they meet, the vault's lines stand.

import (
	"sort"
	"strings"

	"github.com/pmezard/go-difflib/difflib"
)

// lineChange is one stretch of the base, base[from:to], said as lines.
type lineChange struct {
	from, to int
	lines    []string
}

// linesOf splits text into lines, each keeping its newline; a last line without one is a line.
func linesOf(text []byte) []string {
	return strings.SplitAfter(string(text), "\n")
}

// changesFrom is every stretch of base that changed into what, in order.
func changesFrom(base, changed []string) []lineChange {
	var out []lineChange
	for _, op := range difflib.NewMatcherWithJunk(base, changed, false, nil).GetOpCodes() {
		if op.Tag == 'e' {
			continue
		}
		out = append(out, lineChange{from: op.I1, to: op.I2, lines: changed[op.J1:op.J2]})
	}
	return out
}

// over writes base[from:to] with changes laid over it, each inside that stretch.
func over(base []string, from, to int, changes []lineChange) []string {
	var out []string
	at := from
	for _, c := range changes {
		out = append(out, base[at:c.from]...)
		out = append(out, c.lines...)
		at = c.to
	}
	return append(out, base[at:to]...)
}

// sideChange is a change, and whether it is the vault's.
type sideChange struct {
	lineChange
	vaults bool
}

// mergeVaultWins is the note holding the vault's changes and main's, against
// what they last agreed on. Where they meet, the vault's lines are the note's.
func mergeVaultWins(base, vault, main []byte) []byte {
	b := linesOf(base)
	var all []sideChange
	for _, c := range changesFrom(b, linesOf(vault)) {
		all = append(all, sideChange{lineChange: c, vaults: true})
	}
	for _, c := range changesFrom(b, linesOf(main)) {
		all = append(all, sideChange{lineChange: c, vaults: false})
	}
	sort.SliceStable(all, func(i, j int) bool { return all[i].from < all[j].from })

	var out []string
	at := 0
	for i := 0; i < len(all); {
		// One change, and every change of either side reaching into or touching the stretch they cover.
		from, to := all[i].from, all[i].to
		vaults := false
		start := i
		for ; i < len(all) && all[i].from <= to; i++ {
			to = max(to, all[i].to)
			vaults = vaults || all[i].vaults
		}
		// The vault's changes where the vault changed anything in the stretch, else main's.
		var side []lineChange
		for _, c := range all[start:i] {
			if c.vaults == vaults {
				side = append(side, c.lineChange)
			}
		}
		out = append(out, b[at:from]...)
		out = append(out, over(b, from, to, side)...)
		at = to
	}
	out = append(out, b[at:]...)
	return []byte(strings.Join(out, ""))
}
