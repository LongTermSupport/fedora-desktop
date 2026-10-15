package watermark

import (
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// Settings are the values a watermark is made from, each as the text a config file or the
// command line gave it (M2).
type Settings struct {
	Artist         string
	Copyright      string
	LicenceURL     string
	LicenceSummary string
	Text           string
	Font           string
	OpacityCorner  string
	OpacityTile    string
	TileAngle      string
	SizePctCorner  string
	SizePctTile    string
	Quality        string
}

// Defaults are the presentation values a config file may override; the semantic five have no
// default and must be given (M2, M7).
func Defaults() Settings {
	return Settings{
		Font:          "DejaVu-Sans-Bold",
		OpacityCorner: "0.85",
		OpacityTile:   "0.10",
		TileAngle:     "-30",
		SizePctCorner: "0.030",
		SizePctTile:   "0.018",
		Quality:       "95",
	}
}

// field is the setting a config key names, nil for a key that is not one of the twelve.
func (s *Settings) field(key string) *string {
	fields := map[string]*string{
		"artist":          &s.Artist,
		"copyright":       &s.Copyright,
		"licence_url":     &s.LicenceURL,
		"licence_summary": &s.LicenceSummary,
		"text":            &s.Text,
		"font":            &s.Font,
		"opacity_corner":  &s.OpacityCorner,
		"opacity_tile":    &s.OpacityTile,
		"tile_angle":      &s.TileAngle,
		"size_pct_corner": &s.SizePctCorner,
		"size_pct_tile":   &s.SizePctTile,
		"quality":         &s.Quality,
	}
	return fields[key]
}

var (
	keyName  = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)
	unsigned = regexp.MustCompile(`^([0-9]+(\.[0-9]*)?|\.[0-9]+)$`)
	signed   = regexp.MustCompile(`^-?([0-9]+(\.[0-9]*)?|\.[0-9]+)$`)
	whole    = regexp.MustCompile(`^[0-9]+$`)
)

// errNotKeyValue is a line that is neither blank, a comment, nor KEY=VALUE.
var errNotKeyValue = errors.New("not a KEY=VALUE line")

// ParseConfig reads a config file's text into s (M3): data, never shell. Each line that sets a
// key overrides it; a line that is not KEY=VALUE, names an unknown key or holds anything a
// shell would have run or expanded is an error naming the file and line.
func ParseConfig(path string, data []byte, s *Settings) error {
	for i, line := range strings.Split(string(data), "\n") {
		key, value, ok, err := parseLine(strings.TrimSuffix(line, "\r"))
		if err != nil {
			return fmt.Errorf("%s:%d: %w", path, i+1, err)
		}
		if !ok {
			continue
		}
		target := s.field(key)
		if target == nil {
			return fmt.Errorf("%s:%d: unknown key '%s'", path, i+1, key)
		}
		*target = value
	}
	return nil
}

// parseLine is one line's key and value, and false for a blank or comment line.
func parseLine(line string) (string, string, bool, error) {
	trimmed := strings.TrimLeft(line, " \t")
	if trimmed == "" || trimmed[0] == '#' {
		return "", "", false, nil
	}
	key, rest, found := strings.Cut(trimmed, "=")
	if !found || !keyName.MatchString(key) {
		return "", "", false, errNotKeyValue
	}
	value, tail, err := parseValue(rest)
	if err != nil {
		return "", "", false, err
	}
	if tail != "" {
		if tail[0] != ' ' && tail[0] != '\t' {
			return "", "", false, errors.New("unexpected text after the value")
		}
		if rest := strings.TrimLeft(tail, " \t"); rest != "" && rest[0] != '#' {
			return "", "", false, errors.New("unexpected text after the value")
		}
	}
	return key, value, true, nil
}

// parseValue reads one value from the start of rest and returns it with what follows it.
func parseValue(rest string) (string, string, error) {
	switch {
	case strings.HasPrefix(rest, "'"):
		end := strings.IndexByte(rest[1:], '\'')
		if end < 0 {
			return "", "", errors.New("unterminated single quote")
		}
		return rest[1 : end+1], rest[end+2:], nil
	case strings.HasPrefix(rest, `"`):
		return parseDoubleQuoted(rest[1:])
	default:
		end := strings.IndexAny(rest, " \t")
		if end < 0 {
			end = len(rest)
		}
		word := rest[:end]
		if i := strings.IndexAny(word, "\"'$`\\;&|<>()"); i >= 0 {
			return "", "", fmt.Errorf("a bare value may not hold '%c': quote it", word[i])
		}
		return word, rest[end:], nil
	}
}

// parseDoubleQuoted reads up to the closing quote as a shell would, but expands nothing: the
// escapes \" \\ \$ \` stand for their character, any other backslash is itself, and a bare $
// or backquote is refused rather than run.
func parseDoubleQuoted(rest string) (string, string, error) {
	var b strings.Builder
	for i := 0; i < len(rest); i++ {
		c := rest[i]
		switch c {
		case '"':
			return b.String(), rest[i+1:], nil
		case '$', '`':
			return "", "", errors.New("nothing is expanded: escape $ and ` in double quotes, or use single quotes")
		case '\\':
			if i+1 < len(rest) && strings.IndexByte("\"\\$`", rest[i+1]) >= 0 {
				i++
				c = rest[i]
			}
		}
		b.WriteByte(c)
	}
	return "", "", errors.New("unterminated double quote")
}

// checkPresentation refuses a presentation value magick would misread (M7, B22): each one is
// written into magick's arguments as it stands.
func (s *Settings) checkPresentation() error {
	checks := []struct {
		key, value, kind string
		ok               func(string) bool
	}{
		{"opacity_corner", s.OpacityCorner, "a decimal from 0 to 1", fraction(false)},
		{"opacity_tile", s.OpacityTile, "a decimal from 0 to 1", fraction(false)},
		{"tile_angle", s.TileAngle, "a decimal number of degrees", signed.MatchString},
		{"size_pct_corner", s.SizePctCorner, "a decimal above 0 and at most 1", fraction(true)},
		{"size_pct_tile", s.SizePctTile, "a decimal above 0 and at most 1", fraction(true)},
		{"quality", s.Quality, "a whole number from 1 to 100", percent},
		{"font", s.Font, "a font name", fontName},
	}
	for _, c := range checks {
		if !c.ok(c.value) {
			return fmt.Errorf("%s must be %s, got '%s'", c.key, c.kind, c.value)
		}
	}
	return nil
}

// fraction accepts a decimal from 0 (or above it, when positive) to 1.
func fraction(positive bool) func(string) bool {
	return func(v string) bool {
		if !unsigned.MatchString(v) {
			return false
		}
		f, err := strconv.ParseFloat(v, 64)
		if err != nil || f > 1 {
			return false
		}
		return !positive || f > 0
	}
}

func percent(v string) bool {
	if !whole.MatchString(v) {
		return false
	}
	n, err := strconv.Atoi(v)
	return err == nil && n >= 1 && n <= 100
}

func fontName(v string) bool {
	return v != "" && strings.IndexFunc(v, func(r rune) bool { return r < ' ' || r == 0x7f }) < 0
}
