package ops

import (
	"context"
	"errors"
	"io"
	"slices"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/declare"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
)

const (
	opsInstaller = "sudo -n /usr/local/sbin/agent-bus-install "
)

// removeRig is a rig set up for the happy path of "remove alpha" on a declared team.
func removeRig(t *testing.T) *opsRig {
	t.Helper()
	r := newOpsRig(t)
	r.sudoOK()
	r.on("gum style", execx.Result{})
	r.on("gum confirm", execx.Result{})
	r.on("gum choose", execx.Result{Stdout: []byte("Take it down, keep its data\n")})
	r.on(opsInstaller+"backup-now --team alpha", execx.Result{})
	r.on(opsDeclare+"remove --file "+opsHostVars+" --team alpha", execx.Result{Stdout: []byte("CHANGED\tteam alpha declared absent\n")})
	r.on(opsPlay, execx.Result{})
	r.statuses = []map[string]declare.Status{
		{"alpha": st("current", "")},
		{"alpha": st("removed", "")},
	}
	return r
}

func TestRemoveUsage(t *testing.T) {
	for _, args := range [][]string{nil, {"a", "b"}, {"--yes"}, {"-x"}} {
		r := newOpsRig(t)
		if code := r.ops.Remove(context.Background(), args); code != 64 {
			t.Errorf("%v: code %d", args, code)
		}
		if !strings.Contains(r.said(), "ccy-teams: 'remove' takes one team (and no --yes: it always asks). See ccy-teams --help.") {
			t.Errorf("%v: said %q", args, r.said())
		}
	}
}

func TestRemoveNeedsATerminal(t *testing.T) {
	r := newOpsRig(t)
	r.tty = false
	if code := r.ops.Remove(context.Background(), []string{"alpha"}); code != 64 {
		t.Errorf("code %d", code)
	}
	if !strings.Contains(r.said(), "ccy-teams: 'remove' asks on a terminal, always: taking a team down is a human's decision. See ccy-teams --help.") {
		t.Errorf("said %q", r.said())
	}
}

func TestRemoveTerminalRuleIsStdinAndStderr(t *testing.T) {
	r := removeRig(t)
	stdinOnly := false
	r.ops.IsTerminal = func(v any) bool { return v == r.ops.Stdin && !stdinOnly }
	if code := r.ops.Remove(context.Background(), []string{"alpha"}); code != 64 {
		t.Errorf("stderr not a terminal: code %d", code)
	}
}

func TestRemoveCheckoutGoneAndGumMissingAndRefreshFail(t *testing.T) {
	r := newOpsRig(t)
	r.checkout = errors.New("gone")
	if code := r.ops.Remove(context.Background(), []string{"alpha"}); code != 1 {
		t.Errorf("checkout: code %d", code)
	}

	r = newOpsRig(t)
	r.tools["gum"] = false
	if code := r.ops.Remove(context.Background(), []string{"alpha"}); code != 1 {
		t.Errorf("gum: code %d", code)
	}
	want := "ERROR: gum is not installed, and ccy-teams needs it here. playbooks/imports/play-claude-yolo.yml installs it; run that play and try again."
	if !strings.Contains(r.said(), want) {
		t.Errorf("said %q", r.said())
	}

	r = newOpsRig(t)
	r.refreshEr = errors.New("x")
	if code := r.ops.Remove(context.Background(), []string{"alpha"}); code != 1 {
		t.Errorf("refresh: code %d", code)
	}
}

func TestRemoveRefusesWhatIsNotBuilt(t *testing.T) {
	for _, s := range []declare.Status{st("", ""), st("removed", ""), st("unbuilt", "")} {
		r := newOpsRig(t)
		r.statuses = []map[string]declare.Status{{"alpha": s}}
		if s.State == "" {
			r.statuses = []map[string]declare.Status{{}}
		}
		if code := r.ops.Remove(context.Background(), []string{"alpha"}); code != 64 {
			t.Errorf("%+v: code %d", s, code)
		}
		if !strings.Contains(r.said(), "ccy-teams: team alpha is not built here; there is nothing to take down. See ccy-teams --help.") {
			t.Errorf("%+v: said %q", s, r.said())
		}
		if len(r.fake.Calls()) != 0 {
			t.Errorf("%+v: ran %q", s, r.lines())
		}
	}
}

func TestRemoveRefusesAnUnreadableEntry(t *testing.T) {
	r := newOpsRig(t)
	r.statuses = []map[string]declare.Status{{"alpha": st("unreadable", "bad port")}}
	if code := r.ops.Remove(context.Background(), []string{"alpha"}); code != 64 {
		t.Errorf("code %d", code)
	}
	if !strings.Contains(r.said(), "team alpha's entry cannot be read: bad port.") {
		t.Errorf("said %q", r.said())
	}
}

func TestRemoveDeclaredKeepsDataAfterBackup(t *testing.T) {
	r := removeRig(t)
	if code := r.ops.Remove(context.Background(), []string{"alpha"}); code != 0 {
		t.Fatalf("code %d: %s", code, r.said())
	}
	want := []string{
		`gum style --bold --border rounded --padding 0 1 Take team alpha down`,
		"gum style --width 76 --padding 0 0 0 0 Its homeserver and its backup timer stop: its agents and people can no longer reach it. Its data (the room's whole history, the accounts, the backups) stays on this machine unless you choose to delete it, which cannot be undone. There may be history in it worth keeping.",
		"gum confirm --default=true Back alpha up first?",
		"sudo -n -v",
		opsInstaller + "backup-now --team alpha",
		"gum choose --header What happens to alpha? --selected Take it down, keep its data Take it down, keep its data Take it down and delete its data Leave it running",
		opsDeclare + "remove --file " + opsHostVars + " --team alpha",
		opsPlay + ` -e {"agent_bus_only_teams": ["alpha"]}`,
	}
	got := r.lines()
	if len(got) != len(want) {
		t.Fatalf("calls:\n%q\nwant\n%q", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("call %d:\n%q\nwant\n%q", i, got[i], want[i])
		}
	}
	wantHeadings := []string{
		"Teams › alpha › Take down · step 1/2 Backup\nteam alpha",
		"Teams › alpha › Take down · step 2/2 Its data\nteam alpha",
	}
	if got := r.headings(); !slices.Equal(got, wantHeadings) {
		t.Errorf("headings:\n got %q\nwant %q", got, wantHeadings)
	}
	if r.out.String() != "CHANGED\tteam alpha declared absent\n" {
		t.Errorf("stdout %q", r.out.String())
	}
	if !strings.Contains(r.said(), "Team alpha is down; its data is kept in /var/lib/agent-bus/alpha.") {
		t.Errorf("said %q", r.said())
	}
	if r.refreshes != 2 {
		t.Errorf("refreshes %d", r.refreshes)
	}
}

func TestRemoveTakesTheTeamsLiveSessionsOffItFirst(t *testing.T) {
	r := removeRig(t)
	in := []string{"ccy-a", "ccy-b"}
	var taken []string
	r.ops.SessionsIn = func(context.Context, string) ([]string, error) { return in, nil }
	r.ops.TakeOut = func(_ context.Context, session, team string) bool {
		taken = append(taken, session+"@"+team)
		in = in[1:]
		return true
	}
	if code := r.ops.Remove(context.Background(), []string{"alpha"}); code != 0 {
		t.Fatalf("code %d: %s", code, r.said())
	}
	if strings.Join(taken, " ") != "ccy-a@alpha ccy-b@alpha" {
		t.Fatalf("taken off %v", taken)
	}
	if got := r.lines(); got[0] != "gum confirm --default=true Take ccy-a, ccy-b off alpha first? They keep running." {
		t.Fatalf("first call %q", got[0])
	}
	if got := r.headings()[0]; got != "Teams › alpha › Take down › Sessions in it\nteam alpha · running in it: ccy-a, ccy-b" {
		t.Fatalf("first heading %q", got)
	}
}

func TestRemoveStopsWhileASessionIsStillInTheTeam(t *testing.T) {
	r := removeRig(t)
	r.ops.SessionsIn = func(context.Context, string) ([]string, error) { return []string{"ccy-a"}, nil }
	r.ops.TakeOut = func(context.Context, string, string) bool { return false }
	if code := r.ops.Remove(context.Background(), []string{"alpha"}); code != 1 {
		t.Fatalf("code %d: %s", code, r.said())
	}
	if !strings.Contains(r.said(), "ccy-a is still in alpha, so alpha was not taken down") {
		t.Fatalf("said %q", r.said())
	}
	for _, line := range r.lines() {
		if strings.HasPrefix(line, opsDeclare) || strings.HasPrefix(line, opsInstaller) {
			t.Fatalf("taken down with a session in it: %q", line)
		}
	}
}

func TestRemoveExplainWidthFollowsTheTerminal(t *testing.T) {
	for _, c := range []struct {
		cols int
		want string
	}{{120, "--width 116"}, {43, "--width 76"}, {44, "--width 40"}} {
		r := removeRig(t)
		r.ops.Width = func(context.Context) int { return c.cols }
		if code := r.ops.Remove(context.Background(), []string{"alpha"}); code != 0 {
			t.Fatalf("code %d", code)
		}
		if !r.called("gum style " + c.want) {
			t.Errorf("cols %d: calls %q", c.cols, r.lines())
		}
	}
	r := removeRig(t)
	r.ops.Width = nil
	if code := r.ops.Remove(context.Background(), []string{"alpha"}); code != 0 {
		t.Fatalf("code %d", code)
	}
	if !r.called("gum style --width 76") {
		t.Errorf("nil Width: calls %q", r.lines())
	}
}

func TestRemoveWithoutBackupSkipsSudoAndInstaller(t *testing.T) {
	r := removeRig(t)
	r.onExit("gum confirm", 1, "")
	if code := r.ops.Remove(context.Background(), []string{"alpha"}); code != 0 {
		t.Fatalf("code %d: %s", code, r.said())
	}
	if r.called("sudo") {
		t.Errorf("sudo used: %q", r.lines())
	}
}

func TestRemoveBackupFailureStopsBeforeTakingDown(t *testing.T) {
	r := removeRig(t)
	r.onExit(opsInstaller+"backup-now", 1, "")
	if code := r.ops.Remove(context.Background(), []string{"alpha"}); code != 1 {
		t.Errorf("code %d", code)
	}
	if !strings.Contains(r.said(), "ERROR: the backup failed (above), so alpha was not taken down.") {
		t.Errorf("said %q", r.said())
	}
	if r.called("gum choose") || r.called(opsPlay) {
		t.Errorf("went on: %q", r.lines())
	}
}

func TestRemoveSudoNotGivenStops(t *testing.T) {
	r := removeRig(t)
	r.onExit("sudo -n -v", 1, "sudo: a password is required")
	r.onExit("sudo -v", 1, "")
	if code := r.ops.Remove(context.Background(), []string{"alpha"}); code != 1 {
		t.Errorf("code %d", code)
	}
	if !strings.Contains(r.said(), "ERROR: sudo was not given your password") {
		t.Errorf("said %q", r.said())
	}
	if !strings.Contains(r.said(), "A backup is taken as root, by agent-bus-install.") {
		t.Errorf("reason not said: %q", r.said())
	}
}

func TestRemoveConfirmCancelled(t *testing.T) {
	r := removeRig(t)
	r.onExit("gum confirm", 130, "")
	if code := r.ops.Remove(context.Background(), []string{"alpha"}); code != 130 {
		t.Errorf("code %d", code)
	}
	if !strings.Contains(r.said(), "Cancelled; nothing was changed.") {
		t.Errorf("said %q", r.said())
	}
}

func TestRemoveConfirmBrokenGum(t *testing.T) {
	r := removeRig(t)
	r.fake.On([]string{"gum", "confirm"}, execx.Result{})
	r.ops.Gum.Runner = &failingRunner{}
	if code := r.ops.Remove(context.Background(), []string{"alpha"}); code != 1 {
		t.Errorf("code %d", code)
	}
}

// failingRunner cannot start any program.
type failingRunner struct{}

func (*failingRunner) Run(context.Context, execx.Cmd) (execx.Result, error) {
	return execx.Result{}, errors.New("cannot start")
}

func TestRemoveChooseCancelledAndLeave(t *testing.T) {
	r := removeRig(t)
	r.onExit("gum choose", 130, "")
	if code := r.ops.Remove(context.Background(), []string{"alpha"}); code != 130 {
		t.Errorf("cancel: code %d", code)
	}

	r = removeRig(t)
	r.on("gum choose", execx.Result{Stdout: []byte("Leave it running\n")})
	if code := r.ops.Remove(context.Background(), []string{"alpha"}); code != 1 {
		t.Errorf("leave: code %d", code)
	}
	if !strings.Contains(r.said(), "Nothing was changed.") {
		t.Errorf("said %q", r.said())
	}
	if r.called(opsDeclare+"remove") || r.called(opsPlay) {
		t.Errorf("changed something: %q", r.lines())
	}
}

func deleteRig(t *testing.T, typed string) *opsRig {
	t.Helper()
	r := removeRig(t)
	r.on("gum choose", execx.Result{Stdout: []byte("Take it down and delete its data\n")})
	r.on("gum input", execx.Result{Stdout: []byte(typed + "\n")})
	r.on(opsDeclare+"remove --file "+opsHostVars+" --team alpha --purge", execx.Result{Stdout: []byte("CHANGED\tteam alpha declared absent, purge: true\n")})
	return r
}

func TestRemoveDeleteNeedsTheTypedName(t *testing.T) {
	r := deleteRig(t, "alpah")
	if code := r.ops.Remove(context.Background(), []string{"alpha"}); code != 1 {
		t.Errorf("code %d", code)
	}
	if !strings.Contains(r.said(), "That is not the team's name; nothing was changed.") {
		t.Errorf("said %q", r.said())
	}
	if r.called(opsDeclare + "remove") {
		t.Error("removed despite the wrong name")
	}
	if !r.called("gum input --header Type alpha to delete its data for good --placeholder the team's name --value  --width 0") {
		t.Errorf("calls %q", r.lines())
	}
}

func TestRemoveDeletePurges(t *testing.T) {
	r := deleteRig(t, "alpha")
	if code := r.ops.Remove(context.Background(), []string{"alpha"}); code != 0 {
		t.Fatalf("code %d: %s", code, r.said())
	}
	if !r.called(opsDeclare + "remove --file " + opsHostVars + " --team alpha --purge") {
		t.Errorf("calls %q", r.lines())
	}
	if !strings.Contains(r.said(), "Team alpha is down and its data deleted.") {
		t.Errorf("said %q", r.said())
	}
}

func TestRemoveDeleteTypedNameCancelled(t *testing.T) {
	r := deleteRig(t, "alpha")
	r.onExit("gum input", 130, "")
	if code := r.ops.Remove(context.Background(), []string{"alpha"}); code != 130 {
		t.Errorf("code %d", code)
	}
}

func TestRemoveUndeclaredGoesThroughTheInstaller(t *testing.T) {
	r := removeRig(t)
	r.statuses = []map[string]declare.Status{{"zeta": st("decide", "undeclared")}, {}}
	r.on(opsInstaller+"remove --team zeta", execx.Result{})
	r.onExit("gum confirm", 1, "")
	if code := r.ops.Remove(context.Background(), []string{"zeta"}); code != 0 {
		t.Fatalf("code %d: %s", code, r.said())
	}
	if !r.called(opsInstaller + "remove --team zeta") {
		t.Errorf("calls %q", r.lines())
	}
	for _, c := range r.fake.Calls() {
		if strings.Contains(c.Line(), "--purge") || strings.Contains(c.Line(), "declare") || c.Name == opsPlay {
			t.Errorf("unexpected %q", c.Line())
		}
	}
	if !strings.Contains(r.said(), "Team zeta is down; its data is kept in /var/lib/agent-bus/zeta.") {
		t.Errorf("said %q", r.said())
	}
}

func TestRemoveUndeclaredPurgeFlag(t *testing.T) {
	r := removeRig(t)
	r.statuses = []map[string]declare.Status{{"zeta": st("decide", "undeclared")}, {"zeta": st("removed", "")}}
	r.on("gum choose", execx.Result{Stdout: []byte("Take it down and delete its data\n")})
	r.on("gum input", execx.Result{Stdout: []byte("zeta\n")})
	r.on(opsInstaller+"remove --team zeta --purge", execx.Result{})
	r.onExit("gum confirm", 1, "")
	if code := r.ops.Remove(context.Background(), []string{"zeta"}); code != 0 {
		t.Fatalf("code %d: %s", code, r.said())
	}
	if !r.called(opsInstaller + "remove --team zeta --purge") {
		t.Errorf("calls %q", r.lines())
	}
}

func TestRemoveUndeclaredFailureAndNoSudo(t *testing.T) {
	r := removeRig(t)
	r.statuses = []map[string]declare.Status{{"zeta": st("decide", "undeclared")}}
	r.onExit(opsInstaller+"remove", 1, "")
	r.onExit("gum confirm", 1, "")
	if code := r.ops.Remove(context.Background(), []string{"zeta"}); code != 1 {
		t.Errorf("code %d", code)
	}
	if !strings.Contains(r.said(), "ERROR: zeta was not taken down (above).") {
		t.Errorf("said %q", r.said())
	}

	r = removeRig(t)
	r.statuses = []map[string]declare.Status{{"zeta": st("decide", "undeclared")}}
	r.onExit("gum confirm", 1, "")
	r.onExit("sudo -n -v", 1, "sudo: a password is required")
	r.onExit("sudo -v", 1, "")
	if code := r.ops.Remove(context.Background(), []string{"zeta"}); code != 1 {
		t.Errorf("code %d", code)
	}
	if !strings.Contains(r.said(), "A team that is not declared is taken down as root, by agent-bus-install.") {
		t.Errorf("said %q", r.said())
	}
}

func TestRemoveDeclareFailureKeepsItsExitCode(t *testing.T) {
	r := removeRig(t)
	r.onExit("gum confirm", 1, "")
	r.onExit(opsDeclare+"remove", 78, "declare: refused: the file has a tab")
	if code := r.ops.Remove(context.Background(), []string{"alpha"}); code != 78 {
		t.Errorf("code %d", code)
	}
	if !strings.Contains(r.said(), "declare: refused: the file has a tab") {
		t.Errorf("declare's words lost: %q", r.said())
	}
	if r.called(opsPlay) {
		t.Error("ran the play after the declare failed")
	}
}

func TestRemovePlayFailure(t *testing.T) {
	r := removeRig(t)
	r.onExit("gum confirm", 1, "")
	r.onExit(opsPlay, 2, "")
	if code := r.ops.Remove(context.Background(), []string{"alpha"}); code != 1 {
		t.Errorf("code %d", code)
	}
	want := "ERROR: alpha is declared absent in ~/hv/localhost.yml but is still running; once the cause above is fixed, ccy-teams remove alpha runs the play again."
	if !strings.Contains(r.said(), want) {
		t.Errorf("said %q", r.said())
	}
}

func TestRemoveStillThereAfterThePlay(t *testing.T) {
	r := removeRig(t)
	r.onExit("gum confirm", 1, "")
	r.statuses = []map[string]declare.Status{{"alpha": st("current", "")}, {"alpha": st("outdated", "config")}}
	if code := r.ops.Remove(context.Background(), []string{"alpha"}); code != 1 {
		t.Errorf("code %d", code)
	}
	if !strings.Contains(r.said(), "ERROR: alpha is still out of date: config after the play.") {
		t.Errorf("said %q", r.said())
	}
}

func TestRemoveAbsentFromTheFinalRefreshIsDone(t *testing.T) {
	r := removeRig(t)
	r.onExit("gum confirm", 1, "")
	r.statuses = []map[string]declare.Status{{"alpha": st("current", "")}, {}}
	if code := r.ops.Remove(context.Background(), []string{"alpha"}); code != 0 {
		t.Errorf("code %d: %s", code, r.said())
	}
}

func TestRemoveFinalRefreshFailure(t *testing.T) {
	r := removeRig(t)
	r.onExit("gum confirm", 1, "")
	calls := 0
	r.ops.Refresh = func(context.Context) (map[string]declare.Status, error) {
		calls++
		if calls == 1 {
			return map[string]declare.Status{"alpha": st("current", "")}, nil
		}
		return nil, errors.New("x")
	}
	if code := r.ops.Remove(context.Background(), []string{"alpha"}); code != 1 {
		t.Errorf("code %d", code)
	}
}

// ---- backup ---------------------------------------------------------------------------

func TestBackupUsage(t *testing.T) {
	for _, args := range [][]string{nil, {"a", "b"}, {"-x"}} {
		r := newOpsRig(t)
		if code := r.ops.Backup(context.Background(), args); code != 64 {
			t.Errorf("%v: code %d", args, code)
		}
		if !strings.Contains(r.said(), "ccy-teams: 'backup' takes one team. See ccy-teams --help.") {
			t.Errorf("%v: said %q", args, r.said())
		}
	}
}

func TestBackupRunsTheInstallerAfterSudoIsReady(t *testing.T) {
	r := newOpsRig(t)
	r.sudoOK()
	r.on(opsInstaller+"backup-now --team alpha", execx.Result{Stdout: []byte("backup 3 written\n")})
	if code := r.ops.Backup(context.Background(), []string{"alpha"}); code != 0 {
		t.Fatalf("code %d", code)
	}
	got := r.lines()
	if len(got) != 2 || got[0] != "sudo -n -v" || got[1] != opsInstaller+"backup-now --team alpha" {
		t.Errorf("calls %q", got)
	}
	if r.out.String() != "backup 3 written\n" {
		t.Errorf("stdout %q", r.out.String())
	}
}

func TestBackupSudoNotGiven(t *testing.T) {
	r := newOpsRig(t)
	r.tty = false
	r.onExit("sudo -n -v", 1, "sudo: a password is required")
	if code := r.ops.Backup(context.Background(), []string{"alpha"}); code != 1 {
		t.Errorf("code %d", code)
	}
	if !strings.Contains(r.said(), "A backup is taken as root, by agent-bus-install. That needs sudo, and with no terminal") {
		t.Errorf("said %q", r.said())
	}
	if r.called(opsInstaller) {
		t.Error("ran the installer without sudo")
	}
}

func TestBackupKeepsTheInstallersExitCode(t *testing.T) {
	for _, code := range []int{1, 64, 77, 78} {
		r := newOpsRig(t)
		r.sudoOK()
		r.onExit(opsInstaller+"backup-now", code, "")
		if got := r.ops.Backup(context.Background(), []string{"alpha"}); int(got) != code {
			t.Errorf("installer %d: exit %d", code, got)
		}
	}
}

// ---- restore --------------------------------------------------------------------------

func TestRestoreArgumentErrors(t *testing.T) {
	cases := []struct {
		args []string
		want string
	}{
		{[]string{"--bogus"}, "unknown restore option '--bogus'."},
		{[]string{"-y", "alpha"}, "unknown restore option '-y'."},
		{[]string{"alpha", "2", "3"}, "unexpected argument '3'."},
		{nil, "'restore' needs a team."},
		{[]string{"--yes"}, "'restore' needs a team."},
		{[]string{"alpha", "abc"}, "a backup is named by its number; got 'abc'."},
		{[]string{"alpha", "0"}, "a backup is named by its number; got '0'."},
		{[]string{"alpha", "01"}, "a backup is named by its number; got '01'."},
		{[]string{"alpha", "-1"}, "unknown restore option '-1'."},
	}
	for _, c := range cases {
		r := newOpsRig(t)
		if code := r.ops.Restore(context.Background(), c.args); code != 64 {
			t.Errorf("%v: code %d", c.args, code)
		}
		if !strings.Contains(r.said(), "ccy-teams: "+c.want+" See ccy-teams --help.") {
			t.Errorf("%v: said %q", c.args, r.said())
		}
		if len(r.fake.Calls()) != 0 {
			t.Errorf("%v: ran %q", c.args, r.lines())
		}
	}
}

func TestRestoreSudoNotGiven(t *testing.T) {
	r := newOpsRig(t)
	r.tty = false
	r.onExit("sudo -n -v", 1, "sudo: a password is required")
	if code := r.ops.Restore(context.Background(), []string{"alpha", "2", "--yes"}); code != 1 {
		t.Errorf("code %d", code)
	}
	if !strings.Contains(r.said(), "The backups are listed and restored as root, by agent-bus-install.") {
		t.Errorf("said %q", r.said())
	}
}

func TestRestoreNamedBackupWithYes(t *testing.T) {
	r := newOpsRig(t)
	r.tty = false // --yes needs no terminal
	r.sudoOK()
	r.on(opsInstaller+"restore --team alpha --backup 2", execx.Result{})
	if code := r.ops.Restore(context.Background(), []string{"alpha", "2", "--yes"}); code != 0 {
		t.Fatalf("code %d: %s", code, r.said())
	}
	got := r.lines()
	if len(got) != 2 || got[1] != opsInstaller+"restore --team alpha --backup 2" {
		t.Errorf("calls %q", got)
	}
}

func TestRestoreYesMayComeFirst(t *testing.T) {
	r := newOpsRig(t)
	r.sudoOK()
	r.on(opsInstaller+"restore --team alpha --backup 2", execx.Result{})
	if code := r.ops.Restore(context.Background(), []string{"--yes", "alpha", "2"}); code != 0 {
		t.Fatalf("code %d: %s", code, r.said())
	}
}

func TestRestoreKeepsTheInstallersExitCode(t *testing.T) {
	r := newOpsRig(t)
	r.sudoOK()
	r.onExit(opsInstaller+"restore", 77, "")
	if code := r.ops.Restore(context.Background(), []string{"alpha", "2", "--yes"}); code != 77 {
		t.Errorf("code %d", code)
	}
}

func TestRestoreWithoutYesAndNoTerminal(t *testing.T) {
	r := newOpsRig(t)
	r.tty = false
	r.sudoOK()
	if code := r.ops.Restore(context.Background(), []string{"alpha", "2"}); code != 64 {
		t.Errorf("code %d", code)
	}
	if !strings.Contains(r.said(), "a restore replaces the team's database and state: confirm it with --yes when there is no terminal.") {
		t.Errorf("said %q", r.said())
	}
	if r.called(opsInstaller + "restore") {
		t.Error("restored without confirmation")
	}
}

func TestRestoreConfirmYes(t *testing.T) {
	r := newOpsRig(t)
	r.sudoOK()
	r.on("fzf", execx.Result{Stdout: []byte("Yes, restore backup 2\n")})
	r.on(opsInstaller+"restore --team alpha --backup 2", execx.Result{})
	if code := r.ops.Restore(context.Background(), []string{"alpha", "2"}); code != 0 {
		t.Fatalf("code %d: %s", code, r.said())
	}
	var fzf string
	for _, c := range r.fake.Calls() {
		if c.Name == "fzf" {
			fzf = strings.Join(c.Args, "|")
		}
	}
	for _, s := range []string{
		"--border-label= Teams ",
		"--header=Teams › alpha › Restore · step 2/2 Confirm\nteam alpha · backup 2\n───",
		"Restore backup 2 of alpha? Its homeserver stops, and every message and change since that backup is lost.",
		"--bind=start:last",
	} {
		if !strings.Contains(fzf, s) {
			t.Errorf("fzf argv missing %q: %s", s, fzf)
		}
	}
}

func TestRestoreConfirmDeclined(t *testing.T) {
	for name, res := range map[string]execx.Result{
		"esc":      {ExitCode: 130},
		"exit row": {Stdout: []byte("Exit\n")},
	} {
		r := newOpsRig(t)
		r.sudoOK()
		r.on("fzf", res)
		if code := r.ops.Restore(context.Background(), []string{"alpha", "2"}); code != 0 {
			t.Errorf("%s: code %d", name, code)
		}
		if !strings.Contains(r.said(), "Nothing restored.") {
			t.Errorf("%s: said %q", name, r.said())
		}
		if r.called(opsInstaller + "restore") {
			t.Errorf("%s: restored", name)
		}
	}
}

func TestRestoreConfirmBrokenFzf(t *testing.T) {
	r := newOpsRig(t)
	r.sudoOK()
	// no fzf scripted: the fake reports it cannot be run
	if code := r.ops.Restore(context.Background(), []string{"alpha", "2"}); code != 1 {
		t.Errorf("code %d", code)
	}
	if !strings.Contains(r.said(), "ERROR:") {
		t.Errorf("said %q", r.said())
	}
}

const opsListing = "BACKUP\t2\t2026-10-01 10:00\nBACKUP\t1\t2026-09-30 09:00\n"

func TestRestoreNoBackups(t *testing.T) {
	r := newOpsRig(t)
	r.sudoOK()
	r.on(opsInstaller+"backups --team alpha", execx.Result{})
	if code := r.ops.Restore(context.Background(), []string{"alpha"}); code != 1 {
		t.Errorf("code %d", code)
	}
	if !strings.Contains(r.said(), "ERROR: team alpha has no backup to restore.") {
		t.Errorf("said %q", r.said())
	}
}

func TestRestoreListingFailureKeepsItsExit(t *testing.T) {
	r := newOpsRig(t)
	r.sudoOK()
	r.onExit(opsInstaller+"backups", 77, "no")
	if code := r.ops.Restore(context.Background(), []string{"alpha"}); code != 77 {
		t.Errorf("code %d", code)
	}
}

func TestRestoreListingNeedsPassword(t *testing.T) {
	r := newOpsRig(t)
	r.sudoOK()
	r.onExit(opsInstaller+"backups", 1, "sudo: a password is required")
	if code := r.ops.Restore(context.Background(), []string{"alpha"}); code != 1 {
		t.Errorf("code %d", code)
	}
	if !strings.Contains(r.said(), "ERROR:") {
		t.Errorf("said %q", r.said())
	}
}

func TestRestoreListsOnStdoutWithoutATerminal(t *testing.T) {
	r := newOpsRig(t)
	r.tty = false
	r.sudoOK()
	r.on(opsInstaller+"backups --team alpha", execx.Result{Stdout: []byte(opsListing)})
	if code := r.ops.Restore(context.Background(), []string{"alpha"}); code != 64 {
		t.Errorf("code %d", code)
	}
	if r.out.String() != opsListing {
		t.Errorf("stdout %q", r.out.String())
	}
	if !strings.Contains(r.said(), "ccy-teams: name the backup to restore: ccy-teams restore alpha <n> --yes. See ccy-teams --help.") {
		t.Errorf("said %q", r.said())
	}
	if r.called("fzf") {
		t.Error("picker opened without a terminal")
	}
}

func TestRestorePicksThenConfirmsThenRestores(t *testing.T) {
	r := newOpsRig(t)
	r.sudoOK()
	r.on(opsInstaller+"backups --team alpha", execx.Result{Stdout: []byte(opsListing)})
	r.on("fzf", execx.Result{Stdout: []byte("Backup 1      2026-09-30 09:00\t1\n")})
	r.on(opsInstaller+"restore --team alpha --backup 1", execx.Result{})
	// the confirm is the second fzf call; the rules answer by argv prefix, so tell them apart
	// by what each call reads
	r.ops.Picker.Runner = &fzfScript{fake: r.fake, confirm: "Yes, restore backup 1\n", pick: "Backup 1      2026-09-30 09:00\t1\n"}
	if code := r.ops.Restore(context.Background(), []string{"alpha"}); code != 0 {
		t.Fatalf("code %d: %s", code, r.said())
	}
	if !r.called(opsInstaller + "restore --team alpha --backup 1") {
		t.Errorf("calls %q", r.lines())
	}
	script, ok := r.ops.Picker.Runner.(*fzfScript)
	if !ok {
		t.Fatal("runner type")
	}
	if script.rows != "Backup 2      2026-10-01 10:00\t2\nBackup 1      2026-09-30 09:00\t1\nExit\n" {
		t.Errorf("rows %q", script.rows)
	}
	if !strings.Contains(script.args[0], "--border-label= Restore alpha ") ||
		!strings.Contains(script.args[0], "The homeserver is stopped, the backup's database and state put back, and it is started again.") {
		t.Errorf("pick argv %q", script.args[0])
	}
}

// fzfScript answers the first fzf call as the backup picker and the second as the confirm.
type fzfScript struct {
	fake    *execx.Fake
	pick    string
	confirm string
	rows    string
	args    []string
	n       int
}

func (f *fzfScript) Run(_ context.Context, c execx.Cmd) (execx.Result, error) {
	f.n++
	f.args = append(f.args, strings.Join(c.Args, "|"))
	if f.n == 1 {
		b, err := io.ReadAll(c.Stdin)
		if err != nil {
			return execx.Result{}, err
		}
		f.rows = string(b)
		return execx.Result{Stdout: []byte(f.pick)}, nil
	}
	return execx.Result{Stdout: []byte(f.confirm)}, nil
}

func TestRestorePickerLeftIsNothingRestored(t *testing.T) {
	r := newOpsRig(t)
	r.sudoOK()
	r.on(opsInstaller+"backups --team alpha", execx.Result{Stdout: []byte(opsListing)})
	r.onExit("fzf", 130, "")
	if code := r.ops.Restore(context.Background(), []string{"alpha"}); code != 0 {
		t.Errorf("code %d", code)
	}
	if !strings.Contains(r.said(), "Nothing restored.") {
		t.Errorf("said %q", r.said())
	}
}

func TestRestorePickerBrokenFzfIsAFailure(t *testing.T) {
	r := newOpsRig(t)
	r.sudoOK()
	r.on(opsInstaller+"backups --team alpha", execx.Result{Stdout: []byte(opsListing)})
	if code := r.ops.Restore(context.Background(), []string{"alpha"}); code != 1 {
		t.Errorf("code %d", code)
	}
}

func TestRestorePickedYesSkipsTheConfirm(t *testing.T) {
	r := newOpsRig(t)
	r.sudoOK()
	r.on(opsInstaller+"backups --team alpha", execx.Result{Stdout: []byte(opsListing)})
	r.on("fzf", execx.Result{Stdout: []byte("Backup 2      2026-10-01 10:00\t2\n")})
	r.on(opsInstaller+"restore --team alpha --backup 2", execx.Result{})
	if code := r.ops.Restore(context.Background(), []string{"alpha", "--yes"}); code != 0 {
		t.Fatalf("code %d: %s", code, r.said())
	}
	n := 0
	for _, c := range r.fake.Calls() {
		if c.Name == "fzf" {
			n++
		}
	}
	if n != 1 {
		t.Errorf("fzf called %d times", n)
	}
}

// The backup restored is the picked row's tag, not a number read back out of the row's text.
func TestRestoreTakesTheBackupFromTheTagNotTheRowText(t *testing.T) {
	r := newOpsRig(t)
	r.sudoOK()
	r.on(opsInstaller+"backups --team alpha", execx.Result{Stdout: []byte(opsListing)})
	r.on("fzf", execx.Result{Stdout: []byte("Backup 2      2026-10-01 10:00 reworded\t1\n")})
	r.on(opsInstaller+"restore --team alpha --backup 1", execx.Result{})
	if code := r.ops.Restore(context.Background(), []string{"alpha", "--yes"}); code != 0 {
		t.Fatalf("code %d: %s", code, r.said())
	}
	if !r.called(opsInstaller + "restore --team alpha --backup 1") {
		t.Errorf("calls %q", r.lines())
	}
}

func TestRestorePickWithNoTagIsAFailure(t *testing.T) {
	r := newOpsRig(t)
	r.sudoOK()
	r.on(opsInstaller+"backups --team alpha", execx.Result{Stdout: []byte(opsListing)})
	r.on("fzf", execx.Result{Stdout: []byte("Backup 1      2026-09-30 09:00\n")})
	if code := r.ops.Restore(context.Background(), []string{"alpha", "--yes"}); code != 1 {
		t.Errorf("code %d", code)
	}
	if !strings.Contains(r.said(), "names no backup") {
		t.Errorf("said %q", r.said())
	}
}

// brokenWriter fails every write.
type brokenWriter struct{}

func (brokenWriter) Write([]byte) (int, error) { return 0, errors.New("disk full") }

func TestRemoveChangeLineWriteFailure(t *testing.T) {
	r := removeRig(t)
	r.onExit("gum confirm", 1, "")
	r.ops.Stdout = brokenWriter{}
	if code := r.ops.Remove(context.Background(), []string{"alpha"}); code != 1 {
		t.Errorf("code %d", code)
	}
	if !strings.Contains(r.said(), "disk full") {
		t.Errorf("said %q", r.said())
	}
}

func TestRemoveUnchangedDeclareLineIsPassedOn(t *testing.T) {
	r := removeRig(t)
	r.onExit("gum confirm", 1, "")
	r.on(opsDeclare+"remove --file "+opsHostVars+" --team alpha", execx.Result{Stdout: []byte("UNCHANGED\talready absent\n")})
	if code := r.ops.Remove(context.Background(), []string{"alpha"}); code != 0 {
		t.Fatalf("code %d", code)
	}
	if r.out.String() != "UNCHANGED\talready absent\n" {
		t.Errorf("stdout %q", r.out.String())
	}
}

func TestRemoveHeadingFailures(t *testing.T) {
	r := removeRig(t)
	r.onExit("gum style --bold", 1, "")
	if code := r.ops.Remove(context.Background(), []string{"alpha"}); code != 1 {
		t.Errorf("frame: code %d", code)
	}
	r = removeRig(t)
	r.onExit("gum style --width", 1, "")
	if code := r.ops.Remove(context.Background(), []string{"alpha"}); code != 1 {
		t.Errorf("explain: code %d", code)
	}
	if !strings.Contains(r.said(), "ERROR:") || r.called("gum confirm") {
		t.Errorf("said %q calls %q", r.said(), r.lines())
	}
}

func TestRemoveChooseBrokenGumIsAFailureNotACancel(t *testing.T) {
	r := removeRig(t)
	r.onExit("gum confirm", 1, "")
	r.ops.Gum.Runner = &failOn{inner: r.fake, name: "choose"}
	if code := r.ops.Remove(context.Background(), []string{"alpha"}); code != 1 {
		t.Errorf("code %d", code)
	}
}

// failOn runs everything through inner except the gum subcommand named, which cannot start.
type failOn struct {
	inner execx.Runner
	name  string
}

func (f *failOn) Run(ctx context.Context, c execx.Cmd) (execx.Result, error) {
	if c.Name == "gum" && len(c.Args) > 0 && c.Args[0] == f.name {
		return execx.Result{}, errors.New("cannot start")
	}
	return f.inner.Run(ctx, c)
}

func TestRestoreListingWriteFailure(t *testing.T) {
	r := newOpsRig(t)
	r.tty = false
	r.sudoOK()
	r.on(opsInstaller+"backups --team alpha", execx.Result{Stdout: []byte(opsListing)})
	r.ops.Stdout = brokenWriter{}
	if code := r.ops.Restore(context.Background(), []string{"alpha"}); code != 1 {
		t.Errorf("code %d", code)
	}
}

func TestChildExitReportsAnErrorThatIsNotAnExit(t *testing.T) {
	r := newOpsRig(t)
	if code := r.ops.childExit(&execx.ExitError{Name: "x", Code: 9}); code != 9 {
		t.Errorf("exit error: %d", code)
	}
	if code := r.ops.childExit(errors.New("cannot start")); code != 1 {
		t.Errorf("other: %d", code)
	}
	if !strings.Contains(r.said(), "ERROR: cannot start") {
		t.Errorf("said %q", r.said())
	}
}

func TestApplyTeamsPlayThatCannotStartIsExitOne(t *testing.T) {
	r := newOpsRig(t) // no play scripted: the fake cannot run it
	if err := r.ops.ApplyTeams(context.Background(), []string{"beta"}); err == nil {
		t.Fatal("want an error")
	}
	if !strings.Contains(r.said(), "play-agent-bus.yml failed (exit 1, above).") {
		t.Errorf("said %q", r.said())
	}
}

func TestPlayJSONQuotesNames(t *testing.T) {
	if got := playJSON([]string{`a"b`}); got != `{"agent_bus_only_teams": ["a\"b"]}` {
		t.Errorf("got %s", got)
	}
}

func TestQuestionFailedOtherError(t *testing.T) {
	r := newOpsRig(t)
	if code := r.ops.questionFailed(errors.New("boom")); code != 1 {
		t.Errorf("code %d", code)
	}
}
