package seatadd

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/placeissued"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/status"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/system"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

// issuedAnswer stands for member add's answer, which carries the seat's password, so no stream
// may show it; answerMark is the part a stream is searched for.
const (
	answerMark   = "ISSUED-ANSWER-MARK"
	issuedAnswer = `{"issued":"` + answerMark + `"}`
)

type fakeSudo struct {
	readyErr   error
	res        execx.Result
	captureErr error
	readied    bool
	argv       []string
}

func (s *fakeSudo) Ready(context.Context, string) error {
	s.readied = true
	return s.readyErr
}

func (s *fakeSudo) Capture(_ context.Context, argv ...string) (execx.Result, error) {
	s.argv = argv
	return s.res, s.captureErr
}

type world struct {
	t        *testing.T
	checkout string
	env      map[string]string
	record   status.Record
	found    bool
	origin   string
	site     string
	sudo     *fakeSudo
	placed   []byte
	deps     placeissued.Deps
	placeErr error
	placeAs  string
	out, err bytes.Buffer
}

func newWorld(t *testing.T) *world {
	t.Helper()
	root := t.TempDir()
	checkout := filepath.Join(root, "My Checkout")
	if err := os.MkdirAll(checkout, 0o700); err != nil {
		t.Fatal(err)
	}
	return &world{
		t: t, checkout: checkout,
		env:    map[string]string{"HOME": root, "XDG_STATE_HOME": filepath.Join(root, "state"), "XDG_RUNTIME_DIR": filepath.Join(root, "run")},
		record: status.Record{Team: alpha(t)}, found: true,
		origin: "git@github.com:owner/Fedora-Desktop.git",
		sudo:   &fakeSudo{res: execx.Result{Stdout: []byte(issuedAnswer + "\n")}},
	}
}

func alpha(t *testing.T) paths.TeamID {
	t.Helper()
	id, err := paths.ParseTeamID("alpha")
	if err != nil {
		t.Fatal(err)
	}
	return id
}

// pool gives the checkout a pool seat directory of team alpha.
func (w *world) pool(name string) {
	w.t.Helper()
	store, key, _, err := system.Here(w.getenv, func() (string, error) { return w.checkout, nil })
	if err != nil {
		w.t.Fatal(err)
	}
	seatName, err := paths.ParseSeatName(name)
	if err != nil {
		w.t.Fatal(err)
	}
	if err := os.MkdirAll(store.PoolSeat(key, alpha(w.t), seatName).Dir(), 0o700); err != nil {
		w.t.Fatal(err)
	}
}

func (w *world) member(handle, state string) {
	w.record.Members = append(w.record.Members, status.Member{Handle: handle, State: state})
}

func (w *world) getenv(k string) string { return w.env[k] }

func (w *world) run(args ...string) term.ExitCode {
	return w.cmd().Run(context.Background(), args)
}

func (w *world) cmd() *Cmd {
	return &Cmd{
		Stdout: &w.out, Stderr: &w.err,
		Getwd: func() (string, error) { return w.checkout, nil },
		Adder: w.adder(),
	}
}

func (w *world) adder() *Adder {
	return &Adder{
		Getenv: w.getenv,
		Status: func(paths.TeamID) (status.Record, bool, error) { return w.record, w.found, nil },
		Origin: func(_ context.Context, dir string) (string, error) {
			if dir != w.checkout {
				w.t.Errorf("origin asked of %s, not the checkout", dir)
			}
			return w.origin, nil
		},
		Site:   func() (string, error) { return w.site, nil },
		Issuer: SudoIssuer{Sudo: w.sudo},
		Place: func(_ context.Context, d placeissued.Deps, doc io.Reader) (placeissued.Placed, error) {
			data, err := io.ReadAll(doc)
			if err != nil {
				return placeissued.Placed{}, err
			}
			w.placed, w.deps = data, d
			if w.placeErr != nil {
				return placeissued.Placed{}, w.placeErr
			}
			name := w.placeAs
			if name == "" {
				name = w.sudo.argv[slices.Index(w.sudo.argv, "--prefix")+1]
				name = strings.Replace(name, "+", "."+w.sudo.argv[slices.Index(w.sudo.argv, "--seat")+1]+"+", 1)
			}
			seatName, err := paths.ParseSeatName(name)
			if err != nil {
				return placeissued.Placed{}, err
			}
			return placeissued.Placed{Team: alpha(w.t), Seat: seatName}, nil
		},
	}
}

func (w *world) noSecretShown() {
	w.t.Helper()
	if strings.Contains(w.out.String()+w.err.String(), answerMark) {
		w.t.Errorf("the issued document reached a stream:\n%s\n%s", w.out.String(), w.err.String())
	}
}

// The seat's name is derived, never typed: repo from origin, site from teamchat_site (local when
// undeclared), type podman; the number is the lowest free one. It is issued by one password sudo
// and the answer goes straight into this checkout's pool, never to a screen.
func TestAddDerivesIssuesAndPlaces(t *testing.T) {
	w := newWorld(t)
	if code := w.run("alpha"); code != term.ExitOK {
		t.Fatalf("exit %d: %s", code, w.err.String())
	}
	want := []string{
		paths.AdminBinaryPath, "member", "add", "--team", "alpha", "--prefix", "fedora-desktop+local.podman",
		"--seat", "1", "--kind", "podman", "--human-text", "false", "--credential", "password",
	}
	if !slices.Equal(w.sudo.argv, want) {
		t.Errorf("argv %q\nwant %q", w.sudo.argv, want)
	}
	if string(w.placed) != issuedAnswer+"\n" {
		t.Errorf("placed %q, want member add's answer", w.placed)
	}
	_, key, state, err := system.Here(w.getenv, func() (string, error) { return w.checkout, nil })
	if err != nil {
		t.Fatal(err)
	}
	if w.deps.Checkout != key || w.deps.StateHome != state || w.deps.RuntimeDir != w.env["XDG_RUNTIME_DIR"] {
		t.Errorf("placed into %+v, want this checkout's pool", w.deps)
	}
	if w.out.String() != "1@alpha\n" {
		t.Errorf("stdout %q, want the seat to launch", w.out.String())
	}
	for _, s := range []string{"fedora-desktop.1+local.podman", "Launch it here: ccy --teams 1@alpha"} {
		if !strings.Contains(w.err.String(), s) {
			t.Errorf("stderr does not say %q:\n%s", s, w.err.String())
		}
	}
	w.noSecretShown()
}

// The number skips every seat part this checkout's pool holds (the launch chooses by seat part
// alone) and every number the roster keeps under the same prefix, in any state (parked and lost
// keep theirs; a retired one never returns). Another prefix's numbers do not count.
func TestAddTakesTheLowestFreeNumber(t *testing.T) {
	w := newWorld(t)
	w.pool("fedora-desktop.1+elsewhere.podman")
	w.member("fedora-desktop.2+local.podman", "parked")
	w.member("fedora-desktop.3+local.podman", "retired")
	w.member("fedora-desktop.4+local.lxc", "enrolled")
	w.member("other.5+local.podman", "enrolled")
	if code := w.run("alpha"); code != term.ExitOK {
		t.Fatalf("exit %d: %s", code, w.err.String())
	}
	if got := w.sudo.argv[slices.Index(w.sudo.argv, "--seat")+1]; got != "4" {
		t.Errorf("seat %s, want 4", got)
	}
}

func TestAddSiteAndRepoSources(t *testing.T) {
	w := newWorld(t)
	w.site, w.origin = "desk", ""
	if code := w.run("alpha"); code != term.ExitOK {
		t.Fatalf("exit %d: %s", code, w.err.String())
	}
	if got := w.sudo.argv[slices.Index(w.sudo.argv, "--prefix")+1]; got != "my-checkout+desk.podman" {
		t.Errorf("prefix %s, want the declared site and the checkout's base name", got)
	}
}

// --host is a host cc seat: type and kind host, launched with cc. --human-text issues a seat
// that takes the humans' text (a worker seat does not by default, the owner's word).
func TestAddHostSeat(t *testing.T) {
	w := newWorld(t)
	if code := w.run("--host", "alpha", "--human-text"); code != term.ExitOK {
		t.Fatalf("exit %d: %s", code, w.err.String())
	}
	argv := strings.Join(w.sudo.argv, " ")
	for _, s := range []string{"--prefix fedora-desktop+local.host", "--kind host", "--human-text true"} {
		if !strings.Contains(argv, s) {
			t.Errorf("argv %q lacks %q", argv, s)
		}
	}
	if !strings.Contains(w.err.String(), "cc --teams 1@alpha") {
		t.Errorf("stderr does not name the cc launch:\n%s", w.err.String())
	}
}

// A team whose status record is not on this machine has its homeserver elsewhere: this verb
// issues only where the homeserver runs, and names the cross-machine pipe instead. No sudo.
func TestAddRefusesATeamNotServedHere(t *testing.T) {
	w := newWorld(t)
	w.found = false
	if code := w.run("alpha"); code != term.ExitConfig {
		t.Fatalf("exit %d, want 78", code)
	}
	if w.sudo.readied || w.sudo.argv != nil {
		t.Error("sudo was used for a team not served here")
	}
	if !strings.Contains(w.err.String(), "place-issued") {
		t.Errorf("the refusal does not name the cross-machine route:\n%s", w.err.String())
	}
}

func TestAddStopsWhenSudoIsNotGiven(t *testing.T) {
	w := newWorld(t)
	w.sudo.readyErr = errors.New("no password")
	if code := w.run("alpha"); code != term.ExitFailure {
		t.Fatalf("exit %d, want 1", code)
	}
	if w.sudo.argv != nil || w.placed != nil {
		t.Error("issued or placed without sudo")
	}
}

// A refused issue says teamchat-admin's own words and never what it wrote on stdout.
func TestAddReportsAFailedIssue(t *testing.T) {
	w := newWorld(t)
	w.sudo.captureErr = errors.New("exit status 70")
	w.sudo.res.Stderr = []byte("teamchat-admin: the seat number is taken\n")
	if code := w.run("alpha"); code != term.ExitFailure {
		t.Fatalf("exit %d, want 1", code)
	}
	if w.placed != nil {
		t.Error("placed after a failed issue")
	}
	if !strings.Contains(w.err.String(), "the seat number is taken") {
		t.Errorf("stderr lacks teamchat-admin's reason:\n%s", w.err.String())
	}
	w.noSecretShown()
}

// A seat issued but not placed names the reissue pipe for the handle member add claimed.
func TestAddReportsAFailedPlacement(t *testing.T) {
	w := newWorld(t)
	w.placeErr = errors.New("the seat store is busy")
	if code := w.run("alpha"); code != term.ExitFailure {
		t.Fatalf("exit %d, want 1", code)
	}
	for _, s := range []string{"the seat store is busy", "member reissue --team alpha --handle fedora-desktop.1+local.podman --credential password", "place-issued --checkout"} {
		if !strings.Contains(w.err.String(), s) {
			t.Errorf("stderr lacks %q:\n%s", s, w.err.String())
		}
	}
	if w.out.Len() != 0 {
		t.Errorf("stdout %q after a failure", w.out.String())
	}
	w.noSecretShown()
}

func TestAddRefusesAPlacementOfAnotherSeat(t *testing.T) {
	w := newWorld(t)
	w.placeAs = "fedora-desktop.9+local.podman"
	if code := w.run("alpha"); code != term.ExitFailure {
		t.Fatalf("exit %d, want 1", code)
	}
}

// Each seam that fails stops the run before anything is issued, with exit 1.
func TestAddStopsOnASeamFailure(t *testing.T) {
	boom := errors.New("boom")
	for name, spoil := range map[string]func(*Cmd){
		"working directory": func(c *Cmd) { c.Getwd = func() (string, error) { return "", boom } },
		"status record": func(c *Cmd) {
			c.Adder.Status = func(paths.TeamID) (status.Record, bool, error) { return status.Record{}, false, boom }
		},
		"origin": func(c *Cmd) { c.Adder.Origin = func(context.Context, string) (string, error) { return "", boom } },
		"site":   func(c *Cmd) { c.Adder.Site = func() (string, error) { return "", boom } },
		"repo": func(c *Cmd) {
			c.Adder.Origin = func(context.Context, string) (string, error) { return "https://example.com/---.git", nil }
		},
	} {
		w := newWorld(t)
		c := w.cmd()
		spoil(c)
		if code := c.Run(context.Background(), []string{"alpha"}); code != term.ExitFailure {
			t.Errorf("%s: exit %d, want 1", name, code)
		}
		if w.sudo.readied {
			t.Errorf("%s: sudo was asked", name)
		}
	}
}

// The launcher's chooser runs the same add in process: Add takes the request, says its progress
// through Note only, writes nothing else, and answers the seat to launch.
func TestAdderInProcess(t *testing.T) {
	w := newWorld(t)
	a := w.adder()
	var notes []string
	a.Note = func(s string) { notes = append(notes, s) }
	got, err := a.Add(context.Background(), Request{Team: alpha(t), Checkout: w.checkout, Host: true})
	if err != nil {
		t.Fatal(err)
	}
	if got.Handle.String() != "fedora-desktop.1+local.host" || got.List != "1@alpha" || got.Launcher != "cc" {
		t.Errorf("added %+v", got)
	}
	if len(notes) != 2 || w.out.Len() != 0 || w.err.Len() != 0 {
		t.Errorf("notes %q, out %q, err %q", notes, w.out.String(), w.err.String())
	}
	if !strings.Contains(strings.Join(w.sudo.argv, " "), "--human-text false") {
		t.Errorf("argv %q: a request with no HumanText issues false", w.sudo.argv)
	}
	a.Note = nil
	w.sudo.argv = nil
	w.member("fedora-desktop.1+local.host", "enrolled")
	if _, err = a.Add(context.Background(), Request{Team: alpha(t), Checkout: w.checkout, Host: true}); err != nil {
		t.Fatalf("a nil Note: %v", err)
	}
}

// Register R5: a bare `cc --teams <team>` or `ccy --teams <team>` with no free seat adds one
// through the chooser's NewSeat: the seat of the launcher's kind, its progress said, its
// <seat>@<team> the launch's; a team served elsewhere is refused (78) naming the route, any other
// failure is 1, and nothing is printed but through say.
func TestNewSeatAddsTheLaunchersSeat(t *testing.T) {
	w := newWorld(t)
	var said []string
	say := func(s string) { said = append(said, s) }
	getwd := func() (string, error) { return w.checkout, nil }
	newSeat := w.adder().NewSeat(getwd, say)
	got, code := newSeat(context.Background(), "alpha", "host")
	if code != term.ExitOK || got != "1@alpha" {
		t.Fatalf("host: %q %d; said %q", got, code, said)
	}
	// The launch goes on with the seat, so it is said as taken, not as a command to run.
	text := strings.Join(said, "\n")
	if !strings.Contains(strings.Join(w.sudo.argv, " "), "--kind host") || !strings.Contains(text, "This cc launch takes seat 1@alpha.") ||
		strings.Contains(text, "Launch it here") {
		t.Errorf("argv %q, said %q", w.sudo.argv, said)
	}
	w.pool("fedora-desktop.1+local.host")
	if got, code = newSeat(context.Background(), "alpha", "podman"); code != term.ExitOK || got != "2@alpha" {
		t.Fatalf("podman: %q %d; said %q", got, code, said)
	}
	if !strings.Contains(strings.Join(w.sudo.argv, " "), "--kind podman") {
		t.Errorf("argv %q", w.sudo.argv)
	}
	if w.out.Len() != 0 || w.err.Len() != 0 {
		t.Errorf("out %q, err %q", w.out.String(), w.err.String())
	}

	said = nil
	w.found = false
	if _, code = newSeat(context.Background(), "alpha", "podman"); code != term.ExitConfig ||
		!strings.Contains(strings.Join(said, "\n"), "no new seat of alpha could be added here") || !strings.Contains(strings.Join(said, "\n"), "place-issued") {
		t.Errorf("a team served elsewhere: %d, said %q", code, said)
	}
	w.found = true
	w.sudo.readyErr = errors.New("no password")
	if _, code = newSeat(context.Background(), "alpha", "podman"); code != term.ExitFailure {
		t.Errorf("sudo refused: %d", code)
	}
	if _, code = newSeat(context.Background(), "not a team!", "podman"); code != term.ExitUsage {
		t.Errorf("a bad team: %d", code)
	}
	broken := w.adder().NewSeat(func() (string, error) { return "", errors.New("gone") }, say)
	if _, code = broken(context.Background(), "alpha", "podman"); code != term.ExitFailure {
		t.Errorf("no working directory: %d", code)
	}
}

type failWriter struct{}

func (failWriter) Write([]byte) (int, error) { return 0, errors.New("closed") }

// A placed seat whose line cannot be printed is still a failure: the caller did not get it.
func TestAddFailsWhenItCannotPrint(t *testing.T) {
	for name, set := range map[string]func(*Cmd){
		"stdout": func(c *Cmd) { c.Stdout = failWriter{} },
		"stderr": func(c *Cmd) { c.Stderr = failWriter{} },
	} {
		w := newWorld(t)
		c := w.cmd()
		set(c)
		if code := c.Run(context.Background(), []string{"alpha"}); code != term.ExitFailure {
			t.Errorf("%s: exit %d, want 1", name, code)
		}
	}
}

func TestAddUsage(t *testing.T) {
	for name, args := range map[string][]string{
		"no team":      {},
		"two teams":    {"alpha", "beta"},
		"bad team":     {"Alpha!"},
		"unknown flag": {"alpha", "--seat"},
		"old spelling": {"alpha", "--no-human-text"},
	} {
		w := newWorld(t)
		if code := w.run(args...); code != term.ExitUsage {
			t.Errorf("%s: exit %d, want 64", name, code)
		}
		if w.sudo.readied {
			t.Errorf("%s: sudo was asked", name)
		}
	}
}
