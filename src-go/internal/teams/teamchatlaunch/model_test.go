package teamchatlaunch

import (
	"context"
	"errors"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/seatmodel"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

// setModels writes the store's seat models: team alpha's default, and seat's own when not "".
func setModels(t *testing.T, store paths.Store, teamDefault, seat, model string) {
	t.Helper()
	alpha, err := paths.ParseTeamID("alpha")
	if err != nil {
		t.Fatal(err)
	}
	var s seatmodel.Settings
	s.SetDefault(alpha, teamDefault)
	if seat != "" {
		name, err := paths.ParseSeatName(seat)
		if err != nil {
			t.Fatal(err)
		}
		s.SetSeat(alpha, name, model)
	}
	if err := seatmodel.Write(store, s); err != nil {
		t.Fatal(err)
	}
}

// Register R39: a launch whose seat has a model gives the container ANTHROPIC_MODEL, which
// claude ranks below a --model on its command line; the take says where the model came from.
func TestLaunchGivesTheSeatsModelToTheContainer(t *testing.T) {
	w := newLaunchWorld(t)
	setModels(t, w.launch.Store, "opus", "", "")
	session := mustSession(t, "s1")
	taken, err := w.launch.Take(context.Background(), session, "1@alpha", nil, true)
	if err != nil {
		t.Fatal(err)
	}
	want := append(RunArgs(w.launch.Store, session, true), "-e", "ANTHROPIC_MODEL=opus")
	if !slices.Equal(taken.Args, want) {
		t.Fatalf("args %q\nwant %q", taken.Args, want)
	}
	if taken.Model.Model != "opus" || taken.Model.FromSeat || taken.Model.Seat.Name.String() != "repo.1+site.podman" {
		t.Fatalf("model %+v", taken.Model)
	}
	// A seat's own model wins over its team's.
	w = newLaunchWorld(t)
	setModels(t, w.launch.Store, "opus", "repo.1+site.podman", "sonnet")
	args, err := w.launch.Run(context.Background(), session, "1@alpha", nil, false)
	if err != nil || args[len(args)-1] != "ANTHROPIC_MODEL=sonnet" {
		t.Fatalf("args %q: %v", args, err)
	}
}

func TestLaunchWithNoModelAddsNothing(t *testing.T) {
	w := newLaunchWorld(t)
	session := mustSession(t, "s1")
	taken, err := w.launch.Take(context.Background(), session, "1@alpha", nil, false)
	if err != nil || taken.Model.Model != "" || !slices.Equal(taken.Args, RunArgs(w.launch.Store, session, false)) {
		t.Fatalf("taken %+v: %v", taken, err)
	}
}

// A models file that cannot be trusted refuses the launch before any seat is taken.
func TestLaunchRefusesABrokenModelsFileBeforeTaking(t *testing.T) {
	w := newLaunchWorld(t)
	if err := os.MkdirAll(w.launch.Store.Root(), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(seatmodel.File(w.launch.Store), []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := w.launch.Run(context.Background(), mustSession(t, "s1"), "1@alpha", nil, false)
	if !errors.Is(err, seatmodel.ErrFile) || w.taker.took != nil {
		t.Fatalf("got %v, took %+v", err, w.taker.took)
	}
}

func TestLaunchArgsSaysTheModelOnStderr(t *testing.T) {
	w := newVerbs(t)
	w.pool("repo.1+site.podman")
	setModels(t, w.store, "opus", "", "")
	if code := w.run("launch-args", "s1", w.checkout, "1@alpha", "", "--"); code != term.ExitOK {
		t.Fatalf("exit %d: %s", code, w.err)
	}
	lines := w.lines()
	if lines[len(lines)-1] != "ANTHROPIC_MODEL=opus" || strings.Contains(w.out.String(), "Model:") {
		t.Fatalf("stdout %q", w.out)
	}
	if !strings.Contains(w.err.String(), "Model: opus, team alpha's model") {
		t.Fatalf("stderr %q", w.err)
	}
}

// Host cc: the model reaches claude as ANTHROPIC_MODEL in the session's environment, and a
// person's own ANTHROPIC_MODEL is kept (said); a --model the person gave stays in claude's
// arguments, which claude ranks first.
func TestCCRunGivesTheSeatsModelUnlessThePersonSetOne(t *testing.T) {
	w := newCC(t)
	w.pool("repo.1+site.host")
	setModels(t, w.store, "", "repo.1+site.host", "opus")
	if code := w.ccRun("cc-s1", "1@alpha", "false", "--model", "haiku"); code != term.ExitOK {
		t.Fatalf("exit %d: %s", code, w.err)
	}
	cmd := w.fg.cmds[0]
	if !slices.Equal(cmd.Env, []string{"PINGBUS_MEMBERSHIP=off", "ANTHROPIC_MODEL=opus"}) || cmd.Args[len(cmd.Args)-1] != "haiku" {
		t.Fatalf("env %q args %q", cmd.Env, cmd.Args)
	}
	if !strings.Contains(w.err.String(), "Model: opus, the model of seat repo.1+site.host of team alpha") {
		t.Fatalf("stderr %q", w.err)
	}

	w = newCC(t)
	w.pool("repo.1+site.host")
	setModels(t, w.store, "opus", "", "")
	getenv := w.verbs.Getenv
	w.verbs.Getenv = func(k string) string {
		if k == seatmodel.EnvVar {
			return "sonnet"
		}
		return getenv(k)
	}
	if code := w.ccRun("cc-s1", "1@alpha", "false"); code != term.ExitOK {
		t.Fatalf("exit %d: %s", code, w.err)
	}
	if !slices.Equal(w.fg.cmds[0].Env, []string{"PINGBUS_MEMBERSHIP=off"}) || !strings.Contains(w.err.String(), "ANTHROPIC_MODEL=sonnet") {
		t.Fatalf("env %q stderr %q", w.fg.cmds[0].Env, w.err)
	}
}

type brokenWriter struct{}

func (brokenWriter) Write([]byte) (int, error) { return 0, errors.New("closed") }

// A model note that cannot be written fails the launch: on cc after the take, so the session
// is ended and its seats released.
func TestAModelNoteThatCannotBeWrittenFails(t *testing.T) {
	w := newVerbs(t)
	w.pool("repo.1+site.podman")
	setModels(t, w.store, "opus", "", "")
	w.verbs.Stderr = brokenWriter{}
	if code := w.verbs.Run(context.Background(), []string{"launch-args", "s1", w.checkout, "1@alpha", "", "--"}); code == term.ExitOK {
		t.Fatal("launch-args passed")
	}
	c := newCC(t)
	c.pool("repo.1+site.host")
	setModels(t, c.store, "opus", "", "")
	c.verbs.Stderr = brokenWriter{}
	if code := c.verbs.Run(context.Background(), []string{"cc-run", c.tmp, c.checkout, "cc-s1", "4242", "1@alpha", "false", "--"}); code == term.ExitOK {
		t.Fatal("cc-run passed")
	}
	if len(c.fg.cmds) != 0 || !slices.Equal(c.forgot, []string{"cc-s1"}) {
		t.Fatalf("started %+v, forgot %q", c.fg.cmds, c.forgot)
	}
}
