package difftest

import (
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/sessprobe"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/tmuxccy"
)

// TestSessionsFindEveryRecordedLaunch holds ccy-sessions to the launcher's real command lines:
// every container launch a golden records (the call that labels a container ccy=true), placed
// in a pane's process tree as ps prints it, must be found as that session's container. A
// launch shape the recogniser does not know fails here, not on the owner's host.
func TestSessionsFindEveryRecordedLaunch(t *testing.T) {
	files, err := filepath.Glob(filepath.Join("testdata", "golden", "*.txt"))
	if err != nil {
		t.Fatal(err)
	}
	launches := 0
	for _, file := range files {
		raw, readErr := os.ReadFile(filepath.Clean(file))
		if readErr != nil {
			t.Fatal(readErr)
		}
		for _, argv := range recordedCalls(t, file, string(raw)) {
			if !labelsCCYContainer(argv) {
				continue
			}
			launches++
			engine, name, ok := runNameOf(argv)
			if !ok {
				t.Errorf("%s: a ccy container launch with no `<engine> run --name`: %q", file, argv)
				continue
			}
			tree := []sessprobe.Proc{
				{PID: 100, PPID: 1, Args: "/bin/bash /var/local/claude-yolo/claude-yolo.bash"},
				{PID: 110, PPID: 100, Args: strings.Join(argv, " ")},
			}
			line := strings.Join(argv, " ")
			got := sessprobe.SessionContainers([]tmuxccy.Pane{{Session: "ccy-s", PID: 100}}, tree)
			if len(got) != 1 || got[0].Container != name || got[0].Engine != engine {
				t.Errorf("%s: ccy-sessions does not find container %s (%s) in the launch %q: got %+v", file, name, engine, line, got)
			}
			// lib/docker-health.bash finds a session's live client with this pgrep -f pattern
			// (a container without one is a zombie, and is removed).
			attached := regexp.MustCompile(engine + " run.*--name[= ]" + regexp.QuoteMeta(name) + "( |$)")
			if !attached.MatchString(line) {
				t.Errorf("%s: docker-health's pgrep %q does not match the launch %q", file, attached, line)
			}
		}
	}
	if launches == 0 {
		t.Fatal("no golden records a ccy container launch: the guard checks nothing")
	}
}

func labelsCCYContainer(argv []string) bool {
	for i := 0; i+1 < len(argv); i++ {
		if argv[i] == "--label" && argv[i+1] == "ccy=true" {
			return true
		}
	}
	return false
}

// runNameOf is the engine and the --name of the `<engine> run` inside a launch's argv: the
// name the engine itself gives the container, whatever wraps the run.
func runNameOf(argv []string) (string, string, bool) {
	for i := 0; i+1 < len(argv); i++ {
		engine := path.Base(argv[i])
		if (engine != "podman" && engine != "docker") || argv[i+1] != "run" {
			continue
		}
		for j := i + 2; j+1 < len(argv); j++ {
			if argv[j] == "--name" {
				return engine, argv[j+1], true
			}
		}
	}
	return "", "", false
}

// recordedCalls reads back the argv of each call in a golden's calls section, which render
// writes with shellword.Join: one call per line, a quoted word running on over the line ends it
// holds, and the indented env lines between calls.
func recordedCalls(t *testing.T, file, golden string) [][]string {
	t.Helper()
	_, rest, found := strings.Cut(golden, "\n=== calls\n")
	if !found {
		t.Fatalf("%s: no calls section", file)
	}
	section, _, found := strings.Cut(rest, "=== files\n")
	if !found {
		t.Fatalf("%s: no files section", file)
	}
	var calls [][]string
	pending := ""
	for line := range strings.SplitSeq(strings.TrimSuffix(section, "\n"), "\n") {
		if pending == "" && strings.HasPrefix(line, "    env: ") {
			continue
		}
		if pending != "" {
			pending += "\n"
		}
		pending += line
		if argv, complete := shellWords(pending); complete {
			calls = append(calls, argv)
			pending = ""
		}
	}
	if pending != "" {
		t.Fatalf("%s: the calls section ends inside a call", file)
	}
	return calls
}

// shellWords splits what shellword.Join wrote back into its words: plain words, and single
// quotes, a quote inside them closed, backslash-escaped and reopened. complete is false while
// a quote is still open.
func shellWords(line string) ([]string, bool) {
	var words []string
	var word strings.Builder
	inWord, quoted, escaped := false, false, false
	for _, c := range line {
		switch {
		case escaped:
			word.WriteRune(c)
			escaped = false
		case quoted:
			quoted = c != '\''
			if quoted {
				word.WriteRune(c)
			}
		case c == '\'' || c == '\\':
			quoted, escaped, inWord = c == '\'', c == '\\', true
		case c == ' ':
			if inWord {
				words = append(words, word.String())
			}
			word.Reset()
			inWord = false
		default:
			word.WriteRune(c)
			inWord = true
		}
	}
	if inWord {
		words = append(words, word.String())
	}
	return words, !quoted && !escaped
}
