package a2a

import (
	"bytes"
	"encoding/json"
	"io"
	"math"
	"slices"
	"strconv"
	"strings"
	"unicode/utf16"

	errors "github.com/teranos/sacred-error"
)

// Canonical is JSON in the JSON Canonicalization Scheme (RFC 8785), which an
// Agent Card is put in before it is signed (§8.4.1): object members sorted by
// their names as UTF-16 code units, numbers as ECMAScript writes them, strings
// escaped as JSON.stringify escapes them, and no whitespace.
func Canonical(raw []byte) ([]byte, error) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return nil, errors.Wrap(err, "what was to be canonicalized is not JSON")
	}
	if after, err := decoder.Token(); !errors.Is(err, io.EOF) {
		return nil, errors.Newf("what was to be canonicalized is more than one JSON value: %v follows it", after)
	}
	var b bytes.Buffer
	if err := canonical(&b, value); err != nil {
		return nil, err
	}
	return b.Bytes(), nil
}

func canonical(b *bytes.Buffer, value any) error {
	switch v := value.(type) {
	case map[string]any:
		names := make([]string, 0, len(v))
		for name := range v {
			names = append(names, name)
		}
		// RFC 8785 §3.2.3: by the names' UTF-16 code units.
		slices.SortFunc(names, func(a, b string) int {
			return slices.Compare(utf16.Encode([]rune(a)), utf16.Encode([]rune(b)))
		})
		b.WriteByte('{')
		separator := ""
		for _, name := range names {
			b.WriteString(separator)
			separator = ","
			canonicalString(b, name)
			b.WriteByte(':')
			if err := canonical(b, v[name]); err != nil {
				return err
			}
		}
		b.WriteByte('}')
		return nil
	case []any:
		b.WriteByte('[')
		separator := ""
		for _, element := range v {
			b.WriteString(separator)
			separator = ","
			if err := canonical(b, element); err != nil {
				return err
			}
		}
		b.WriteByte(']')
		return nil
	case string:
		canonicalString(b, v)
		return nil
	case json.Number:
		number, err := strconv.ParseFloat(string(v), 64)
		if err != nil {
			return errors.Wrapf(err, "%s is no IEEE 754 double, which RFC 8785 §3.2.2.3 writes every number as", v)
		}
		written, err := canonicalNumber(number)
		if err != nil {
			return err
		}
		b.WriteString(written)
		return nil
	case bool:
		b.WriteString(strconv.FormatBool(v))
		return nil
	case nil:
		// JSON's null is a value, and is written as one.
		b.WriteString("null")
		return nil
	}
	return errors.Newf("%T is no JSON value", value)
}

// escapes is what JSON.stringify writes for the characters it escapes by
// name (RFC 8785 §3.2.2.2).
var escapes = map[rune]string{'"': `\"`, '\\': `\\`, '\b': `\b`, '\f': `\f`, '\n': `\n`, '\r': `\r`, '\t': `\t`}

// canonicalString is a string as ECMAScript's JSON.stringify writes it
// (RFC 8785 §3.2.2.2): the quote, the backslash and the control characters
// escaped, and every other character as itself.
func canonicalString(b *bytes.Buffer, s string) {
	b.WriteByte('"')
	for _, r := range s {
		if escaped, named := escapes[r]; named {
			b.WriteString(escaped)
			continue
		}
		if r < 0x20 {
			b.WriteString(`\u00`)
			b.WriteString(strconv.FormatInt(int64(r)>>4, 16))
			b.WriteString(strconv.FormatInt(int64(r)&0xf, 16))
			continue
		}
		b.WriteRune(r)
	}
	b.WriteByte('"')
}

// canonicalNumber is a double as ECMAScript's Number.prototype.toString writes
// it (RFC 8785 §3.2.2.3): the shortest digits that read back as the same
// double, in plain notation from 1e-6 up to 1e21 and in exponent notation
// past either.
func canonicalNumber(f float64) (string, error) {
	// d.ddde±x: the shortest digits, and the exponent of the first.
	written := strconv.FormatFloat(math.Abs(f), 'e', -1, 64)
	mantissa, exponent, cut := strings.Cut(written, "e")
	if !cut {
		return "", errors.Newf("strconv wrote %s with no exponent, asked for one", written)
	}
	digits := strings.Replace(mantissa, ".", "", 1)
	power, err := strconv.Atoi(exponent)
	if err != nil {
		return "", errors.Wrapf(err, "strconv wrote %s with an exponent it does not read", written)
	}
	// ECMAScript's k and n: how many digits, and where the point falls after
	// the first n of them.
	k, n := len(digits), power+1
	sign := ""
	if math.Signbit(f) && digits != "0" {
		sign = "-"
	}
	switch {
	case k <= n && n <= 21:
		return sign + digits + strings.Repeat("0", n-k), nil
	case 0 < n && n <= 21:
		return sign + digits[:n] + "." + digits[n:], nil
	case -6 < n && n <= 0:
		return sign + "0." + strings.Repeat("0", -n) + digits, nil
	}
	written = digits[:1]
	if k > 1 {
		written += "." + digits[1:]
	}
	if n-1 < 0 {
		return sign + written + "e-" + strconv.Itoa(1-n), nil
	}
	return sign + written + "e+" + strconv.Itoa(n-1), nil
}
