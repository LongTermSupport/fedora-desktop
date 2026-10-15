package watermark

import (
	"slices"
	"testing"
)

func TestM1ReadsEveryOptionAndTheImages(t *testing.T) {
	o, err := parseArgs([]string{
		"--text", "T", "--artist", "A", "--copyright", "C", "--licence-url", "U", "--licence-summary", "S",
		"--profile", "blog", "--config", "/c", "-o", "out.jpg", "--force", "--dry-run", "-v",
		"--no-tile", "--no-corner", "--show-config", "a.jpg", "--", "-b.jpg",
	})
	if err != nil {
		t.Fatal(err)
	}
	values := []string{*o.text, *o.artist, *o.copyright, *o.licenceURL, *o.licenceSummary, o.profile, o.configFile, o.output}
	if !slices.Equal(values, []string{"T", "A", "C", "U", "S", "blog", "/c", "out.jpg"}) {
		t.Fatalf("values: %q", values)
	}
	flags := []bool{o.force, o.dryRun, o.verbose, o.noTile, o.noCorner, o.showConfig, !o.help}
	if slices.Contains(flags, false) {
		t.Fatalf("flags: %v", flags)
	}
	if !slices.Equal(o.images, []string{"a.jpg", "-b.jpg"}) {
		t.Fatalf("images: %q", o.images)
	}
}

func TestM1AnOptionNotGivenIsNilSoAnEmptyOneStillOverrides(t *testing.T) {
	o, err := parseArgs([]string{"--text", "", "--output", "x", "--verbose", "x.png"})
	if err != nil {
		t.Fatal(err)
	}
	if o.text == nil || *o.text != "" || o.artist != nil || o.output != "x" || !o.verbose {
		t.Fatalf("got %+v", o)
	}
}

func TestM1HelpStopsReadingButAnEarlierErrorWins(t *testing.T) {
	o, err := parseArgs([]string{"-h", "--bogus"})
	if err != nil || !o.help {
		t.Fatalf("got %+v, %v", o, err)
	}
	if _, err := parseArgs([]string{"--bogus", "--help"}); err == nil || err.Error() != "unknown option: --bogus (try --help)" {
		t.Fatalf("got %v", err)
	}
}

func TestM1AValueOptionAtTheEndNeedsItsValue(t *testing.T) {
	for _, opt := range []string{"--text", "--artist", "--copyright", "--licence-url", "--licence-summary", "--profile", "--config", "--output"} {
		if _, err := parseArgs([]string{opt}); err == nil || err.Error() != opt+" requires a value" {
			t.Errorf("%s: got %v", opt, err)
		}
	}
	if _, err := parseArgs([]string{"-o"}); err == nil || err.Error() != "--output requires a value" {
		t.Errorf("-o: got %v", err)
	}
}
