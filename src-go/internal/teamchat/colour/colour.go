// Package colour is the teams' colours on the status line (Plan 00209 B27, Plan 00217
// SPEC-team-colours C1-C7, C10): the palette, the automatic colour a team's name picks, the
// override grammar and the escape a piece is drawn with. It is the standard library alone, so
// the status line, the member files and the admin share one definition.
package colour

import (
	"errors"
	"math"
	"regexp"
	"strconv"
	"strings"
)

// Colour is one palette entry: its override name and its stored hex.
type Colour struct{ Name, Hex string }

// Palette is C1's ten colours: legible on a dark terminal (C2), out of the hues the status
// line already gives a meaning (C3), and each distinct from the rest (C4).
var Palette = [10]Colour{
	{"teal", "#2dd4bf"},
	{"cyan", "#22d3ee"},
	{"sky", "#7dd3fc"},
	{"blue", "#60a5fa"},
	{"indigo", "#a5b4fc"},
	{"violet", "#a78bfa"},
	{"purple", "#d8b4fe"},
	{"magenta", "#e879f9"},
	{"pink", "#f9a8d4"},
	{"mint", "#99f6e4"},
}

// DarkBackgrounds are the typical dark terminal backgrounds every colour, palette or override,
// must be legible on: black, two editor darks, the GNOME and Ptyxis default, and Nord.
var DarkBackgrounds = [5]string{"#000000", "#1e1e1e", "#242424", "#282c34", "#2e3440"}

// MinContrast is WCAG AA's ratio for text (C2).
const MinContrast = 4.5

// Reset ends a coloured piece (C10).
const Reset = "\x1b[0m"

var (
	hexRe    = regexp.MustCompile(`^#[0-9A-Fa-f]{6}$`)
	storedRe = regexp.MustCompile(`^#[0-9a-f]{6}$`)
)

// FNV-1a 32-bit's offset basis and prime (C5).
const (
	fnvOffset = 0x811c9dc5
	fnvPrime  = 0x01000193
)

func fnv1a32(s string) uint32 {
	h := uint32(fnvOffset)
	for i := range len(s) {
		h = (h ^ uint32(s[i])) * fnvPrime
	}
	return h
}

// Auto is the team's automatic colour (C5): the palette entry its name's FNV-1a 32-bit hash
// picks, the same in every session and on every machine.
func Auto(team string) string { return Palette[fnv1a32(team)%uint32(len(Palette))].Hex }

// Parse is an override as stored (C7): a palette name's colour, or a #rrggbb in either case
// legible on every dark background, lower-cased. The refusal is the text the owner reads.
func Parse(v string) (string, error) {
	for _, c := range Palette {
		if v == c.Name {
			return c.Hex, nil
		}
	}
	if !hexRe.MatchString(v) {
		names := make([]string, len(Palette))
		for i, c := range Palette {
			names[i] = c.Name
		}
		return "", errors.New("must be one of " + strings.Join(names, ", ") + ", or #rrggbb")
	}
	hex := strings.ToLower(v)
	ratio, against := math.Inf(1), ""
	for _, bg := range DarkBackgrounds {
		if r := Contrast(hex, bg); r < ratio {
			ratio, against = r, bg
		}
	}
	if ratio < MinContrast {
		return "", errors.New(hex + " is too dark to read on a dark terminal (contrast " +
			strconv.FormatFloat(ratio, 'f', 2, 64) + ":1 against " + against + ", at least " +
			strconv.FormatFloat(MinContrast, 'g', -1, 64) + ":1 needed)")
	}
	return hex, nil
}

// Stored is whether v is a colour as stored: lower-case #rrggbb.
func Stored(v string) bool { return storedRe.MatchString(v) }

// Contrast is the WCAG 2.x contrast ratio of two #rrggbb colours, either case, from 1 to 21
// (C2). Anything that is not such a colour has a ratio of 0, so no legibility check passes it.
func Contrast(a, b string) float64 {
	la, okA := luminance(a)
	lb, okB := luminance(b)
	if !okA || !okB {
		return 0
	}
	return (max(la, lb) + 0.05) / (min(la, lb) + 0.05)
}

// SGR is the 24-bit foreground escape of a #rrggbb colour, either case (C10); "" for anything
// else, so a piece whose colour is not one is drawn plain.
func SGR(hex string) string {
	c, ok := rgb(hex)
	if !ok {
		return ""
	}
	return "\x1b[38;2;" + strconv.Itoa(c[0]) + ";" + strconv.Itoa(c[1]) + ";" + strconv.Itoa(c[2]) + "m"
}

// rgb is a #rrggbb colour's red, green and blue, 0 to 255.
func rgb(hex string) ([3]int, bool) {
	if !hexRe.MatchString(hex) {
		return [3]int{}, false
	}
	n, err := strconv.ParseUint(hex[1:], 16, 32)
	if err != nil {
		return [3]int{}, false
	}
	return [3]int{int(n >> 16 & 0xff), int(n >> 8 & 0xff), int(n & 0xff)}, true
}

// linear is an sRGB channel (0 to 255) linearised, as WCAG's relative luminance takes it.
func linear(channel int) float64 {
	c := float64(channel) / 255
	if c <= 0.04045 {
		return c / 12.92
	}
	return math.Pow((c+0.055)/1.055, 2.4)
}

func luminance(hex string) (float64, bool) {
	c, ok := rgb(hex)
	if !ok {
		return 0, false
	}
	return 0.2126*linear(c[0]) + 0.7152*linear(c[1]) + 0.0722*linear(c[2]), true
}
