package statusline

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
	"syscall"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/pingbus/pyjson"
)

// The team colours (Plan 00217, SPEC-team-colours.md C1 to C12): a copy of
// helpers/pingbus/colours.py, which TestThePaletteAndAutoColourAreThePythons keeps equal, and the
// reader of the override the syncer keeps in each team's state/colour.json.
const (
	fnvOffset      = 0x811c9dc5
	fnvPrime       = 0x01000193
	colourFile     = "colour.json"
	colourMaxBytes = 4096
	colourUnknown  = "colour?"
	hexDigits      = "0123456789abcdef"
	paletteSize    = 10
)

// palette is colours.PALETTE: name and #rrggbb, in order (C1).
var palette = [paletteSize][2]string{
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

// darkBackgrounds is colours.DARK_BACKGROUNDS, the references of C2's contrast test.
var darkBackgrounds = []string{"#000000", "#1e1e1e", "#242424", "#282c34", "#2e3440"}

var storedColourPattern = regexp.MustCompile(`^#[0-9a-f]{6}$`)

// fnv1a32 is 32-bit FNV-1a of data (C5).
func fnv1a32(data []byte) uint32 {
	value := uint32(fnvOffset)
	for _, b := range data {
		value = (value ^ uint32(b)) * fnvPrime
	}
	return value
}

// autoColour is the team's automatic colour, from its name alone (C5).
func autoColour(team string) string {
	return palette[fnv1a32([]byte(team))%paletteSize][1]
}

// isStoredColour is colours.is_stored_colour: lower-case #rrggbb.
func isStoredColour(value string) bool { return storedColourPattern.MatchString(value) }

// channels is a stored colour's red, green and blue, 0 to 255: every colour drawn is one
// (the palette, or an override isStoredColour accepted).
func channels(hexColour string) (int, int, int) {
	var c [3]int
	for i := range c {
		c[i] = 16*strings.IndexByte(hexDigits, hexColour[1+2*i]) + strings.IndexByte(hexDigits, hexColour[2+2*i])
	}
	return c[0], c[1], c[2]
}

// sgr is the 24-bit foreground escape of a #rrggbb colour (C10).
func sgr(hexColour string) string {
	r, g, b := channels(hexColour)
	return fmt.Sprintf("\x1b[38;2;%d;%d;%dm", r, g, b)
}

// colourOverride is C12's reading of one team's colour.json: the override, "" when there is
// none, and why it cannot be read, "" when it was read or is absent.
func (w *world) colourOverride(path string) (string, string) {
	got, err := w.read(path, true, colourMaxBytes)
	switch {
	case isErrno(err, syscall.ENOENT):
		return "", ""
	case isErrno(err, syscall.ELOOP):
		return "", "the colour file is a symlink"
	case errors.Is(err, errNotRegular):
		return "", "the colour file is not a regular file"
	case errors.Is(err, errTooLarge):
		return "", "the colour file is too large"
	case err != nil:
		return "", "the colour file cannot be read"
	}
	data, err := pyjson.DecodeUTF8(got.raw, pyjson.Options{})
	if err != nil {
		return "", "the colour file is not JSON"
	}
	object, ok := data.(pyjson.Object)
	version, isNumber := object["v"].(pyjson.Number)
	colour, isText := object["colour"].(string)
	if !ok || len(object) != 2 || !isNumber || !version.IsInt(1) || !isText || !isStoredColour(colour) {
		return "", `the colour file is not {"v": 1, "colour": "#rrggbb"}`
	}
	return colour, ""
}

// readColours is each team's colour, its override or else its automatic colour; the teams whose
// override cannot be read; and the one diagnostic line naming them, "" when there are none.
// With no home every team is automatic: the role line already says why (C12).
func (w *world) readColours(env map[string]string, teams []string) (map[string]string, map[string]bool, string) {
	shown := make(map[string]string, len(teams))
	unknown := map[string]bool{}
	for _, team := range teams {
		shown[team] = autoColour(team)
	}
	home, err := resolveHome(env)
	if err != nil {
		return shown, unknown, ""
	}
	var failed []string
	for _, team := range teams {
		override, reason := w.colourOverride(pyJoin(pyJoin(pyJoin(home, team), stateDir), colourFile))
		if reason != "" {
			unknown[team] = true
			failed = append(failed, team+": "+reason)
		}
		if override != "" {
			shown[team] = override
		}
	}
	if len(failed) == 0 {
		return shown, unknown, ""
	}
	return shown, unknown, "pingbus status-line: the colour cannot be read (" + strings.Join(failed, "; ") + ")"
}
