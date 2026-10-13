package teams

import (
	"context"
	"errors"
	"io"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/ui"
)

// fakeOps records the flows the picker hands off, and answers what the test set.
type fakeOps struct {
	calls     []string
	createRC  int
	applyRC   int
	removeRC  int
	teamsErr  error
	elementEr error
}

func (f *fakeOps) create(_ context.Context, args []string) int {
	f.calls = append(f.calls, "create "+strings.Join(args, " "))
	return f.createRC
}

func (f *fakeOps) apply(_ context.Context, args []string) int {
	f.calls = append(f.calls, "apply "+strings.Join(args, " "))
	return f.applyRC
}

func (f *fakeOps) remove(_ context.Context, args []string) int {
	f.calls = append(f.calls, "remove "+strings.Join(args, " "))
	return f.removeRC
}

func (f *fakeOps) applyTeams(_ context.Context, teams []string) error {
	f.calls = append(f.calls, "applyTeams "+strings.Join(teams, " "))
	return f.teamsErr
}

func (f *fakeOps) runElementPlay(_ context.Context, teams []string) error {
	f.calls = append(f.calls, "elementPlay "+strings.Join(teams, " "))
	return f.elementEr
}

var colourCodes = regexp.MustCompile("\x1b\\[[0-9;]*m")

// scripted stands in for fzf and gum: each fzf call takes the next answer, "<key>|<choice>"
// where choice is text the chosen row contains, YES, EXIT or ABORT; each gum call takes the
// next of gum, "YES", "NO" or "CANCEL".
type scripted struct {
	t       *testing.T
	fzf     []string
	gum     []string
	rows    [][]string // the rows each fzf call was shown, colour stripped
	fzfArgs [][]string
	gumLog  []string
	// rewrite, when set, changes fzf's answer before it is returned.
	rewrite func(string) string
}

func (s *scripted) Run(_ context.Context, c execx.Cmd) (execx.Result, error) {
	switch c.Name {
	case "fzf":
		return s.runFzf(c)
	case "gum":
		return s.runGum(c)
	}
	s.t.Fatalf("scripted runner asked for %s", c.Line())
	return execx.Result{}, nil
}

func (s *scripted) runFzf(c execx.Cmd) (execx.Result, error) {
	in, err := io.ReadAll(c.Stdin)
	if err != nil {
		s.t.Fatal(err)
	}
	rows := strings.Split(strings.TrimSuffix(colourCodes.ReplaceAllString(string(in), ""), "\n"), "\n")
	s.rows = append(s.rows, rows)
	s.fzfArgs = append(s.fzfArgs, c.Args)
	if len(s.fzf) == 0 {
		s.t.Fatalf("no fzf answer queued for call %d (rows %q)", len(s.rows), rows)
	}
	answer := s.fzf[0]
	s.fzf = s.fzf[1:]
	key, choice, found := strings.Cut(answer, "|")
	if !found {
		s.t.Fatalf("fzf answer %q has no |", answer)
	}
	row := ""
	switch choice {
	case "ABORT":
		return execx.Result{ExitCode: 130}, &execx.ExitError{Name: "fzf", Code: 130}
	case "EXIT":
		row = ui.ExitRow
	case "YES":
		for _, r := range rows {
			if strings.HasPrefix(r, "Yes, ") {
				row = r
				break
			}
		}
	default:
		for _, r := range rows {
			if strings.Contains(r, choice) {
				row = r
				break
			}
		}
	}
	if row == "" {
		s.t.Fatalf("no row contains %q in %q", choice, rows)
	}
	out := row + "\n"
	if s.rewrite != nil {
		out = s.rewrite(out)
	}
	if slices.ContainsFunc(c.Args, func(a string) bool { return strings.HasPrefix(a, "--expect") }) {
		out = key + "\n" + out
	}
	return execx.Result{Stdout: []byte(out)}, nil
}

func (s *scripted) runGum(c execx.Cmd) (execx.Result, error) {
	s.gumLog = append(s.gumLog, c.Line())
	if len(s.gum) == 0 {
		s.t.Fatalf("no gum answer queued for %s", c.Line())
	}
	answer := s.gum[0]
	s.gum = s.gum[1:]
	switch answer {
	case "YES":
		return execx.Result{}, nil
	case "NO":
		return execx.Result{ExitCode: 1}, &execx.ExitError{Name: "gum", Code: 1}
	}
	return execx.Result{ExitCode: 130}, &execx.ExitError{Name: "gum", Code: 130}
}

// execCall is one replacement of the process by the launcher.
type execCall struct {
	path string
	argv []string
	dir  string
}

// pickerHarness is world() on terminals, with scripted fzf and gum, a fake operator, a
// terminal that answers Enter, and a launcher that is recorded, not run.
type pickerHarness struct {
	harness

	script *scripted
	ops    *fakeOps
	execs  []execCall
	tty    string
}

func newPickerHarness(t *testing.T) *pickerHarness {
	t.Helper()
	h := &pickerHarness{harness: world(t), script: &scripted{t: t}, ops: &fakeOps{}, tty: "\n"}
	h.env["NO_COLOR"] = "1"
	h.env["PWD"] = h.root
	h.onDeclare("limited", "")
	h.app.Term.IsTerminal = func(any) bool { return true }
	h.app.Gum = &ui.Gum{Runner: h.script, Stdin: h.app.Stdin, Stderr: h.errOut}
	h.app.Picker = &ui.Picker{Runner: h.script, Stderr: h.errOut}
	h.app.Ops = h.ops
	h.app.LookPath = func(name string) (string, error) { return "/usr/bin/" + name, nil }
	h.app.OpenTTY = func() (io.ReadCloser, error) { return io.NopCloser(strings.NewReader(h.tty)), nil }
	h.app.Exec = func(path string, argv []string, dir string) error {
		h.execs = append(h.execs, execCall{path: path, argv: argv, dir: dir})
		return nil
	}
	h.app.Launcher = "/x/launcher"
	h.app.Home = h.env["HOME"]
	return h
}

// inProject makes the working directory a work tree whose top is the checkout of world().
func (h *pickerHarness) inProject() {
	h.on("git rev-parse --is-inside-work-tree", "true\n", 0)
	h.on("git rev-parse --show-toplevel", h.checkout()+"\n", 0)
}

func (h *pickerHarness) run() int { return h.app.runPicker(context.Background()) }

// headerOf is the --header of fzf call n.
func (h *pickerHarness) headerOf(n int) string {
	for _, a := range h.script.fzfArgs[n] {
		if v, ok := strings.CutPrefix(a, "--header="); ok {
			return v
		}
	}
	return ""
}

func TestDispatchWithNoArgumentsIsThePicker(t *testing.T) {
	h := newHarness(t)
	if code := h.app.Run(nil); code != term.ExitFailure {
		t.Fatalf("exit %d", code)
	}
	if !strings.Contains(h.errOut.String(), "ccy-teams needs a terminal") {
		t.Fatalf("stderr %q", h.errOut.String())
	}
	if h.out.String() != "" {
		t.Fatalf("stdout %q", h.out.String())
	}
}

func TestPickerNeedsATerminal(t *testing.T) {
	h := newPickerHarness(t)
	h.app.Term.IsTerminal = func(v any) bool { return v != h.app.Stdout }
	if code := h.run(); code != term.ExitFailure {
		t.Fatalf("exit %d", code)
	}
	want := "ccy-teams needs a terminal: it attaches sessions to the one you are in. ccy-teams --list prints the table instead.\n"
	if h.errOut.String() != want {
		t.Fatalf("stderr %q", h.errOut.String())
	}
}

func TestPickerEscLeavesWithTheHouseStyleArgvAndTaggedRows(t *testing.T) {
	h := newPickerHarness(t)
	h.script.fzf = []string{"|ABORT"}
	if code := h.run(); code != term.ExitOK {
		t.Fatalf("exit %d: %s", code, h.errOut.String())
	}
	args := h.script.fzfArgs[0]
	if !slices.Contains(args, "--expect=ctrl-n,ctrl-r,ctrl-x,ctrl-e,ctrl-p,ctrl-u,?") ||
		!slices.Contains(args, "--border-label= Agent teams ") || !slices.Contains(args, "--with-nth=1") {
		t.Fatalf("args %q", args)
	}
	if len(h.script.rows[0]) != 8+1 || h.script.rows[0][8] != "Exit" {
		t.Fatalf("rows %q", h.script.rows[0])
	}
	if !strings.HasPrefix(h.script.rows[0][0], "alpha ") || !strings.HasSuffix(h.script.rows[0][0], "\t0") {
		t.Fatalf("first row %q", h.script.rows[0][0])
	}
}

func TestPickerHeaderNamesTheCheckoutTopNotTheSubdirectory(t *testing.T) {
	h := newPickerHarness(t)
	h.inProject()
	h.env["PWD"] = h.checkout() + "/sub/dir"
	h.script.fzf = []string{"|ABORT"}
	if code := h.run(); code != term.ExitOK {
		t.Fatalf("exit %d: %s", code, h.errOut.String())
	}
	header := h.headerOf(0)
	for _, want := range []string{
		"↑↓ choose   Enter attach a seat / password for a person   Ctrl-P password Ctrl-E Element   ? help   Esc or q quit\n",
		"Ctrl-N new session   Ctrl-U update out-of-date teams   Ctrl-R restore on/off   Ctrl-X remove/park/take down\n",
		"Ctrl-N starts a new ccy session in the row's team, in " + h.checkout() + "\n",
		"session restore on this machine is off (ccy_restore_sessions)\n",
		"TEAM ",
	} {
		if !strings.Contains(header, want) {
			t.Fatalf("header %q lacks %q", header, want)
		}
	}
	if strings.Contains(header, "sub/dir") {
		t.Fatalf("header names the subdirectory: %q", header)
	}
}

func TestPickerHeaderSaysCtrlNIsUnavailableOutsideAProjectAndRestoreOn(t *testing.T) {
	h := newPickerHarness(t)
	mustWrite(t, h.env["HOME"]+"/.config/systemd/user/default.target.wants/ccy-sessions-restore.service", "")
	h.script.fzf = []string{"|ABORT"}
	h.run()
	header := h.headerOf(0)
	if !strings.Contains(header, "Ctrl-N unavailable: "+h.root+" is not a project folder (no git repository)\n") ||
		!strings.Contains(header, "restore on this machine is on (") {
		t.Fatalf("header %q", header)
	}
}

func TestPickerHeaderHeadingIsBoldOnAColourTerminal(t *testing.T) {
	h := newPickerHarness(t)
	delete(h.env, "NO_COLOR")
	h.script.fzf = []string{"|ABORT"}
	h.run()
	if !strings.HasSuffix(h.headerOf(0), cBold+headingText()+cReset+"\n") {
		t.Fatalf("header %q", h.headerOf(0))
	}
}

func TestPickerAsksForSudoOnceAndRefusalLeavesWithTheCost(t *testing.T) {
	h := newPickerHarness(t)
	h.fake.On([]string{"sudo", "-n", "/usr/local/bin/agent-bus", "list", "alpha"},
		execx.Result{Stderr: []byte("sudo: a password is required\n"), ExitCode: 1})
	h.onErr("sudo -n -v", "sudo: a password is required", 1)
	h.onErr("sudo -v", "sudo: 3 incorrect password attempts", 1)
	if code := h.run(); code != term.ExitFailure {
		t.Fatalf("exit %d", code)
	}
	for _, want := range []string{
		"done as root", "play-agent-bus.yml", "sudo was not given your password",
		"ERROR: so the teams' members cannot be shown, and nothing was changed. ccy-teams --list shows the teams without them.\n",
	} {
		if !strings.Contains(h.errOut.String(), want) {
			t.Fatalf("stderr %q lacks %q", h.errOut.String(), want)
		}
	}
	if len(h.script.rows) != 0 {
		t.Fatal("no picker is shown")
	}
}

func TestPickerStillRefusedAfterSudoLeaves(t *testing.T) {
	h := newPickerHarness(t)
	h.fake.On([]string{"sudo", "-n", "/usr/local/bin/agent-bus", "list", "alpha"},
		execx.Result{Stderr: []byte("sudo: a password is required\n"), ExitCode: 1})
	h.on("sudo -n -v", "", 1)
	h.on("sudo -v", "", 0)
	if code := h.run(); code != term.ExitFailure {
		t.Fatalf("exit %d", code)
	}
	if !strings.Contains(h.errOut.String(), "ERROR: sudo took your password but still refuses agent-bus list, so the members cannot be shown.\n") {
		t.Fatalf("stderr %q", h.errOut.String())
	}
}

func TestPickerNoTeamsOffersCreate(t *testing.T) {
	h := newPickerHarness(t)
	h.onDeclare("declared", "")
	h.on("systemctl list-units", "", 0)
	h.script.gum = []string{"YES"}
	h.ops.createRC = 7
	if code := h.run(); code != 7 {
		t.Fatalf("exit %d: %s", code, h.errOut.String())
	}
	if !slices.Equal(h.ops.calls, []string{"create "}) {
		t.Fatalf("ops %v", h.ops.calls)
	}
	if !slices.Equal(h.script.gumLog, []string{"gum confirm --default=false Create one now?"}) {
		t.Fatalf("gum %v", h.script.gumLog)
	}
	if !strings.Contains(h.errOut.String(), "No agent teams on this machine yet.") {
		t.Fatalf("stderr %q", h.errOut.String())
	}
}

func TestPickerNoTeamsDeclinedAndCancelled(t *testing.T) {
	h := newPickerHarness(t)
	h.onDeclare("declared", "")
	h.on("systemctl list-units", "", 0)
	h.script.gum = []string{"NO"}
	if code := h.run(); code != term.ExitOK {
		t.Fatalf("exit %d", code)
	}
	if !strings.Contains(h.errOut.String(), "Nothing created. ccy-teams create starts one whenever you like.\n") || len(h.ops.calls) != 0 {
		t.Fatalf("stderr %q ops %v", h.errOut.String(), h.ops.calls)
	}
	h = newPickerHarness(t)
	h.onDeclare("declared", "")
	h.on("systemctl list-units", "", 0)
	h.script.gum = []string{"CANCEL"}
	if code := h.run(); code != term.ExitCancelled {
		t.Fatalf("exit %d", code)
	}
	if !strings.Contains(h.errOut.String(), "Cancelled; nothing was changed.\n") {
		t.Fatalf("stderr %q", h.errOut.String())
	}
}

func TestPickerMissingGumIsRefusedWhereItIsNeeded(t *testing.T) {
	h := newPickerHarness(t)
	h.onDeclare("declared", "")
	h.on("systemctl list-units", "", 0)
	h.app.LookPath = func(name string) (string, error) { return "", errors.New("not found: " + name) }
	if code := h.run(); code != term.ExitFailure {
		t.Fatalf("exit %d", code)
	}
	if !strings.Contains(h.errOut.String(), "ERROR: gum is not installed, and ccy-teams needs it here. playbooks/imports/play-claude-yolo.yml installs it; run that play and try again.\n") {
		t.Fatalf("stderr %q", h.errOut.String())
	}
}

func TestPickerWithoutFzfSaysWhatInstallsIt(t *testing.T) {
	h := newPickerHarness(t)
	h.app.LookPath = func(name string) (string, error) {
		if name == "fzf" {
			return "", errors.New("not found")
		}
		return "/usr/bin/" + name, nil
	}
	if code := h.run(); code != term.ExitFailure {
		t.Fatalf("exit %d", code)
	}
	if !strings.Contains(h.errOut.String(), "ERROR: fzf is not installed; playbooks/imports/play-claude-yolo.yml installs it.\n") {
		t.Fatalf("stderr %q", h.errOut.String())
	}
}

func TestPickerCancelledByInterruptIsExit130(t *testing.T) {
	h := newPickerHarness(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if code := h.app.runPicker(ctx); code != term.ExitCancelled {
		t.Fatalf("exit %d", code)
	}
	if !strings.Contains(h.errOut.String(), "Cancelled; nothing was changed.\n") {
		t.Fatalf("stderr %q", h.errOut.String())
	}
}

func TestPickerInterruptWhileChoosingIsExit130(t *testing.T) {
	h := newPickerHarness(t)
	ctx, cancel := context.WithCancel(context.Background())
	h.app.Picker = &ui.Picker{Runner: runnerFunc(func(context.Context, execx.Cmd) (execx.Result, error) {
		cancel()
		return execx.Result{ExitCode: 130}, &execx.ExitError{Name: "fzf", Code: 130}
	}), Stderr: h.errOut}
	if code := h.app.runPicker(ctx); code != term.ExitCancelled {
		t.Fatalf("exit %d", code)
	}
}

func TestPickerRowOfTellsIdenticalRowsApartByTheirTag(t *testing.T) {
	h := newPickerHarness(t)
	s, err := h.app.collect(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	s.Rows = append(s.Rows, s.Rows[1])
	row, ok := s.rowAt("8")
	if !ok || row.Member != s.Rows[1].Member {
		t.Fatalf("row %+v %v", row, ok)
	}
	if _, ok := s.rowAt("99"); ok {
		t.Fatal("an index past the rows is a changed list")
	}
	if _, ok := s.rowAt("x"); ok {
		t.Fatal("a tag that is not a number is a changed list")
	}
}

func TestPickerChangedRowRefreshesAfterBackToList(t *testing.T) {
	h := newPickerHarness(t)
	h.script.fzf = []string{"|owner", "|ABORT"}
	h.script.rewrite = func(out string) string { return strings.Replace(out, "\t5", "\t99", 1) }
	if code := h.run(); code != term.ExitOK {
		t.Fatalf("exit %d", code)
	}
	if !strings.Contains(h.errOut.String(), "That row has changed; the list has been refreshed.\n") ||
		!strings.Contains(h.errOut.String(), "Press Enter to go back to the list.\n") {
		t.Fatalf("stderr %q", h.errOut.String())
	}
	if len(h.script.rows) != 2 {
		t.Fatalf("the list was drawn %d times", len(h.script.rows))
	}
}

type runnerFunc func(context.Context, execx.Cmd) (execx.Result, error)

func (f runnerFunc) Run(ctx context.Context, c execx.Cmd) (execx.Result, error) { return f(ctx, c) }

func TestPickerCountsOnlyConsecutiveNoOps(t *testing.T) {
	h := newPickerHarness(t)
	// outside a project, Ctrl-N is refused each time
	h.script.fzf = []string{"ctrl-n|port 6167", "ctrl-n|port 6167", "ctrl-n|port 6167"}
	if code := h.run(); code != term.ExitFailure {
		t.Fatalf("exit %d", code)
	}
	if !strings.Contains(h.errOut.String(), "Nothing attached or started after 3 tries.\n") {
		t.Fatalf("stderr %q", h.errOut.String())
	}

	h = newPickerHarness(t)
	// a key that does something in between starts the count again
	h.script.fzf = []string{"ctrl-n|port 6167", "ctrl-n|port 6167", "?|port 6167", "ctrl-n|port 6167", "ctrl-n|port 6167", "|ABORT"}
	if code := h.run(); code != term.ExitOK {
		t.Fatalf("exit %d: %s", code, h.errOut.String())
	}
}

func TestPickerEnterOnAnythingElseSaysSoAndWaits(t *testing.T) {
	h := newPickerHarness(t)
	h.script.fzf = []string{"|proj.box+role.lxc", "|ABORT"}
	if code := h.run(); code != term.ExitOK {
		t.Fatalf("exit %d", code)
	}
	want := "Enter on proj.box+role.lxc does nothing: Ctrl-X parks a member. ? shows every key.\nPress Enter to go back to the list.\n"
	if !strings.Contains(h.errOut.String(), want) {
		t.Fatalf("stderr %q", h.errOut.String())
	}
}

func TestPickerStartupOfferUpdateRunsApplyAll(t *testing.T) {
	h := newPickerHarness(t)
	h.onDeclare("status", "STATUS\talpha\toutdated\tconfig\n")
	h.onDeclare("limited", "")
	h.script.gum = []string{"YES"}
	h.script.fzf = []string{"|ABORT"}
	if code := h.run(); code != term.ExitOK {
		t.Fatalf("exit %d: %s", code, h.errOut.String())
	}
	if !slices.Equal(h.ops.calls, []string{"apply --all"}) {
		t.Fatalf("ops %v", h.ops.calls)
	}
	if !strings.Contains(h.errOut.String(), "Not as declared: alpha (out of date: config).\n") {
		t.Fatalf("stderr %q", h.errOut.String())
	}
}

func TestPickerCtrlUUpdatesEverythingAndWaits(t *testing.T) {
	h := newPickerHarness(t)
	h.onDeclare("limited", "")
	h.script.fzf = []string{"ctrl-u|port 6167", "|ABORT"}
	if code := h.run(); code != term.ExitOK {
		t.Fatalf("exit %d", code)
	}
	if !slices.Equal(h.ops.calls, []string{"apply --all"}) || !strings.Contains(h.errOut.String(), "Press Enter to go back to the list.") {
		t.Fatalf("ops %v stderr %q", h.ops.calls, h.errOut.String())
	}
}

func TestPickerNamesTheVerbWhenAFlowFailsAndStopsWhenItIsCancelled(t *testing.T) {
	h := newPickerHarness(t)
	h.onDeclare("limited", "")
	h.ops.applyRC = 1
	h.script.fzf = []string{"ctrl-u|port 6167", "|ABORT"}
	if code := h.run(); code != term.ExitOK {
		t.Fatalf("exit %d", code)
	}
	if !strings.Contains(h.errOut.String(), "(ccy-teams apply --all failed, exit 1: see above.)\n") ||
		strings.Contains(h.errOut.String(), "cmd_apply") {
		t.Fatalf("stderr %q", h.errOut.String())
	}

	h = newPickerHarness(t)
	h.onDeclare("limited", "")
	h.ops.applyRC = 130
	h.script.fzf = []string{"ctrl-u|port 6167"}
	if code := h.run(); code != term.ExitCancelled {
		t.Fatalf("exit %d", code)
	}
}

func TestLeaveRoundTripsThroughExitCodeOf(t *testing.T) {
	if code, ok := exitCodeOf(leave(3)); !ok || code != 3 {
		t.Fatalf("%d %v", code, ok)
	}
	if _, ok := exitCodeOf(errReported); ok {
		t.Fatal("a reported error is not an exit request")
	}
	if leave(2).Error() == "" {
		t.Fatal("an exit request says what it is")
	}
}
