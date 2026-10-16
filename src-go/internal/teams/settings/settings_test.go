package settings

import (
	"context"
	"slices"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/agentbus"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/board"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/coordinator"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/flow"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/ops"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/teamtest"
)

// alphaSettings is what declare.py settings prints for team alpha.
const alphaSettings = "SETTING\tteam\t\"alpha\"\n" +
	"SETTING\tdescription\t\"Builds the site\"\n" +
	"SETTING\thumans\t[\"owner\", \"guest\"]\n" +
	"SETTING\torg\t\"example-org\"\n" +
	"SETTING\trepos\t[{\"repo\": \"example-org/one\", \"branches\": [\"main\"]}]\n" +
	"SETTING\texclude\t-\n" +
	"SETTING\tpath_prefixes\t-\n" +
	"SETTING\tallow_from\t[]\n" +
	"SETTING\tport\t6167\n" +
	"SETTING\tlisten\t[\"192.0.2.10\"]\n" +
	"SETTING\tserver_name\t\"alpha.agent-bus.internal\"\n" +
	"SETTING\tforge_api\t\"https://api.github.com\"\n" +
	"SETTING\tremoved_humans\t-\n"

// roleCall is one call of the ChangeRole the screen was given.
type roleCall struct {
	team, handle string
	want         bool
	// running is what the Lookup it was given says of the first member, proj.1+local.podman.
	running coordinator.Running
}

type rig struct {
	*teamtest.World
	*teamtest.Console

	s     *Settings
	roles []roleCall
	// look is the running state the picker hands the screen: proj.1 runs, the rest do not.
	look coordinator.Lookup
}

func newRig(t *testing.T) *rig {
	t.Helper()
	w := teamtest.Populated(t)
	c := teamtest.Interactive(t, w)
	h := w.Host
	kit := flow.New(h)
	b := board.New(h)
	o := ops.New(h, b.Statuses, func() { b.ForgetDevices("") })
	w.On(h.Play, "", 0)
	w.OnDeclare("element-teams", "")
	w.OnDeclare("settings", alphaSettings)
	w.OnDeclare("set", "")
	r := &rig{World: w, Console: c}
	r.look = func(m agentbus.Member) coordinator.Running {
		if m.Handle == "proj.1+local.podman" {
			return coordinator.Active
		}
		return coordinator.Stopped
	}
	r.s = New(h, kit, o.ApplyTeams, o.Locked, func(_ context.Context, team, handle string, want bool, look coordinator.Lookup) (bool, error) {
		running := coordinator.Unknown
		if look != nil {
			running = look(agentbus.Member{Handle: "proj.1+local.podman"})
		}
		r.roles = append(r.roles, roleCall{team, handle, want, running})
		return true, nil
	})
	return r
}

// coordinatorIs makes the roster name handle the team's coordinator.
func (r *rig) coordinatorIs(handle string) {
	r.On(teamtest.SudoBus+"list alpha", strings.Replace(teamtest.BusList, "MEMBER\t"+handle+"\tdev", "MEMBER\t"+handle+"\tcoordinator", 1), 0)
}

func (r *rig) edit(t *testing.T) {
	t.Helper()
	if _, err := r.s.Edit(context.Background(), "alpha", r.look); err != nil {
		t.Fatalf("%v: %s", err, r.ErrOut.String())
	}
}

// rows is fzf call n's rows without their tags.
func (r *rig) rows(n int) []string {
	out := make([]string, len(r.Script.Rows[n]))
	for i, row := range r.Script.Rows[n] {
		out[i] = strings.SplitN(row, "\t", 2)[0]
	}
	return out
}

func (r *rig) title(n int) string {
	for _, a := range r.Script.FzfArgs[n] {
		if v, ok := strings.CutPrefix(a, "--border-label="); ok {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

// saves is the set runs that write (not --check), as command lines.
func (r *rig) saves() []string {
	var out []string
	for _, c := range r.Fake.Calls() {
		line := c.Line()
		if strings.HasPrefix(line, r.DeclarePrefix("set")+" ") && !strings.HasSuffix(line, "--check") {
			out = append(out, line)
		}
	}
	return out
}

// summaryHas fails unless fzf call n's header holds every one of wants.
func (r *rig) summaryHas(t *testing.T, n int, wants ...string) {
	t.Helper()
	for _, want := range wants {
		if !strings.Contains(r.HeaderOf(n), want) {
			t.Fatalf("the summary lacks %q: %s", want, r.HeaderOf(n))
		}
	}
}

func (r *rig) plays() int {
	n := 0
	for _, c := range r.Fake.Calls() {
		if c.Name == r.Host.Play {
			n++
		}
	}
	return n
}

// S-F7: one screen lists every field with its current value, the coordinator, the fixed
// fields, then Save and apply, Discard changes and Back, its title naming the team.
func TestTheScreenListsEveryFieldThenSaveDiscardAndBack(t *testing.T) {
	r := newRig(t)
	r.Script.Fzf = []string{"|EXIT"}
	r.edit(t)
	want := []string{
		"Description: Builds the site",
		"People: owner, guest",
		"GitHub owner: example-org",
		"Repositories: example-org/one (branches main)",
		"Excluded repositories: (none)",
		"Path limit: (none)",
		"Allowed networks: (none)",
		"Coordinator: (none)   (changed at once)",
		"Name: alpha   (fixed)",
		"Port: 6167   (fixed)",
		"Listen addresses: 192.0.2.10   (fixed)",
		"Server name: alpha.agent-bus.internal   (fixed)",
		"Forge API: https://api.github.com   (fixed)",
		"Removed people: (none)   (fixed)",
		"Save and apply (nothing changed yet)",
		"Discard changes",
		"Back",
	}
	if got := r.rows(0); !slices.Equal(got, want) {
		t.Fatalf("rows\n%q\nwant\n%q", got, want)
	}
	if got := r.title(0); got != "Team alpha › Settings" {
		t.Fatalf("title %q", got)
	}
	if len(r.saves()) != 0 {
		t.Fatal("Back with nothing changed ran set")
	}
}

// S-F8, S-F10: a field is edited in place, judged with the pending changes by declare set
// --check (re-asked when refused), shown changed, and Save and apply writes every change in
// one set run and runs the apply play once; removing a person makes No the default.
func TestAnEditIsCheckedShownChangedThenSavedAndAppliedOnce(t *testing.T) {
	r := newRig(t)
	r.Fake.OnFunc(strings.Fields(r.DeclarePrefix("set")), func(c execx.Cmd) (execx.Result, error) {
		if slices.Contains(c.Args, "[]") {
			return execx.Result{ExitCode: 64, Stderr: []byte("declare: team file: humans: must hold 1 to 16 entries, has 0\n")}, nil
		}
		if slices.Contains(c.Args, "--check") {
			return execx.Result{}, nil
		}
		return execx.Result{Stdout: []byte("CHANGED\tteam alpha: humans\n")}, nil
	})
	r.Script.Fzf = []string{"|People:", "|Save and apply", "|YES"}
	r.Script.Gum = []string{"CHOOSE:", "CHOOSE:owner, second"}
	r.edit(t)
	if !strings.Contains(r.ErrOut.String(), "humans: must hold 1 to 16 entries") {
		t.Fatalf("the refusal was not shown: %s", r.ErrOut.String())
	}
	if len(r.Script.GumLog) != 2 || !strings.Contains(r.Script.GumLog[0], "Team alpha › Settings › People") {
		t.Fatalf("gum %q", r.Script.GumLog)
	}
	if !slices.Contains(r.rows(1), "People: owner, second   (changed)") || !slices.Contains(r.rows(1), "Save and apply (1 change)") {
		t.Fatalf("rows %q", r.rows(1))
	}
	r.summaryHas(t, 2, "People: owner, guest → owner, second", "deactivated", "play-agent-bus.yml",
		"guest can never be added back")
	if r.title(2) != "Team alpha › Settings › Save and apply" || !slices.Contains(r.Script.FzfArgs[2], "--bind=start:last") {
		t.Fatalf("title %q args %q", r.title(2), r.Script.FzfArgs[2])
	}
	want := r.DeclarePrefix("set") + ` --file ` + r.Host.HostVars + ` --team alpha --value humans ["owner","second"]`
	if saves := r.saves(); len(saves) != 1 || saves[0] != want {
		t.Fatalf("saves %q, want %q", saves, want)
	}
	if r.plays() != 1 {
		t.Fatalf("the play ran %d times", r.plays())
	}
	if !strings.Contains(r.ErrOut.String(), "Team alpha's settings are saved and applied.") {
		t.Fatalf("stderr %s", r.ErrOut.String())
	}
}

// Save and apply writes the host_vars and runs the play in one hold of the play lock, so no
// other play run can come between the write and its apply.
func TestSaveWritesAndAppliesInsideOneHoldOfThePlayLock(t *testing.T) {
	r := newRig(t)
	heldAtWrite := false
	r.Fake.OnFunc(strings.Fields(r.DeclarePrefix("set")), func(c execx.Cmd) (execx.Result, error) {
		if slices.Contains(c.Args, "--check") {
			return execx.Result{}, nil
		}
		heldAtWrite = r.PlayLockHeld(t)
		return execx.Result{Stdout: []byte("CHANGED\tteam alpha: description\n")}, nil
	})
	r.Script.Fzf = []string{"|Description:", "|Save and apply", "|YES"}
	r.Script.Gum = []string{"CHOOSE:Runs the site"}
	r.edit(t)
	if !heldAtWrite {
		t.Error("the host_vars were written outside the play lock")
	}
	if r.plays() != 1 || r.PlayLockHeld(t) {
		t.Errorf("plays %d, lock still held %v", r.plays(), r.PlayLockHeld(t))
	}
}

// The owner's defect: Save and apply while another play run holds the lock. It waits on a screen
// naming the holder; Stop waiting saves nothing, runs nothing, and keeps the changes on the screen
// to save again.
func TestStoppingTheWaitForThePlayLockSavesNothingAndKeepsTheChanges(t *testing.T) {
	r := newRig(t)
	r.HoldPlayLock(t)
	r.Script.Fzf = []string{"|Description:", "|Save and apply", "|YES", "|EXIT", "|EXIT", "|YES"}
	r.Script.Gum = []string{"CHOOSE:Runs the site"}
	r.edit(t)
	if len(r.saves()) != 0 || r.plays() != 0 {
		t.Fatalf("saves %q, plays %d", r.saves(), r.plays())
	}
	if r.title(3) != "Waiting for the play lock" ||
		!strings.Contains(r.HeaderOf(3), "Held by run.bash playbooks/imports/play-basic-configs.yml (pid 999), held since 11:59:20 UTC") {
		t.Fatalf("wait screen %q: %s", r.title(3), r.HeaderOf(3))
	}
	if !slices.Contains(r.rows(4), "Description: Runs the site   (changed)") {
		t.Fatalf("the change is gone: %q", r.rows(4))
	}
	if !strings.Contains(r.HeaderOf(4), "Nothing was saved: the play lock was not free (above). Your changes are still here") {
		t.Fatalf("header %s", r.HeaderOf(4))
	}
	if !r.PlayLockHeld(t) || strings.Contains(r.ErrOut.String(), "saved in the host_vars") {
		t.Fatalf("stderr %s", r.ErrOut.String())
	}
}

// S-F10: a change that removes nobody is saved with Yes highlighted.
func TestASaveThatRemovesNobodyDefaultsToYes(t *testing.T) {
	r := newRig(t)
	r.OnDeclare("set", "CHANGED\tteam alpha: description\n")
	r.Script.Fzf = []string{"|Description:", "|Save and apply", "|YES"}
	r.Script.Gum = []string{"CHOOSE:Runs the site"}
	r.edit(t)
	if slices.Contains(r.Script.FzfArgs[2], "--bind=start:last") {
		t.Fatal("a description change starts on No")
	}
	if !strings.Contains(r.HeaderOf(2), "room's topic") {
		t.Fatalf("summary %q", r.HeaderOf(2))
	}
}

// S-F11: Discard changes forgets every pending edit; Back then asks nothing.
func TestDiscardForgetsThePendingEdits(t *testing.T) {
	r := newRig(t)
	r.Script.Fzf = []string{"|Description:", "|Discard changes", "|EXIT"}
	r.Script.Gum = []string{"CHOOSE:Runs the site"}
	r.edit(t)
	if !slices.Contains(r.rows(1), "Description: Runs the site   (changed)") {
		t.Fatalf("rows %q", r.rows(1))
	}
	if !slices.Contains(r.rows(2), "Description: Builds the site") || !strings.Contains(r.HeaderOf(2), "Discarded 1 change.") {
		t.Fatalf("rows %q header %q", r.rows(2), r.HeaderOf(2))
	}
	if len(r.Script.Rows) != 3 || len(r.saves()) != 0 {
		t.Fatalf("drawn %d times, saves %q", len(r.Script.Rows), r.saves())
	}
}

// S-F11: Back with unsaved changes asks first, No highlighted; No stays on the screen.
func TestBackWithUnsavedChangesAsksFirst(t *testing.T) {
	r := newRig(t)
	r.Script.Fzf = []string{"|Description:", "|EXIT", "|EXIT", "|EXIT", "|YES"}
	r.Script.Gum = []string{"CHOOSE:Runs the site"}
	r.edit(t)
	if r.title(2) != "Team alpha › Settings › Back" || !strings.Contains(r.HeaderOf(2), "1 change is not saved") {
		t.Fatalf("title %q header %q", r.title(2), r.HeaderOf(2))
	}
	if !slices.Contains(r.Script.FzfArgs[2], "--bind=start:last") {
		t.Fatal("leaving without saving must not be the default")
	}
	if len(r.Script.Rows) != 5 || len(r.saves()) != 0 || r.plays() != 0 {
		t.Fatalf("drawn %d times, saves %q, plays %d", len(r.Script.Rows), r.saves(), r.plays())
	}
}

// S-F9: a fixed field says why it cannot be changed, and changes nothing.
func TestAFixedFieldSaysWhyItCannotBeChanged(t *testing.T) {
	r := newRig(t)
	r.Script.Fzf = []string{"|Name: alpha", "|EXIT"}
	r.edit(t)
	if h := r.HeaderOf(1); !strings.Contains(h, "Name cannot be changed here") || !strings.Contains(h, "new team") {
		t.Fatalf("header %q", h)
	}
	if len(r.Script.GumLog) != 0 {
		t.Fatalf("a fixed field asked %q", r.Script.GumLog)
	}
}

// Esc in a field's input leaves the field as it was and goes back to the screen.
func TestEscInAFieldLeavesItAsItWas(t *testing.T) {
	r := newRig(t)
	r.Script.Fzf = []string{"|GitHub owner:", "|EXIT"}
	r.Script.Gum = []string{"CANCEL"}
	r.edit(t)
	if !slices.Contains(r.rows(1), "GitHub owner: example-org") || !slices.Contains(r.rows(1), "Save and apply (nothing changed yet)") {
		t.Fatalf("rows %q", r.rows(1))
	}
}

// Typing the value a field already has is not a change.
func TestTheSameValueAgainIsNoChange(t *testing.T) {
	r := newRig(t)
	r.Script.Fzf = []string{"|Repositories:", "|EXIT"}
	r.Script.Gum = []string{"CHOOSE:example-org/one"}
	r.edit(t)
	if !slices.Contains(r.rows(1), "Save and apply (nothing changed yet)") {
		t.Fatalf("rows %q", r.rows(1))
	}
	if !strings.Contains(r.Script.GumLog[0], "--value example-org/one") {
		t.Fatalf("the input did not start from the value: %q", r.Script.GumLog[0])
	}
}

// Save and apply with nothing changed says so and runs nothing.
func TestSaveWithNothingChangedRunsNothing(t *testing.T) {
	r := newRig(t)
	r.Script.Fzf = []string{"|Save and apply", "|EXIT"}
	r.edit(t)
	if !strings.Contains(r.HeaderOf(1), "Nothing has changed yet.") || len(r.saves()) != 0 || r.plays() != 0 {
		t.Fatalf("header %q saves %q plays %d", r.HeaderOf(1), r.saves(), r.plays())
	}
}

// S-F10: a saved change whose apply fails says it is saved but not applied, and how to apply it.
func TestAFailedApplySaysTheChangeIsSavedButNotApplied(t *testing.T) {
	r := newRig(t)
	r.On(r.Host.Play, "", 2)
	r.Script.Fzf = []string{"|Description:", "|Save and apply", "|YES"}
	r.Script.Gum = []string{"CHOOSE:Runs the site"}
	r.OnDeclare("set", "CHANGED\tteam alpha: description\n")
	r.edit(t)
	for _, want := range []string{"saved in the host_vars but not applied", "ccy-teams apply alpha"} {
		if !strings.Contains(r.ErrOut.String(), want) {
			t.Fatalf("stderr lacks %q: %s", want, r.ErrOut.String())
		}
	}
}

// S-F12, H25's plain path: a team with no coordinator is offered one plainly, and the choice is
// made at once through the one role path (seats.ChangeRole), given the running state.
func TestATeamWithNoCoordinatorIsOfferedOnePlainly(t *testing.T) {
	r := newRig(t)
	r.Script.Fzf = []string{"|Coordinator:", "|proj.1+local.podman", "|EXIT"}
	r.edit(t)
	if r.title(1) != "Team alpha › Settings › Coordinator" || slices.Contains(r.rows(1), "No coordinator") {
		t.Fatalf("title %q rows %q", r.title(1), r.rows(1))
	}
	if h := r.HeaderOf(1); !strings.Contains(h, "alpha has no coordinator.") || strings.Contains(h, "worker") {
		t.Fatalf("header %q", h)
	}
	want := roleCall{"alpha", "proj.1+local.podman", true, coordinator.Active}
	if len(r.roles) != 1 || r.roles[0] != want {
		t.Fatalf("roles %+v", r.roles)
	}
	if !strings.Contains(r.HeaderOf(2), "proj.1+local.podman is now the coordinator of alpha.") {
		t.Fatalf("header %q", r.HeaderOf(2))
	}
	if len(r.saves()) != 0 {
		t.Fatal("a role change is not a host_vars change")
	}
}

// S-F12, H25's takeover: with a coordinator set, the field and the list name it and whether it
// runs, and say it becomes a worker; choosing another member is one ChangeRole call (which asks
// the takeover question, on No, and demotes and promotes with its roll-back), never a demote
// made apart from the promote.
func TestReplacingACoordinatorIsATakeoverThatNamesIt(t *testing.T) {
	r := newRig(t)
	r.coordinatorIs("proj.1+local.podman")
	r.Script.Fzf = []string{"|Coordinator:", "|proj.box+role.lxc", "|EXIT"}
	r.edit(t)
	if !slices.Contains(r.rows(0), "Coordinator: proj.1+local.podman (running)   (changed at once)") {
		t.Fatalf("rows %q", r.rows(0))
	}
	h := r.HeaderOf(1)
	for _, want := range []string{"alpha's coordinator is proj.1+local.podman (running)", "becomes a worker", "starts on No"} {
		if !strings.Contains(h, want) {
			t.Fatalf("the takeover header lacks %q: %q", want, h)
		}
	}
	want := roleCall{"alpha", "proj.box+role.lxc", true, coordinator.Active}
	if len(r.roles) != 1 || r.roles[0] != want {
		t.Fatalf("roles %+v", r.roles)
	}
}

// With a coordinator set, No coordinator is offered, and it is that member made a worker.
func TestNoCoordinatorMakesTheCurrentOneAWorker(t *testing.T) {
	r := newRig(t)
	r.coordinatorIs("proj.1+local.podman")
	r.Script.Fzf = []string{"|Coordinator:", "|No coordinator", "|EXIT"}
	r.edit(t)
	want := roleCall{"alpha", "proj.1+local.podman", false, coordinator.Active}
	if len(r.roles) != 1 || r.roles[0] != want {
		t.Fatalf("roles %+v", r.roles)
	}
	if !strings.Contains(r.HeaderOf(2), "alpha has no coordinator now") {
		t.Fatalf("header %q", r.HeaderOf(2))
	}
}

// S-F13: a vault-encrypted value is shown as such, and the team's entry is not edited here.
func TestAVaultedEntryIsNotEditedHere(t *testing.T) {
	r := newRig(t)
	r.OnDeclare("settings", strings.Replace(alphaSettings, "[\"owner\", \"guest\"]", "!vaulted", 1))
	r.Script.Fzf = []string{"|Description:", "|EXIT"}
	r.edit(t)
	if !slices.Contains(r.rows(0), "People: (vault-encrypted)") {
		t.Fatalf("rows %q", r.rows(0))
	}
	if h := r.HeaderOf(1); !strings.Contains(h, "vault-encrypted") || !strings.Contains(h, "by hand") || len(r.Script.GumLog) != 0 {
		t.Fatalf("header %q gum %q", h, r.Script.GumLog)
	}
}

// Settings that cannot be read are reported, and nothing is shown or changed.
func TestUnreadableSettingsAreReported(t *testing.T) {
	r := newRig(t)
	r.OnErr(r.DeclarePrefix("settings"), "declare: --team: team alpha is not declared present, with a port, in agent_bus_teams\n", 64)
	r.edit(t)
	if !strings.Contains(r.ErrOut.String(), "not declared present") || len(r.Script.Rows) != 0 {
		t.Fatalf("stderr %s, drawn %d times", r.ErrOut.String(), len(r.Script.Rows))
	}
}

// settingsThen answers declare settings with first, then with later on every read after it.
func (r *rig) settingsThen(first, later string) {
	reads := 0
	r.Fake.OnFunc(strings.Fields(r.DeclarePrefix("settings")), func(execx.Cmd) (execx.Result, error) {
		reads++
		if reads == 1 {
			return execx.Result{Stdout: []byte(first)}, nil
		}
		return execx.Result{Stdout: []byte(later)}, nil
	})
}

// S-F10: the summary's old value is the host_vars' at save time, read again then.
func TestTheSummaryShowsTheValueOnDiskAtSaveTime(t *testing.T) {
	r := newRig(t)
	r.settingsThen(alphaSettings, strings.Replace(alphaSettings, "Builds the site", "Edited elsewhere", 1))
	r.OnDeclare("set", "CHANGED\tteam alpha: description\n")
	r.Script.Fzf = []string{"|Description:", "|Save and apply", "|YES"}
	r.Script.Gum = []string{"CHOOSE:Runs the site"}
	r.edit(t)
	r.summaryHas(t, 2, "Description: Edited elsewhere → Runs the site")
}

// S-F10: a set that changes nothing says so and stays on the screen; the play does not run.
func TestASaveThatChangesNothingSaysSo(t *testing.T) {
	r := newRig(t)
	r.OnDeclare("set", "UNCHANGED\talpha\n")
	r.Script.Fzf = []string{"|Description:", "|Save and apply", "|YES", "|EXIT"}
	r.Script.Gum = []string{"CHOOSE:Runs the site"}
	r.edit(t)
	if !strings.Contains(r.HeaderOf(3), "Nothing to save: the host_vars already had these values.") || r.plays() != 0 {
		t.Fatalf("header %q plays %d", r.HeaderOf(3), r.plays())
	}
}

// S-F13: a value that became unreadable since the screen opened stops the save, saying so.
func TestASaveStopsWhenAValueBecameUnreadable(t *testing.T) {
	r := newRig(t)
	r.settingsThen(alphaSettings, strings.Replace(alphaSettings, "[\"owner\", \"guest\"]", "!vaulted", 1))
	r.Script.Fzf = []string{"|Description:", "|Save and apply", "|EXIT", "|YES"}
	r.Script.Gum = []string{"CHOOSE:Runs the site"}
	r.edit(t)
	if h := r.HeaderOf(2); !strings.Contains(h, "Nothing was saved") || !strings.Contains(h, "People") || len(r.saves()) != 0 {
		t.Fatalf("header %q saves %q", h, r.saves())
	}
}
