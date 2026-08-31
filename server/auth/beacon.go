package auth

import "strings"

// A beacon (ADR-034) is a token whose transport is the URL: a public receive
// door that records arrivals as attestations. Everything here is the shared
// vocabulary between the mint, which hands out the path, and the door, which
// answers on it.

// The path a beacon answers on: BeaconPathPrefix + raw + BeaconPathSuffix.
// The suffix makes the URL an image, which is what lets a page carry it as a
// pixel — no CORS, no preflight, no script API between the paper and the node.
const (
	BeaconPathPrefix = "/beacon/"
	BeaconPathSuffix = ".gif"
)

// Arrivals are claims by strangers, so what they carry is capped rather than
// trusted. The subject holds an identifier; the attributes hold short strings.
const (
	maxBeaconSubject        = 128
	maxBeaconAttributes     = 8
	maxBeaconAttributeKey   = 32
	maxBeaconAttributeValue = 128
)

// BeaconSubject builds the attestation subject for an arrival: the beacon's
// vocabulary, a colon, and the local part the caller sent. A beacon writes
// subjects in the vocabulary of its predicate — `card:scanned` speaks about
// `card:{id}` — so the caller names the individual and never the kind.
//
// The empty string means the arrival carries no usable subject. The local
// part is an identifier off a printed thing, so it is letters, digits and
// -_. only; a stranger's string does not get to be anything else.
func BeaconSubject(predicate, local string) string {
	if local == "" || len(local) > maxBeaconSubject {
		return ""
	}
	for _, r := range local {
		alnum := (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9')
		if !alnum && r != '-' && r != '_' && r != '.' {
			return ""
		}
	}
	vocabulary := predicate
	if head, _, found := strings.Cut(predicate, ":"); found {
		vocabulary = head
	}
	return vocabulary + ":" + local
}

// BeaconAttributes is what survives of the query string: every parameter but
// the subject, capped in count and size. Arrivals past the cap lose their
// tail rather than being refused — the arrival is the fact being recorded,
// and a stranger's extra parameters are not a reason to lose it.
func BeaconAttributes(params map[string][]string) map[string]any {
	out := make(map[string]any)
	for key, values := range params {
		if key == "subject" || len(values) == 0 {
			continue
		}
		if len(out) >= maxBeaconAttributes {
			break
		}
		if len(key) > maxBeaconAttributeKey || len(values[0]) > maxBeaconAttributeValue {
			continue
		}
		out[key] = values[0]
	}
	return out
}
