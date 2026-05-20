package storage

import "go.uber.org/zap"

// looksLikeID returns a non-empty reason if subject matches an id-like
// heuristic. Subjects should be claim-bearing names, not identifiers.
//
// Active heuristics: UUID shape, ≥16-char hex run, trailing _<digits> or
// -<digits>, all-numeric. Dates (e.g. 2026-05-20) and bare years are
// correctly caught — time belongs in the temporal slot, not the subject.
func looksLikeID(subject string) string {
	if isUUIDShape(subject) {
		return "UUID shape"
	}
	if hasLongHexRun(subject, 16) {
		return "≥16-char hex run"
	}
	if hasTrailingDigitSuffix(subject) {
		return "trailing _<digits> or -<digits>"
	}
	if isAllDigits(subject) {
		return "all-numeric"
	}
	return ""
}

func isUUIDShape(s string) bool {
	if len(s) != 36 {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if i == 8 || i == 13 || i == 18 || i == 23 {
			if c != '-' {
				return false
			}
			continue
		}
		if !isHexByte(c) {
			return false
		}
	}
	return true
}

func hasLongHexRun(s string, minLen int) bool {
	run := 0
	for i := 0; i < len(s); i++ {
		if isHexByte(s[i]) {
			run++
			if run >= minLen {
				return true
			}
		} else {
			run = 0
		}
	}
	return false
}

func hasTrailingDigitSuffix(s string) bool {
	if len(s) < 3 {
		return false
	}
	sep := -1
	for i := len(s) - 1; i >= 0; i-- {
		if s[i] == '_' || s[i] == '-' {
			sep = i
			break
		}
	}
	if sep <= 0 || sep == len(s)-1 {
		return false
	}
	for i := sep + 1; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}

func isAllDigits(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}

func isHexByte(c byte) bool {
	return (c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')
}

func warnIDLikeSubjects(log *zap.SugaredLogger, asID string, subjects []string) {
	if log == nil {
		return
	}
	for _, subj := range subjects {
		if reason := looksLikeID(subj); reason != "" {
			log.Warnw("subject looks id-like; subjects should be claim-bearing names, not identifiers",
				"asid", asID,
				"subject", subj,
				"reason", reason,
			)
		}
	}
}
