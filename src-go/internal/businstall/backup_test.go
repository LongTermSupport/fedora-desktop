package businstall

import (
	"archive/tar"
	"bytes"
	"errors"
	"io"
	"os"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

func TestBackupNowSignalsTheHomeserverAndKeepsTheStateInRootsDirectory(t *testing.T) {
	h := newHarness(t)
	h.installTeam(nil)
	if code := h.run("backup-now", "--team", "alpha"); code != 0 {
		t.Fatalf("exit %d:\n%s", code, h.stderr())
	}
	if h.sim.lines(`^systemctl kill --kill-whom=main --signal=SIGUSR2 agent-bus-hs@alpha.service$`) != 1 {
		t.Fatalf("log %v", h.sim.log)
	}
	poll, kill := indexOfLine(h.sim.log, `^curl .*_tuwunel/server_version`), indexOfLine(h.sim.log, `^systemctl kill `)
	if poll < 0 || kill < 0 || poll > kill {
		t.Fatalf("the homeserver must answer before it is signalled: poll %d, kill %d", poll, kill)
	}
	tarPath := "var/lib/agent-bus-install/alpha/backups/agent-bus-state-1.tar"
	if h.mode(tarPath) != 0o600 || h.mode("var/lib/agent-bus-install/alpha/backups") != 0o700 {
		t.Fatalf("modes %o %o", h.mode(tarPath), h.mode("var/lib/agent-bus-install/alpha/backups"))
	}
	if h.stdout() != "CHANGED\tdirectory /var/lib/agent-bus-install/alpha/backups\nCHANGED\tbackup 1 of team alpha\n" {
		t.Fatalf("stdout %q", h.stdout())
	}
	names := tarNames(t, h.rel(tarPath))
	for _, want := range []string{"secrets/registration_shared_secret", "team.json"} {
		if !slices.Contains(names, want) {
			t.Errorf("the tar lacks %s: %v", want, names)
		}
	}
	entries, err := os.ReadDir(h.rel("var/lib/agent-bus/alpha/backups"))
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".tar") {
			t.Errorf("a tar is in the agent-bus user's backups directory: %s", e.Name())
		}
	}
}

func indexOfLine(lines []string, re string) int {
	rx := regexp.MustCompile(re)
	for i, l := range lines {
		if rx.MatchString(l) {
			return i
		}
	}
	return -1
}

func tarNames(t *testing.T, path string) []string {
	t.Helper()
	data, err := readRooted(path)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	tr := tar.NewReader(bytes.NewReader(data))
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return names
		}
		if err != nil {
			t.Fatal(err)
		}
		names = append(names, strings.TrimSuffix(hdr.Name, "/"))
	}
}

func TestBackupNowIncludesTheOptionalStateOnlyWhenPresent(t *testing.T) {
	h := newHarness(t)
	h.installTeam(nil)
	h.write(h.rel("var/lib/agent-bus/alpha/registry.json"), "{}", 0o600)
	h.write(h.rel("var/lib/agent-bus/alpha/room_id"), "!room", 0o600)
	h.run("backup-now", "--team", "alpha")
	names := tarNames(t, h.rel("var/lib/agent-bus-install/alpha/backups/agent-bus-state-1.tar"))
	if !slices.Contains(names, "registry.json") || !slices.Contains(names, "room_id") {
		t.Fatalf("names %v", names)
	}
}

func TestBackupNowDropsTheStateTarsOfBackupsTuwunelPruned(t *testing.T) {
	h := newHarness(t)
	h.installTeam(nil)
	h.run("backup-now", "--team", "alpha")
	h.run("backup-now", "--team", "alpha")
	tars := "var/lib/agent-bus-install/alpha/backups/"
	if !h.exists(tars+"agent-bus-state-1.tar") || !h.exists(tars+"agent-bus-state-2.tar") {
		t.Fatal("two backups, two tars expected")
	}
	if err := os.Remove(h.rel("var/lib/agent-bus/alpha/backups/meta/1")); err != nil {
		t.Fatal(err)
	}
	h.write(h.rel(tars+"agent-bus-state-notmine.tar"), "x", 0o600)
	h.run("backup-now", "--team", "alpha")
	if h.exists(tars + "agent-bus-state-1.tar") {
		t.Error("the tar of a pruned backup was kept")
	}
	if !h.exists(tars+"agent-bus-state-2.tar") || !h.exists(tars+"agent-bus-state-3.tar") || !h.exists(tars+"agent-bus-state-notmine.tar") {
		t.Error("a tar that should stay is gone")
	}
}

func TestBackupNowWithoutANewBackupFailsAfterSixtySeconds(t *testing.T) {
	h := newHarness(t)
	h.installTeam(nil)
	h.fake.OnFunc([]string{"systemctl", "kill"}, func(execx.Cmd) (execx.Result, error) { return ok("") })
	code := h.run("backup-now", "--team", "alpha")
	if code != term.ExitFailure || !strings.Contains(h.stderr(), "no new backup in /var/lib/agent-bus/alpha/backups/meta after 60 s (is admin_signal_execute set?)") {
		t.Fatalf("exit %d, stderr %q", code, h.stderr())
	}
	if h.sleeps != 60 {
		t.Fatalf("slept %d times", h.sleeps)
	}
}

func TestBackupNowRefusesATeamThatIsNotInstalledOrNotRunning(t *testing.T) {
	h := newHarness(t)
	if code := h.run("backup-now", "--team", "alpha"); code != term.ExitConfig {
		t.Fatalf("exit %d", code)
	}
	h.installTeam(nil)
	h.sim.units[hsUnit("alpha")].active = false
	code := h.run("backup-now", "--team", "alpha")
	if code != term.ExitFailure || !strings.Contains(h.stderr(), "agent-bus-hs@alpha.service is inactive; the homeserver needs it running") {
		t.Fatalf("exit %d, stderr %q", code, h.stderr())
	}
	if h.sim.lines(`^systemctl kill`) != 0 {
		t.Fatal("a stopped homeserver was signalled")
	}
}

func TestBackupNowRefusesToStoreASymlinkTheHomeserverPlanted(t *testing.T) {
	h := newHarness(t)
	h.installTeam(nil)
	if err := symlink("/etc/shadow", h.rel("var/lib/agent-bus/alpha/secrets/planted")); err != nil {
		t.Fatal(err)
	}
	code := h.run("backup-now", "--team", "alpha")
	if code != term.ExitFailure || !strings.Contains(h.stderr(), "planted is a symlink, not a regular file") {
		t.Fatalf("exit %d, stderr %q", code, h.stderr())
	}
	if h.exists("var/lib/agent-bus-install/alpha/backups/agent-bus-state-1.tar") {
		t.Fatal("a tar was written")
	}
	if h.stdout() != "" {
		t.Fatalf("stdout %q", h.stdout())
	}
}

func TestBackupNowEveryCommandItRunsPropagatesItsFailure(t *testing.T) {
	probe := newHarness(t)
	probe.installTeam(nil)
	probe.run("backup-now", "--team", "alpha")
	seen := map[string]bool{}
	for _, line := range probe.sim.log {
		seen[firstWords(line)] = true
	}
	for prefix := range seen {
		if prefix == "curl --fail" {
			continue // a failed poll is retried while the unit is active
		}
		h := newHarness(t)
		h.installTeam(nil)
		h.sim.fails = []failRule{{prefix: prefix, code: 5}}
		if code := h.run("backup-now", "--team", "alpha"); code == 0 {
			t.Errorf("a failing %q did not fail the run", prefix)
		}
	}
}

func TestBackupsListsOnlyBackupsRestoreCanUse(t *testing.T) {
	h := newHarness(t)
	h.installTeam(nil)
	h.run("backup-now", "--team", "alpha")
	when := time.Date(2026, 3, 4, 5, 6, 7, 0, time.UTC)
	if err := os.Chtimes(h.rel("var/lib/agent-bus-install/alpha/backups/agent-bus-state-1.tar"), when, when); err != nil {
		t.Fatal(err)
	}
	if code := h.run("backups", "--team", "alpha"); code != 0 {
		t.Fatalf("exit %d", code)
	}
	if h.stdout() != "BACKUP\t1\t2026-03-04 05:06 UTC\n" {
		t.Fatalf("stdout %q", h.stdout())
	}
	h.mkdir("var/lib/agent-bus/alpha/backups/meta/2")
	h.run("backups", "--team", "alpha")
	if h.stdout() != "BACKUP\t1\t2026-03-04 05:06 UTC\n" {
		t.Fatalf("a backup with no state tar was listed: %q", h.stdout())
	}
}

func TestBackupsSortsNumericallyAndSkipsStrayNames(t *testing.T) {
	h := newHarness(t)
	h.installTeam(nil)
	for _, id := range []string{"2", "10", "9", "01", "x", "100"} {
		h.mkdir("var/lib/agent-bus/alpha/backups/meta/" + id)
		h.write(h.rel("var/lib/agent-bus-install/alpha/backups/agent-bus-state-"+id+".tar"), "x", 0o600)
	}
	h.run("backups", "--team", "alpha")
	var ids []string
	for line := range strings.Lines(h.stdout()) {
		ids = append(ids, strings.Split(line, "\t")[1])
	}
	if strings.Join(ids, ",") != "2,9,10,100" {
		t.Fatalf("ids %v", ids)
	}
}

func TestBackupsWithNoneSucceedsQuietly(t *testing.T) {
	h := newHarness(t)
	h.installTeam(nil)
	if code := h.run("backups", "--team", "alpha"); code != 0 || h.stdout() != "" {
		t.Fatalf("exit %d, stdout %q", code, h.stdout())
	}
}

func TestBackupsRefusesAnUnknownTeam(t *testing.T) {
	h := newHarness(t)
	code := h.run("backups", "--team", "nosuch")
	if code != term.ExitConfig || !strings.Contains(h.stderr(), "team nosuch is not installed here") {
		t.Fatalf("exit %d, stderr %q", code, h.stderr())
	}
}
