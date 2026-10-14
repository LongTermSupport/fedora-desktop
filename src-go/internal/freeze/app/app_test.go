package app

// The flow both freeze tools share, driven by a small engine of its own so the order of
// operations is pinned here once rather than in each tool: parse, help, list-with-a-target,
// host guard, preflight, one load, then list, menu or one-shot. The tools' own suites drive the
// same flow end to end through their real engines.

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/freeze"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/freeze/cli"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

const usage = "Usage: testfreeze\n"

type engine struct {
	order       *[]string
	preflight   error
	entries     []freeze.Entry
	selected    []string
	gotMode     string
	gotValue    string
	gotNames    []string
	selectedErr error
	acted       []string
}

func (e *engine) Preflight(context.Context) error {
	*e.order = append(*e.order, "preflight")
	return e.preflight
}

func (e *engine) Load(context.Context) (freeze.Inventory, error) {
	*e.order = append(*e.order, "load")
	return freeze.NewInventory(cfg(), e.entries), nil
}

func (e *engine) Groups(inv freeze.Inventory) []freeze.Group {
	return []freeze.Group{{Key: "all", Description: "everything", Members: inv.Names()}}
}

func (e *engine) Select(_ context.Context, inv freeze.Inventory, _ string) ([]string, error) {
	return inv.Names(), nil
}

func (e *engine) SelectTarget(_ context.Context, _ freeze.Inventory, mode, value string, names []string) ([]string, error) {
	*e.order = append(*e.order, "select")
	e.gotMode, e.gotValue, e.gotNames = mode, value, names
	return e.selected, e.selectedErr
}

func (e *engine) Act(_ context.Context, verb freeze.Verb, name string) (string, error) {
	e.acted = append(e.acted, string(verb)+" "+name)
	return "", nil
}

func (e *engine) Columns() string         { return "COL" }
func (e *engine) Row(freeze.Entry) string { return "row" }

func cfg() freeze.Config {
	return freeze.Config{
		Tool: "testfreeze", StateRunning: "running", StateFrozen: "paused",
		HostOnlyNote: "  not from in here.", TargetHint: "NAME... or --all",
	}
}

type quitChooser struct{}

func (quitChooser) Group(context.Context, []string, []string) (string, error) {
	return "", freeze.ErrQuit
}

func (quitChooser) Members(context.Context, freeze.MemberScreen) (freeze.Choice, error) {
	return freeze.Choice{All: true}, nil
}

type rig struct {
	t     *testing.T
	eng   *engine
	order []string
	out   *bytes.Buffer
	errb  *bytes.Buffer
	env   freeze.Env
	tool  Tool
}

func newRig(t *testing.T) *rig {
	t.Helper()
	r := &rig{t: t, out: &bytes.Buffer{}, errb: &bytes.Buffer{}}
	r.eng = &engine{order: &r.order, entries: []freeze.Entry{
		{Name: "alpha", State: "running"}, {Name: "bravo", State: "paused"},
	}}
	r.env = freeze.NewEnv(r.out, r.errb, &execx.Fake{})
	dir := t.TempDir()
	r.env.ContainerEnvPath, r.env.DockerEnvPath = dir+"/containerenv", dir+"/dockerenv"
	r.env.Getenv = func(string) string { return "" }
	r.env.IsTerminal = func(any) bool { return false }
	r.tool = Tool{
		Spec:   cli.Spec{Tool: "testfreeze", Flags: []cli.Flag{{Name: "--all"}, {Name: "--group", Needs: "a group"}}},
		Usage:  usage,
		Cfg:    cfg(),
		Engine: r.eng,
	}
	return r
}

func (r *rig) run(chooser freeze.Chooser, args ...string) error {
	r.t.Helper()
	return Execute(context.Background(), r.env, chooser, r.tool, args)
}

func TestHelpPrintsUsageOnStdoutAndTouchesNothing(t *testing.T) {
	r := newRig(t)
	if err := r.run(nil, "--help"); err != nil {
		t.Fatal(err)
	}
	if r.out.String() != usage || r.errb.Len() != 0 || len(r.order) != 0 {
		t.Fatalf("stdout %q stderr %q order %v", r.out, r.errb, r.order)
	}
}

func TestAFailedUsageWriteIsReturned(t *testing.T) {
	r := newRig(t)
	r.env.Out = failWriter{}
	if err := r.run(nil, "-h"); err == nil || !strings.Contains(err.Error(), "write usage") {
		t.Fatalf("got %v", err)
	}
}

type failWriter struct{}

func (failWriter) Write([]byte) (int, error) { return 0, errors.New("closed") }

func TestAParseMistakeIsReturnedBeforeAnythingRuns(t *testing.T) {
	r := newRig(t)
	err := r.run(nil, "--bogus")
	if !strings.Contains(err.Error(), "unknown option '--bogus'") || len(r.order) != 0 {
		t.Fatalf("got %v, order %v", err, r.order)
	}
}

func TestListWithATargetIsRefusedBeforeTheGuardAndThePreflight(t *testing.T) {
	r := newRig(t)
	r.env.Getenv = func(string) string { return "oci" } // would fail the host guard
	err := r.run(nil, "list", "--all")
	var die *freeze.DieError
	if !errors.As(err, &die) || die.Msg != "'list' takes no target — it reports everything." || len(r.order) != 0 {
		t.Fatalf("got %v, order %v", err, r.order)
	}
}

func TestTheHostGuardPrecedesThePreflight(t *testing.T) {
	r := newRig(t)
	r.env.Getenv = func(k string) string {
		if k == "container" {
			return "oci"
		}
		return ""
	}
	err := r.run(nil, "list")
	if err == nil || !strings.Contains(err.Error(), "this is a container") || len(r.order) != 0 {
		t.Fatalf("got %v, order %v", err, r.order)
	}
}

func TestAFailedPreflightStopsBeforeTheLoad(t *testing.T) {
	r := newRig(t)
	r.eng.preflight = freeze.Die("not installed")
	err := r.run(nil, "list")
	if err == nil || err.Error() != "not installed" || strings.Join(r.order, ",") != "preflight" {
		t.Fatalf("got %v, order %v", err, r.order)
	}
}

func TestListLoadsOnceAndWritesToStdout(t *testing.T) {
	r := newRig(t)
	if err := r.run(nil, "list"); err != nil {
		t.Fatal(err)
	}
	if strings.Join(r.order, ",") != "preflight,load" || !strings.Contains(r.out.String(), "=== frozen (1) ===") {
		t.Fatalf("order %v, stdout %q", r.order, r.out)
	}
}

func TestNoTargetRunsTheMenuOnTheStartupLoad(t *testing.T) {
	r := newRig(t)
	if err := r.run(quitChooser{}); err != nil {
		t.Fatal(err)
	}
	if strings.Join(r.order, ",") != "preflight,load" {
		t.Fatalf("one load serves the menu's first pass: %v", r.order)
	}
}

func TestATargetIsResolvedByTheEngineAndActedOn(t *testing.T) {
	r := newRig(t)
	r.eng.selected = []string{"alpha"}
	if err := r.run(nil, "freeze", "--group", "g1"); err != nil {
		// --group is not a flag of this spec's Needs form for the engine; it is, so no error.
		t.Fatal(err)
	}
	if r.eng.gotMode != "--group" || r.eng.gotValue != "g1" || strings.Join(r.eng.acted, ",") != "freeze alpha" {
		t.Fatalf("mode %q value %q acted %v", r.eng.gotMode, r.eng.gotValue, r.eng.acted)
	}
	if strings.Join(r.order, ",") != "preflight,load,select" {
		t.Fatalf("order %v", r.order)
	}
}

func TestNamesReachTheEngineInTheNamesMode(t *testing.T) {
	r := newRig(t)
	r.eng.selected = []string{"bravo"}
	if err := r.run(nil, "bravo", "nosuch"); err != nil {
		t.Fatal(err)
	}
	if r.eng.gotMode != cli.ModeNames || strings.Join(r.eng.gotNames, ",") != "bravo,nosuch" {
		t.Fatalf("mode %q names %v", r.eng.gotMode, r.eng.gotNames)
	}
	if strings.Join(r.eng.acted, ",") != "thaw bravo" {
		t.Fatalf("the derived verb thaws a paused set: %v", r.eng.acted)
	}
}

func TestASelectionErrorEndsTheRun(t *testing.T) {
	r := newRig(t)
	r.eng.selectedErr = freeze.Die("no such group")
	err := r.run(nil, "--all")
	if err == nil || err.Error() != "no such group" || len(r.eng.acted) != 0 {
		t.Fatalf("got %v, acted %v", err, r.eng.acted)
	}
}

func TestTheChoosersCloseErrorIsJoinedToTheResult(t *testing.T) {
	r := newRig(t)
	boom := errors.New("close terminal")
	err := ExecuteClosing(context.Background(), r.env, func() (freeze.Chooser, func() error) {
		return quitChooser{}, func() error { return boom }
	}, r.tool, nil)
	if !errors.Is(err, boom) {
		t.Fatalf("got %v", err)
	}
}

func TestWithNoChooserGivenTheTerminalOneIsBuiltAndNeedsATerminal(t *testing.T) {
	r := newRig(t)
	r.env.OpenTTY = func() (io.ReadCloser, error) { return nil, errors.New("no tty") }
	err := r.run(nil)
	if err == nil || strings.Contains(err.Error(), "internal") {
		t.Fatalf("the menu cannot be asked on without a terminal: %v", err)
	}
}

func TestRunMapsTheResultToAnExitStatus(t *testing.T) {
	r := newRig(t)
	if code := Run(r.env, nil, r.tool, []string{"--help"}); code != term.ExitOK {
		t.Fatalf("--help: %d", code)
	}
	if code := Run(r.env, nil, r.tool, []string{"--bogus"}); code != term.ExitFailure {
		t.Fatalf("--bogus: %d", code)
	}
	if !strings.Contains(r.errb.String(), "testfreeze: unknown option '--bogus'") {
		t.Fatalf("stderr %q", r.errb)
	}
}
