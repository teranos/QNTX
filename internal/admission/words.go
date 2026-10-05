package admission

import "strings"

// Namespace is what a word ends in to mean every predicate under it: `tag:`
// is every tag there will ever be.
//
// The colon is written down rather than inferred, so a line that says `type`
// says type and nothing that merely starts with it. Widening is a word somebody
// wrote, which is the same shape as `all` on a READ line.
const Namespace = ":"

// Every is the word that means every predicate: `WRITE is * of GROUND`. A
// token that records what happens rather than what a role is for names no
// list, and the line is a word like any other — written down, outranked,
// revoked. "i think * is more clea then all": `all` stays what it is, a word
// on a READ line about whose rows.
const Every = "*"

func permits(words []string, predicate string) bool {
	for _, word := range words {
		if word == predicate || word == Every {
			return true
		}
		if strings.HasSuffix(word, Namespace) && strings.HasPrefix(predicate, word) {
			return true
		}
	}
	return false
}

// Permits reports whether these words permit this predicate, exactly or by the
// namespace one of them names. What MayRead and MayWrite ask, asked by a caller
// holding the words rather than the admission — narrowing a query is the same
// question about the same list.
func Permits(words []string, predicate string) bool { return permits(words, predicate) }

// Names reports whether any of these words is a namespace rather than one
// predicate. A namespace has no literal list — `tag:` is every tag there will
// ever be, and `*` is every predicate — so a read narrowed by one is filtered
// after the store answers rather than handed to it as a filter.
func Names(words []string) bool {
	for _, word := range words {
		if word == Every || strings.HasSuffix(word, Namespace) {
			return true
		}
	}
	return false
}
