package server

import (
	"bytes"
	"fmt"
	"runtime/pprof"
	"sort"
	"strings"
)

// The pool says how many connections are in use and how many wait, never who.
// A stall that names no holder is one nobody can act on once it has passed.

// inThePool is every goroutine inside database/sql when it is read: the ones
// running a statement and the ones waiting for a connection, each stack once
// with how many goroutines stand on it, most first.
//
// A connection held between statements is held from outside database/sql, so it is not here.
func inThePool() []string {
	var dump bytes.Buffer
	if err := pprof.Lookup("goroutine").WriteTo(&dump, 2); err != nil {
		return []string{"the goroutines could not be read: " + err.Error()}
	}
	return poolStacks(dump.String())
}

// poolStacks reads a goroutine dump: the stacks that pass through
// database/sql, without each goroutine's header, counted.
func poolStacks(dump string) []string {
	counted := map[string]int{}
	for _, block := range strings.Split(dump, "\n\n") {
		if !strings.Contains(block, "database/sql.") {
			continue
		}
		header, stack, cut := strings.Cut(block, "\n")
		if !cut {
			continue
		}
		state := header
		if open := strings.Index(header, "["); open >= 0 {
			state = strings.TrimSuffix(header[open:], ":")
		}
		counted[state+"\n"+stack]++
	}
	stacks := make([]string, 0, len(counted))
	for stack := range counted {
		stacks = append(stacks, stack)
	}
	sort.Slice(stacks, func(i, j int) bool {
		if counted[stacks[i]] != counted[stacks[j]] {
			return counted[stacks[i]] > counted[stacks[j]]
		}
		return stacks[i] < stacks[j]
	})
	said := make([]string, 0, len(stacks))
	for _, stack := range stacks {
		said = append(said, fmt.Sprintf("%d goroutines %s", counted[stack], stack))
	}
	return said
}
