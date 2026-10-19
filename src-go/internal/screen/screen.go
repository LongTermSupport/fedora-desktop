// Package screen reads what a ccy or cc pane shows: the text with its terminal escapes
// removed, whether Claude's input box is drawn, busy or holding typed text, which launcher
// prompt (if any) the pane is waiting at, and whether it is at Claude Code's conversation picker. The screen reading lives only here; the launchers'
// prompt table in lib/common-pure.bash stays the owner of the prompt texts while they are bash.
package screen

import (
	"regexp"
	"strings"
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

var csi = regexp.MustCompile(`\x1b\[[0-9;:?]*[A-Za-z]`)

// sgr is a leading Select Graphic Rendition sequence, with its parameters captured.
var sgr = regexp.MustCompile(`^\x1b\[([0-9;:]*)m`)

// Plain is the screen with its CSI escape sequences removed. tmux capture-pane -e writes
// attributes into the text; everything but the input box's placeholder test reads the plain
// text. Removal repeats until none is left.
func Plain(s string) string {
	for {
		next := csi.ReplaceAllString(s, "")
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
	screen = strings.TrimRight(screen, "\n")
	rawLines := strings.Split(screen, "\n")
	plain := strings.Split(Plain(screen), "\n")
	for i := 1; i < len(plain); i++ {
		if !strings.HasPrefix(plain[i], "❯") || !strings.HasPrefix(plain[i-1], rulePrefix) {
			continue
		}
		for j := i + 1; j < len(plain); j++ {
			if !strings.HasPrefix(plain[j], rulePrefix) {
				continue
			}
			if busyAbove(plain, i) {
				return Busy
			}
			if inputLineEmpty(rawLines[i]) {
				return Ready
			}
			return Typed
		}
	}
	return NotDrawn
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

// inputLineEmpty says whether the input box line holds no typed text. Claude draws the arrow,
// a (no-break) space and, when the box is empty, a placeholder in dim (SGR 2); typed text has
// no dim on it.
func inputLineEmpty(raw string) bool {
	rest := raw
	if parts := strings.SplitN(raw, "❯", 2); len(parts) == 2 {
		rest = parts[1]
	}
	for rest != "" {
		switch {
		case strings.HasPrefix(rest, " "), strings.HasPrefix(rest, " "):
			rest = strings.TrimPrefix(rest, " ")
			rest = strings.TrimPrefix(rest, " ")
		case strings.HasPrefix(rest, "\x1b"):
			m := sgr.FindStringSubmatch(rest)
			if m == nil {
				return false
			}
			if strings.Contains(";"+m[1]+";", ";2;") {
				return true
			}
			rest = rest[len(m[0]):]
		default:
			return false
		}
	}
	return true
}
