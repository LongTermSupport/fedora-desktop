package statusline

import (
	"context"
	"encoding/json"
	"math"
	"path/filepath"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
)

// sampleTeams are the team names the parity test hashes: short, long, digits and hyphens.
var sampleTeams = []string{"alpha", "beta", "gamma", "delta", "dev-team", "a", "x1", "fedora-desktop", "abcdefghijklmnopqrstuvwx"}

// C6: the palette, the backgrounds, the auto colour and the escape are the Python's, read from
// helpers/pingbus/colours.py in this checkout.
func TestThePaletteAndAutoColourAreThePythons(t *testing.T) {
	script, err := filepath.Abs(filepath.Join("testdata", "palette.py"))
	if err != nil {
		t.Fatal(err)
	}
	args := append([]string{"-I", script, checkoutOf(t)}, sampleTeams...)
	res, err := (execx.OS{}).Run(context.Background(), execx.Cmd{Name: "python3", Args: args})
	if err != nil {
		t.Fatalf("the Python palette: %v", err)
	}
	var python struct {
		Palette     [][2]string       `json:"palette"`
		Backgrounds []string          `json:"backgrounds"`
		Auto        map[string]string `json:"auto"`
		SGR         map[string]string `json:"sgr"`
	}
	if err := json.Unmarshal(res.Stdout, &python); err != nil {
		t.Fatal(err)
	}
	if len(python.Palette) != len(palette) {
		t.Fatalf("Python has %d colours, Go %d", len(python.Palette), len(palette))
	}
	for i, entry := range python.Palette {
		if entry != palette[i] {
			t.Errorf("colour %d: Python %v, Go %v", i, entry, palette[i])
		}
	}
	if len(python.Backgrounds) != len(darkBackgrounds) {
		t.Fatalf("backgrounds: Python %v, Go %v", python.Backgrounds, darkBackgrounds)
	}
	for i, bg := range python.Backgrounds {
		if bg != darkBackgrounds[i] {
			t.Errorf("background %d: Python %s, Go %s", i, bg, darkBackgrounds[i])
		}
	}
	for _, team := range sampleTeams {
		if got := autoColour(team); got != python.Auto[team] {
			t.Errorf("%s: Go %s, Python %s", team, got, python.Auto[team])
		}
		if got := sgr(autoColour(team)); got != python.SGR[team] {
			t.Errorf("%s: Go escape %q, Python %q", team, got, python.SGR[team])
		}
	}
}

// C5: FNV-1a 32 over the name's bytes, with its published offset basis and prime.
func TestFNV1a32KnownValues(t *testing.T) {
	cases := map[string]uint32{"": 0x811c9dc5, "a": 0xe40c292c, "foobar": 0xbf9cf968}
	for in, want := range cases {
		if got := fnv1a32([]byte(in)); got != want {
			t.Errorf("%q: got %#x, want %#x", in, got, want)
		}
	}
}

// C2: every palette colour reaches WCAG AA on every reference dark background, computed here
// from linearised sRGB, as the Python test computes it.
func TestEveryColourMeetsWCAGAAOnEveryDarkBackground(t *testing.T) {
	for _, entry := range palette {
		for _, bg := range darkBackgrounds {
			if ratio := contrastRatio(entry[1], bg); ratio < 4.5 {
				t.Errorf("%s %s on %s: %.2f:1", entry[0], entry[1], bg, ratio)
			}
		}
	}
	if got := contrastRatio("#ffffff", "#000000"); math.Abs(got-21) > 1e-9 {
		t.Errorf("white on black: %v", got)
	}
}

// C10: the escape is 24-bit foreground with decimal components; C12: only lower-case #rrggbb
// is a stored colour.
func TestSGRAndStoredColour(t *testing.T) {
	if got := sgr("#2dd4bf"); got != "\x1b[38;2;45;212;191m" {
		t.Errorf("sgr: %q", got)
	}
	for value, want := range map[string]bool{"#2dd4bf": true, "#2DD4BF": false, "teal": false, "#2dd4b": false, "#2dd4bf0": false, "##2dd4bf": false} {
		if got := isStoredColour(value); got != want {
			t.Errorf("%q: got %v", value, got)
		}
	}
}

func contrastRatio(first, second string) float64 {
	a, b := luminance(first), luminance(second)
	return (math.Max(a, b) + 0.05) / (math.Min(a, b) + 0.05)
}

func luminance(hexColour string) float64 {
	r, g, b := channels(hexColour)
	return 0.2126*linear(r) + 0.7152*linear(g) + 0.0722*linear(b)
}

func linear(channel int) float64 {
	c := float64(channel) / 255
	if c <= 0.04045 {
		return c / 12.92
	}
	return math.Pow((c+0.055)/1.055, 2.4)
}
