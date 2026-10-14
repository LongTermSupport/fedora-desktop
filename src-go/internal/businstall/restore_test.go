package businstall

import (
	"archive/tar"
	"bytes"
	"os"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

type tarMember struct {
	name     string
	typeflag byte
	mode     int64
	data     string
	link     string
}

// writeTar writes a tar with exactly the given members at path.
func writeTar(t *testing.T, path string, members ...tarMember) {
	t.Helper()
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	for _, m := range members {
		hdr := &tar.Header{Name: m.name, Typeflag: m.typeflag, Mode: m.mode, Linkname: m.link, Size: int64(len(m.data))}
		if m.typeflag == tar.TypeDir || m.typeflag == tar.TypeSymlink {
			hdr.Size = 0
		}
		if err := tw.WriteHeader(hdr); err != nil {
			t.Fatal(err)
		}
		if hdr.Size > 0 {
			if _, err := tw.Write([]byte(m.data)); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, buf.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
}

func goodMembers() []tarMember {
	return []tarMember{
		{name: "secrets", typeflag: tar.TypeDir, mode: 0o700},
		{name: "secrets/registration_shared_secret", typeflag: tar.TypeReg, mode: 0o600, data: "backed-up-secret"},
		{name: "team.json", typeflag: tar.TypeReg, mode: 0o640, data: `{"team":"alpha","port":8448,"state":"present","listen":["192.0.2.10"]}`},
	}
}

const tarOfAlpha = "var/lib/agent-bus-install/alpha/backups/agent-bus-state-1.tar"

// backedUp is a running alpha with one backup.
func backedUp(t *testing.T) *harness {
	t.Helper()
	h := newHarness(t)
	h.installTeam(nil)
	if code := h.run("backup-now", "--team", "alpha"); code != 0 {
		t.Fatalf("backup-now exit %d:\n%s", code, h.stderr())
	}
	return h
}

func TestRestoreReplacesTheStateAndRestartsTheHomeserver(t *testing.T) {
	h := backedUp(t)
	sharedPath := "var/lib/agent-bus/alpha/secrets/registration_shared_secret"
	original := h.read(sharedPath)
	h.write(h.rel(sharedPath), "tampered", 0o600)
	h.write(h.rel("var/lib/agent-bus/alpha/registry.json"), "tampered", 0o600)
	if code := h.run("restore", "--team", "alpha", "--backup", "1"); code != 0 {
		t.Fatalf("exit %d:\n%s", code, h.stderr())
	}
	if h.read(sharedPath) != original || h.mode(sharedPath) != 0o600 || h.mode("var/lib/agent-bus/alpha/secrets") != 0o700 {
		t.Error("the secret was not restored with its modes")
	}
	if h.exists("var/lib/agent-bus/alpha/registry.json") {
		t.Error("a state file absent from the backup was kept")
	}
	if h.mode("var/lib/agent-bus/alpha/team.json") != 0o640 {
		t.Errorf("team.json mode %o", h.mode("var/lib/agent-bus/alpha/team.json"))
	}
	if h.exists("var/lib/agent-bus-applied/alpha.json") {
		t.Error("the stamp survived a restore")
	}
	var seq []string
	for _, line := range h.sim.log {
		switch {
		case strings.HasPrefix(line, "systemctl stop"), strings.HasPrefix(line, "systemd-run"),
			strings.HasPrefix(line, "systemctl start"), strings.HasPrefix(line, "agent-bus bootstrap"):
			seq = append(seq, firstWords(line)+" "+lastWord(line))
		}
	}
	want := []string{
		"systemctl stop agent-bus-hs@alpha.service", "systemd-run --unit=agent-bus-restore-alpha.service",
		"systemctl stop agent-bus-restore-alpha.service", "systemctl start agent-bus-hs@alpha.service",
		"agent-bus bootstrap alpha",
	}
	if len(seq) != len(want) {
		t.Fatalf("sequence %v", seq)
	}
	if !strings.HasSuffix(seq[1], "--restore-backup 1") && !strings.Contains(strings.Join(h.sim.log, "\n"), "--restore-backup 1") {
		t.Errorf("sequence %v", seq)
	}
	if h.stdout() != "CHANGED\tremoved the alpha applied stamp\nCHANGED\trestored team alpha from backup 1\n" {
		t.Fatalf("stdout %q", h.stdout())
	}
}

func lastWord(line string) string {
	fields := strings.Fields(line)
	return fields[len(fields)-1]
}

func TestRestoreRunsTheOneOffUnderTheHomeserversSandbox(t *testing.T) {
	h := backedUp(t)
	h.run("restore", "--team", "alpha", "--backup", "1")
	var run string
	for _, line := range h.sim.log {
		if strings.HasPrefix(line, "systemd-run") {
			run = line
		}
	}
	for _, want := range []string{
		"--unit=agent-bus-restore-alpha.service --collect ",
		"--property=User=agent-bus ",
		"--property=Environment=TUWUNEL_CONFIG=/var/lib/agent-bus/alpha/tuwunel.toml ",
		"--property=ExecStartPre=/bin/true ",
		"--property=NoNewPrivileges=yes ",
		"--property=ReadWritePaths=/var/lib/agent-bus/alpha ",
		"--property=IPAddressDeny=any ",
		"--property=IPAddressAllow=127.0.0.1/32 ::1/128 ",
		"--property=SocketBindAllow=tcp:8448 ",
		"-- /usr/local/lib/agent-bus/tuwunel --restore-backup 1",
	} {
		if !strings.Contains(run, want) {
			t.Errorf("the one-off lacks %q:\n%s", want, run)
		}
	}
	for _, unwanted := range []string{"--property=ExecStart=", "--property=Description", "WantedBy"} {
		if strings.Contains(run, unwanted) {
			t.Errorf("the one-off carries %q", unwanted)
		}
	}
}

func TestRestoreRefusesAnUnknownBackupAndAMissingStateTar(t *testing.T) {
	h := backedUp(t)
	code := h.run("restore", "--team", "alpha", "--backup", "7")
	if code != term.ExitConfig || !strings.Contains(h.stderr(), "team alpha has no database backup 7") {
		t.Fatalf("exit %d, stderr %q", code, h.stderr())
	}
	if err := os.Remove(h.rel(tarOfAlpha)); err != nil {
		t.Fatal(err)
	}
	code = h.run("restore", "--team", "alpha", "--backup", "1")
	if code != term.ExitConfig || !strings.Contains(h.stderr(), "team alpha has no state tar for backup 1") {
		t.Fatalf("exit %d, stderr %q", code, h.stderr())
	}
	if h.sim.lines(`^systemctl stop`) != 0 {
		t.Fatal("the homeserver was stopped")
	}
}

func TestRestoreNeedsTheTeamAndTheSoftware(t *testing.T) {
	h := newHarness(t)
	if code := h.run("restore", "--team", "alpha", "--backup", "1"); code != term.ExitConfig {
		t.Fatalf("exit %d", code)
	}
	h2 := backedUp(t)
	if err := os.Remove(h2.rel("usr/local/lib/agent-bus/agent-bus.pyz")); err != nil {
		t.Fatal(err)
	}
	code := h2.run("restore", "--team", "alpha", "--backup", "1")
	if code != term.ExitFailure || !strings.Contains(h2.stderr(), "run agent-bus-install software first") {
		t.Fatalf("exit %d, stderr %q", code, h2.stderr())
	}
}

func TestRestoreRefusesATamperedStateTarBeforeStoppingAnything(t *testing.T) {
	cases := []struct {
		name    string
		members []tarMember
		want    string
	}{
		{"a stray member", append(goodMembers(), tarMember{name: "etc/passwd", typeflag: tar.TypeReg, mode: 0o644, data: "x"}), "etc/passwd"},
		{"a setuid file", append(goodMembers(), tarMember{name: "secrets/sh", typeflag: tar.TypeReg, mode: 0o4755, data: "x"}), "setuid"},
		{"a member climbing out", append(goodMembers(), tarMember{name: "secrets/../../x", typeflag: tar.TypeReg, mode: 0o600, data: "x"}), "secrets/../../x"},
		{"a symlink", append(goodMembers(), tarMember{name: "registry.json", typeflag: tar.TypeSymlink, mode: 0o777, link: "/etc"}), "registry.json"},
		{"no secrets", []tarMember{{name: "team.json", typeflag: tar.TypeReg, mode: 0o640, data: "{}"}}, "secrets is missing"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := backedUp(t)
			writeTar(t, h.rel(tarOfAlpha), tc.members...)
			secret := h.read("var/lib/agent-bus/alpha/secrets/registration_shared_secret")
			code := h.run("restore", "--team", "alpha", "--backup", "1")
			if code != term.ExitConfig {
				t.Fatalf("exit %d, stderr %q", code, h.stderr())
			}
			contains(t, "stderr", h.stderr(), tc.want)
			contains(t, "stderr", h.stderr(), "state tar /var/lib/agent-bus-install/alpha/backups/agent-bus-state-1.tar refused (above); nothing changed")
			if h.sim.lines(`^systemctl stop`) != 0 || h.sim.lines(`^systemd-run`) != 0 {
				t.Errorf("something ran: %v", h.sim.log)
			}
			if h.read("var/lib/agent-bus/alpha/secrets/registration_shared_secret") != secret {
				t.Error("the secret changed")
			}
		})
	}
}

func TestRestoreRefusesAFileThatIsNotATar(t *testing.T) {
	h := backedUp(t)
	h.write(h.rel(tarOfAlpha), "this is not a tar archive at all", 0o600)
	code := h.run("restore", "--team", "alpha", "--backup", "1")
	if code != term.ExitConfig || h.sim.lines(`^systemctl stop`) != 0 {
		t.Fatalf("exit %d, stderr %q", code, h.stderr())
	}
}

func TestRestoreAFailedOneOffRunLeavesAHintAndTheNextTryWorks(t *testing.T) {
	h := backedUp(t)
	h.sim.failStart["agent-bus-restore-alpha.service"] = true
	code := h.run("restore", "--team", "alpha", "--backup", "1")
	if code != term.ExitFailure || !strings.Contains(h.stderr(), "agent-bus-restore-alpha.service is inactive") {
		t.Fatalf("exit %d, stderr %q", code, h.stderr())
	}
	contains(t, "stderr", h.stderr(), "agent-bus-install: the homeserver of alpha is stopped and its state is replaced: run restore again, or team to rebuild\n")
	delete(h.sim.failStart, "agent-bus-restore-alpha.service")
	if code := h.run("restore", "--team", "alpha", "--backup", "1"); code != 0 {
		t.Fatalf("the retry exited %d:\n%s", code, h.stderr())
	}
}

func TestRestoreDoesNotHintWhenItFailsBeforeStoppingAnything(t *testing.T) {
	h := backedUp(t)
	writeTar(t, h.rel(tarOfAlpha), tarMember{name: "team.json", typeflag: tar.TypeReg, mode: 0o640, data: "{}"})
	h.run("restore", "--team", "alpha", "--backup", "1")
	if strings.Contains(h.stderr(), "is stopped and its state is replaced") {
		t.Fatalf("stderr %q", h.stderr())
	}
}

func TestRestoreRefusesAUnitWithASpecifierItCannotExpand(t *testing.T) {
	h := backedUp(t)
	h.write(h.rel("etc/systemd/system/agent-bus-hs@.service"), strings.Replace(hsUnitText, "IPAddressDeny=any\n", "IPAddressDeny=any\nDescription=%H\n", 1), 0o644)
	code := h.run("restore", "--team", "alpha", "--backup", "1")
	if code != term.ExitFailure || !strings.Contains(h.stderr(), "agent-bus-hs@.service: unsupported specifier in 'Description=%H'") {
		t.Fatalf("exit %d, stderr %q", code, h.stderr())
	}
	if h.sim.lines(`^systemctl stop`) != 0 {
		t.Fatal("the homeserver was stopped")
	}
}

func TestRestoreWritesOptionalStateAndNestedSecrets(t *testing.T) {
	h := backedUp(t)
	members := append(goodMembers(),
		tarMember{name: "secrets/nested", typeflag: tar.TypeDir, mode: 0o700},
		tarMember{name: "secrets/nested/key", typeflag: tar.TypeReg, mode: 0o644, data: "k"},
		tarMember{name: "registry.json", typeflag: tar.TypeReg, mode: 0o644, data: `{"r":1}`},
		tarMember{name: "room_id", typeflag: tar.TypeReg, mode: 0o644, data: "!room"},
	)
	writeTar(t, h.rel(tarOfAlpha), members...)
	if code := h.run("restore", "--team", "alpha", "--backup", "1"); code != 0 {
		t.Fatalf("exit %d:\n%s", code, h.stderr())
	}
	team := "var/lib/agent-bus/alpha/"
	if h.read(team+"secrets/nested/key") != "k" || h.mode(team+"secrets/nested/key") != 0o600 || h.mode(team+"secrets/nested") != 0o700 {
		t.Error("the nested secret is wrong")
	}
	if h.read(team+"registry.json") != `{"r":1}` || h.mode(team+"registry.json") != 0o600 || h.read(team+"room_id") != "!room" {
		t.Error("the optional state is wrong")
	}
}

func TestRestoreSandboxPropertiesSkipCommentsExecStartAndOtherSections(t *testing.T) {
	text := "# c\n[Unit]\nDescription=skipped\n\n[Service]\nExecStart=/x\nExecStartPre=/y\nUser=u %i\nWeird\n# comment\n[Install]\nWantedBy=x\n"
	got, err := sandboxProperties(text, "t")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(got, "|") != "ExecStartPre=/y|User=u t|Weird" {
		t.Fatalf("got %v", got)
	}
	if _, err = sandboxProperties("[Service]\nA=%n\n", "t"); err == nil {
		t.Fatal("an unsupported specifier was accepted")
	}
	got, err = sandboxProperties("[Service]\nLast=no newline", "t")
	if err != nil || len(got) != 1 || got[0] != "Last=no newline" {
		t.Fatalf("got %v, %v", got, err)
	}
}

func TestRestoreEveryCommandItRunsPropagatesItsFailure(t *testing.T) {
	probe := backedUp(t)
	probe.run("restore", "--team", "alpha", "--backup", "1")
	seen := map[string]bool{}
	for _, line := range probe.sim.log {
		seen[firstWords(line)] = true
	}
	for prefix := range seen {
		if prefix == "curl --fail" {
			continue
		}
		h := backedUp(t)
		h.sim.fails = []failRule{{prefix: prefix, code: 5}}
		if code := h.run("restore", "--team", "alpha", "--backup", "1"); code == 0 {
			t.Errorf("a failing %q did not fail the run", prefix)
		}
	}
}
