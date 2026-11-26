package ccylaunch

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/checkoutpin"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

// savedConfig is a saved launch configuration as the bash launcher writes it.
const savedConfig = "# CCY Launch Configuration\n# Config Version: 2\n# CCY Version: 3.104.5\n" +
	"# CCY Hash: abc123\n# Generated: 2026-10-30 09:15:00\n" +
	"SAVED_CONFIG_VERSION=2\nSAVED_CCY_VERSION=\"3.104.5\"\nSAVED_CCY_HASH=\"abc123\"\n" +
	"LAST_TOKEN=\"personal\"\nLAST_SSH_KEYS=\"/keys/id_a /keys/id b\"\nLAST_NETWORK=\"\"\n" +
	"LAST_TEAMS=\"dev1@team-a,qa2@team-b\"\nLAST_LAUNCH_DATE=\"2026-10-30\"\n"

// configPath is the saved configuration's path in a fresh checkout, holding text unless text
// is nil.
func configPath(t *testing.T, text *string) string {
	t.Helper()
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, checkoutpin.LastLaunch)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if text != nil {
		if err := os.WriteFile(path, []byte(*text), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return path
}

// checkoutOf is the checkout a configPath is in.
func checkoutOf(path string) string { return strings.TrimSuffix(path, "/"+checkoutpin.LastLaunch) }

// stateEnv is the launcher's state directory for a run, beside the checkout.
func stateEnv(path string) string { return "CCY_STATE_DIR=" + checkoutOf(path) + "-state" }

// configRun is an attended --read-launch-config of the checkout the file at path is in.
func configRun(t *testing.T, path string) *run {
	t.Helper()
	return configRunAs(t, path, "attended")
}

func configRunAs(t *testing.T, path, mode string) *run {
	t.Helper()
	r := newRun(install(t), launchConfigFlag, checkoutOf(path), mode)
	r.env(stateEnv(path))
	return r
}

// pinned records the file at path as the launcher's own write.
func pinned(t *testing.T, path string) {
	t.Helper()
	store, err := checkoutpin.Locate(func(k string) string {
		return map[string]string{"CCY_STATE_DIR": checkoutOf(path) + "-state"}[k]
	})
	if err != nil {
		t.Fatal(err)
	}
	text, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := checkoutpin.WriteLastLaunch(store, checkoutOf(path), text, time.Unix(0, 0)); err != nil {
		t.Fatal(err)
	}
}

func TestAGoodSavedConfigIsHandedBackOneKeyALine(t *testing.T) {
	path := configPath(t, new(savedConfig))
	pinned(t, path)
	r := configRun(t, path)
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

// The roles the launch summary remembers per team are a key of the file; a file written before
// it existed has none, and is still read.
func TestTheRememberedTeamRolesAreAKeyOfTheFile(t *testing.T) {
	path := configPath(t, new(strings.Replace(savedConfig, "LAST_LAUNCH_DATE", "LAST_TEAM_ROLES=\"team-a=coordinator,team-b=worker\"\nLAST_LAUNCH_DATE", 1)))
	pinned(t, path)
	r := configRun(t, path)
	if code := r.start(t); code != term.ExitOK {
		t.Fatalf("exit %d, stderr %q", code, r.stderr)
	}
	if want := "LAST_TEAMS\tdev1@team-a,qa2@team-b\nLAST_TEAM_ROLES\tteam-a=coordinator,team-b=worker\nLAST_LAUNCH_DATE\t"; !strings.Contains(r.stdout.String(), want) {
		t.Fatalf("stdout %q", r.stdout.String())
	}
}

func TestAFileOfFewerKeysIsReadAsItIs(t *testing.T) {
	// Which keys a format needs is the launcher's decision; the reader hands back what is there.
	path := configPath(t, new("SAVED_CONFIG_VERSION=3\n"))
	pinned(t, path)
	r := configRun(t, path)
	if code := r.start(t); code != term.ExitOK || r.stdout.String() != "SAVED_CONFIG_VERSION\t3\n" {
		t.Fatalf("exit %d, stdout %q, stderr %q", code, r.stdout, r.stderr)
	}
	path = configPath(t, new(""))
	pinned(t, path)
	r = configRun(t, path)
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

func TestAMissingFileIsNothingSaved(t *testing.T) {
	r := configRun(t, configPath(t, nil))
	if code := r.start(t); code != term.ExitOK || r.stdout.Len() != 0 || r.stderr.Len() != 0 {
		t.Fatalf("exit %d, stdout %q, stderr %q", code, r.stdout, r.stderr)
	}
}

func TestAnUnreadableFileIsReportedAndKept(t *testing.T) {
	// A directory cannot be read as a file, by root too.
	path := configPath(t, nil)
	if err := os.Mkdir(path, 0o700); err != nil {
		t.Fatal(err)
	}
	refused(t, configRun(t, path), path, "not a regular file", nil)
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

func TestTheFlagTakesACheckoutAndWhetherAnyoneIsThere(t *testing.T) {
	for _, args := range [][]string{{launchConfigFlag}, {launchConfigFlag, "a"}, {launchConfigFlag, "/a", "maybe"}, {launchConfigFlag, "/a", "attended", "b"}} {
		r := newRun(install(t), args...)
		if code := r.start(t); code != term.ExitUsage {
			t.Fatalf("%q: exit %d, want %d", args, code, term.ExitUsage)
		}
	}
}

func TestTheFlagIsAnsweredUnderLegacyToo(t *testing.T) {
	// The bash launcher asks it; handing it back to the bash would ask the bash itself.
	path := configPath(t, new(savedConfig))
	pinned(t, path)
	r := configRun(t, path)
	r.env(LegacyEnv + "=1")
	if code := r.start(t); code != term.ExitOK || len(r.execer.Execs()) != 0 {
		t.Fatalf("exit %d, execs %v", code, r.execer.Execs())
	}
}

// The hazard (Plan 00266 K5): a launch nobody attends takes only saved choices the launcher
// wrote itself or a person approved. A container's rewrite (another SSH key, another network)
// is refused, naming the file and the verb that approves it, and nothing is handed back.
func TestAnUnattendedLaunchTakesOnlyPinnedChoices(t *testing.T) {
	path := configPath(t, new(savedConfig))
	refused(t, configRunAs(t, path, "unattended"), path, "ccy-teams approve last-launch "+checkoutOf(path), new(savedConfig))
	pinned(t, path)
	r := configRunAs(t, path, "unattended")
	if code := r.start(t); code != term.ExitOK || !strings.Contains(r.stdout.String(), "LAST_SSH_KEYS\t/keys/id_a /keys/id b\n") {
		t.Fatalf("the launcher's own write: exit %d, stdout %q, stderr %q", code, r.stdout, r.stderr)
	}
	changed := strings.Replace(savedConfig, "/keys/id_a", "/home/u/.ssh/id_other", 1)
	if err := os.WriteFile(path, []byte(changed), 0o600); err != nil {
		t.Fatal(err)
	}
	refused(t, configRunAs(t, path, "unattended"), path, "changed since", new(changed))
}

// attendedRun is an attended read at a terminal (stdin and stderr; stdout is the bash's
// capture), the person answering answer.
func attendedRun(t *testing.T, path, answer string) *run {
	t.Helper()
	r := configRun(t, path)
	r.front.Stdin = strings.NewReader(answer)
	stdout := r.stdout
	r.front.IsTerminal = func(v any) bool { return v != stdout }
	return r
}

// Plan 00266 review finding 8: saved choices ccy did not write are shown, every value through
// checkoutpin.Shown under a heading naming the file, and asked about with a default of no. Only
// a typed yes hands them back and records them as approved; Enter or no hands nothing back
// (exit 0, so the launcher asks each choice), records nothing, and leaves the file as it is.
func TestAnAttendedLaunchAsksBeforeItTakesChoicesCcyDidNotWrite(t *testing.T) {
	for _, answer := range []string{"\n", "n\n"} {
		path := configPath(t, new(savedConfig))
		r := attendedRun(t, path, answer)
		if code := r.start(t); code != term.ExitOK || r.stdout.Len() != 0 {
			t.Fatalf("answer %q: exit %d, stdout %q, want exit 0 and nothing handed back", answer, code, r.stdout)
		}
		msg := r.stderr.String()
		for _, want := range []string{path, "LAST_SSH_KEYS", "/keys/id_a /keys/id b", "[y/N]", "asks each choice"} {
			if !strings.Contains(msg, want) {
				t.Fatalf("answer %q: stderr %q lacks %q", answer, msg, want)
			}
		}
		refused(t, configRunAs(t, path, "unattended"), path, "ccy-teams approve last-launch", new(savedConfig))
	}
	path := configPath(t, new(savedConfig))
	r := attendedRun(t, path, "y\n")
	if code := r.start(t); code != term.ExitOK || !strings.Contains(r.stdout.String(), "LAST_SSH_KEYS\t/keys/id_a /keys/id b\n") {
		t.Fatalf("a yes: exit %d, stdout %q, stderr %q", code, r.stdout, r.stderr)
	}
	unattended := configRunAs(t, path, "unattended")
	if code := unattended.start(t); code != term.ExitOK || !strings.Contains(unattended.stdout.String(), "LAST_TOKEN\tpersonal\n") {
		t.Fatalf("after a yes, a launch nobody attends: exit %d, stderr %q", code, unattended.stderr)
	}
}

// With no terminal to ask at, an attended read of choices ccy did not write is refused as an
// unattended one is: nothing is accepted on nobody's word.
func TestAnAttendedLaunchWithNoTerminalRefusesChoicesCcyDidNotWrite(t *testing.T) {
	path := configPath(t, new(savedConfig))
	r := configRun(t, path)
	r.front.Stdin = strings.NewReader("y\n")
	r.front.IsTerminal = func(any) bool { return false }
	refused(t, r, path, "ccy-teams approve last-launch", new(savedConfig))
}

// Choices ccy wrote are handed back unasked, attended or not.
func TestAnAttendedLaunchTakesItsOwnChoicesUnasked(t *testing.T) {
	path := configPath(t, new(savedConfig))
	pinned(t, path)
	r := attendedRun(t, path, "")
	if code := r.start(t); code != term.ExitOK || !strings.Contains(r.stdout.String(), "LAST_TOKEN\tpersonal\n") || strings.Contains(r.stderr.String(), "[y/N]") {
		t.Fatalf("exit %d, stdout %q, stderr %q", code, r.stdout, r.stderr)
	}
}

// writeRun is --write-launch-config of the checkout path is in, given text on stdin.
func writeRun(t *testing.T, path, text string) *run {
	t.Helper()
	r := newRun(install(t), writeConfigFlag, checkoutOf(path))
	r.env(stateEnv(path))
	r.front.Stdin = strings.NewReader(text)
	return r
}

// The launcher's write goes through the front, which pins exactly the bytes it writes, so the
// next launch nobody attends takes them.
func TestTheWriteIsPinned(t *testing.T) {
	path := configPath(t, nil)
	r := writeRun(t, path, savedConfig)
	if code := r.start(t); code != term.ExitOK {
		t.Fatalf("exit %d, stderr %q", code, r.stderr)
	}
	if got, err := os.ReadFile(path); err != nil || string(got) != savedConfig {
		t.Fatalf("wrote %q, %v", got, err)
	}
	r = configRunAs(t, path, "unattended")
	if code := r.start(t); code != term.ExitOK {
		t.Fatalf("the write was not pinned: exit %d, stderr %q", code, r.stderr)
	}
	r = writeRun(t, path, "LAST_TOKEN=\"$(id)\"\n")
	if code := r.start(t); code != term.ExitFailure {
		t.Fatalf("a value bash would expand was written: exit %d", code)
	}
	for _, args := range [][]string{{writeConfigFlag}, {writeConfigFlag, "rel"}, {writeConfigFlag, "/a", "b"}} {
		if code := newRun(install(t), args...).start(t); code != term.ExitUsage {
			t.Errorf("%q: exit %d, want %d", args, code, term.ExitUsage)
		}
	}
}

// networkRun is --launch-network over the checkout path is in.
func networkRun(t *testing.T, path string, args ...string) *run {
	t.Helper()
	r := newRun(install(t), append([]string{launchNetworkFlag, checkoutOf(path)}, args...)...)
	r.env(stateEnv(path))
	return r
}

// --launch-network prints the network the saved choices join; --forget blanks it, and the
// result is pinned again only when what it changes was pinned: a file nobody approved is
// removed instead, so a forget never approves a container's other choices.
func TestTheSavedNetworkIsReadAndForgotten(t *testing.T) {
	text := strings.Replace(savedConfig, "LAST_NETWORK=\"\"", "LAST_NETWORK=\"proj_net\"", 1)
	path := configPath(t, new(text))
	pinned(t, path)
	r := networkRun(t, path)
	if code := r.start(t); code != term.ExitOK || r.stdout.String() != "proj_net\n" {
		t.Fatalf("exit %d, stdout %q, stderr %q", code, r.stdout, r.stderr)
	}
	r = networkRun(t, path, "--forget", "other_net")
	if code := r.start(t); code != term.ExitOK || r.stderr.Len() != 0 {
		t.Fatalf("another network: exit %d, stderr %q", code, r.stderr)
	}
	r = networkRun(t, path, "--forget", "proj_net")
	if code := r.start(t); code != term.ExitOK || !strings.Contains(r.stdout.String(), "Cleared proj_net") {
		t.Fatalf("forget: exit %d, stdout %q, stderr %q", code, r.stdout, r.stderr)
	}
	if got, err := os.ReadFile(path); err != nil || string(got) != savedConfig {
		t.Fatalf("after the forget %q, %v", got, err)
	}
	if code := configRunAs(t, path, "unattended").start(t); code != term.ExitOK {
		t.Fatal("the forget's own write was not pinned")
	}
	if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
	r = networkRun(t, path, "--forget", "proj_net")
	if code := r.start(t); code != term.ExitOK || !strings.Contains(r.stdout.String(), "Removed") {
		t.Fatalf("an unapproved file: exit %d, stdout %q, stderr %q", code, r.stdout, r.stderr)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("the unapproved file was kept: %v", err)
	}
	r = networkRun(t, path)
	if code := r.start(t); code != term.ExitOK || r.stdout.Len() != 0 {
		t.Fatalf("no file: exit %d, stdout %q", code, r.stdout)
	}
}
