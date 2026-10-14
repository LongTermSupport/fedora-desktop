package teams

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/ui"
)

func TestTheCreatorIsBuiltOverTheAppsAdapters(t *testing.T) {
	h := newHarness(t)
	c := h.app.newCreator()
	if c.Declare != h.app.Declare || c.Gum != h.app.Gum || c.Git != h.app.Git || c.Launcher != h.app.Launcher ||
		c.ApplyTeams == nil || c.NewPassword == nil || c.LaunchElement == nil || c.ExecLauncher == nil ||
		c.LookPath == nil || c.NewTerm == nil {
		t.Fatalf("creator %+v", c)
	}
}

func TestNewPasswordForTheCreatorFailsWhenTheTeamsCannotBeRead(t *testing.T) {
	h := newHarness(t)
	if err := os.Remove(filepath.Join(h.root, "helpers", "agent_bus", "declare.py")); err != nil {
		t.Fatal(err)
	}
	if err := h.app.newCreator().NewPassword(t.Context(), "alpha", "owner"); err == nil {
		t.Fatal("no error")
	}
}

func TestAnOfferAfterCreateIsOnlyAsFailedAsThePickerFlowSaid(t *testing.T) {
	plain := errors.New("plain")
	cases := []struct {
		in   error
		want error
		text string
	}{
		{nil, nil, ""},
		{leave(term.ExitOK), nil, ""},
		{leave(term.ExitCancelled), ui.ErrCancelled, ""},
		{leave(term.ExitFailure), nil, "it stopped with exit 1"},
		{plain, plain, ""},
	}
	for _, c := range cases {
		got := offerResult(c.in)
		switch {
		case c.text != "":
			if got == nil || got.Error() != c.text {
				t.Fatalf("%v: got %v", c.in, got)
			}
		case !errors.Is(got, c.want):
			t.Fatalf("%v: got %v, want %v", c.in, got, c.want)
		}
	}
}

func TestCreatorExecLauncherReplacesTheProcessInTheDirectoryGiven(t *testing.T) {
	h := newHarness(t)
	var gotPath, gotDir string
	var gotArgv []string
	h.app.Exec = func(path string, argv []string, dir string) error {
		gotPath, gotArgv, gotDir = path, argv, dir
		return nil
	}
	if err := h.app.newCreator().ExecLauncher("/top", []string{"ccy", "--teams", "a"}); err != nil {
		t.Fatal(err)
	}
	if gotPath != h.app.Launcher || gotDir != "/top" || len(gotArgv) != 3 {
		t.Fatalf("exec %q %v %q", gotPath, gotArgv, gotDir)
	}
}
