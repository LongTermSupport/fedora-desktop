package create

import (
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/board"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/flow"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/login"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/ops"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/teamtest"
)

// built is a Wizard over the fake machine, with the flows it is handed.
func built(t *testing.T) (*teamtest.World, *Wizard) {
	t.Helper()
	w := teamtest.Populated(t)
	b := board.New(w.Host)
	o := ops.New(w.Host, b.Statuses, nil)
	return w, New(w.Host, o, login.New(w.Host, flow.New(w.Host), b, o))
}

func TestTheWizardIsBuiltOverTheHostsAdapters(t *testing.T) {
	w, c := built(t)
	h := w.Host
	if c.Declare != h.Declare || c.Gum != h.Gum || c.Git != h.Git || c.Launcher != h.Launcher || c.HostVars != h.HostVars ||
		c.ApplyTeams == nil || c.NewPassword == nil || c.LaunchElement == nil || c.ExecLauncher == nil ||
		c.LookPath == nil || c.NewTerm == nil {
		t.Fatalf("wizard %+v", c)
	}
	if a, b := c.NewTerm(), c.NewTerm(); a == b {
		t.Fatal("each run has a Term of its own")
	}
}

func TestTheWizardsExecLauncherReplacesTheProcessInTheDirectoryGiven(t *testing.T) {
	w := teamtest.Populated(t)
	var gotPath, gotDir string
	var gotArgv []string
	w.Host.Exec = func(path string, argv []string, dir string) error {
		gotPath, gotArgv, gotDir = path, argv, dir
		return nil
	}
	c := New(w.Host, ops.New(w.Host, nil, nil), nil)
	if err := c.ExecLauncher("/top", []string{"ccy", "--teams", "a"}); err != nil {
		t.Fatal(err)
	}
	if gotPath != w.Host.Launcher || gotDir != "/top" || strings.Join(gotArgv, " ") != "ccy --teams a" {
		t.Fatalf("exec %q %v %q", gotPath, gotArgv, gotDir)
	}
}

func TestTheWizardsApplyTeamsRunsThePlayForThoseTeams(t *testing.T) {
	w, c := built(t)
	w.On(w.Host.Play, "", 1)
	if err := c.ApplyTeams(t.Context(), "alpha"); err == nil {
		t.Fatal("a failing play is a failure")
	}
	if !w.Fake.Called(w.Host.Play) {
		t.Fatal("the play was not run")
	}
}
