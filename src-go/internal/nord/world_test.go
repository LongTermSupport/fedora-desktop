package nord

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

// The account's credentials in a test.
const (
	testUser     = "svcuser"
	testPassword = "s3cret pass"
)

// profile is one NetworkManager connection in the fake.
type profile struct {
	name, uuid, kind string
	username         string
	// active is "" when the connection is down, else its state (activated, activating).
	active string
	device string
}

// failure is what one nmcli verb answers instead of doing it.
type failure struct {
	stderr string
	exit   int
}

// world is one machine for a test: a temporary home, the programs installed, NetworkManager's
// connections, the lines typed at the terminal and the streams a run writes.
type world struct {
	t         *testing.T
	home      string
	installed map[string]bool
	nmRunning bool
	// systemctlFails makes systemctl unable to run.
	systemctlFails bool
	profiles       []*profile
	nextUUID       int
	// fail answers an nmcli verb (import, modify, up, down, delete, show) with a failure.
	fail map[string]failure
	// importOut replaces what a successful import prints.
	importOut *string
	curl      failure
	curlOut   string
	fake      *execx.Fake
	stdout    bytes.Buffer
	stderr    bytes.Buffer
	tty       string
	ttyIn     io.Reader
	noTTY     bool
	stdinTTY  bool
	// fed is the standard input each `nmcli connection up` was given, as text.
	fed []string
	// during runs while the command whose line it is keyed by runs: a Ctrl-C mid-command.
	during map[string]func()
	// cancelledUnder are the lines of the commands whose own context ended while they ran.
	cancelledUnder []string
	unscripted     []string
	runCtx         func() context.Context
	cancel         context.CancelCauseFunc
	now            time.Time
}

func newWorld(t *testing.T) *world {
	t.Helper()
	w := &world{
		t: t, home: filepath.Join(t.TempDir(), "home"),
		installed: map[string]bool{"nmcli": true, "openvpn": true, "systemctl": true, "curl": true},
		nmRunning: true, fail: map[string]failure{}, stdinTTY: true,
		during: map[string]func(){},
		now:    time.Date(2026, 10, 28, 9, 30, 0, 0, time.UTC),
	}
	ctx, cancel := context.WithCancelCause(context.Background())
	w.runCtx, w.cancel = func() context.Context { return ctx }, cancel
	t.Cleanup(func() { w.cancel(context.Canceled) })
	w.dir(".config/nordvpn/configs")
	w.dir(".local/share/nordvpn")
	w.file(".config/nordvpn/.credentials", testUser+"\n"+testPassword+"\n")
	w.fake = &execx.Fake{}
	w.fake.OnFunc([]string{}, w.dispatch)
	t.Cleanup(func() {
		if len(w.unscripted) > 0 {
			t.Errorf("unscripted commands ran: %q", w.unscripted)
		}
	})
	return w
}

// config puts an .ovpn file in the configs directory.
func (w *world) config(names ...string) {
	for _, n := range names {
		w.file(".config/nordvpn/configs/"+n, "client\nremote example.com 1194\n")
	}
}

// imported adds a profile as an earlier import left it.
func (w *world) imported(name string) *profile {
	w.nextUUID++
	p := &profile{name: name, uuid: fmt.Sprintf("00000000-0000-4000-8000-%012d", w.nextUUID), kind: "vpn"}
	w.profiles = append(w.profiles, p)
	return p
}

func (w *world) file(rel, content string) {
	w.t.Helper()
	p := filepath.Join(w.home, rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o750); err != nil {
		w.t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
		w.t.Fatal(err)
	}
}

func (w *world) dir(rel string) {
	w.t.Helper()
	if err := os.MkdirAll(filepath.Join(w.home, rel), 0o750); err != nil {
		w.t.Fatal(err)
	}
}

func (w *world) typed(lines ...string) { w.tty = strings.Join(lines, "\n") + "\n" }

// interrupt ends the run's context with SIGINT's cause, as Ctrl-C does.
func (w *world) interrupt() { w.cancel(&execx.SignalError{Signal: syscall.SIGINT}) }

// ctxRunner is the world's runner: the Fake, with the context each command ran under watched.
type ctxRunner struct{ w *world }

func (r ctxRunner) Run(ctx context.Context, c execx.Cmd) (execx.Result, error) {
	res, err := r.w.fake.Run(ctx, c)
	if hook := r.w.during[c.Line()]; hook != nil {
		hook()
	}
	if ctx.Err() != nil {
		r.w.cancelledUnder = append(r.w.cancelledUnder, c.Line())
	}
	return res, err
}

func (w *world) dispatch(c execx.Cmd) (execx.Result, error) {
	if !w.installed[c.Name] {
		return execx.Result{}, &execx.NotInstalledError{Name: c.Name}
	}
	switch c.Name {
	case "systemctl":
		if w.systemctlFails {
			return execx.Result{}, errors.New("systemctl: cannot connect to the bus")
		}
		if slices.Equal(c.Args, []string{"is-active", "--quiet", "NetworkManager"}) {
			if w.nmRunning {
				return execx.Result{}, nil
			}
			return execx.Result{ExitCode: 3}, nil
		}
	case "curl":
		if slices.Equal(c.Args, []string{"-fsS", "--max-time", "5", "https://api.ipify.org"}) {
			return execx.Result{Stdout: []byte(w.curlOut), Stderr: []byte(w.curl.stderr), ExitCode: w.curl.exit}, nil
		}
	case "nmcli":
		if res, ok := w.nmcli(c); ok {
			return res, nil
		}
	}
	w.unscripted = append(w.unscripted, c.Line())
	return execx.Result{}, fmt.Errorf("%w: %s", execx.ErrUnexpected, c.Line())
}

// terse escapes one field as nmcli -t does.
func terse(s string) string {
	return strings.NewReplacer(`\`, `\\`, `:`, `\:`).Replace(s)
}

func (w *world) failed(verb string) (execx.Result, bool) {
	if f, ok := w.fail[verb]; ok {
		return execx.Result{Stderr: []byte(f.stderr), ExitCode: f.exit}, true
	}
	return execx.Result{}, false
}

// nmVerb is one nmcli command the fake knows: which argv it is, and what it does.
type nmVerb struct {
	name  string
	match func(a []string) bool
	do    func(w *world, c execx.Cmd) execx.Result
}

var nmVerbs = []nmVerb{
	{"show", func(a []string) bool {
		return slices.Equal(a, []string{"-t", "-f", "NAME,UUID,TYPE", "connection", "show"})
	}, (*world).showProfiles},
	{"show", func(a []string) bool {
		return slices.Equal(a, []string{"-t", "-f", "NAME,UUID,TYPE,DEVICE,STATE", "connection", "show", "--active"})
	}, (*world).showActive},
	{"import", func(a []string) bool {
		return len(a) == 6 && slices.Equal(a[:5], []string{"connection", "import", "type", "openvpn", "file"})
	}, (*world).importConfig},
	{"modify", func(a []string) bool {
		return len(a) == 8 && slices.Equal(a[:3], []string{"connection", "modify", "uuid"}) &&
			a[4] == "connection.id" && a[6] == "+vpn.data"
	}, (*world).modify},
	{"up", func(a []string) bool {
		return len(a) == 6 && slices.Equal(a[:3], []string{"connection", "up", "uuid"}) &&
			slices.Equal(a[4:], []string{"passwd-file", "/dev/stdin"})
	}, (*world).up},
	{"down", func(a []string) bool {
		return len(a) == 4 && slices.Equal(a[:3], []string{"connection", "down", "uuid"})
	}, (*world).down},
	{"delete", func(a []string) bool {
		return len(a) == 4 && slices.Equal(a[:3], []string{"connection", "delete", "uuid"})
	}, (*world).deleteProfile},
}

// nmcli is the fake NetworkManager: the listings from w.profiles, and each verb applied to them.
func (w *world) nmcli(c execx.Cmd) (execx.Result, bool) {
	for _, v := range nmVerbs {
		if !v.match(c.Args) {
			continue
		}
		if v.name == "up" {
			w.fed = append(w.fed, fedText(c))
		}
		if res, bad := w.failed(v.name); bad {
			return res, true
		}
		return v.do(w, c), true
	}
	return execx.Result{}, false
}

var unknownConnection = execx.Result{Stderr: []byte("Error: unknown connection\n"), ExitCode: 10}

func (w *world) showProfiles(execx.Cmd) execx.Result {
	var out strings.Builder
	for _, p := range w.profiles {
		fmt.Fprintf(&out, "%s:%s:%s\n", terse(p.name), p.uuid, p.kind)
	}
	return execx.Result{Stdout: []byte(out.String())}
}

func (w *world) showActive(execx.Cmd) execx.Result {
	var out strings.Builder
	for _, p := range w.profiles {
		if p.active != "" {
			fmt.Fprintf(&out, "%s:%s:%s:%s:%s\n", terse(p.name), p.uuid, p.kind, p.device, p.active)
		}
	}
	return execx.Result{Stdout: []byte(out.String())}
}

func (w *world) importConfig(c execx.Cmd) execx.Result {
	p := w.imported(strings.TrimSuffix(filepath.Base(c.Args[5]), ".ovpn"))
	out := fmt.Sprintf("Connection '%s' (%s) successfully added.\n", p.name, p.uuid)
	if w.importOut != nil {
		out = *w.importOut
	}
	return execx.Result{Stdout: []byte(out)}
}

func (w *world) modify(c execx.Cmd) execx.Result {
	p := w.byUUID(c.Args[3])
	user, ok := strings.CutPrefix(c.Args[7], "username=")
	if p == nil || !ok {
		return unknownConnection
	}
	p.name, p.username = c.Args[5], user
	return execx.Result{}
}

func (w *world) up(c execx.Cmd) execx.Result {
	p := w.byUUID(c.Args[3])
	if p == nil {
		return unknownConnection
	}
	p.active, p.device = "activated", "wlp1s0"
	return execx.Result{Stdout: []byte("Connection successfully activated\n")}
}

func (w *world) down(c execx.Cmd) execx.Result {
	if p := w.byUUID(c.Args[3]); p != nil {
		p.active, p.device = "", ""
	}
	return execx.Result{}
}

func (w *world) deleteProfile(c execx.Cmd) execx.Result {
	w.profiles = slices.DeleteFunc(w.profiles, func(p *profile) bool { return p.uuid == c.Args[3] })
	return execx.Result{}
}

// fedText is what a command's Feed puts on its stdin, the password shown as <password> when it
// is the test's, so a test can assert the line without the value being anywhere else.
func fedText(c execx.Cmd) string {
	if c.Feed == nil {
		return "<no feed>"
	}
	value := "<another secret>"
	if c.Feed.Secret.Is(testPassword) {
		value = "<password>"
	}
	return c.Feed.Before + value + c.Feed.After
}

// profileAt is the i-th profile, failing the test when there are not that many.
func (w *world) profileAt(i int) *profile {
	w.t.Helper()
	if i >= len(w.profiles) {
		w.t.Fatalf("no profile %d: %d profiles", i, len(w.profiles))
	}
	return w.profiles[i]
}

func (w *world) byUUID(uuid string) *profile {
	for _, p := range w.profiles {
		if p.uuid == uuid {
			return p
		}
	}
	return nil
}

func (w *world) getenv(k string) string {
	if k == "HOME" {
		return w.home
	}
	return ""
}

func (w *world) envFor() Env {
	return Env{
		Runner: ctxRunner{w},
		Stdout: &w.stdout, Stderr: &w.stderr, Stdin: "stdin",
		Getenv: w.getenv,
		LookPath: func(p string) (string, error) {
			if w.installed[p] {
				return "/usr/bin/" + p, nil
			}
			return "", errors.New("not found")
		},
		IsTerminal: func(v any) bool { return v == "stdin" && w.stdinTTY },
		OpenTTY: func() (io.ReadCloser, error) {
			if w.noTTY {
				return nil, errors.New("no tty")
			}
			if w.ttyIn != nil {
				return io.NopCloser(w.ttyIn), nil
			}
			return io.NopCloser(strings.NewReader(w.tty)), nil
		},
		Columns: func() int { return 100 },
		Now:     func() time.Time { return w.now },
	}
}

func (w *world) run(args ...string) term.ExitCode {
	return App{Env: w.envFor()}.Run(w.runCtx(), args)
}

// changes are the nmcli commands run that change something, in order.
func (w *world) changes() []string {
	var out []string
	for _, c := range w.fake.Calls() {
		if c.Name == "nmcli" && len(c.Args) > 1 && c.Args[0] == "connection" && c.Args[1] != "show" {
			out = append(out, c.Line())
		}
	}
	return out
}

func (w *world) logText() string {
	w.t.Helper()
	b, err := os.ReadFile(filepath.Join(w.home, ".local/share/nordvpn/nord.log"))
	if err != nil {
		w.t.Fatal(err)
	}
	return string(b)
}

func wantCode(t *testing.T, got, want term.ExitCode) {
	t.Helper()
	if got != want {
		t.Errorf("exit %d, want %d", got, want)
	}
}

func wantChanges(t *testing.T, w *world, want ...string) {
	t.Helper()
	if got := w.changes(); !slices.Equal(got, want) {
		t.Errorf("changes\n%q\nwant\n%q", got, want)
	}
}

func (w *world) wantOut(subs ...string) {
	w.t.Helper()
	for _, s := range subs {
		if !strings.Contains(w.stdout.String(), s) {
			w.t.Errorf("stdout lacks %q:\n%s", s, w.stdout.String())
		}
	}
}

func (w *world) wantErr(subs ...string) {
	w.t.Helper()
	for _, s := range subs {
		if !strings.Contains(w.stderr.String(), s) {
			w.t.Errorf("stderr lacks %q:\n%s", s, w.stderr.String())
		}
	}
}

func (w *world) wantNoErr(subs ...string) {
	w.t.Helper()
	for _, s := range subs {
		if strings.Contains(w.stderr.String(), s) {
			w.t.Errorf("stderr has %q:\n%s", s, w.stderr.String())
		}
	}
}

// wantNoPassword fails when the password is on either stream or in the log.
func (w *world) wantNoPassword() {
	w.t.Helper()
	all := w.stdout.String() + w.stderr.String()
	if b, err := os.ReadFile(filepath.Join(w.home, ".local/share/nordvpn/nord.log")); err == nil {
		all += string(b)
	}
	if strings.Contains(all, testPassword) {
		w.t.Errorf("the password was printed or logged:\n%s", all)
	}
	for _, c := range w.fake.Calls() {
		if strings.Contains(c.Line(), testPassword) {
			w.t.Errorf("the password is in an argv: %s", c.Line())
		}
	}
}
