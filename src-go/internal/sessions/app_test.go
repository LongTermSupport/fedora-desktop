package sessions

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestHelpPrintsTheHeaderTextOnStdoutBeforeAnyGuard(t *testing.T) {
	h := newHarness(t)
	h.env["CCY_LIB"] = "/nonexistent"
	for _, flag := range []string{"-h", "--help"} {
		h.out.Reset()
		exitCodeIs(t, h.run(flag), 0, h)
		contains(t, flag, h.stdout(), "Usage:", "notify", "reboot", "restore", "Runs on the HOST")
	}
	if h.stderr() != "" {
		t.Fatalf("help writes nothing to stderr: %q", h.stderr())
	}
}

func TestUsageErrorsAreExit64WithTheSeeHelpSuffix(t *testing.T) {
	cases := []struct {
		args []string
		msg  string
	}{
		{[]string{"--bogus"}, "ccy-sessions: unknown option '--bogus'. See ccy-sessions --help.\n"},
		{[]string{"--list", "extra"}, "ccy-sessions: '--list' takes no further arguments. See ccy-sessions --help.\n"},
		{[]string{"-l", "x"}, "ccy-sessions: '-l' takes no further arguments. See ccy-sessions --help.\n"},
	}
	for _, c := range cases {
		h := newHarness(t)
		exitCodeIs(t, h.run(c.args...), 64, h)
		if h.stderr() != c.msg || h.stdout() != "" {
			t.Fatalf("%v: stderr %q stdout %q", c.args, h.stderr(), h.stdout())
		}
	}
}

func TestGuardRefusesWithoutTheCCYLibraryAndSaysWhere(t *testing.T) {
	h := newHarness(t)
	h.env["CCY_LIB"] = filepath.Join(h.root, "missing")
	exitCodeIs(t, h.run("--list"), 1, h)
	contains(t, "host without ccy", h.stderr(), "CCY is not installed here ("+h.env["CCY_LIB"]+"/tmux-session.bash missing). Run playbooks/imports/play-claude-yolo.yml.")

	h = newHarness(t)
	h.env["CCY_LIB"] = filepath.Join(h.root, "missing")
	h.app.InContainer = func() bool { return true }
	exitCodeIs(t, h.run("notify", "reboot-cancelled"), 1, h)
	if h.stderr() != "ccy-sessions runs on the host, not inside a CCY container.\n" {
		t.Fatalf("stderr %q", h.stderr())
	}

	h = newHarness(t)
	h.env["CCY_LIB"] = h.root // a directory without the file
	exitCodeIs(t, h.run("--list"), 1, h)
}

func TestTheDefaultLibraryDirectoryIsUsedWhenCCYLibIsUnset(t *testing.T) {
	h := newHarness(t)
	delete(h.env, "CCY_LIB")
	exitCodeIs(t, h.run("--list"), 1, h)
	contains(t, "default lib", h.stderr(), "/var/local/claude-yolo/lib/tmux-session.bash missing")
}

func TestConfigReadsTheKnobs(t *testing.T) {
	env := map[string]string{
		"CCY_SESSIONS_MINUTE_SECONDS": "0", "CCY_SESSIONS_POLL_SECONDS": "0.5", "CCY_SESSIONS_SET_GOING_WAIT": "7",
		"CCY_SESSIONS_START_WINDOW": "3600", "CCY_SESSIONS_SUBMIT_DELAY": "0", "CCY_CPU_SAMPLE_SECONDS": "0",
		"CCY_TMUX_TAKE_OVER_WAIT_TENTHS": "3", "CCY_RESTORE_COMPACT_FLOOR_TOKENS": "150000",
		unlockFileEnv: "/p", "CCY_BOOT_ID_FILE": "/b",
	}
	cfg, err := loadConfig(func(k string) string { return env[k] })
	if err != nil {
		t.Fatal(err)
	}
	want := Config{
		CcyLib: defaultLib, Minute: 0, Poll: 500 * time.Millisecond, SetGoingWait: 7 * time.Second, StartWindow: time.Hour,
		SubmitDelay: 0, CPUSample: 0, TakeOverWaitTenths: 3, CompactFloor: ParseTokenFloor("150000"), PassphraseFile: "/p", BootIDFile: "/b",
	}
	if cfg != want {
		t.Fatalf("config %+v, want %+v", cfg, want)
	}
}

func TestConfigDefaults(t *testing.T) {
	cfg, err := loadConfig(func(string) string { return "" })
	want := Config{
		CcyLib: defaultLib, Minute: time.Minute, Poll: 5 * time.Second, SetGoingWait: 1200 * time.Second,
		StartWindow: 120 * time.Second, SubmitDelay: 200 * time.Millisecond, CPUSample: time.Second, TakeOverWaitTenths: 50,
	}
	if err != nil || cfg != want {
		t.Fatalf("defaults %+v, %v", cfg, err)
	}
}

func TestConfigRefusesWhatIsNotANumber(t *testing.T) {
	for _, bad := range []string{"soon", "-1", "NaN", "Inf"} {
		_, err := loadConfig(func(k string) string {
			if k == "CCY_SESSIONS_POLL_SECONDS" {
				return bad
			}
			return ""
		})
		if err == nil || !strings.Contains(err.Error(), "CCY_SESSIONS_POLL_SECONDS is '"+bad+"', not a number of seconds") {
			t.Fatalf("%q: %v", bad, err)
		}
	}
	_, err := loadConfig(func(k string) string {
		if k == "CCY_TMUX_TAKE_OVER_WAIT_TENTHS" {
			return "-2"
		}
		return ""
	})
	if err == nil || !strings.Contains(err.Error(), "CCY_TMUX_TAKE_OVER_WAIT_TENTHS is '-2'") {
		t.Fatalf("tenths: %v", err)
	}
}

func TestABadKnobStopsTheRunBeforeAnythingHappens(t *testing.T) {
	h := newHarness(t)
	h.env["CCY_SESSIONS_START_WINDOW"] = "later"
	exitCodeIs(t, h.run("--list"), 1, h)
	contains(t, "bad knob", h.stderr(), "ERROR: CCY_SESSIONS_START_WINDOW is 'later', not a number of seconds")
	if len(h.ran()) != 0 {
		t.Fatalf("nothing runs: %v", h.ran())
	}
}

func TestWireFillsEverySeam(t *testing.T) {
	app := &App{}
	app.wire()
	set := map[string]bool{
		"Runner": app.Runner != nil, "Execer": app.Execer != nil, "Stdin": app.Stdin != nil, "Stdout": app.Stdout != nil,
		"Stderr": app.Stderr != nil, "Getenv": app.Getenv != nil, "Unsetenv": app.Unsetenv != nil, "Term": app.Term != nil,
		"Tmux": app.Tmux != nil, "Git": app.Git != nil, "Picker": app.Picker != nil, "Now": app.Now != nil,
		"Pause": app.Pause != nil, "Signals": app.Signals != nil, "IsTerminal": app.IsTerminal != nil,
		"InContainer": app.InContainer != nil, "Executable": app.Executable != nil, "Getuid": app.Getuid != nil,
	}
	for name, ok := range set {
		if !ok {
			t.Fatalf("%s was left unset", name)
		}
	}
	if NewApp() == nil {
		t.Fatal("NewApp")
	}
}

func TestWireKeepsWhatWasAlreadySet(t *testing.T) {
	app := &App{}
	app.wire()
	kept := app.Git
	app.wire()
	if app.Git != kept {
		t.Fatal("wire replaced an adapter that was already set")
	}
}

func TestProbeIsBuiltFromTheConfiguredSample(t *testing.T) {
	h := newHarness(t)
	h.app.Probe = nil
	h.env["CCY_CPU_SAMPLE_SECONDS"] = "0"
	h.tmuxOut("list-sessions", "")
	exitCodeIs(t, h.run("--list"), 0, h)
	if h.app.Probe == nil || h.app.Probe.SampleFor != time.Nanosecond {
		t.Fatalf("a zero sample is the shortest one, not the default second: %+v", h.app.Probe)
	}
}

func TestPauseForWaitsOrIsInterrupted(t *testing.T) {
	if pauseFor(0, nil) {
		t.Fatal("a zero wait with no signal does not interrupt")
	}
	sig := make(chan os.Signal, 1)
	sig <- os.Interrupt
	if !pauseFor(time.Hour, sig) {
		t.Fatal("a waiting signal interrupts a long wait")
	}
	sig <- os.Interrupt
	if !pauseFor(0, sig) {
		t.Fatal("a waiting signal interrupts a zero wait")
	}
	if pauseFor(time.Millisecond, nil) {
		t.Fatal("an unsignalled wait elapses")
	}
	ch, stop := notifySignals()
	if ch == nil {
		t.Fatal("no channel")
	}
	stop()
}

func TestIsTerminalIsFalseForEverythingThatIsNotOne(t *testing.T) {
	if isTerminal("not a file") {
		t.Fatal("only a file can be a terminal")
	}
	null, err := os.Open(os.DevNull)
	if err != nil {
		t.Fatal(err)
	}
	if isTerminal(null) {
		t.Fatal("/dev/null is not a terminal")
	}
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	if isTerminal(reader) {
		t.Fatal("a pipe is not a terminal")
	}
	if err = errors.Join(null.Close(), reader.Close(), writer.Close()); err != nil {
		t.Fatal(err)
	}
	if isTerminal(null) {
		t.Fatal("a closed file is not a terminal")
	}
}

func TestIsExecutable(t *testing.T) {
	dir := t.TempDir()
	script := filepath.Join(dir, "x")
	if err := os.WriteFile(script, []byte("#!/bin/sh\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	chmod(t, script, 0o700)
	plain := filepath.Join(dir, "y")
	if err := os.WriteFile(plain, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if !isExecutable(script) || isExecutable(plain) || isExecutable(filepath.Join(dir, "none")) {
		t.Fatal("executable means the execute bit is usable")
	}
}

func TestStdoutIsFedByOutAndStderrByTheTerm(t *testing.T) {
	h := newHarness(t)
	h.app.out("payload\n")
	h.app.sayf("status %d\n", 1)
	h.app.fail("broke")
	h.app.failErr(errBoom)
	if h.stdout() != "payload\n" || h.stderr() != "status 1\nERROR: broke\nERROR: boom\n" {
		t.Fatalf("stdout %q stderr %q", h.stdout(), h.stderr())
	}
}

func TestParseTokenFloor(t *testing.T) {
	if n, ok := ParseTokenFloor("150000").Tokens(); !ok || n != 150000 {
		t.Fatalf("got %d, %v", n, ok)
	}
	for _, bad := range []string{"", "0", "-5", "012", "1.5", "lots", "99999999999999999999"} {
		f := ParseTokenFloor(bad)
		if _, ok := f.Tokens(); ok {
			t.Errorf("%q should not be a floor", bad)
		}
		if f.String() != bad {
			t.Errorf("%q is reported as %q", bad, f.String())
		}
	}
}
