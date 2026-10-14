package sessions

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/registry"
)

// pickerRig scripts fzf from a queue of "<key>|<choice>" answers, as the bash suite's fake does:
// <choice> is a session name (the row starting with it), YES, EXIT or ABORT. Every call's argv
// and rows are kept.
type pickerRig struct {
	h       *harness
	answers []string
	argv    []string
	shown   []string
	n       int
}

func newPickerRig(h *harness, answers ...string) *pickerRig {
	r := &pickerRig{h: h, answers: answers}
	h.fake.OnFunc([]string{"fzf"}, r.answer)
	h.tty = true
	return r
}

func (r *pickerRig) answer(c execx.Cmd) (execx.Result, error) {
	rows, err := io.ReadAll(c.Stdin)
	if err != nil {
		return execx.Result{}, err
	}
	r.argv = append(r.argv, strings.Join(c.Args, " "))
	r.shown = append(r.shown, string(rows))
	if r.n >= len(r.answers) {
		return execx.Result{ExitCode: 2, Stderr: []byte("no answer queued")}, nil
	}
	key, choice, found := strings.Cut(r.answers[r.n], "|")
	if !found {
		key, choice = "", key
	}
	r.n++
	var row string
	switch choice {
	case "ABORT":
		return execx.Result{ExitCode: 130}, nil
	case "EXIT":
		row = "Exit"
	case "YES":
		row = firstLineWithPrefix(string(rows), "Yes, ")
	default:
		row = firstLineWithPrefix(string(rows), choice+" ")
	}
	out := row + "\n"
	if strings.Contains(strings.Join(c.Args, " "), "--expect") {
		out = key + "\n" + out
	}
	return execx.Result{Stdout: []byte(out)}, nil
}

func firstLineWithPrefix(text, prefix string) string {
	for line := range strings.SplitSeq(text, "\n") {
		if strings.HasPrefix(line, prefix) {
			return line
		}
	}
	return ""
}

// server is a scripted tmux server: sessions with clients, the single-attach hook, and what the
// last attach did.
type server struct {
	h       *harness
	clients map[string][]string // session -> "tty activity" lines
	// onAttach lets a test change the world as attach-session runs.
	onAttach func(name string)
	attached []string
}

// target is the session a tmux command names: "=name" is its fifth word, after -L ccy <sub> -t.
func target(c execx.Cmd) string { return strings.TrimPrefix(c.Args[4], "=") }

func newServer(h *harness) *server {
	s := &server{h: h, clients: map[string][]string{}}
	tmux := func(sub string, fn func(execx.Cmd) execx.Result) {
		h.fake.OnFunc([]string{"tmux", "-L", "ccy", sub}, func(c execx.Cmd) (execx.Result, error) { return fn(c), nil })
	}
	tmux("list-clients", func(c execx.Cmd) execx.Result {
		lines, ok := s.clients[target(c)]
		if !ok {
			return execx.Result{ExitCode: 1, Stderr: []byte("can't find session: " + target(c))}
		}
		return execx.Result{Stdout: []byte(strings.Join(lines, "\n") + "\n")}
	})
	tmux("detach-client", func(c execx.Cmd) execx.Result {
		s.clients[target(c)] = nil
		return execx.Result{}
	})
	tmux("set-hook", func(execx.Cmd) execx.Result { return execx.Result{} })
	tmux("attach-session", func(c execx.Cmd) execx.Result {
		s.attached = append(s.attached, target(c))
		if s.onAttach != nil {
			s.onAttach(target(c))
		}
		return execx.Result{}
	})
	tmux("kill-session", func(c execx.Cmd) execx.Result {
		if _, ok := s.clients[target(c)]; !ok {
			return execx.Result{ExitCode: 1, Stderr: []byte("can't find session: " + target(c))}
		}
		delete(s.clients, target(c))
		return execx.Result{}
	})
	h.tmuxOut("list-panes", "")
	return s
}

// listSessions publishes the sessions: "name:tty" is attached to tty, idle three hours.
func (s *server) listSessions(dir string, specs ...string) {
	lines := make([]string, 0, len(specs))
	s.clients = map[string][]string{}
	for _, spec := range specs {
		name, tty, attached := strings.Cut(spec, ":")
		count := 0
		s.clients[name] = nil
		if attached {
			count = 1
			s.clients[name] = []string{tty + " " + strconv.FormatInt(s.h.clock.Unix()-3*3600-120, 10)}
		}
		lines = append(lines, fmt.Sprintf("%s %d %s", name, count, dir))
	}
	s.h.sessions(lines...)
}

func (s *server) did(prefix string) int { return s.h.count("tmux -L ccy " + prefix) }

// inProject makes git say the working directory is a work tree.
func inProject(h *harness) {
	h.fake.On([]string{"git", "rev-parse"}, execx.Result{Stdout: []byte("true\n")})
}

func notInProject(h *harness) {
	h.fake.On([]string{"git", "rev-parse"}, execx.Result{ExitCode: 128, Stderr: []byte("fatal: not a git repository")})
}

func TestThePickerNeedsATerminalAndSaysWhyOnStderr(t *testing.T) {
	h := newHarness(t)
	exitCodeIs(t, h.run(), 1, h)
	if h.stderr() != "ccy-sessions needs a terminal: it attaches sessions to the one you are in. ccy-sessions --list prints the table instead.\n" || h.stdout() != "" {
		t.Fatalf("stdout %q stderr %q", h.stdout(), h.stderr())
	}
	if len(h.ran()) != 0 {
		t.Fatalf("nothing runs: %v", h.ran())
	}
	exitCodeIs(t, h.run(""), 1, h) // an empty first word is the picker too
}

func TestThePickerOffersCtrlTAndListensForTheKeys(t *testing.T) {
	h := newHarness(t)
	s := newServer(h)
	s.listSessions("/srv/a", "ccy-a:/dev/pts/3")
	notInProject(h)
	rig := newPickerRig(h, "|ABORT")
	exitCodeIs(t, h.run(), 0, h)
	if len(rig.argv) != 1 {
		t.Fatalf("%v", rig.argv)
	}
	contains(t, "argv", rig.argv[0], "--expect=ctrl-t,ctrl-x,ctrl-n", "--border-label= CCY sessions ",
		"Enter attach   Ctrl-T take over   Ctrl-X end   Ctrl-N new   Esc or q quit",
		"Ctrl-N unavailable: ~/proj is not a project folder (no git repository)",
		`"open elsewhere" is attached in another terminal: Ctrl-T detaches that terminal and attaches this one; the session keeps running`,
		`CPU: since the list last redrew ("-" until it has), 100% is one core`,
		`network, token, SSH key: what its container is on and was started with — "no container" and "-" mean nothing to ask`)
}

// A redraw must not wait out a CPU sampling interval, so the picker measures against its last draw.
func TestThePickerKeepsItsCPUSamplesBetweenRedraws(t *testing.T) {
	h := newHarness(t)
	s := newServer(h)
	s.listSessions("/srv/a", "ccy-a")
	notInProject(h)
	newPickerRig(h, "|ABORT")
	if h.app.Probe.KeepSamples {
		t.Fatal("a one-off listing takes two samples")
	}
	exitCodeIs(t, h.run(), 0, h)
	if !h.app.Probe.KeepSamples {
		t.Fatal("the picker redraws, so it must not sample twice per redraw")
	}
}

func TestThePickerHeaderOffersCtrlNInAProject(t *testing.T) {
	h := newHarness(t)
	s := newServer(h)
	s.listSessions("/srv/a", "ccy-a")
	inProject(h)
	rig := newPickerRig(h, "|ABORT")
	exitCodeIs(t, h.run(), 0, h)
	contains(t, "header", rig.argv[0], "Ctrl-N starts a new ccy session in ~/proj")
}

func TestEnterAttachesADetachedSessionAndExitsWhenTheUserLeavesIt(t *testing.T) {
	h := newHarness(t)
	s := newServer(h)
	s.listSessions("/srv/a", "ccy-a", "ccy-b")
	notInProject(h)
	newPickerRig(h, "|ccy-b")
	exitCodeIs(t, h.run(), 0, h)
	if len(s.attached) != 1 || s.attached[0] != "ccy-b" || s.did("set-hook") != 1 {
		t.Fatalf("attached %v, ran %v", s.attached, h.ran())
	}
	contains(t, "attach", h.stderr(), "Attaching to 'ccy-b'.", "Detached. 'ccy-b' keeps running")
}

func TestEnterOnASessionThatEndedWhileAttachedExitsZero(t *testing.T) {
	h := newHarness(t)
	s := newServer(h)
	s.listSessions("/srv/a", "ccy-a")
	notInProject(h)
	newPickerRig(h, "|ccy-a")
	s.onAttach = func(name string) { delete(s.clients, name) }
	exitCodeIs(t, h.run(), 0, h)
	contains(t, "ended", h.stderr(), "'ccy-a' has ended.")
}

func TestEnterOnAnOpenElsewhereRowPointsAtCtrlTAndCountsATry(t *testing.T) {
	h := newHarness(t)
	s := newServer(h)
	s.listSessions("/srv/a", "ccy-a:/dev/pts/3")
	notInProject(h)
	rig := newPickerRig(h, "|ccy-a", "|ccy-a", "|ccy-a")
	exitCodeIs(t, h.run(), 1, h)
	contains(t, "refusal", h.stderr(), "'ccy-a' is open in another terminal. Ctrl-T takes it over here; Ctrl-X ends it.",
		"Nothing attached after 3 tries. Nothing changed.")
	if len(s.attached) != 0 || rig.n != 3 {
		t.Fatalf("attached %v after %d picks", s.attached, rig.n)
	}
}

func TestARowIsOpenElsewhereByItsDataNotByItsText(t *testing.T) {
	// Spec bug 9.9: a session in a directory named for the phrase is still detached.
	h := newHarness(t)
	s := newServer(h)
	s.listSessions("/srv/open elsewhere", "ccy-open")
	notInProject(h)
	newPickerRig(h, "|ccy-open")
	exitCodeIs(t, h.run(), 0, h)
	if len(s.attached) != 1 {
		t.Fatalf("attached %v", s.attached)
	}
}

func TestCtrlTTakesOverAnOpenSessionAfterAYes(t *testing.T) {
	h := newHarness(t)
	s := newServer(h)
	s.listSessions("/srv/a", "ccy-a:/dev/pts/3", "ccy-b")
	notInProject(h)
	rig := newPickerRig(h, "ctrl-t|ccy-a", "|YES")
	exitCodeIs(t, h.run(), 0, h)
	if h.count("tmux -L ccy detach-client -s =ccy-a") != 1 || len(s.attached) != 1 || s.attached[0] != "ccy-a" {
		t.Fatalf("%v", h.ran())
	}
	contains(t, "question", rig.argv[1], "It is open on /dev/pts/3 (idle 3 h). That terminal is detached, not closed, and the session keeps running.",
		"--border-label= Take over ccy-a? ", "--bind=start:last")
	contains(t, "yes row", rig.shown[1], "Yes, take over 'ccy-a'")
	if h.count("tmux -L ccy kill") != 0 {
		t.Fatal("no session is killed")
	}
}

func TestCtrlTDeclinedChangesNothingAndCostsNoTry(t *testing.T) {
	h := newHarness(t)
	s := newServer(h)
	s.listSessions("/srv/a", "ccy-a:/dev/pts/3")
	notInProject(h)
	rig := newPickerRig(h, "ctrl-t|ccy-a", "|EXIT", "ctrl-t|ccy-a", "|EXIT", "ctrl-t|ccy-a", "|EXIT", "ctrl-t|ccy-a", "|EXIT", "|ABORT")
	exitCodeIs(t, h.run(), 0, h)
	if rig.n != 9 || s.did("detach-client") != 0 || len(s.attached) != 0 {
		t.Fatalf("picks %d, ran %v", rig.n, h.ran())
	}
	contains(t, "declined", h.stderr(), "Left 'ccy-a' where it is.")
}

func TestCtrlTOnADetachedSessionJustAttachesIt(t *testing.T) {
	h := newHarness(t)
	s := newServer(h)
	s.listSessions("/srv/a", "ccy-b")
	notInProject(h)
	rig := newPickerRig(h, "ctrl-t|ccy-b")
	exitCodeIs(t, h.run(), 0, h)
	if rig.n != 1 || s.did("detach-client") != 0 || len(s.attached) != 1 {
		t.Fatalf("picks %d ran %v", rig.n, h.ran())
	}
}

func TestCtrlTOnAClientTheRowDoesNotCountStillTakesItOver(t *testing.T) {
	h := newHarness(t)
	s := newServer(h)
	s.listSessions("/srv/a", "ccy-c")
	s.clients["ccy-c"] = []string{fmt.Sprintf("/dev/pts/4 %d", h.clock.Unix()-600)}
	notInProject(h)
	rig := newPickerRig(h, "ctrl-t|ccy-c", "|YES")
	exitCodeIs(t, h.run(), 0, h)
	contains(t, "question", rig.argv[1], "/dev/pts/4 (idle 10 min)")
	if s.did("detach-client") != 1 || len(s.attached) != 1 {
		t.Fatalf("%v", h.ran())
	}
}

func TestCtrlTOnASessionThatEndedMeanwhileRefreshesTheList(t *testing.T) {
	h := newHarness(t)
	s := newServer(h)
	s.listSessions("/srv/a", "ccy-a:/dev/pts/3")
	notInProject(h)
	newPickerRig(h, "ctrl-t|ccy-a", "|ABORT")
	h.fake.OnFunc([]string{"tmux", "-L", "ccy", "list-clients"}, func(execx.Cmd) (execx.Result, error) {
		return execx.Result{ExitCode: 1, Stderr: []byte("can't find session: ccy-a")}, nil
	})
	exitCodeIs(t, h.run(), 0, h)
	contains(t, "gone", h.stderr(), "'ccy-a' has ended — the list has been refreshed.")
}

func TestCtrlTStopsOnAnyOtherFailureReadingTheTerminals(t *testing.T) {
	h := newHarness(t)
	s := newServer(h)
	s.listSessions("/srv/a", "ccy-a:/dev/pts/3")
	notInProject(h)
	newPickerRig(h, "ctrl-t|ccy-a")
	h.fake.On([]string{"tmux", "-L", "ccy", "list-clients"}, execx.Result{ExitCode: 1, Stderr: []byte("protocol version mismatch")})
	exitCodeIs(t, h.run(), 1, h)
	contains(t, "error", h.stderr(), "could not list the terminals on 'ccy-a'")
}

func TestThreeTakeOversThatDidNotAttachNameThemInTheLastWord(t *testing.T) {
	h := newHarness(t)
	s := newServer(h)
	s.listSessions("/srv/a", "ccy-a:/dev/pts/3", "ccy-b:/dev/pts/4", "ccy-c:/dev/pts/5", "ccy-d:/dev/pts/6")
	notInProject(h)
	// The session ends as it is detached: its other terminal was told to leave, and nothing was attached.
	h.fake.OnFunc([]string{"tmux", "-L", "ccy", "detach-client"}, func(c execx.Cmd) (execx.Result, error) {
		delete(s.clients, target(c))
		return execx.Result{}, nil
	})
	newPickerRig(h, "ctrl-t|ccy-a", "|YES", "ctrl-t|ccy-b", "|YES", "ctrl-t|ccy-c", "|YES")
	exitCodeIs(t, h.run(), 1, h)
	contains(t, "last word", h.stderr(), "Nothing attached after 3 tries. A take-over was tried on ccy-a ccy-b ccy-c; what each one did is above.",
		"The list has been refreshed.")
	lacks(t, "last word", h.stderr(), "Nothing changed")
	if s.did("detach-client") != 3 {
		t.Fatalf("three terminals were detached: %v", h.ran())
	}
}

func TestACtrlTWhoseAttachWasKickedBackEndsThePicker(t *testing.T) {
	h := newHarness(t)
	s := newServer(h)
	s.listSessions("/srv/a", "ccy-a:/dev/pts/3")
	notInProject(h)
	s.onAttach = func(name string) { s.clients[name] = []string{"/dev/pts/9 1"} }
	newPickerRig(h, "ctrl-t|ccy-a", "|YES")
	exitCodeIs(t, h.run(), 1, h)
	contains(t, "kicked", h.stderr(), "was not attached")
}

func TestACtrlTWhoseDetachIsRefusedAndTheOtherTerminalStaysEndsThePicker(t *testing.T) {
	h := newHarness(t)
	s := newServer(h)
	s.listSessions("/srv/a", "ccy-a:/dev/pts/3")
	notInProject(h)
	h.fake.OnFunc([]string{"tmux", "-L", "ccy", "detach-client"}, func(execx.Cmd) (execx.Result, error) {
		return execx.Result{ExitCode: 1, Stderr: []byte("server exited unexpectedly")}, nil
	})
	newPickerRig(h, "ctrl-t|ccy-a", "|YES")
	exitCodeIs(t, h.run(), 1, h)
	contains(t, "refused", h.stderr(), "could not detach 'ccy-a' from /dev/pts/3 (idle 3 h): server exited unexpectedly")
	if len(s.attached) != 0 {
		t.Fatal("nothing attached")
	}
}

func TestACtrlTWhoseSessionIsTakenAgainBeforeTheAttachRefreshesTheList(t *testing.T) {
	h := newHarness(t)
	s := newServer(h)
	s.listSessions("/srv/a", "ccy-a:/dev/pts/3")
	notInProject(h)
	calls := 0
	h.fake.OnFunc([]string{"tmux", "-L", "ccy", "list-clients"}, func(execx.Cmd) (execx.Result, error) {
		calls++
		// Two looks find the old terminal (the question's, the take-over's); once detached it is
		// gone; the attach's own look finds a third terminal that got there first.
		switch calls {
		case 1, 2:
			return execx.Result{Stdout: []byte("/dev/pts/3 1\n")}, nil
		case 3:
			return execx.Result{}, nil
		}
		return execx.Result{Stdout: []byte("/dev/pts/9 1\n")}, nil
	})
	newPickerRig(h, "ctrl-t|ccy-a", "|YES", "|ABORT")
	exitCodeIs(t, h.run(), 0, h)
	contains(t, "taken again", h.stderr(), "is open in another terminal, or gone", "The list has been refreshed.")
}

func TestCtrlXEndsASessionAndItsRecordAfterAYesAndCostsNoTry(t *testing.T) {
	h := newHarness(t)
	s := newServer(h)
	s.listSessions("/srv/a", "ccy-a", "ccy-b")
	notInProject(h)
	a := realDir(t, h, "a")
	file := record(t, h, registry.Record{Name: "ccy-a", Dir: a, Launcher: "/l", Prefix: "ccy", Restore: true})
	// Three declined Ctrl-X, one confirmed, and then Esc: none of them used a try.
	rig := newPickerRig(h, "ctrl-x|ccy-b", "|EXIT", "ctrl-x|ccy-b", "|EXIT", "ctrl-x|ccy-b", "|EXIT", "ctrl-x|ccy-a", "|YES", "|ABORT")
	exitCodeIs(t, h.run(), 0, h)
	if rig.n != 9 || h.count("tmux -L ccy kill-session -t =ccy-a") != 1 || h.count("tmux -L ccy kill-session -t =ccy-b") != 0 {
		t.Fatalf("picks %d ran %v", rig.n, h.ran())
	}
	if _, err := os.Stat(file); err == nil {
		t.Fatal("the record goes with the session")
	}
	contains(t, "ended", h.stderr(), "Ended 'ccy-a'.", "Kept 'ccy-b'.")
	contains(t, "question", rig.argv[7], "Everything running in it will be killed.", "--border-label= End ccy-a? ")
}

func TestCtrlXOnASessionThatAlreadyEndedRemovesItsRecordAndGoesOn(t *testing.T) {
	// Spec bug 9.5: a session gone since the list was drawn is not a reason to abort the picker.
	h := newHarness(t)
	s := newServer(h)
	s.listSessions("/srv/a", "ccy-a")
	notInProject(h)
	a := realDir(t, h, "a")
	file := record(t, h, registry.Record{Name: "ccy-a", Dir: a, Launcher: "/l", Prefix: "ccy", Restore: true})
	delete(s.clients, "ccy-a") // gone for kill-session, still listed
	newPickerRig(h, "ctrl-x|ccy-a", "|YES", "|ABORT")
	exitCodeIs(t, h.run(), 0, h)
	contains(t, "gone", h.stderr(), "'ccy-a' had already ended.")
	if _, err := os.Stat(file); err == nil {
		t.Fatal("its record is removed")
	}
}

func TestCtrlXStopsWhenTheSessionCannotBeEndedOrItsRecordRemoved(t *testing.T) {
	h := newHarness(t)
	s := newServer(h)
	s.listSessions("/srv/a", "ccy-a")
	notInProject(h)
	h.fake.On([]string{"tmux", "-L", "ccy", "kill-session"}, execx.Result{ExitCode: 1, Stderr: []byte("protocol error")})
	newPickerRig(h, "ctrl-x|ccy-a", "|YES")
	exitCodeIs(t, h.run(), 1, h)
	contains(t, "kill", h.stderr(), "could not end 'ccy-a'")

	h = newHarness(t)
	s = newServer(h)
	s.listSessions("/srv/a", "ccy-a")
	notInProject(h)
	newPickerRig(h, "ctrl-x|ccy-a", "|YES")
	regdir, err := registry.Dir(h.app.Getenv)
	if err != nil {
		t.Fatal(err)
	}
	// A directory with something in it where the record should be makes the removal fail.
	if err = os.MkdirAll(filepath.Join(regdir, "ccy-a", "x"), 0o700); err != nil {
		t.Fatal(err)
	}
	exitCodeIs(t, h.run(), 1, h)
}

func TestCtrlNInAProjectStartsCcyHereByReplacingTheProcess(t *testing.T) {
	h := newHarness(t)
	s := newServer(h)
	s.listSessions("/srv/a", "ccy-a")
	inProject(h)
	newPickerRig(h, "ctrl-n|ccy-a")
	exitCodeIs(t, h.run(), 0, h)
	execs := h.fake.Execs()
	if len(execs) != 1 || execs[0].Name != "/var/local/claude-yolo/claude-yolo" || len(execs[0].Args) != 0 {
		t.Fatalf("execs %+v", execs)
	}
	contains(t, "starting", h.stderr(), "Starting ccy in ~/proj.")
}

func TestCtrlNThatCannotStartTheLauncherFails(t *testing.T) {
	h := newHarness(t)
	s := newServer(h)
	s.listSessions("/srv/a", "ccy-a")
	inProject(h)
	newPickerRig(h, "ctrl-n|ccy-a")
	h.fake.ExecError(&execx.NotInstalledError{Name: "claude-yolo"})
	exitCodeIs(t, h.run(), 1, h)
	contains(t, "exec", h.stderr(), "claude-yolo is not installed")
}

func TestCtrlNOutsideAProjectSaysSoAndCountsATry(t *testing.T) {
	h := newHarness(t)
	s := newServer(h)
	s.listSessions("/srv/a", "ccy-a")
	notInProject(h)
	newPickerRig(h, "ctrl-n|ccy-a", "ctrl-n|ccy-a", "ctrl-n|ccy-a")
	exitCodeIs(t, h.run(), 1, h)
	contains(t, "outside", h.stderr(), "~/proj is not a project folder (no git repository), so ccy cannot be started here.",
		"Nothing attached after 3 tries. Nothing changed.")
	if len(h.fake.Execs()) != 0 {
		t.Fatal("nothing started")
	}
}

func TestWithNoSessionsInAProjectItOffersToStartCcy(t *testing.T) {
	h := newHarness(t)
	s := newServer(h)
	s.listSessions("/srv/a")
	inProject(h)
	rig := newPickerRig(h, "|YES")
	exitCodeIs(t, h.run(), 0, h)
	contains(t, "offer", rig.argv[0], "No sessions are running. Start ccy in ~/proj?", "--border-label= CCY sessions ")
	contains(t, "yes row", rig.shown[0], "Yes, start ccy here")
	if len(h.fake.Execs()) != 1 {
		t.Fatalf("execs %v", h.fake.Execs())
	}

	h = newHarness(t)
	s = newServer(h)
	s.listSessions("/srv/a")
	inProject(h)
	newPickerRig(h, "|EXIT")
	exitCodeIs(t, h.run(), 0, h)
	if len(h.fake.Execs()) != 0 {
		t.Fatal("declined")
	}
}

func TestWithNoSessionsOutsideAProjectItSaysHowToStartOne(t *testing.T) {
	h := newHarness(t)
	s := newServer(h)
	s.listSessions("/srv/a")
	notInProject(h)
	h.tty = true
	exitCodeIs(t, h.run(), 0, h)
	contains(t, "none", h.stderr(), "No CCY or cc sessions are running. Start one with ccy or cc in a project directory.")
}

func TestThePickerStopsWhenTheListCannotBeRead(t *testing.T) {
	h := newHarness(t)
	h.tmuxFail("list-sessions", "lost server")
	h.tmuxOut("list-panes", "")
	h.tty = true
	exitCodeIs(t, h.run(), 1, h)
}

func TestAnFzfThatFailsOrIsMissingIsAFailureNotAnEsc(t *testing.T) {
	// Spec bugs 9.1 and 9.2.
	h := newHarness(t)
	s := newServer(h)
	s.listSessions("/srv/a", "ccy-a")
	notInProject(h)
	newPickerRig(h)
	exitCodeIs(t, h.run(), 1, h)
	contains(t, "crash", h.stderr(), "fzf")

	h = newHarness(t)
	s = newServer(h)
	s.listSessions("/srv/a", "ccy-a")
	notInProject(h)
	h.tty = true
	h.fake.OnError([]string{"fzf"}, &execx.NotInstalledError{Name: "fzf"})
	exitCodeIs(t, h.run(), 1, h)
	contains(t, "missing", h.stderr(), "ERROR: fzf is not installed; playbooks/imports/play-claude-yolo.yml installs it.")
}

func TestAConfirmThatFailsStopsThePicker(t *testing.T) {
	h := newHarness(t)
	s := newServer(h)
	s.listSessions("/srv/a", "ccy-a")
	notInProject(h)
	newPickerRig(h, "ctrl-x|ccy-a") // no answer queued for the confirm: fzf exits 2
	exitCodeIs(t, h.run(), 1, h)
}

func TestAnAttachThatFailsToStartIsReportedAsThatNotAsATakenSession(t *testing.T) {
	// Spec bugs 9.4 and 9.6: a tmux failure is reported as itself.
	h := newHarness(t)
	s := newServer(h)
	s.listSessions("/srv/a", "ccy-a")
	notInProject(h)
	h.fake.On([]string{"tmux", "-L", "ccy", "set-hook"}, execx.Result{ExitCode: 1, Stderr: []byte("no server running")})
	newPickerRig(h, "|ccy-a", "|ABORT")
	exitCodeIs(t, h.run(), 0, h)
	contains(t, "hook", h.stderr(), "no server running", "The list has been refreshed.")
	lacks(t, "hook", h.stderr(), "was taken or ended just now")
}

func TestAnAttachRefusedBecauseSomeoneGotThereFirstSaysSo(t *testing.T) {
	h := newHarness(t)
	s := newServer(h)
	s.listSessions("/srv/a", "ccy-a")
	notInProject(h)
	calls := 0
	h.fake.OnFunc([]string{"tmux", "-L", "ccy", "list-clients"}, func(execx.Cmd) (execx.Result, error) {
		calls++
		if calls == 1 {
			return execx.Result{Stdout: []byte("/dev/pts/9 1\n")}, nil
		}
		return execx.Result{}, nil
	})
	newPickerRig(h, "|ccy-a", "|ABORT")
	exitCodeIs(t, h.run(), 0, h)
	contains(t, "refused", h.stderr(), "ERROR: 'ccy-a' is open in another terminal, or gone. It can be attached from one terminal only.",
		"'ccy-a' was taken or ended just now — the list has been refreshed.")
}

func TestAnAttachKickedByTheHookIsATry(t *testing.T) {
	h := newHarness(t)
	s := newServer(h)
	s.listSessions("/srv/a", "ccy-a")
	notInProject(h)
	s.onAttach = func(name string) { s.clients[name] = []string{"/dev/pts/9 1"} }
	newPickerRig(h, "|ccy-a", "|ABORT")
	exitCodeIs(t, h.run(), 0, h)
	contains(t, "kicked", h.stderr(), "ERROR: 'ccy-a' is open in another terminal, so this terminal was not attached to it.")
	lacks(t, "kicked", h.stderr(), "was taken or ended just now")
}
