package businstall

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

func TestNoArgumentsPrintsTheUsageOnStderrAndExits64(t *testing.T) {
	h := newHarness(t)
	if code := h.run(); code != term.ExitUsage {
		t.Fatalf("exit %d", code)
	}
	if h.stdout() != "" || !strings.HasPrefix(h.stderr(), "Usage: agent-bus-install COMMAND [OPTIONS]") {
		t.Fatalf("stdout %q stderr %q", h.stdout(), h.stderr())
	}
}

func TestHelpPrintsTheUsageOnStdoutAndIgnoresTheRest(t *testing.T) {
	for _, args := range [][]string{{"--help"}, {"-h"}, {"help"}, {"--help", "software", "--frob"}} {
		h := newHarness(t)
		if code := h.run(args...); code != 0 {
			t.Fatalf("%v: exit %d", args, code)
		}
		if !strings.HasPrefix(h.stdout(), "Usage: agent-bus-install COMMAND [OPTIONS]") || h.stderr() != "" {
			t.Fatalf("%v: stdout %q stderr %q", args, h.stdout(), h.stderr())
		}
		if h.stdout() != usageText {
			t.Fatalf("%v: the usage differs from the embedded text", args)
		}
	}
}

func TestEveryCommandOfTheUsageIsADispatchedCommand(t *testing.T) {
	var listed []string
	for line := range strings.Lines(usageText) {
		if rest, found := strings.CutPrefix(line, "  "); found && !strings.HasPrefix(rest, " ") {
			listed = append(listed, strings.Fields(rest)[0])
		}
	}
	want := []string{"software", "team", "remove", "backup-now", "restore", "backups", "check"}
	if !slices.Equal(listed, want) {
		t.Fatalf("usage lists %v", listed)
	}
}

func TestVersionPrintsTheSourceDigest(t *testing.T) {
	h := newHarness(t)
	if code := h.run("--version"); code != 0 || h.stdout() != h.digest+"\n" {
		t.Fatalf("exit %d, stdout %q", code, h.stdout())
	}
}

func TestUnknownCommandIsAUsageError(t *testing.T) {
	for _, command := range []string{"frobnicate", "", "--frob", "Software"} {
		h := newHarness(t)
		code := h.run(command)
		if code != term.ExitUsage || h.stderr() != "agent-bus-install: unknown command '"+command+"' (see --help)\n" {
			t.Errorf("%q: exit %d, stderr %q", command, code, h.stderr())
		}
	}
}

func TestOptionParsingErrors(t *testing.T) {
	cases := []struct {
		args []string
		want string
	}{
		{[]string{"software", "positional"}, "unexpected argument 'positional' (see --help)"},
		{[]string{"software", "-h"}, "unexpected argument '-h' (see --help)"},
		{[]string{"software", "--help"}, "unknown option --help (see --help)"},
		{[]string{"software", "--frob"}, "unknown option --frob (see --help)"},
		{[]string{"software", "--"}, "unknown option -- (see --help)"},
		{[]string{"software", "--=x"}, "unknown option -- (see --help)"},
		{[]string{"software", "--source", "/a", "--source", "/b"}, "--source given twice"},
		{[]string{"software", "--source=/a", "--source", "/b"}, "--source given twice"},
		{[]string{"remove", "--team", "alpha", "--purge", "--purge"}, "--purge given twice"},
		{[]string{"remove", "--team", "alpha", "--purge=yes"}, "--purge takes no value"},
		{[]string{"remove", "--team", "alpha", "--purge="}, "--purge takes no value"},
		{[]string{"software", "--source"}, "--source needs a value"},
		{[]string{"software", "--source="}, "--source needs a non-empty value"},
		{[]string{"software", "--source", ""}, "--source needs a non-empty value"},
		{[]string{"software"}, "software needs --source"},
		{[]string{"team"}, "team needs --team-file"},
		{[]string{"remove"}, "remove needs --team"},
		{[]string{"backup-now"}, "backup-now needs --team"},
		{[]string{"restore", "--team", "alpha"}, "restore needs --backup"},
		{[]string{"restore", "--backup", "1"}, "restore needs --team"},
		{[]string{"backups"}, "backups needs --team"},
		{[]string{"check"}, "check needs --team"},
		{[]string{"check", "--team", "Bad_Name"}, "--team 'Bad_Name' is not a team name (^[a-z][a-z0-9-]{0,23}$)"},
		{[]string{"check", "--team", "alpha\n"}, "--team 'alpha\n' is not a team name (^[a-z][a-z0-9-]{0,23}$)"},
		{[]string{"check", "--team", "../x"}, "--team '../x' is not a team name (^[a-z][a-z0-9-]{0,23}$)"},
		{[]string{"check", "--team", "-x"}, "--team '-x' is not a team name (^[a-z][a-z0-9-]{0,23}$)"},
		{[]string{"check", "--team", strings.Repeat("a", 25)}, "--team '" + strings.Repeat("a", 25) + "' is not a team name (^[a-z][a-z0-9-]{0,23}$)"},
		{[]string{"restore", "--team", "alpha", "--backup", "latest"}, "--backup 'latest' is not a backup number"},
		{[]string{"restore", "--team", "alpha", "--backup", "0"}, "--backup '0' is not a backup number"},
		{[]string{"restore", "--team", "alpha", "--backup", "01"}, "--backup '01' is not a backup number"},
		{[]string{"restore", "--team", "alpha", "--backup", "-1"}, "--backup '-1' is not a backup number"},
		{[]string{"restore", "--team", "alpha", "--backup", "1\n"}, "--backup '1\n' is not a backup number"},
	}
	for _, tc := range cases {
		h := newHarness(t)
		code := h.run(tc.args...)
		if code != term.ExitUsage || h.stderr() != "agent-bus-install: "+tc.want+"\n" {
			t.Errorf("%q: exit %d, stderr %q; want %q", tc.args, code, h.stderr(), tc.want)
		}
		if len(h.sim.log) != 0 {
			t.Errorf("%q ran %v", tc.args, h.sim.log)
		}
	}
}

func TestOptionsTakeTheValueAfterTheNameOrAfterAnEquals(t *testing.T) {
	h := newHarness(t)
	if code := h.run("backups", "--team=alpha"); code != term.ExitConfig {
		t.Fatalf("--team=alpha: exit %d, stderr %q", code, h.stderr())
	}
	contains(t, "stderr", h.stderr(), "team alpha is not installed here")
	// The value may itself look like an option.
	if code := h.run("software", "--source", "--frob"); code != term.ExitUsage {
		t.Fatalf("exit %d", code)
	}
	contains(t, "stderr", h.stderr(), "--source must be an absolute path")
	if code := h.run("software", "--source=a=b"); code != term.ExitUsage {
		t.Fatalf("exit %d", code)
	}
	contains(t, "stderr", h.stderr(), "--source must be an absolute path")
}

func TestUsageErrorsComeBeforeTheRootCheck(t *testing.T) {
	h := newHarness(t)
	h.app.cfg.Prefix = ""
	if code := h.run("check", "--team", "Bad"); code != term.ExitUsage {
		t.Fatalf("exit %d", code)
	}
}

func TestANonRootCallerIsRefusedWithoutAPrefix(t *testing.T) {
	for _, args := range [][]string{
		{"check", "--team", "alpha"},
		{"remove", "--team", "alpha"},
		{"backup-now", "--team", "alpha"},
		{"backups", "--team", "alpha"},
		{"restore", "--team", "alpha", "--backup", "1"},
		{"team", "--team-file", "/x"},
		{"software", "--source", "/x"},
	} {
		h := newHarness(t)
		h.app.cfg.Prefix = ""
		code := h.run(args...)
		if code != term.ExitNotRoot || h.stderr() != "agent-bus-install: run it as root: sudo agent-bus-install ...\n" {
			t.Errorf("%v: exit %d, stderr %q", args, code, h.stderr())
		}
		if len(h.sim.log) != 0 {
			t.Errorf("%v ran %v", args, h.sim.log)
		}
	}
}

func TestRootPassesTheCheckWithoutAPrefix(t *testing.T) {
	h := newHarness(t)
	h.app.cfg.Prefix = ""
	h.app.cfg.Euid = func() int { return 0 }
	// The check passes; the run then fails on the real /var/lib/agent-bus it does not have.
	if code := h.run("backups", "--team", "alpha"); code == term.ExitNotRoot {
		t.Fatal("root was refused")
	}
}

func TestSayAndDieUseTheProgramPrefixOnStderr(t *testing.T) {
	h := newHarness(t)
	h.app.sayf("hello %d", 3)
	if h.stderr() != "agent-bus-install: hello 3\n" || h.stdout() != "" {
		t.Fatalf("stderr %q stdout %q", h.stderr(), h.stdout())
	}
}

func TestAFailedWriteToStdoutFailsTheRun(t *testing.T) {
	h := newHarness(t)
	h.app.cfg.Stdout = failingWriter{}
	h.app.stdout = &sink{w: failingWriter{}}
	if code := h.run("--help"); code != term.ExitFailure {
		t.Fatalf("exit %d", code)
	}
	contains(t, "stderr", h.stderr(), "writing to stdout")
}

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) { return 0, errors.New("disk full") }

func TestProductionConfigIsTheRootedHostConfiguration(t *testing.T) {
	var out, errw bytes.Buffer
	cfg := Production(&out, &errw, "digest")
	if cfg.Prefix != "" || cfg.AdminTool != "/usr/local/bin/agent-bus" ||
		cfg.Path != "/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin" || cfg.SourceDigest != "digest" {
		t.Fatalf("config %+v", cfg)
	}
	seams := []bool{
		cfg.Euid != nil, cfg.RootOwner != nil, cfg.SvcOwner != nil, cfg.Executable != nil,
		cfg.Runner != nil, cfg.Sleep != nil, cfg.Rand != nil, cfg.Pid != 0,
	}
	if slices.Contains(seams, false) {
		t.Fatalf("a seam is not wired: %+v", cfg)
	}
	exe, err := cfg.Executable()
	if err != nil || !filepath.IsAbs(exe) {
		t.Fatalf("executable %q, %v", exe, err)
	}
	if cfg.Euid() != os.Geteuid() {
		t.Fatal("Euid is not the process's")
	}
}

func TestTestConfigRootsEverythingUnderThePrefixAndKeepsThePath(t *testing.T) {
	prefix := t.TempDir()
	var out, errw bytes.Buffer
	cfg, err := Test(prefix, &out, &errw, "digest")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Prefix != prefix || cfg.AdminTool != "agent-bus" || cfg.Path != "" {
		t.Fatalf("config %+v", cfg)
	}
	root, rootErr := cfg.RootOwner()
	svc, svcErr := cfg.SvcOwner()
	if rootErr != nil || svcErr != nil || root != svc || root.UID != os.Getuid() {
		t.Fatalf("owners %v %v (%v, %v)", root, svc, rootErr, svcErr)
	}
}

func TestTestConfigRefusesAPrefixThatIsNotAnExistingAbsoluteDirectory(t *testing.T) {
	file := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(file, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	for _, prefix := range []string{"relative/dir", filepath.Join(t.TempDir(), "absent"), file, ""} {
		_, err := Test(prefix, &bytes.Buffer{}, &bytes.Buffer{}, "d")
		if err == nil || err.Error() != "AGENT_BUS_INSTALL_TEST_PREFIX must be an existing absolute directory" {
			t.Errorf("Test(%q) = %v", prefix, err)
		}
	}
}

func TestParseOptionsDirectly(t *testing.T) {
	spec := map[string]bool{"team": true, "purge": false}
	opts, err := parseOptions([]string{"--team=a", "--purge"}, spec)
	if err != nil || opts["team"] != "a" || opts["purge"] != "1" {
		t.Fatalf("%v, %v", opts, err)
	}
	if _, set := opts["absent"]; set {
		t.Fatal("an unset option is set")
	}
	if _, err := parseOptions(nil, spec); err != nil {
		t.Fatal(err)
	}
}
