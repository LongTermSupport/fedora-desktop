package ftpcamera

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

// E1: help on stdout, exit 0, without the camera account (B5).
func TestHelp(t *testing.T) {
	for _, arg := range []string{"-h", "--help"} {
		k := newKit(t)
		k.fake.On([]string{"getent"}, execx.Result{ExitCode: 2})
		k.wantCode(k.run("--sort", arg), term.ExitOK)
		k.wantOut("Usage: ftp-camera [PRIMARY-MODE] [MODIFIER]", "--copy-preflight", "photos/YYYY/MM/DD/RAW/<name>.arw")
		if k.err.Len() != 0 {
			t.Errorf("stderr %q", k.err.String())
		}
	}
}

// E1, E2: arguments are read in order; an earlier unknown argument wins over --help.
func TestUnknownArgument(t *testing.T) {
	k := newKit(t)
	k.wantCode(k.run("--bogus", "--help"), exitUsage)
	k.wantErr("ERROR: unknown argument: --bogus\nRun 'ftp-camera --help' for usage\n")
	if k.out.Len() != 0 {
		t.Errorf("stdout %q", k.out.String())
	}
}

// E3, E4, E5.
func TestUsageRefusals(t *testing.T) {
	cases := []struct {
		args []string
		want string
	}{
		{[]string{"--view", "--view-jpg"}, "ERROR: --view and --view-jpg are mutually exclusive\n"},
		{[]string{"--view-jpg", "--view"}, "ERROR: --view and --view-jpg are mutually exclusive\n"},
		{[]string{"--sort", "--push"}, "ERROR: only one primary mode at a time\n  Pick one of: --sort --pass --push --copy --copy-preflight --prune --async --async-copy\n"},
		{[]string{"--async", "--copy"}, "ERROR: only one primary mode at a time\n"},
		{[]string{"--prune", "--view"}, "ERROR: --view / --view-jpg requires an FTP-server mode\n  (default, --async, or --async-copy). The mode you picked\n  exits before any uploads arrive, so there's nothing to view.\n"},
		{[]string{"--copy-preflight", "--hotspot"}, "ERROR: --hotspot requires an FTP-server mode\n  (default, --async, or --async-copy). The mode you picked\n  exits without running the FTP server.\n"},
		{[]string{"--pass", "--debug-ftp"}, "ERROR: --debug-ftp requires an FTP-server mode\n  (default, --async, or --async-copy). The mode you picked\n  never starts vsftpd, so there is no protocol to trace.\n"},
		{[]string{"--sort", "--view", "--hotspot"}, "requires an FTP-server mode"},
	}
	for _, tc := range cases {
		k := newKit(t)
		k.wantCode(k.run(tc.args...), exitUsage)
		k.wantErr(tc.want)
	}
}

// --async with --async-copy is one mode, the copying one, as the bash's flag variables made it.
func TestAsyncAndAsyncCopyTogetherAreOneMode(t *testing.T) {
	o, help, problems := parse([]string{"--async", "--async-copy", "--no-tui"})
	if help || len(problems) != 0 || !o.async || !o.asyncCopy || !o.server() {
		t.Fatalf("%+v %v %v", o, help, problems)
	}
}

// E6.
func TestBadTunableIsAUsageError(t *testing.T) {
	k := newKit(t)
	k.env["LINK_REPEAT_THRESHOLD"] = "three"
	k.wantCode(k.run("--sort"), exitUsage)
	k.wantErr("ERROR: LINK_REPEAT_THRESHOLD must be a non-negative whole number, got 'three'\n")
}

// E7.
func TestCameraAccountMissing(t *testing.T) {
	for _, res := range []execx.Result{{ExitCode: 2}, {Stdout: []byte("camera:x:1:1::")}} {
		k := newKit(t)
		k.fake.On([]string{"getent", "passwd", "camera"}, res)
		k.wantCode(k.run("--sort"), term.ExitFailure)
		k.wantErr("ERROR: 'camera' user not found — run play-ftp-camera.yml first\n")
	}
}

// E8: gum, three steps, the chosen flags carried into the run.
func TestLauncherWithGum(t *testing.T) {
	k := newKit(t)
	k.path("gum", "systemd-inhibit")
	answers := []string{"async-copy   [+viewer]  Sort + per-file rclone copy as uploads arrive", "view JPG", "use hotspot"}
	k.fake.OnFunc([]string{"gum", "choose"}, func(execx.Cmd) (execx.Result, error) {
		a := answers[0]
		answers = answers[1:]
		return execx.Result{Stdout: []byte(a + "\n")}, nil
	})
	k.wantCode(k.run(), term.ExitOK)
	var headers, selected []string
	for _, c := range k.fake.Calls() {
		if c.Name == "gum" {
			headers = append(headers, c.Args[2])
			selected = append(selected, c.Args[4])
		}
	}
	if len(headers) != 3 || !strings.HasPrefix(headers[0], "Step 1/3") || !strings.HasPrefix(headers[2], "Step 3/3") {
		t.Fatalf("headers %q", headers)
	}
	if selected[1] != "no viewer" || selected[2] != "no hotspot" {
		t.Errorf("selected %q", selected)
	}
	execs := k.fake.Execs()
	if len(execs) != 1 {
		t.Fatalf("execs %v", execs)
	}
	want := "systemd-inhibit --what=idle:sleep --who=ftp-camera --why=Camera FTP transfer in progress --mode=block -- /home/u/.local/bin/ftp-camera --async-copy --view-jpg --hotspot"
	if execs[0].Line() != want || !slices.Equal(execs[0].Env, []string{"_FTP_CAMERA_INHIBITED=1"}) {
		t.Errorf("exec %q env %q", execs[0].Line(), execs[0].Env)
	}
}

// E8: a non-server mode asks once; fzf when gum is missing.
func TestLauncherWithFzfNonServerMode(t *testing.T) {
	k := newKit(t)
	k.path("fzf", "exiftool")
	k.fake.On([]string{"fzf"}, execx.Result{Stdout: []byte("sort                    Sort already-uploaded files and exit\n")})
	k.realRoot()
	k.wantCode(k.run(), term.ExitOK)
	n := 0
	for _, c := range k.fake.Calls() {
		if c.Name == "fzf" {
			n++
		}
	}
	if n != 1 {
		t.Errorf("fzf asked %d times", n)
	}
	k.wantOut("Sort complete: 0 moved, 0 skipped")
	if len(k.fake.Execs()) != 0 {
		t.Error("--sort is not inhibit-wrapped")
	}
}

// E8: leaving a step is "Cancelled.", 130.
func TestLauncherCancelled(t *testing.T) {
	k := newKit(t)
	k.path("gum")
	k.fake.On([]string{"gum", "choose"}, execx.Result{ExitCode: 130})
	k.wantCode(k.run(), term.ExitCancelled)
	k.wantErr("Cancelled.\n")
	k2 := newKit(t)
	k2.path("fzf")
	k2.fake.On([]string{"fzf"}, execx.Result{ExitCode: 130})
	k2.wantCode(k2.run(), term.ExitCancelled)
	k3 := newKit(t)
	k3.path("fzf")
	k3.fake.On([]string{"fzf"}, execx.Result{})
	k3.wantCode(k3.run(), term.ExitCancelled)
}

func TestLauncherBrokenChooserIsAFailure(t *testing.T) {
	k := newKit(t)
	k.path("gum")
	k.fake.OnError([]string{"gum"}, errors.New("exec format error"))
	k.wantCode(k.run(), term.ExitFailure)
	k.wantErr("ERROR: the launcher could not ask: ")
}

// E8, E9, B9: no chooser, the note, the default mode, re-entered with --no-tui.
func TestNoChooserNoteAndDefaultMode(t *testing.T) {
	k := newKit(t)
	k.path("systemd-inhibit")
	k.wantCode(k.run(), term.ExitOK)
	k.wantErr("Note: neither 'gum' nor 'fzf' is installed — TUI unavailable.\n  Starting default FTP-server mode. Run 'ftp-camera --help'\n  for the full flag list, or install gum/fzf to get the picker.\n")
	if e := k.fake.Execs(); len(e) != 1 || !strings.HasSuffix(e[0].Line(), "-- /home/u/.local/bin/ftp-camera --no-tui") {
		t.Fatalf("execs %v", e)
	}
}

// E9: not wrapped when already inhibited; a failed exec is an error.
func TestInhibitGuardAndFailure(t *testing.T) {
	k := newKit(t)
	k.path("systemd-inhibit")
	k.fake.ExecError(errors.New("permission denied"))
	k.wantCode(k.run("--push"), term.ExitFailure)
	k.wantErr("ERROR: could not run systemd-inhibit: permission denied\n")
}

// E12.
func TestPass(t *testing.T) {
	k := newKit(t)
	k.wantCode(k.run("--pass"), term.ExitFailure)
	k.wantErr("ERROR: " + k.config + " not found — run play-ftp-camera.yml first\n")

	k.writeConfig("RCLONE_REMOTE=x:\n")
	k.wantCode(k.run("--pass"), term.ExitFailure)
	k.wantErr("ERROR: REPO_PATH not set in " + k.config + "\n")

	repo := t.TempDir()
	k.writeConfig("REPO_PATH=" + repo + "\n")
	k.wantCode(k.run("--pass"), term.ExitFailure)
	k.wantErr("ERROR: " + repo + "/vault.bash not found or not executable\n  Repo may have moved — re-run play-ftp-camera.yml from current location\n")

	vault := filepath.Join(repo, "vault.bash")
	writeFile(t, vault, "#!/bin/bash\n")
	if err := os.Chmod(vault, 0o755); err != nil {
		t.Fatal(err)
	}
	k.wantCode(k.run("--pass"), term.ExitOK)
	execs := k.fake.Execs()
	if len(execs) != 1 || execs[0].Line() != vault+" get ftp_camera_password" {
		t.Fatalf("execs %v", execs)
	}
	if k.out.Len() != 0 {
		t.Errorf("stdout %q", k.out.String())
	}
	k.fake.ExecError(errors.New("no such file"))
	k.wantCode(k.run("--pass"), term.ExitFailure)
	k.wantErr("ERROR: could not run " + vault + ": no such file\n")
}

// E11: a bad config line is an error naming the line.
func TestBadConfigLine(t *testing.T) {
	k := newKit(t)
	k.writeConfig("REPO_PATH=/x\nnonsense\n")
	k.wantCode(k.run("--pass"), term.ExitFailure)
	k.wantErr("ERROR: " + k.config + ":2: not a KEY=VALUE line\n")
}

// E10: colour on a terminal, never with NO_COLOR.
func TestColourOnlyOnATerminal(t *testing.T) {
	k := newKit(t)
	k.path("exiftool")
	k.colour = true
	k.realRoot()
	k.wantCode(k.run("--sort"), term.ExitOK)
	k.wantOut("\x1b[1mSorting uploads by EXIF date...\x1b[0m")
	k.env["NO_COLOR"] = "1"
	k.wantCode(k.run("--sort"), term.ExitOK)
	if strings.Contains(k.out.String(), "\x1b[") {
		t.Errorf("NO_COLOR still coloured: %q", k.out.String())
	}
}

func TestOutputThatCannotBeWrittenFailsTheRun(t *testing.T) {
	k := newKit(t)
	app := k.app()
	app.Env.Stdout = failingWriter{}
	if code := app.Run(t.Context(), []string{"--help"}); code != term.ExitFailure {
		t.Fatalf("exit %d", code)
	}
}

func TestNewEnvIsTheRealMachine(t *testing.T) {
	env := NewEnv()
	if env.ConfigPath != DefaultConfigPath || env.PID != os.Getpid() || env.Runner == nil || env.Self == "" {
		t.Fatalf("%+v", env)
	}
	if err := env.Sleep(t.Context(), 0); err != nil {
		t.Fatal(err)
	}
}
