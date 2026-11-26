package checkoutpin

import (
	"context"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

// rlo is RIGHT-TO-LEFT OVERRIDE, a format character (Cf) that makes a terminal draw the text
// after it backwards, so an approval screen would show a value other than the one approved.
const rlo = "\u202e"

// D1: Shown writes every rune that is not printable as an escape, keeps the printable ones
// (symbols and accents included), and keeps newlines and tabs, so a file shows as its lines.
func TestShownEscapesWhatAScreenWouldNotShow(t *testing.T) {
	for in, want := range map[string]string{
		"plain ✓ → é":     "plain ✓ → é",
		"a" + rlo + "b":   `a\u{202E}b`,
		"zero\u200bwidth": `zero\u{200B}width`,
		"bell\a":          `bell\u{0007}`,
		"bad\xffbyte":     `bad\x{FF}byte`,
		"two\nlines\t":    "two\nlines\t",
		"esc\x1b[31m":     `esc\u{001B}[31m`,
	} {
		if got := Shown(in); got != want {
			t.Errorf("Shown(%q) = %q, want %q", in, got, want)
		}
	}
}

// D2 (the hazard): every approval screen prints a checkout's text through Shown, so a format
// character in it is seen as an escape, never drawn.
func TestApprovalScreensEscapeFormatCharacters(t *testing.T) {
	r := newRig(t)
	write(t, r.dir, Mounts, "/srv/"+rlo+"evil:/x\n")
	r.answer = no()
	if code := r.cmd().Approve(context.Background(), []string{"mounts"}); code != term.ExitFailure {
		t.Fatalf("approve: %v", code)
	}
	if strings.Contains(r.err.String(), rlo) || !strings.Contains(r.err.String(), `\u{202E}evil`) {
		t.Fatalf("mounts screen drew the override: %q", r.err.String())
	}
	write(t, r.dir, "compose.yml", "services: {}\n# "+rlo+"evil\n")
	if code := r.cmd().Approve(context.Background(), []string{"compose"}); code != term.ExitFailure {
		t.Fatalf("approve compose: %v", code)
	}
	if strings.Contains(r.err.String(), rlo) || !strings.Contains(r.err.String(), `# \u{202E}evil`) {
		t.Fatalf("compose screen drew the override: %q", r.err.String())
	}
}

// D3: the saved launch choices and the mounts paths refuse a format character outright, as
// they refuse a control character: neither ccy nor a person writes one.
func TestFormatCharactersAreRefusedInValues(t *testing.T) {
	if _, err := ParseLastLaunch([]byte("LAST_NETWORK=\"a" + rlo + "b\"\n")); err == nil {
		t.Error("a format character in a saved value passed")
	}
	if _, problems := parse(t, "/srv/a"+rlo+"b:/x\n"); len(problems) == 0 {
		t.Error("a format character in a mounts path passed")
	}
	if _, problems := parse(t, "/srv/a\xffb:/x\n"); len(problems) == 0 {
		t.Error("a byte that is not UTF-8 in a mounts path passed")
	}
}
