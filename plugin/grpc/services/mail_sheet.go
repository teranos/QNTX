package services

import (
	"io/fs"
	"strings"

	"github.com/teranos/errors"
)

// sheet is what QNTX's stylesheets say: the tokens :root sets, and the
// properties each rule sets. A mail reads its values from here, so it is drawn
// in whatever web/css holds when the binary is built.
type sheet struct {
	tokens map[string]string
	rules  map[string]map[string]string
}

// readSheet reads stylesheets in order; a later rule for the same selector
// adds to an earlier one. Rules inside an at-rule (@media, @keyframes) are not
// read: a mail has no pointer and no animation.
func readSheet(fsys fs.FS, paths ...string) (sheet, error) {
	s := sheet{tokens: map[string]string{}, rules: map[string]map[string]string{}}
	for _, path := range paths {
		src, err := fs.ReadFile(fsys, path)
		if err != nil {
			return sheet{}, errors.Wrapf(err, "failed to read stylesheet %s", path)
		}
		if err := s.add(withoutComments(string(src))); err != nil {
			return sheet{}, errors.Wrapf(err, "stylesheet %s does not read", path)
		}
	}
	return s, nil
}

func withoutComments(css string) string {
	var b strings.Builder
	for {
		open := strings.Index(css, "/*")
		if open < 0 {
			b.WriteString(css)
			return b.String()
		}
		b.WriteString(css[:open])
		end := strings.Index(css[open+2:], "*/")
		if end < 0 {
			return b.String()
		}
		css = css[open+2+end+2:]
	}
}

func (s sheet) add(css string) error {
	for {
		open := strings.IndexByte(css, '{')
		if open < 0 {
			return nil
		}
		prelude := strings.TrimSpace(css[:open])
		end, closed := closing(css, open+1, "{", "}")
		if !closed {
			return errors.Newf("the block after %q is not closed", prelude)
		}
		body := css[open+1 : end]
		css = css[end+1:]
		if strings.HasPrefix(prelude, "@") {
			continue
		}
		for _, selector := range strings.Split(prelude, ",") {
			selector = strings.Join(strings.Fields(selector), " ")
			props, seen := s.rules[selector]
			if !seen {
				props = map[string]string{}
				s.rules[selector] = props
			}
			for _, decl := range strings.Split(body, ";") {
				name, value, ok := strings.Cut(decl, ":")
				if !ok {
					continue
				}
				name, value = strings.TrimSpace(name), strings.Join(strings.Fields(value), " ")
				props[name] = value
				if selector == ":root" && strings.HasPrefix(name, "--") {
					s.tokens[name] = value
				}
			}
		}
	}
}

// prop is a property a rule sets, its var()s resolved.
func (s sheet) prop(selector, property string) (string, error) {
	value, ok := s.rules[selector][property]
	if !ok {
		return "", errors.Newf("no rule %q sets %s", selector, property)
	}
	resolved, err := s.resolve(value)
	if err != nil {
		return "", errors.Wrapf(err, "%s of %q", property, selector)
	}
	return resolved, nil
}

// token is a token :root sets, its var()s resolved.
func (s sheet) token(name string) (string, error) {
	value, ok := s.tokens[name]
	if !ok {
		return "", errors.Newf(":root sets no %s", name)
	}
	return s.resolve(value)
}

// resolve replaces each var(--name) with the token, or with the fallback the
// var() names when :root sets no such token, as a browser does.
func (s sheet) resolve(value string) (string, error) {
	for {
		at := strings.Index(value, "var(")
		if at < 0 {
			return value, nil
		}
		end, closed := closing(value, at+len("var("), "(", ")")
		if !closed {
			return "", errors.Newf("%q does not close its var(", value)
		}
		name, fallback, hasFallback := strings.Cut(value[at+len("var("):end], ",")
		name = strings.TrimSpace(name)
		replacement, set := s.tokens[name]
		if !set && !hasFallback {
			return "", errors.Newf(":root sets no %s, and %q names no fallback", name, value)
		}
		if !set {
			replacement = strings.TrimSpace(fallback)
		}
		value = value[:at] + replacement + value[end+1:]
	}
}

// closing is where a bracket opened just before from is closed: the first shut
// past from with as many opens as shuts between them. False is a bracket that
// is never closed.
func closing(text string, from int, open, shut string) (int, bool) {
	for at := from; ; at++ {
		next := strings.Index(text[at:], shut)
		if next < 0 {
			return at, false
		}
		at += next
		inside := text[from:at]
		if strings.Count(inside, open) == strings.Count(inside, shut) {
			return at, true
		}
	}
}
