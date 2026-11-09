package colour

import (
	"math"
	"strconv"
	"strings"
	"testing"
)

// C1: the palette, pinned name by name.
func TestThePaletteIsPlan00217s(t *testing.T) {
	want := [10]Colour{
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
	if Palette != want {
		t.Fatalf("palette %v", Palette)
	}
	if DarkBackgrounds != [5]string{"#000000", "#1e1e1e", "#242424", "#282c34", "#2e3440"} || MinContrast != 4.5 {
		t.Fatalf("backgrounds %v, minimum %v", DarkBackgrounds, MinContrast)
	}
}

// C2: every colour reaches 4.5:1 on every dark background, and Contrast is WCAG's ratio.
func TestEveryColourIsLegibleOnEveryDarkBackground(t *testing.T) {
	for _, c := range Palette {
		if !Stored(c.Hex) {
			t.Errorf("%s: %s is not a stored colour", c.Name, c.Hex)
		}
		for _, bg := range DarkBackgrounds {
			if r := Contrast(c.Hex, bg); r < MinContrast {
				t.Errorf("%s on %s: %.2f:1", c.Name, bg, r)
			}
		}
	}
	for _, c := range []struct {
		a, b string
		want float64
	}{
		{"#ffffff", "#000000", 21},
		{"#000000", "#ffffff", 21},
		{"#000000", "#000000", 1},
		{"#FFFFFF", "#000000", 21},
		{"#808080", "#2e3440", 3.16},
	} {
		if got := Contrast(c.a, c.b); math.Abs(got-c.want) > 0.005 {
			t.Errorf("Contrast(%s, %s) = %v, want %v", c.a, c.b, got, c.want)
		}
	}
	// What is not a colour has no contrast, so no legibility check can pass it.
	for _, bad := range []string{"", "#fff", "2dd4bf", "#2dd4bg", "#2dd4bf0", "teal"} {
		if got := Contrast(bad, "#000000"); got != 0 {
			t.Errorf("Contrast(%q) = %v", bad, got)
		}
	}
}

// C3: every hue (HSV) lies from 160 to 335 degrees, away from the line's red, orange, yellow
// and green.
func TestEveryHueKeepsOutOfTheLinesMeanings(t *testing.T) {
	for _, c := range Palette {
		v, ok := rgb(c.Hex)
		if !ok {
			t.Fatalf("%s does not parse", c.Hex)
		}
		if h := hue(float64(v[0])/255, float64(v[1])/255, float64(v[2])/255); h < 160 || h > 335 {
			t.Errorf("%s: hue %.1f", c.Name, h)
		}
	}
}

func hue(r, g, b float64) float64 {
	hi, lo := max(r, g, b), min(r, g, b)
	d := hi - lo
	var h float64
	switch {
	case d == 0:
		return 0
	case hi == r:
		h = math.Mod((g-b)/d, 6)
	case hi == g:
		h = (b-r)/d + 2
	default:
		h = (r-g)/d + 4
	}
	h *= 60
	if h < 0 {
		h += 360
	}
	return h
}

// C4: every pair is at least 15 apart in CIE76 delta E (CIELAB, D65).
func TestEveryPairIsDistinct(t *testing.T) {
	for i, a := range Palette {
		for _, b := range Palette[i+1:] {
			if d := deltaE(t, a.Hex, b.Hex); d < 15 {
				t.Errorf("%s and %s: delta E %.1f", a.Name, b.Name, d)
			}
		}
	}
}

func deltaE(t *testing.T, a, b string) float64 {
	t.Helper()
	p, q := lab(t, a), lab(t, b)
	return math.Sqrt((p[0]-q[0])*(p[0]-q[0]) + (p[1]-q[1])*(p[1]-q[1]) + (p[2]-q[2])*(p[2]-q[2]))
}

// lab is a #rrggbb colour's CIELAB L*, a* and b* under D65.
func lab(t *testing.T, hex string) [3]float64 {
	t.Helper()
	v, ok := rgb(hex)
	if !ok {
		t.Fatalf("%s does not parse", hex)
	}
	lr, lg, lb := linear(v[0]), linear(v[1]), linear(v[2])
	x := (0.4124*lr + 0.3576*lg + 0.1805*lb) / 0.95047
	y := 0.2126*lr + 0.7152*lg + 0.0722*lb
	z := (0.0193*lr + 0.1192*lg + 0.9505*lb) / 1.08883
	f := func(v float64) float64 {
		if v > 216.0/24389 {
			return math.Cbrt(v)
		}
		return (24389.0/27*v + 16) / 116
	}
	return [3]float64{116*f(y) - 16, 500 * (f(x) - f(y)), 200 * (f(y) - f(z))}
}

// C5: FNV-1a 32-bit over the name's bytes, and the palette entry it picks.
func TestTheAutomaticColourIsTheNamesHash(t *testing.T) {
	for in, want := range map[string]uint32{"": 0x811c9dc5, "a": 0xe40c292c, "foobar": 0xbf9cf968} {
		if got := fnv1a32(in); got != want {
			t.Errorf("fnv1a32(%q) = %#x, want %#x", in, got, want)
		}
	}
	// v1's colours.py gives the same colour for each name (Plan 00217).
	for team, want := range map[string]string{
		"alpha": "#e879f9", "beta": "#22d3ee", "gamma": "#a5b4fc", "docs": "#7dd3fc",
		"ops": "#22d3ee", "myteam": "#f9a8d4", "x": "#a78bfa",
	} {
		if got := Auto(team); got != want {
			t.Errorf("Auto(%q) = %s, want %s", team, got, want)
		}
	}
}

// C7: a palette name or a legible #rrggbb in either case, stored lower case.
func TestParseTakesANameOrALegibleHex(t *testing.T) {
	for in, want := range map[string]string{
		"teal": "#2dd4bf", "mint": "#99f6e4", "#2DD4BF": "#2dd4bf", "#2dd4bf": "#2dd4bf", "#FfFfFf": "#ffffff",
	} {
		got, err := Parse(in)
		if err != nil || got != want {
			t.Errorf("Parse(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
}

func TestParseRefusesWithItsText(t *testing.T) {
	names := make([]string, len(Palette))
	for i, c := range Palette {
		names[i] = c.Name
	}
	grammar := "must be one of " + strings.Join(names, ", ") + ", or #rrggbb"
	if grammar != "must be one of teal, cyan, sky, blue, indigo, violet, purple, magenta, pink, mint, or #rrggbb" {
		t.Fatalf("grammar text %q", grammar)
	}
	for in, want := range map[string]string{
		"":          grammar,
		"Teal":      grammar,
		"red":       grammar,
		"#fff":      grammar,
		"2dd4bf":    grammar,
		"#2dd4bg":   grammar,
		"#2dd4bf\n": grammar,
		"#000000":   "#000000 is too dark to read on a dark terminal (contrast 1.00:1 against #000000, at least 4.5:1 needed)",
		"#808080":   "#808080 is too dark to read on a dark terminal (contrast 3.16:1 against #2e3440, at least 4.5:1 needed)",
		"#5A5A5A":   "#5a5a5a is too dark to read on a dark terminal (contrast 1.81:1 against #2e3440, at least 4.5:1 needed)",
	} {
		got, err := Parse(in)
		if err == nil || err.Error() != want || got != "" {
			t.Errorf("Parse(%q) = %q, %v; want the refusal %q", in, got, err, want)
		}
	}
}

func TestStoredIsLowerCaseHex(t *testing.T) {
	for in, want := range map[string]bool{
		"#2dd4bf": true, "#000000": true, "#2DD4BF": false, "teal": false, "": false, "#2dd4b": false, "#2dd4bf\n": false,
	} {
		if got := Stored(in); got != want {
			t.Errorf("Stored(%q) = %v", in, got)
		}
	}
}

// C10: the 24-bit foreground escape, decimal components; nothing for what is not a colour.
func TestSGRIsTheForegroundEscape(t *testing.T) {
	for in, want := range map[string]string{
		"#2dd4bf": "\x1b[38;2;45;212;191m", "#000000": "\x1b[38;2;0;0;0m", "#FFFFFF": "\x1b[38;2;255;255;255m",
		"teal": "", "": "", "#2dd4b": "",
	} {
		if got := SGR(in); got != want {
			t.Errorf("SGR(%q) = %q, want %q", in, got, want)
		}
	}
	if Reset != "\x1b[0m" {
		t.Errorf("Reset %q", Reset)
	}
	// Every palette colour's escape names its components.
	for _, c := range Palette {
		parts := make([]string, 3)
		for i := range parts {
			n, err := strconv.ParseUint(c.Hex[1+2*i:3+2*i], 16, 8)
			if err != nil {
				t.Fatal(err)
			}
			parts[i] = strconv.FormatUint(n, 10)
		}
		if want := "\x1b[38;2;" + strings.Join(parts, ";") + "m"; SGR(c.Hex) != want {
			t.Errorf("%s: %q", c.Name, SGR(c.Hex))
		}
	}
}
