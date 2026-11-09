// Package screen reads what a ccy or cc pane shows: the text with its terminal escapes
// removed, whether Claude's input box is drawn, busy or holding typed text, which launcher
// prompt (if any) the pane is waiting at, and whether it is at Claude Code's conversation picker. The screen reading lives only here; the launchers'
// prompt table in lib/common-pure.bash stays the owner of the prompt texts while they are bash.
package screen

import (
	"regexp"
	"strings"
	"unicode/utf8"
)

// EndedText is the line a failed session's window is held on (the trampoline prints it), which
// is how a pane whose launcher has exited is told from one that is still starting.
const EndedText = "Press Enter to close this session."

// State is what Claude's input box shows.
type State string

// The four states of Claude's prompt.
const (
	// Ready is a drawn, empty input box.
	Ready State = "ready"
	// Busy is a drawn box with a turn or compaction running just above it.
	Busy State = "busy"
	// Typed is a drawn box that already holds text someone typed.
	Typed State = "typed"
	// NotDrawn is no framed input box on the screen.
	NotDrawn State = "not-drawn"
)

// Prompt is one launcher prompt a pane can wait at, by the text it starts with.
type Prompt struct {
	Name string
	Text string
}

// KnownPrompts is every prompt a ccy or cc launch can wait at for a person, in the order the
// bash table lists them; the first prefix match wins. A test compares it with
// ccy_known_prompts in lib/common-pure.bash.
func KnownPrompts() []Prompt {
	return []Prompt{
		{"migration", "Remove old session directories? [y/N]"},
		{"quick-launch", "Use same configuration? [Y/n]"},
		{"ssh-key", "Select SSH key"},
		{"ssh-no-key", "Press Enter to continue WITHOUT SSH key, or Ctrl+C to cancel:"},
		{"ssh-passphrase", "Enter passphrase for"},
		{"ssh-passphrase-retry", "Hit return to try the passphrase again"},
		{"github-443", "Enable GitHub SSH over 443 for this session? [Y/n]"},
		{"token-expired", "Create a new token now? (Y/n):"},
		{"token-none", "Create a token now? (Y/n):"},
		{"token-replace", "Create a new token? (Y/n):"},
		{"token-select", "Select token"},
		{"token-recovery", "Select option [1-3]:"},
		{"zombie-containers", "Choice [a/s/i/q]:"},
		{"existing-containers", "Choice [c/s/m/q]:"},
		{"network-continue", "Continue without network connection? [Y/n]"},
		{"network-connect", "Connect to this network?"},
		{"network-pick", "Select [0-"},
		{"network-select", "Select network ["},
		{"network-container", "Select container ["},
		{"network-engine-conflict", "Select option [1-4]:"},
		{"relabel-fix", "Give them to you now? sudo asks for your password. [y/N]"},
		{"network-prune", "Choose cleanup method [a/b]:"},
		{"compose-start", "Start services with"},
		{"compose-start-saved", "Start compose services?"},
		{"compose-stop", "Stop compose services?"},
		{"token-setup", "Press Enter to start the token setup container:"},
	}
}

// ConversationPicker is the name a pane at Claude Code's conversation picker (`claude
// --resume` with no conversation named) is reported waiting at.
const ConversationPicker = "thread-picker"

// pickerSigns are the lines, by what they start with once indented space is removed, that only
// Claude Code's conversation picker draws: its header ("Resume session", with a count when the
// list scrolls), and the footer of its preview. They are Claude Code's text, not ccy's, so
// they are not in KnownPrompts; and the picker's last line is a key-hint footer that wraps
// differently at every pane width, so a last-line prefix could not match it.
var pickerSigns = []string{"Resume session", "Enter to resume · Esc to cancel"}

// AtConversationPicker says whether a pane shows Claude Code's conversation picker: a line
// starting with one of pickerSigns, and no framed input box (a conversation that quotes the
// header still has its box drawn below).
func AtConversationPicker(pane string) bool {
	if ClaudeState(pane) != NotDrawn {
		return false
	}
	for line := range strings.SplitSeq(Plain(pane), "\n") {
		line = strings.TrimSpace(line)
		for _, sign := range pickerSigns {
			if strings.HasPrefix(line, sign) {
				return true
			}
		}
	}
	return false
}

// The question Claude Code (2.1.311) asks when /exit finds background work running: its title,
// and its first option, which ends the session and the work. The other options are "Move to
// background and exit" (when offered) and "Stay"; Esc is Stay. Claude Code's text, not ccy's.
const (
	backgroundWorkTitle = "Background work is running"
	exitAndStopTasks    = "Exit and stop tasks"
)

// exitOption is the first option's line once indented space is removed: the cursor arrow when
// focused, then its number.
var exitOption = regexp.MustCompile(`^(❯\s*)?(?:\d+\.\s*)?` + exitAndStopTasks + `$`)

// AtBackgroundWorkExit says whether a pane shows Claude Code's "Background work is running"
// question (a line starting with its title and a line that is its "Exit and stop tasks"
// option), and whether the cursor is on that option, so that Enter chooses it.
func AtBackgroundWorkExit(pane string) (bool, bool) {
	titled, shown, exitFocused := false, false, false
	for line := range strings.SplitSeq(Plain(pane), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, backgroundWorkTitle) {
			titled = true
		}
		if m := exitOption.FindStringSubmatch(line); m != nil {
			shown, exitFocused = true, m[1] != ""
		}
	}
	if !titled {
		return false, false
	}
	return shown, exitFocused
}

// escapeSeq is a control sequence that draws nothing: a CSI (ECMA-48 parameter, intermediate
// and final bytes, so `\x1b[2 q` and `\x1b[>4m` too) or an OSC ended by BEL or ST.
var escapeSeq = regexp.MustCompile(`\x1b\[[0-?]*[ -/]*[@-~]|\x1b\][^\x07\x1b]*(?:\x07|\x1b\\)`)

// leadingEscape is escapeSeq at the start of a string.
var leadingEscape = regexp.MustCompile(`^(?:` + escapeSeq.String() + `)`)

// sgr is a leading Select Graphic Rendition sequence, with its parameters captured.
var sgr = regexp.MustCompile(`^\x1b\[([0-9;:]*)m`)

// Plain is the screen with its CSI and OSC sequences removed: what it shows. tmux capture-pane
// -e writes attributes into the text; everything but the input box's placeholder test reads
// the plain text. Removal repeats until none is left.
func Plain(s string) string {
	for {
		next := escapeSeq.ReplaceAllString(s, "")
		if next == s {
			return s
		}
		s = next
	}
}

// rulePrefix is ten rule characters as a literal prefix: Claude frames its input box with them.
var rulePrefix = strings.Repeat("─", 10)

// interrupt is what Claude's spinner shows while a turn or a compaction runs.
const interrupt = "esc to interrupt"

// ClaudeState says what Claude's input box shows on a pane. The screen is capture-pane -p -e
// output (plain text works too, without the placeholder test). The box is a line beginning
// with the prompt arrow framed by rule lines above and below; the same arrow marks the cursor
// in selection dialogs, which are not framed, so only a framed one counts. Busy is "esc to
// interrupt" in the four nearest non-blank lines above the box; the conversation can quote the
// phrase, so only the status area counts.
func ClaudeState(screen string) State {
	rawLines, plain, i := inputBox(screen)
	switch {
	case i < 0:
		return NotDrawn
	case busyAbove(plain, i):
		return Busy
	case inputLineEmpty(rawLines[i]):
		return Ready
	}
	return Typed
}

// InputLine is the input box line as the pane holds it, escapes and all (empty when no framed
// box is drawn): the evidence behind a Typed verdict.
func InputLine(screen string) string {
	rawLines, _, i := inputBox(screen)
	if i < 0 {
		return ""
	}
	return rawLines[i]
}

// inputBox splits the screen into its raw and plain lines and finds the framed box line in
// them: -1 when there is none.
func inputBox(screen string) ([]string, []string, int) {
	screen = strings.TrimRight(screen, "\n")
	rawLines := strings.Split(screen, "\n")
	plain := strings.Split(Plain(screen), "\n")
	for i := 1; i < len(plain); i++ {
		if !strings.HasPrefix(plain[i], "❯") || !strings.HasPrefix(plain[i-1], rulePrefix) {
			continue
		}
		for j := i + 1; j < len(plain); j++ {
			if strings.HasPrefix(plain[j], rulePrefix) {
				return rawLines, plain, i
			}
		}
	}
	return rawLines, plain, -1
}

// busyAbove looks at the four nearest non-blank lines above the rule that frames line i.
func busyAbove(plain []string, i int) bool {
	seen := 0
	for k := i - 2; k >= 0 && seen < 4; k-- {
		if strings.ReplaceAll(plain[k], " ", "") == "" {
			continue
		}
		seen++
		if strings.Contains(plain[k], interrupt) {
			return true
		}
	}
	return false
}

// blank is what an empty box shows after the arrow: spaces and no-break spaces.
const blank = "  "

// inputLineEmpty says whether the input box line holds no typed text, by what it shows. Nothing
// visible after the arrow is empty, whatever escapes the line carries. Otherwise Claude's
// placeholder is dim (SGR 2) from its first character, or from its second when the cursor
// (inverse video, SGR 7) sits on the first; typed text has no dim on it. An escape that cannot
// be read is shown as text, so it is typed: nothing is typed over what cannot be judged.
func inputLineEmpty(raw string) bool {
	_, rest, found := strings.Cut(raw, "❯")
	if !found {
		rest = raw
	}
	if strings.Trim(Plain(rest), blank) == "" {
		return true
	}
	var a attrs
	seen := 0
	for rest != "" {
		if m := sgr.FindStringSubmatch(rest); m != nil {
			a.apply(m[1])
			rest = rest[len(m[0]):]
			continue
		}
		if m := leadingEscape.FindString(rest); m != "" {
			rest = rest[len(m):]
			continue
		}
		r, size := utf8.DecodeRuneInString(rest)
		rest = rest[size:]
		if strings.ContainsRune(blank, r) {
			continue
		}
		seen++
		if a.dim || seen > 1 || !a.inverse {
			return a.dim
		}
	}
	return false
}

// attrs is the SGR state the placeholder test reads: dim and inverse video.
type attrs struct{ dim, inverse bool }

// apply sets the state an SGR's parameters leave. An extended colour's own numbers (38;5;n,
// 38;2;r;g;b and the 48 and 58 forms) are skipped, so a colour component of 2 is not a dim.
func (a *attrs) apply(params string) {
	ps := strings.Split(params, ";")
	for i := 0; i < len(ps); i++ {
		switch ps[i] {
		case "", "0":
			*a = attrs{}
		case "2":
			a.dim = true
		case "22":
			a.dim = false
		case "7":
			a.inverse = true
		case "27":
			a.inverse = false
		case "38", "48", "58":
			i += extendedColourArgs(ps[i+1:])
		}
	}
}

// extendedColourArgs is how many parameters after 38, 48 or 58 belong to the colour.
func extendedColourArgs(rest []string) int {
	if len(rest) == 0 {
		return 0
	}
	switch rest[0] {
	case "5":
		return 2
	case "2":
		return 4
	}
	return 0
}
