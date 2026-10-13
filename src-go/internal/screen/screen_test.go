package screen

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
)

var rule = strings.Repeat("─", 60)

func lines(l ...string) string { return strings.Join(l, "\n") }

func TestPlainStripsEscapeSequences(t *testing.T) {
	if got := Plain("\x1b[1;31m❯\x1b[0m \x1b[2mhi\x1b[22m"); got != "❯ hi" {
		t.Fatalf("got %q", got)
	}
	if got := Plain("a\x1b[?25lb\x1b[38:2:1:2:3mc"); got != "abc" {
		t.Fatalf("got %q", got)
	}
	if got := Plain("no escapes"); got != "no escapes" {
		t.Fatalf("got %q", got)
	}
}

func TestClaudeStateDrawnBusyTypedNotDrawn(t *testing.T) {
	dimBox := "\x1b[39m❯ \x1b[2mTry \"fix typecheck errors\"\x1b[22m"
	cases := []struct {
		name   string
		screen string
		want   State
	}{
		{"framed box", lines(" ▐▛███▜▌   Claude Code", "", rule, "❯ ", rule, "  ⏵⏵ bypass permissions on (shift+tab to cycle)"), Ready},
		{"dim placeholder", lines(rule, dimBox, rule), Ready},
		{"cursor attributes are not text", lines(rule, "❯ \x1b[7m \x1b[27m", rule), Ready},
		{"typed with escapes", lines(rule, "\x1b[39m❯ \x1b[39mhalf a thought", rule), Typed},
		{"typed plain", lines(rule, "❯ half a thought", rule), Typed},
		{"busy", lines("✻ Compacting conversation… (12s · esc to interrupt)", "", rule, "❯ ", rule), Busy},
		{"phrase quoted higher up", lines("the spinner says esc to interrupt", "one", "two", "three", "four", rule, "❯ ", rule), Ready},
		{"selection dialog", lines("Do you trust the files in this folder?", "", "❯ 1. Yes, proceed", "  2. No, exit"), NotDrawn},
		{"no rule below", lines(rule, "❯ "), NotDrawn},
		{"launcher output", "Building image...\n> ", NotDrawn},
		{"empty", "", NotDrawn},
		{"trailing blank rows as capture-pane prints", lines(rule, "❯ ", rule, "", "", ""), Ready},
		{"box whose first rule has no closing rule then a second box", lines(rule, "❯ a", "text", rule, "❯ ", rule), Typed},
		{"lone NBSP and space before dim", lines(rule, "  ", rule), NotDrawn},
		{"malformed escape in the box line is typed", lines(rule, "❯ \x1bX", rule), Typed},
		{"non-SGR escape is typed", lines(rule, "❯ \x1b[2J", rule), Typed},
		{"colon parameters are not a dim", lines(rule, "❯ \x1b[38:2:1m", rule), Ready},
	}
	for _, c := range cases {
		if got := ClaudeState(c.screen); got != c.want {
			t.Errorf("%s: got %q, want %q", c.name, got, c.want)
		}
	}
}

func TestInputLineEmptyEdgeCases(t *testing.T) {
	cases := []struct {
		raw  string
		want bool
	}{
		{"❯", true},
		{"❯ ", true},
		{"❯  ", true},
		{"❯   ", true},
		{"❯ x", false},
		{"❯ \x1b[2mplaceholder", true},
		{"❯ \x1b[1;2mplaceholder", true},
		{"❯ \x1b[22mtext", false},
		{"❯ \x1b[39m", true},
		{"❯ \x1b[39mtext", false},
		{"❯ \x1b[", false},
		{"❯ \x1b[2", false},
	}
	for _, c := range cases {
		if got := inputLineEmpty(c.raw); got != c.want {
			t.Errorf("%q: got %v, want %v", c.raw, got, c.want)
		}
	}
}

func TestKnownPromptsAreUniqueAndNonEmpty(t *testing.T) {
	seen := map[string]bool{}
	for _, p := range KnownPrompts() {
		if p.Name == "" || p.Text == "" {
			t.Errorf("empty field in %+v", p)
		}
		if seen[p.Name] {
			t.Errorf("duplicate name %q", p.Name)
		}
		seen[p.Name] = true
	}
	if len(KnownPrompts()) != 26 {
		t.Fatalf("got %d prompts", len(KnownPrompts()))
	}
}

func TestKnownPromptsMatchTheBashTableInOrder(t *testing.T) {
	root := repoRoot(t)
	lib := filepath.Join(root, "files/var/local/claude-yolo/lib/common-pure.bash")
	res, err := execx.OS{}.Run(context.Background(), execx.Cmd{
		Name: "bash", Args: []string{"-c", `set -euo pipefail; source "$1"; ccy_known_prompts; printf 'ENDED\t%s\n' "$2"`, "bash", lib, EndedText},
	})
	if err != nil {
		t.Fatalf("bash: %v\n%s", err, res.Stderr)
	}
	var want []Prompt
	for line := range strings.SplitSeq(strings.TrimSuffix(string(res.Stdout), "\n"), "\n") {
		name, text, ok := strings.Cut(line, "\t")
		if !ok {
			t.Fatalf("bad line %q", line)
		}
		if name == "ENDED" {
			continue
		}
		want = append(want, Prompt{Name: name, Text: text})
	}
	got := KnownPrompts()
	if len(got) != len(want) {
		t.Fatalf("got %d prompts, bash has %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("prompt %d: got %+v, bash %+v", i, got[i], want[i])
		}
	}
}

func TestEndedTextIsTheRegistrysSentence(t *testing.T) {
	root := repoRoot(t)
	raw, err := os.ReadFile(filepath.Clean(filepath.Join(root, "files/var/local/claude-yolo/lib/session-registry.bash")))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `CCY_SESSION_ENDED_TEXT="`+EndedText+`"`) {
		t.Fatalf("session-registry.bash does not define %q", EndedText)
	}
}

func repoRoot(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for dir := wd; dir != filepath.Dir(dir); dir = filepath.Dir(dir) {
		if _, statErr := os.Stat(filepath.Join(dir, "files/var/local/claude-yolo/lib/common-pure.bash")); statErr == nil {
			return dir
		}
	}
	t.Fatalf("repo root not found above %s", wd)
	return ""
}
