package ccylaunch

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

// savedConfig is a saved launch configuration as the bash launcher writes it.
const savedConfig = "# CCY Launch Configuration\n# Config Version: 2\n# CCY Version: 3.104.5\n" +
	"# CCY Hash: abc123\n# Generated: 2026-10-30 09:15:00\n" +
	"SAVED_CONFIG_VERSION=2\nSAVED_CCY_VERSION=\"3.104.5\"\nSAVED_CCY_HASH=\"abc123\"\n" +
	"LAST_TOKEN=\"personal\"\nLAST_SSH_KEYS=\"/keys/id_a /keys/id b\"\nLAST_NETWORK=\"\"\n" +
	"LAST_TEAMS=\"dev1@team-a,qa2@team-b\"\nLAST_LAUNCH_DATE=\"2026-10-30\"\n"

// configPath is a saved configuration's path in a fresh directory, holding text unless text
// is nil.
func configPath(t *testing.T, text *string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), ".last-launch.conf")
	if text != nil {
		if err := os.WriteFile(path, []byte(*text), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return path
}

// configRun is --read-launch-config of the file at path.
func configRun(t *testing.T, path string) *run {
	t.Helper()
	return newRun(install(t), launchConfigFlag, path)
}

func TestAGoodSavedConfigIsHandedBackOneKeyALine(t *testing.T) {
	r := configRun(t, configPath(t, new(savedConfig)))
	if code := r.start(t); code != term.ExitOK {
		t.Fatalf("exit %d, stderr %q", code, r.stderr)
	}
	want := "SAVED_CONFIG_VERSION\t2\nSAVED_CCY_VERSION\t3.104.5\nSAVED_CCY_HASH\tabc123\n" +
		"LAST_TOKEN\tpersonal\nLAST_SSH_KEYS\t/keys/id_a /keys/id b\nLAST_NETWORK\t\n" +
		"LAST_TEAMS\tdev1@team-a,qa2@team-b\nLAST_LAUNCH_DATE\t2026-10-30\n"
	if got := r.stdout.String(); got != want {
		t.Fatalf("stdout\n%q\nwant\n%q", got, want)
	}
	if r.stderr.Len() != 0 {
		t.Fatalf("stderr %q", r.stderr)
	}
}

func TestAFileOfFewerKeysIsReadAsItIs(t *testing.T) {
	// Which keys a format needs is the launcher's decision; the reader hands back what is there.
	r := configRun(t, configPath(t, new("SAVED_CONFIG_VERSION=3\n")))
	if code := r.start(t); code != term.ExitOK || r.stdout.String() != "SAVED_CONFIG_VERSION\t3\n" {
		t.Fatalf("exit %d, stdout %q, stderr %q", code, r.stdout, r.stderr)
	}
	r = configRun(t, configPath(t, new("")))
	if code := r.start(t); code != term.ExitOK || r.stdout.Len() != 0 {
		t.Fatalf("empty file: exit %d, stdout %q", code, r.stdout)
	}
}

// refused asserts the run failed, printed nothing on stdout, named the path and the cause on
// stderr, and left the file as it was (when text is not nil).
func refused(t *testing.T, r *run, path, cause string, text *string) {
	t.Helper()
	if code := r.start(t); code != term.ExitFailure {
		t.Fatalf("exit %d, want %d", code, term.ExitFailure)
	}
	if r.stdout.Len() != 0 {
		t.Fatalf("stdout %q: nothing is handed back from a file that does not read", r.stdout)
	}
	msg := r.stderr.String()
	if !strings.Contains(msg, path) || !strings.Contains(msg, cause) {
		t.Fatalf("stderr %q does not name %q and %q", msg, path, cause)
	}
	if text == nil {
		return
	}
	got, err := os.ReadFile(path)
	if err != nil || string(got) != *text {
		t.Fatalf("the file was not kept as it was: %q, %v", got, err)
	}
}

func TestAMissingFileIsReported(t *testing.T) {
	path := configPath(t, nil)
	refused(t, configRun(t, path), path, "no such file", nil)
}

func TestAnUnreadableFileIsReportedAndKept(t *testing.T) {
	// A directory cannot be read as a file, by root too.
	path := configPath(t, nil)
	if err := os.Mkdir(path, 0o700); err != nil {
		t.Fatal(err)
	}
	refused(t, configRun(t, path), path, "is a directory", nil)
	if info, err := os.Stat(path); err != nil || !info.IsDir() {
		t.Fatalf("the path was not kept: %v", err)
	}
}

func TestAMalformedLineIsReportedAndKept(t *testing.T) {
	cases := map[string]struct{ text, cause string }{
		"no equals sign":         {"LAST_TOKEN\n", "line 1"},
		"unquoted value":         {"LAST_TOKEN=personal\n", "line 1"},
		"quoted format":          {"SAVED_CONFIG_VERSION=\"2\"\n", "line 1"},
		"format not a number":    {"SAVED_CONFIG_VERSION=two\n", "line 1"},
		"unclosed quote":         {"# c\nLAST_TOKEN=\"personal\n", "line 2"},
		"quote inside":           {"LAST_TOKEN=\"a\"b\"\n", "line 1"},
		"a shell expansion":      {"LAST_TOKEN=\"$(id)\"\n", "line 1"},
		"a backtick":             {"LAST_TOKEN=\"`id`\"\n", "line 1"},
		"a backslash":            {"LAST_TOKEN=\"a\\b\"\n", "line 1"},
		"a tab":                  {"LAST_TOKEN=\"a\tb\"\n", "line 1"},
		"a blank line":           {"LAST_TOKEN=\"a\"\n\n", "line 2"},
		"a key twice":            {"LAST_TOKEN=\"a\"\nLAST_TOKEN=\"b\"\n", "line 2"},
		"cut short":              {"LAST_TOKEN=\"a\"\nLAST_TEAMS=\"\"", "does not end"},
		"a NUL":                  {"LAST_TOKEN=\"a\x00\"\n", "line 1"},
		"not UTF-8":              {"LAST_TOKEN=\"\xff\"\n", "line 1"},
		"a space before the key": {" LAST_TOKEN=\"a\"\n", "line 1"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			path := configPath(t, new(c.text))
			refused(t, configRun(t, path), path, c.cause, new(c.text))
		})
	}
}

func TestAnUnknownKeyIsReportedAndKept(t *testing.T) {
	text := savedConfig + "LAST_SHELL=\"zsh\"\n"
	path := configPath(t, new(text))
	refused(t, configRun(t, path), path, "LAST_SHELL", new(text))
}

func TestTheFlagTakesOneFile(t *testing.T) {
	for _, args := range [][]string{{launchConfigFlag}, {launchConfigFlag, "a", "b"}} {
		r := newRun(install(t), args...)
		if code := r.start(t); code != term.ExitUsage {
			t.Fatalf("%q: exit %d, want %d", args, code, term.ExitUsage)
		}
	}
}

func TestTheFlagIsAnsweredUnderLegacyToo(t *testing.T) {
	// The bash launcher asks it; handing it back to the bash would ask the bash itself.
	r := configRun(t, configPath(t, new(savedConfig)))
	r.env(LegacyEnv + "=1")
	if code := r.start(t); code != term.ExitOK || len(r.execer.Execs()) != 0 {
		t.Fatalf("exit %d, execs %v", code, r.execer.Execs())
	}
}
