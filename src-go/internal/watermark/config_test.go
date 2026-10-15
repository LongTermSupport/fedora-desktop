package watermark

import (
	"strings"
	"testing"
)

func TestM2DefaultsAreThePresentationValuesAndNoSemanticOnes(t *testing.T) {
	s := Defaults()
	want := Settings{
		Font: "DejaVu-Sans-Bold", OpacityCorner: "0.85", OpacityTile: "0.10", TileAngle: "-30",
		SizePctCorner: "0.030", SizePctTile: "0.018", Quality: "95",
	}
	if s != want {
		t.Fatalf("got %+v", s)
	}
}

func TestM3ReadsEveryKnownKeyInEachQuotingForm(t *testing.T) {
	text := strings.Join([]string{
		"# a comment",
		"",
		"   ",
		`artist="Jane \"JD\" Doe"`,
		`copyright='(c) 2026 Jane $HOME'`,
		"licence_url=https://example.com/licence",
		`licence_summary="All rights reserved"   # trailing comment`,
		`text=""`,
		"  font=DejaVu-Sans",
		"opacity_corner=0.5",
		"opacity_tile='0.2'",
		`tile_angle="-45"`,
		"size_pct_corner=0.04",
		"size_pct_tile=0.02 ",
		`quality="90"`,
		`ignored_backslash="a\nb \$x \` + "`" + ` \\"`,
	}, "\n")
	s := Defaults()
	err := ParseConfig("/c.conf", []byte(strings.Replace(text, "ignored_backslash", "text", 1)), &s)
	if err != nil {
		t.Fatal(err)
	}
	want := Settings{
		Artist: `Jane "JD" Doe`, Copyright: "(c) 2026 Jane $HOME", LicenceURL: "https://example.com/licence",
		LicenceSummary: "All rights reserved", Text: `a\nb $x ` + "`" + ` \`, Font: "DejaVu-Sans",
		OpacityCorner: "0.5", OpacityTile: "0.2", TileAngle: "-45", SizePctCorner: "0.04",
		SizePctTile: "0.02", Quality: "90",
	}
	if s != want {
		t.Fatalf("got  %+v\nwant %+v", s, want)
	}
}

func TestM3AFileOnlySetsTheKeysItNames(t *testing.T) {
	s := Defaults()
	s.Artist = "earlier"
	if err := ParseConfig("/c.conf", []byte("copyright=me\n"), &s); err != nil {
		t.Fatal(err)
	}
	if s.Artist != "earlier" || s.Copyright != "me" || s.Font != "DejaVu-Sans-Bold" {
		t.Fatalf("got %+v", s)
	}
}

func TestM3RefusesWhatASourcedFileWouldHaveRunOrExpanded(t *testing.T) {
	cases := map[string]string{
		"artist":                      "/c.conf:1: not a KEY=VALUE line",
		"=x":                          "/c.conf:1: not a KEY=VALUE line",
		"export artist=x":             "/c.conf:1: not a KEY=VALUE line",
		"output=/tmp/x":               "/c.conf:1: unknown key 'output'",
		"artist=Jane Doe":             "/c.conf:1: unexpected text after the value",
		`artist="$USER"`:              "/c.conf:1: nothing is expanded: escape $ and ` in double quotes, or use single quotes",
		"artist=\"`id`\"":             "/c.conf:1: nothing is expanded: escape $ and ` in double quotes, or use single quotes",
		"artist=$(id)":                "/c.conf:1: a bare value may not hold '$': quote it",
		"artist=a;rm":                 "/c.conf:1: a bare value may not hold ';': quote it",
		`artist="open`:                "/c.conf:1: unterminated double quote",
		"artist='open":                "/c.conf:1: unterminated single quote",
		`artist="a"b`:                 "/c.conf:1: unexpected text after the value",
		"artist=a\\b":                 "/c.conf:1: a bare value may not hold '\\': quote it",
		"# ok\nquality=9\nfont=a|b\n": "/c.conf:3: a bare value may not hold '|': quote it",
	}
	for text, want := range cases {
		s := Defaults()
		err := ParseConfig("/c.conf", []byte(text), &s)
		if err == nil || err.Error() != want {
			t.Errorf("%q: got %v, want %q", text, err, want)
		}
	}
}

func TestM7PresentationValuesAreValidated(t *testing.T) {
	good := Defaults()
	if err := good.checkPresentation(); err != nil {
		t.Fatalf("the defaults are valid: %v", err)
	}
	cases := []struct {
		edit func(*Settings)
		want string
	}{
		{func(s *Settings) { s.OpacityCorner = "1.5" }, "opacity_corner must be a decimal from 0 to 1, got '1.5'"},
		{func(s *Settings) { s.OpacityTile = "0.1)" }, "opacity_tile must be a decimal from 0 to 1, got '0.1)'"},
		{func(s *Settings) { s.TileAngle = "x" }, "tile_angle must be a decimal number of degrees, got 'x'"},
		{func(s *Settings) { s.SizePctCorner = "0" }, "size_pct_corner must be a decimal above 0 and at most 1, got '0'"},
		{func(s *Settings) { s.SizePctTile = "2" }, "size_pct_tile must be a decimal above 0 and at most 1, got '2'"},
		{func(s *Settings) { s.Quality = "0" }, "quality must be a whole number from 1 to 100, got '0'"},
		{func(s *Settings) { s.Quality = "9.5" }, "quality must be a whole number from 1 to 100, got '9.5'"},
		{func(s *Settings) { s.Font = "" }, "font must be a font name, got ''"},
	}
	for _, c := range cases {
		s := Defaults()
		c.edit(&s)
		err := s.checkPresentation()
		if err == nil || err.Error() != c.want {
			t.Errorf("got %v, want %q", err, c.want)
		}
	}
	edge := Defaults()
	edge.OpacityCorner, edge.OpacityTile, edge.TileAngle, edge.SizePctTile, edge.Quality = "1", "0", "45.5", ".5", "100"
	if err := edge.checkPresentation(); err != nil {
		t.Fatalf("the edges are valid: %v", err)
	}
}
