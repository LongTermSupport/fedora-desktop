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
		{"a non-SGR escape alone is not text", lines(rule, "❯ \x1b[2J", rule), Ready},
		{"erase to end of line alone is not text", lines(rule, "❯ \x1b[K", rule), Ready},
		{"cursor visibility and shape are not text", lines(rule, "❯\u00a0\x1b[?25h\x1b[2 q ", rule), Ready},
		{"an OSC sequence is not text", lines(rule, "❯ \x1b]8;;https://example.com\x1b\\\x1b]8;;\x07", rule), Ready},
		{"placeholder under an inverse-video cursor", lines(rule, "\x1b[39m❯ \x1b[7mT\x1b[27m\x1b[2mry \"fix typecheck errors\"\x1b[22m", rule), Ready},
		{"placeholder under a cursor set with dim off", lines(rule, "❯ \x1b[0;7mT\x1b[0m\x1b[2mry \"refactor\"\x1b[0m", rule), Ready},
		{"colon parameters are not a dim", lines(rule, "❯ \x1b[38:2:1m", rule), Ready},
		{"typed then erase to end of line", lines(rule, "❯ half a thought\x1b[K", rule), Typed},
		{"typed after a non-SGR escape", lines(rule, "❯ \x1b[K\x1b[?25lhalf a thought", rule), Typed},
		{"typed after an OSC sequence", lines(rule, "❯ \x1b]8;;\x07half a thought", rule), Typed},
		{"typed with the cursor after it", lines(rule, "❯ half a thought\x1b[7m \x1b[27m", rule), Typed},
		{"typed with the cursor on its first character", lines(rule, "❯ \x1b[7mh\x1b[27malf a thought", rule), Typed},
		{"one typed character under the cursor", lines(rule, "❯ \x1b[7mh\x1b[27m", rule), Typed},
		{"typed in a colour whose RGB holds a 2", lines(rule, "❯ \x1b[38;2;1;2;3mhalf a thought", rule), Typed},
		{"typed in a 256-colour 2", lines(rule, "❯ \x1b[38;5;2mhalf a thought", rule), Typed},
		{"typed after a dim that was turned off", lines(rule, "❯ \x1b[2m\x1b[22mhalf a thought", rule), Typed},
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
		{"❯ \x1b[K", true},
		{"❯ \x1b[0m", true},
		{"❯ \x1b[mtext", false},
		{"❯ \x1b[2;0mtext", false},
		{"❯ \x1b[7m\x1b[2m \x1b[27mplaceholder", true},
	}
	for _, c := range cases {
		if got := inputLineEmpty(c.raw); got != c.want {
			t.Errorf("%q: got %v, want %v", c.raw, got, c.want)
		}
	}
}

// InputLine gives the raw box line, escapes and all, so that a box judged typed can be shown
// as it was.
func TestInputLineIsTheRawBoxLine(t *testing.T) {
	box := "❯ \x1b[39mhalf\x1b[K"
	if got := InputLine(lines("● hi", "❯ quoted", rule, box, rule)); got != box {
		t.Fatalf("got %q", got)
	}
	if got := InputLine(lines(rule, "❯ ")); got != "" {
		t.Fatalf("an unframed arrow is not the box: got %q", got)
	}
}

// The fixtures are tmux captures of Claude Code 2.1.303 run as `claude --resume` in a
// project with saved conversations, at several pane widths: the footer wraps differently at
// each, so only the header is the same on every one.
func TestConversationPickerIsSeenOnRealCaptures(t *testing.T) {
	for _, name := range []string{
		"picker-wide.txt", "picker-120-escapes.txt", "picker-80.txt", "picker-60.txt",
		"picker-scrolling.txt", "picker-preview.txt",
	} {
		raw, err := os.ReadFile(filepath.Clean(filepath.Join("testdata", name)))
		if err != nil {
			t.Fatal(err)
		}
		if !AtConversationPicker(string(raw)) {
			t.Errorf("%s: the picker was not seen", name)
		}
	}
}

func TestConversationPickerIsNotSeenElsewhere(t *testing.T) {
	cases := map[string]string{
		"a conversation that quotes the header": lines("● The picker says:", "  Resume session", "", rule, "❯ ", rule),
		"the header text inside a sentence":     lines("Building image...", "We will Resume session state later"),
		"a launcher prompt":                     "Use same configuration? [Y/n] ",
		"empty":                                 "",
		"Claude at its prompt":                  lines(" ▐▛███▜▌   Claude Code", "", rule, "❯ ", rule),
	}
	for name, pane := range cases {
		if AtConversationPicker(pane) {
			t.Errorf("%s: seen as the picker", name)
		}
	}
}

// The question Claude Code 2.1.311 asks when /exit finds background work running: its title,
// subtitle and options are its own text (the first option focused); the task line is a stand-in.
var backgroundWorkDialog = lines(
	"  Background work is running",
	"  The following will stop when you exit:",
	"",
	"  npm run dev",
	"",
	"  ❯ 1. Exit and stop tasks",
	"    2. Stay",
)

func TestBackgroundWorkExitIsSeenWithItsFocus(t *testing.T) {
	cases := map[string]struct {
		pane            string
		shown, onExitOp bool
	}{
		"the question, Exit focused": {backgroundWorkDialog, true, true},
		"with Move to background, escapes drawn": {lines(
			"\x1b[1mBackground work is running\x1b[0m", "The following will stop when you exit:",
			"\x1b[36m❯ 1. Exit and stop tasks\x1b[0m", "  2. Move to background and exit", "  3. Stay"), true, true},
		"the cursor moved to Stay":         {strings.Replace(strings.Replace(backgroundWorkDialog, "❯ 1.", "  1.", 1), "  2. Stay", "❯ 2. Stay", 1), true, false},
		"a conversation quoting the title": {lines("● It said Background work is running", "", rule, "❯ ", rule), false, false},
		"Claude at its prompt":             {lines(" ▐▛███▜▌   Claude Code", "", rule, "❯ ", rule), false, false},
		"the title without the option":     {lines("Background work is running", "  ❯ 1. Yes"), false, false},
		"empty":                            {"", false, false},
	}
	for name, c := range cases {
		shown, onExit := AtBackgroundWorkExit(c.pane)
		if shown != c.shown || onExit != c.onExitOp {
			t.Errorf("%s: shown %v, Exit focused %v; want %v, %v", name, shown, onExit, c.shown, c.onExitOp)
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
