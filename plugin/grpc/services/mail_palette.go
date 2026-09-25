package services

import (
	"image/color"
	"math"
	"strconv"
	"strings"

	"github.com/teranos/errors"
)

// ColorOf reads a colour the way web/css writes one, "#rrggbb" or
// "rgba(r, g, b, a)", for what is drawn as pixels rather than styled.
func ColorOf(css string) (color.NRGBA, error) {
	if hex, ok := strings.CutPrefix(css, "#"); ok && len(hex) == 6 {
		n, err := strconv.ParseUint(hex, 16, 32)
		if err != nil {
			return color.NRGBA{}, errors.Wrapf(err, "%q is not a colour", css)
		}
		return color.NRGBA{R: uint8(n >> 16), G: uint8(n >> 8), B: uint8(n), A: 0xff}, nil
	}
	if inner, ok := strings.CutPrefix(css, "rgba("); ok {
		parts := strings.Split(strings.TrimSuffix(inner, ")"), ",")
		if len(parts) != 4 {
			return color.NRGBA{}, errors.Newf("%q is not a colour: rgba takes four parts", css)
		}
		var channels [3]uint8
		for i, p := range parts[:3] {
			n, err := strconv.ParseUint(strings.TrimSpace(p), 10, 8)
			if err != nil {
				return color.NRGBA{}, errors.Wrapf(err, "%q is not a colour", css)
			}
			channels[i] = uint8(n)
		}
		alpha, err := strconv.ParseFloat(strings.TrimSpace(parts[3]), 64)
		if err != nil || alpha < 0 || alpha > 1 {
			return color.NRGBA{}, errors.Newf("%q is not a colour: its alpha is not between 0 and 1", css)
		}
		return color.NRGBA{R: channels[0], G: channels[1], B: channels[2], A: uint8(math.Round(alpha * 0xff))}, nil
	}
	return color.NRGBA{}, errors.Newf("%q is not a colour: neither #rrggbb nor rgba(r, g, b, a)", css)
}
