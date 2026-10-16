package lxcadd

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/ui"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/ui/pathinput"
)

// The directories the container shows, as the host-side reader lists them. home is the rig's
// user's home.
const (
	home = "/home/" + "agent"

	homeListing = `{"path": "` + home + `", "exists": true, "git": false, "truncated": false, "dirs": [` +
		`{"name": ".cache", "git": false}, {"name": "project", "git": true}, {"name": "src", "git": false}]}`
)

// paths is the checkout line as a test answers it. Each Ask takes the next answer; with none
// left it takes the line's starting value, as Enter on it would.
type paths struct {
	specs   []pathinput.Spec
	answers []func(pathinput.Spec) (string, error)
}

func (p *paths) Ask(_ context.Context, s pathinput.Spec) (string, error) {
	p.specs = append(p.specs, s)
	if len(p.answers) == 0 {
		if s.Value == "/" {
			return "", errors.New("the test gave the checkout line no answer")
		}
		return s.Value, nil
	}
	next := p.answers[0]
	p.answers = p.answers[1:]
	return next(s)
}

// give queues paths typed and taken, one per Ask.
func (p *paths) give(typed ...string) {
	for _, path := range typed {
		p.answers = append(p.answers, func(pathinput.Spec) (string, error) { return path, nil })
	}
}

// then queues an answer that does something with the line first, as Tab would.
func (p *paths) then(answer func(pathinput.Spec) (string, error)) {
	p.answers = append(p.answers, answer)
}

// checkoutWritten runs the flow and is the checkout it declared.
func (r *rig) checkoutWritten(t *testing.T) string {
	t.Helper()
	if code := r.run("alpha"); code != term.ExitOK {
		t.Fatalf("exit %d: %s", code, r.ErrOut.String())
	}
	_, checkout, found := strings.Cut(r.addLine(), "--checkout ")
	if !found {
		t.Fatalf("nothing was declared: %s", r.ErrOut.String())
	}
	return checkout
}

// listed runs the flow, reading the directory dir through the line's List as Tab would, and is
// what came back.
func listed(t *testing.T, r *rig, dir string) (pathinput.Listing, error) {
	t.Helper()
	var got pathinput.Listing
	var err error
	r.paths.then(func(s pathinput.Spec) (string, error) {
		got, err = s.List(context.Background(), dir)
		return "/srv/agent/one", nil
	})
	r.checkoutWritten(t)
	return got, err
}

func TestEveryQuestionShowsTheTeamTheStepTheContainerAndTheUser(t *testing.T) {
	r := newRig(t)
	r.happy()
	if code := r.run("alpha"); code != term.ExitOK {
		t.Fatalf("exit %d: %s", code, r.ErrOut.String())
	}
	r.Script.HeadingAt(1, "Teams › alpha › Add LXC member · step 2/4 User\ninto team alpha · container container-a")
	r.Script.HeadingAt(2, "Teams › alpha › Add LXC member · step 4/4 Role\ninto team alpha · container container-a · user agent · checkout /srv/agent/one")
	h := r.paths.specs[0].Heading
	if h.Trail() != "Teams › alpha › Add LXC member · step 3/4 Checkout" || h.Context != "into team alpha · container container-a · user agent" {
		t.Fatalf("the checkout line's heading %q / %q", h.Trail(), h.Context)
	}
	said := r.ErrOut.String()
	for _, want := range []string{"1/4  Container\n", "3/4  Checkout\n"} {
		if !strings.Contains(said, want) {
			t.Fatalf("the step heading lacks %q:\n%s", want, said)
		}
	}
}

func TestTheCheckoutLineStartsWithTheFirstFoundAndSuggestsThemAllCcyProjectsFirst(t *testing.T) {
	r := newRig(t)
	r.OnDeclare("lxc-checkouts", "CHECKOUT\t/srv/agent/two\texample-org/two\tccy\nCHECKOUT\t/var/www/one\texample-org/one\t-\n")
	r.happy()
	if got := r.checkoutWritten(t); got != "/srv/agent/two" {
		t.Fatalf("declared %q", got)
	}
	s := r.paths.specs[0]
	want := []pathinput.Suggestion{
		{Path: "/srv/agent/two", Note: "example-org/two, a ccy project"},
		{Path: "/var/www/one", Note: "example-org/one"},
	}
	if s.Value != "/srv/agent/two" || !slices.Equal(s.Suggestions, want) || s.Prompt != "Checkout in container-a: " {
		t.Fatalf("spec %+v", s)
	}
}

func TestWithNothingFoundTheLineStartsAtTheRootAndSaysWhereItLooked(t *testing.T) {
	r := newRig(t)
	r.OnDeclare("lxc-checkouts", "")
	r.happy()
	r.paths.give("/srv/agent/one")
	r.checkoutWritten(t)
	if s := r.paths.specs[0]; s.Value != "/" || len(s.Suggestions) != 0 {
		t.Fatalf("spec %+v", s)
	}
	if !strings.Contains(r.ErrOut.String(), "No checkout with a GitHub remote was found in container-a (the home of agent, /var/www, /srv, /opt)") {
		t.Fatalf("stderr %s", r.ErrOut.String())
	}
}

func TestTabReadsTheContainersDirectoriesFromTheHostSide(t *testing.T) {
	r := newRig(t)
	r.lxc.dirs = map[string]string{home: homeListing}
	r.happy()
	got, err := listed(t, r, home+"/")
	want := []pathinput.Entry{{Name: ".cache"}, {Name: "project", Note: "a git checkout"}, {Name: "src"}}
	if err != nil || !slices.Equal(got.Entries, want) || got.Note != "" {
		t.Fatalf("listing %+v %v", got, err)
	}
	if !slices.Contains(r.lxc.reads, "dirs container-a agent "+home) {
		t.Fatalf("reads %v", r.lxc.reads)
	}
}

func TestTabAtTheRootAsksForTheRoot(t *testing.T) {
	r := newRig(t)
	r.lxc.dirs = map[string]string{"/": `{"path": "/", "exists": true, "git": false, "truncated": false, "dirs": [{"name": "srv", "git": false}]}`}
	r.happy()
	if got, err := listed(t, r, "/"); err != nil || len(got.Entries) != 1 {
		t.Fatalf("listing %+v %v", got, err)
	}
	if !slices.Contains(r.lxc.reads, "dirs container-a agent /") {
		t.Fatalf("reads %v", r.lxc.reads)
	}
}

func TestADirectoryThatDoesNotExistIsSaid(t *testing.T) {
	r := newRig(t)
	r.happy()
	if _, err := listed(t, r, "/nope/"); err == nil || err.Error() != "/nope/ does not exist in container-a" {
		t.Fatalf("listing error %v", err)
	}
}

func TestAListingCutAtItsLimitSaysSo(t *testing.T) {
	r := newRig(t)
	r.lxc.dirs = map[string]string{home: strings.Replace(homeListing, `"truncated": false`, `"truncated": true`, 1)}
	r.happy()
	if got, err := listed(t, r, home+"/"); err != nil || !strings.Contains(got.Note, "Only the first") {
		t.Fatalf("listing %+v %v", got, err)
	}
}

func TestANameThatCouldSteerTheTerminalIsNotListed(t *testing.T) {
	r := newRig(t)
	r.lxc.dirs = map[string]string{home: `{"path": "` + home + `", "exists": true, "git": false, "truncated": false, "dirs": [` +
		`{"name": "red\u001b[31m", "git": false}, {"name": "plain", "git": false}]}`}
	r.happy()
	got, err := listed(t, r, home+"/")
	if err != nil || !slices.Equal(got.Entries, []pathinput.Entry{{Name: "plain"}}) || !strings.Contains(got.Note, "1 name that cannot be shown") {
		t.Fatalf("listing %+v %v", got, err)
	}
}

func TestAListingThatIsNotWhatTheReaderPrintsIsAnError(t *testing.T) {
	for _, bad := range []string{
		`{`, `{"path": "` + home + `", "exists": true}`,
		`{"path": "` + home + `", "exists": true, "git": false, "truncated": false, "dirs": [{"name": "a/b", "git": false}]}`,
		`{"path": "` + home + `", "exists": true, "git": false, "truncated": false, "dirs": [{"name": "..", "git": false}]}`,
	} {
		r := newRig(t)
		r.lxc.dirs = map[string]string{home: bad}
		r.happy()
		if _, err := listed(t, r, home+"/"); err == nil {
			t.Fatalf("%s: no error", bad)
		}
	}
}

func TestAListingThatFailsOrTakesTooLongIsAnErrorSayingSo(t *testing.T) {
	r := newRig(t)
	r.lxc.dirsErr = map[string]error{home: errors.New(home + "/src is a symbolic link")}
	r.happy()
	if _, err := listed(t, r, home+"/"); err == nil || !strings.Contains(err.Error(), "is a symbolic link") {
		t.Fatalf("listing error %v", err)
	}
	slow := newRig(t)
	slow.lxc.hangDirs = true
	slow.a.ReadTimeout = 1
	slow.happy()
	if _, err := listed(t, slow, home+"/"); err == nil || !strings.Contains(err.Error(), "took longer than") {
		t.Fatalf("listing error %v", err)
	}
}

func TestEscOrCtrlCOnTheLineCancelsWithNothingWritten(t *testing.T) {
	for _, left := range []error{ui.ErrLeave, ui.ErrCancelled} {
		r := newRig(t)
		r.answers(choose("container-a"), choose("agent"))
		r.paths.then(func(pathinput.Spec) (string, error) { return "", left })
		if code := r.run("alpha"); code != term.ExitCancelled || r.addLine() != "" {
			t.Fatalf("%v: exit %d, add %q", left, code, r.addLine())
		}
	}
}

func TestALineThatCannotRunEndsTheRunSayingWhy(t *testing.T) {
	r := newRig(t)
	r.answers(choose("container-a"), choose("agent"))
	r.paths.then(func(pathinput.Spec) (string, error) { return "", errors.New("could not open a terminal") })
	if code := r.run("alpha"); code != term.ExitFailure || !strings.Contains(r.ErrOut.String(), "could not open a terminal") {
		t.Fatalf("exit %d: %s", code, r.ErrOut.String())
	}
}

func TestAHomeSearchPastItsTimeIsSaidAndThePathIsTyped(t *testing.T) {
	r := newRig(t)
	r.lxc.hang = true
	r.a.ReadTimeout = 1
	r.happy()
	r.paths.give("/srv/agent/one")
	if got := r.checkoutWritten(t); got != "/srv/agent/one" {
		t.Fatalf("declared %q", got)
	}
	if !strings.Contains(r.ErrOut.String(), "took too long and was stopped") {
		t.Fatalf("stderr %s", r.ErrOut.String())
	}
}

func TestASearchStoppedAtALimitSaysItMayHaveMissedSome(t *testing.T) {
	r := newRig(t)
	r.OnDeclare("lxc-checkouts", "CHECKOUT\t/srv/agent/one\texample-org/one\t-\nPARTIAL\n")
	r.happy()
	if got := r.checkoutWritten(t); got != "/srv/agent/one" {
		t.Fatalf("declared %q", got)
	}
	if !strings.Contains(r.ErrOut.String(), "may have missed some") {
		t.Fatalf("stderr %s", r.ErrOut.String())
	}
}
