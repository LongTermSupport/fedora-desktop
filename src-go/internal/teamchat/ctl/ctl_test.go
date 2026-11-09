package ctl

import (
	"context"
	"errors"
	"io"
	"math/rand/v2"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/unix"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/admin"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/apply"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/matrix"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/matrix/fakehs"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
)

const (
	t0ms       = 1_760_000_000_000
	registerAt = "/_synapse/admin/v1/register"
	loginAt    = "/_matrix/client/v3/login"
	whoamiAt   = "/_matrix/client/v3/account/whoami"
)

// world is one team's ctl/<team>/ under a temporary root and a fake homeserver on loopback.
// Every error an Open answers is kept and, when the test ends, checked to quote nothing
// hidden: no password, token or registration key the test saw.
type world struct {
	t      *testing.T
	hs     *fakehs.Server
	cfg    Config
	team   paths.TeamID
	port   int
	dir    string
	errs   []error
	hidden []string
}

func fixedNow() time.Time { return time.UnixMilli(t0ms) }

type noSleep struct{}

func (noSleep) Sleep(ctx context.Context, _ time.Duration) error { return ctx.Err() }

func newWorld(t *testing.T) *world {
	t.Helper()
	team, err := paths.ParseTeamID("alpha")
	if err != nil {
		t.Fatalf("ParseTeamID: %v", err)
	}
	hs := fakehs.New(fakehs.Options{ServerName: paths.ServerName(team), Now: fixedNow, Quirks: fakehs.TuwunelQuirks()})
	if startErr := hs.Start(); startErr != nil {
		t.Fatalf("Start: %v", startErr)
	}
	t.Cleanup(func() {
		if stopErr := hs.Stop(); stopErr != nil {
			t.Errorf("Stop: %v", stopErr)
		}
	})
	u, err := url.Parse(hs.URL())
	if err != nil {
		t.Fatalf("URL: %v", err)
	}
	port, err := strconv.Atoi(u.Port())
	if err != nil {
		t.Fatalf("port: %v", err)
	}
	root := t.TempDir()
	w := &world{t: t, hs: hs, team: team, port: port, dir: root + paths.CtlState(team)}
	w.cfg = Config{
		Root: root, Now: fixedNow, Rand: rand.NewChaCha8([32]byte{1}),
		Admin: func(base string) (*matrix.Admin, error) {
			return matrix.NewAdmin(matrix.Config{BaseURL: base, Clock: noSleep{}})
		},
	}
	w.mkdir(w.dir, 0o700)
	w.hidden = append(w.hidden, hs.RegistrationKey())
	t.Cleanup(w.scan)
	return w
}

func (w *world) mkdir(dir string, mode os.FileMode) {
	w.t.Helper()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		w.t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.Chmod(dir, mode); err != nil {
		w.t.Fatalf("Chmod: %v", err)
	}
}

// scan fails the test when any error an Open gave quotes a password, a token or the key (OP5).
func (w *world) scan() {
	for _, err := range w.errs {
		for _, s := range w.hidden {
			if s != "" && strings.Contains(err.Error(), s) {
				w.t.Errorf("error %q quotes a password, token or key", err)
			}
		}
	}
}

func (w *world) key() string { return w.hs.RegistrationKey() }

func (w *world) adminID() string { return "@admin:" + paths.ServerName(w.team) }

func (w *world) open(regKey string) (admin.Opened, error) {
	w.t.Helper()
	o, err := Open(w.t.Context(), w.cfg, w.team, w.port, regKey)
	if err != nil {
		w.errs = append(w.errs, err)
	}
	return o, err
}

func (w *world) mustOpen(regKey string) admin.Opened {
	w.t.Helper()
	o, err := w.open(regKey)
	if err != nil {
		w.t.Fatalf("Open: %v", err)
	}
	return o
}

func (w *world) path(name string) string { return filepath.Join(w.dir, name) }

// has says whether ctl/<team>/ holds name, and remembers its content as hidden.
func (w *world) has(name string) bool {
	w.t.Helper()
	data, err := os.ReadFile(w.path(name))
	if errors.Is(err, os.ErrNotExist) {
		return false
	}
	if err != nil {
		w.t.Fatalf("ReadFile: %v", err)
	}
	w.hidden = append(w.hidden, string(data))
	return true
}

// text is a file of ctl/<team>/ that must be there, remembered as hidden.
func (w *world) text(name string) string {
	w.t.Helper()
	data, err := os.ReadFile(w.path(name))
	if err != nil {
		w.t.Fatalf("ReadFile: %v", err)
	}
	w.hidden = append(w.hidden, string(data))
	return string(data)
}

func (w *world) write(name, content string) {
	w.t.Helper()
	if err := os.WriteFile(w.path(name), []byte(content), 0o600); err != nil {
		w.t.Fatalf("WriteFile: %v", err)
	}
	w.hidden = append(w.hidden, content)
}

func (w *world) mode(name string) os.FileMode {
	w.t.Helper()
	info, err := os.Lstat(w.path(name))
	if err != nil {
		w.t.Fatalf("Lstat: %v", err)
	}
	return info.Mode()
}

// entries are the names in ctl/<team>/.
func (w *world) entries() []string {
	w.t.Helper()
	list, err := os.ReadDir(w.dir)
	if err != nil {
		w.t.Fatalf("ReadDir: %v", err)
	}
	names := make([]string, 0, len(list))
	for _, e := range list {
		names = append(names, e.Name())
	}
	return names
}

func adminOf(t *testing.T, o admin.Opened) *matrix.Admin {
	t.Helper()
	a, ok := o.Admin.(*matrix.Admin)
	if !ok {
		t.Fatalf("Deps.Admin is %T", o.Admin)
	}
	return a
}

// bootstrapped checks the end state of step 1: a token of @admin on TCADMIN, 0600, the one
// server admin, and no password and no temporary file left.
func (w *world) bootstrapped(o admin.Opened) {
	w.t.Helper()
	token := w.text(tokenName)
	if strings.ContainsAny(token, " \n") {
		w.t.Error("the token file holds more than the token")
	}
	if got := w.mode(tokenName); got != 0o600 {
		w.t.Errorf("admin.token mode = %v, want 0600", got)
	}
	if got := w.entries(); !slices.Equal(got, []string{tokenName}) {
		w.t.Errorf("ctl/<team>/ = %v, want only admin.token", got)
	}
	a := adminOf(w.t, o)
	id, err := a.Whoami(w.t.Context())
	if err != nil {
		w.t.Fatalf("Whoami: %v", err)
	}
	if id.UserID != w.adminID() || id.DeviceID != AdminDevice {
		w.t.Errorf("whoami = %+v, want %s on %s", id, w.adminID(), AdminDevice)
	}
	admins, err := a.ServerAdmins(w.t.Context())
	if err != nil {
		w.t.Fatalf("ServerAdmins: %v", err)
	}
	if !slices.Equal(admins, []string{w.adminID()}) {
		w.t.Errorf("server admins = %v", admins)
	}
}

// wantRefused checks err is a refusal of team apply's admin step, which admin maps to 70.
func wantRefused(t *testing.T, err error) {
	t.Helper()
	e, ok := errors.AsType[*apply.Error](err)
	if !ok || !e.Drift || e.Op != apply.OpTeamApply || e.Step != StepAdmin {
		t.Fatalf("err = %v, want a refusal of team apply's admin step", err)
	}
	if code := admin.ExitCodeOf(err); code != admin.ExitRefused {
		t.Errorf("ExitCodeOf = %d, want %d", code, admin.ExitRefused)
	}
}

func notRefused(t *testing.T, err error) {
	t.Helper()
	if err == nil {
		t.Fatal("Open succeeded")
	}
	if code := admin.ExitCodeOf(err); code == admin.ExitRefused {
		t.Fatalf("err = %v is a refusal (70), want a config failure", err)
	}
}

func TestFreshBootstrapRegistersLogsInAndStoresTheToken(t *testing.T) {
	w := newWorld(t)
	o := w.mustOpen(w.key())
	w.bootstrapped(o)
	if o.RosterPath != w.cfg.Root+paths.RosterFile(w.team) || o.RoomsPath != w.cfg.Root+paths.RoomsFile(w.team) ||
		o.GuestDir != w.cfg.Root+paths.GuestTokenDir(w.team) {
		t.Errorf("Deps paths = %q %q %q", o.RosterPath, o.RoomsPath, o.GuestDir)
	}
	if o.Now == nil || !o.Now().Equal(fixedNow()) {
		t.Error("Deps.Now is not the Config's clock")
	}
	if o.Approvals != nil {
		t.Error("Open set Approvals; the request carries them")
	}
	devices, err := adminOf(t, o).Devices(t.Context(), w.adminID())
	if err != nil {
		t.Fatalf("Devices: %v", err)
	}
	if !slices.ContainsFunc(devices, func(d matrix.Device) bool { return d.ID == AdminDevice }) {
		t.Errorf("devices = %+v, want %s among them", devices, AdminDevice)
	}
}

func TestOpenWithATokenUsesItWithoutCallingTheServer(t *testing.T) {
	w := newWorld(t)
	w.mustOpen(w.key())
	before := len(w.hs.Requests())
	o := w.mustOpen("")
	if got := len(w.hs.Requests()); got != before {
		t.Errorf("Open without a key made %d requests", got-before)
	}
	w.bootstrapped(o)
}

func TestApplyAgainChecksWhoamiAndChangesNothing(t *testing.T) {
	w := newWorld(t)
	w.mustOpen(w.key())
	token := w.text(tokenName)
	before := len(w.hs.Requests())
	o := w.mustOpen(w.key())
	reqs := w.hs.Requests()[before:]
	if len(reqs) != 1 || reqs[0].Path != whoamiAt {
		t.Errorf("a second apply sent %+v, want one whoami", reqs)
	}
	w.bootstrapped(o)
	if w.text(tokenName) != token {
		t.Error("a second apply replaced the token")
	}
}

func TestNoTokenWithoutAKeyIsRefusedNamingTeamApply(t *testing.T) {
	w := newWorld(t)
	_, err := w.open("")
	notRefused(t, err)
	if want := "team alpha has no admin token: run team apply"; !strings.Contains(err.Error(), want) {
		t.Errorf("err = %v, want %q", err, want)
	}
	if got := w.entries(); len(got) != 0 {
		t.Errorf("Open without a key wrote %v", got)
	}
}

func TestCrashBeforeRegisterIsFinishedByTheNextApply(t *testing.T) {
	w := newWorld(t)
	w.hs.Inject(fakehs.Fault{Method: http.MethodPost, Path: registerAt, Drop: true})
	_, err := w.open(w.key())
	if !errors.Is(err, matrix.ErrUnreachable) {
		t.Fatalf("err = %v, want unreachable", err)
	}
	if password := w.text(passwordName); len(password) != 2*passwordBytes {
		t.Fatalf("admin.password = %d bytes", len(password))
	}
	if got := w.mode(passwordName); got != 0o600 {
		t.Errorf("admin.password mode = %v", got)
	}
	w.bootstrapped(w.mustOpen(w.key()))
}

func TestCrashAfterRegisterBeforeTheTokenIsFinishedByTheNextApply(t *testing.T) {
	w := newWorld(t)
	w.hs.Inject(fakehs.Fault{Method: http.MethodPost, Path: registerAt, DropAfter: true})
	_, err := w.open(w.key())
	if !errors.Is(err, matrix.ErrUnreachable) {
		t.Fatalf("err = %v, want unreachable", err)
	}
	w.text(passwordName)
	if w.has(tokenName) {
		t.Fatal("a token was stored")
	}
	w.bootstrapped(w.mustOpen(w.key()))
}

func TestCrashAfterLoginBeforeTheTokenIsFinishedByTheNextApply(t *testing.T) {
	w := newWorld(t)
	w.hs.Inject(fakehs.Fault{Method: http.MethodPost, Path: loginAt, DropAfter: true})
	if _, err := w.open(w.key()); !errors.Is(err, matrix.ErrUnreachable) {
		t.Fatalf("err = %v, want unreachable", err)
	}
	w.text(passwordName)
	w.bootstrapped(w.mustOpen(w.key()))
}

func TestCrashAfterTheTokenBeforeThePasswordIsDeletedIsFinishedByTheNextApply(t *testing.T) {
	w := newWorld(t)
	w.mustOpen(w.key())
	w.write(passwordName, strings.Repeat("ab", passwordBytes))
	w.bootstrapped(w.mustOpen(w.key()))
}

// TestACrashedWriteLeavesNoSecretBehind is OP4: a temporary password or token a crash left
// between its create and its unlink is removed by the next Open, before the bootstrap, with or
// without a key; nothing else in the directory is touched.
func TestACrashedWriteLeavesNoSecretBehind(t *testing.T) {
	stale := []string{".admin.password.0123456789abcdef.tmp", ".admin.token.fedcba9876543210.tmp"}
	others := []string{".admin.token.tmp", "admin.token.1.tmp", "notes.tmp"}
	plantStale := func(w *world) {
		for _, name := range stale {
			w.write(name, strings.Repeat("ab", passwordBytes))
		}
	}
	for _, regKey := range []bool{true, false} {
		w := newWorld(t)
		w.mustOpen(w.key())
		plantStale(w)
		for _, name := range others {
			w.write(name, "")
		}
		key := ""
		if regKey {
			key = w.key()
		}
		w.mustOpen(key)
		want := append([]string{tokenName}, others...)
		if got := w.entries(); !slices.Equal(slices.Sorted(slices.Values(got)), slices.Sorted(slices.Values(want))) {
			t.Errorf("key %v: ctl/<team>/ = %v, want %v", regKey, got, want)
		}
	}
	w := newWorld(t)
	plantStale(w)
	w.bootstrapped(w.mustOpen(w.key()))
}

func TestAStaleTemporaryThatCannotBeRemovedFailsTheOpen(t *testing.T) {
	w := newWorld(t)
	w.mkdir(filepath.Join(w.dir, ".admin.token.0123456789abcdef.tmp"), 0o700)
	if _, err := w.open(w.key()); err == nil || !strings.Contains(err.Error(), ".admin.token.0123456789abcdef.tmp") {
		t.Fatalf("err = %v", err)
	}
	if w.has(passwordName) {
		t.Error("the bootstrap ran past a stale temporary it could not remove")
	}
}

func TestALeftoverPasswordStaysWithoutAKey(t *testing.T) {
	w := newWorld(t)
	w.mustOpen(w.key())
	w.write(passwordName, strings.Repeat("ab", passwordBytes))
	w.mustOpen("")
	if !w.has(passwordName) {
		t.Error("an op without the key deleted the password; only the bootstrap may")
	}
}

func TestATokenOfAnotherUserIsRefused(t *testing.T) {
	w := newWorld(t)
	bob := w.hs.AddUser("bob", "", false)
	token, err := w.hs.MintToken(bob)
	if err != nil {
		t.Fatalf("MintToken: %v", err)
	}
	w.write(tokenName, token)
	_, err = w.open(w.key())
	wantRefused(t, err)
	if !strings.Contains(err.Error(), "not "+w.adminID()) {
		t.Errorf("err = %v, want it to name the account", err)
	}
}

func TestATokenTheServerRefusesIsRefused(t *testing.T) {
	w := newWorld(t)
	w.write(tokenName, "syt_not_a_known_token")
	_, err := w.open(w.key())
	wantRefused(t, err)
	if !strings.Contains(err.Error(), "restore") {
		t.Errorf("err = %v, want it to name restore", err)
	}
}

func TestAWhoamiThatIsNotAnsweredIsUnreachable(t *testing.T) {
	w := newWorld(t)
	w.mustOpen(w.key())
	w.hs.Inject(fakehs.Fault{Method: http.MethodGet, Path: whoamiAt, Drop: true})
	_, err := w.open(w.key())
	if !errors.Is(err, matrix.ErrUnreachable) {
		t.Fatalf("err = %v, want unreachable", err)
	}
	if code := admin.ExitCodeOf(err); code != admin.ExitUnreachable {
		t.Errorf("ExitCodeOf = %d, want %d", code, admin.ExitUnreachable)
	}
}

func TestUserInUseWithNoPasswordIsRefusedNamingRestore(t *testing.T) {
	w := newWorld(t)
	w.hs.AddUser("admin", "lost-password", true)
	w.hidden = append(w.hidden, "lost-password")
	_, err := w.open(w.key())
	wantRefused(t, err)
	if !strings.Contains(err.Error(), "restore") {
		t.Errorf("err = %v, want it to name restore", err)
	}
	if got := w.entries(); len(got) != 0 {
		t.Errorf("a refused bootstrap left %v", got)
	}
}

func TestAPasswordTheAccountDoesNotTakeIsRefusedNamingRestore(t *testing.T) {
	w := newWorld(t)
	w.hs.AddUser("admin", "other-password", true)
	w.hidden = append(w.hidden, "other-password")
	w.write(passwordName, strings.Repeat("cd", passwordBytes))
	_, err := w.open(w.key())
	wantRefused(t, err)
	if !strings.Contains(err.Error(), "restore") {
		t.Errorf("err = %v, want it to name restore", err)
	}
}

func TestALoginAsAnotherAccountIsRefused(t *testing.T) {
	w := newWorld(t)
	w.hs.Inject(fakehs.Fault{
		Method: http.MethodPost, Path: loginAt, Status: http.StatusOK,
		Body: []byte(`{"user_id":"@bob:alpha.teamchat.internal","access_token":"tok","device_id":"TCADMIN"}`),
	})
	_, err := w.open(w.key())
	wantRefused(t, err)
	if w.has(tokenName) {
		t.Error("a token of another account was stored")
	}
}

func TestAWrongRegistrationKeyFailsWithoutQuotingIt(t *testing.T) {
	w := newWorld(t)
	wrong := strings.Repeat("e", 64)
	w.hidden = append(w.hidden, wrong)
	_, err := w.open(wrong)
	if !errors.Is(err, matrix.ErrForbidden) {
		t.Fatalf("err = %v, want forbidden", err)
	}
	w.text(passwordName)
}

func TestARateLimitedLoginIsRateLimited(t *testing.T) {
	w := newWorld(t)
	w.hs.Inject(fakehs.Fault{
		Method: http.MethodPost, Path: loginAt, Status: http.StatusTooManyRequests,
		Body: []byte(`{"errcode":"M_LIMIT_EXCEEDED","retry_after_ms":1}`), Times: 10,
	})
	_, err := w.open(w.key())
	if !errors.Is(err, matrix.ErrRateLimited) {
		t.Fatalf("err = %v, want rate limited", err)
	}
}

func TestMalformedFilesAreRefusedWithoutQuotingThem(t *testing.T) {
	cases := map[string]struct{ name, content string }{
		"token with a space":   {tokenName, "abc def"},
		"token with a newline": {tokenName, "abcdef\n"},
		"empty token":          {tokenName, ""},
		"token over the cap":   {tokenName, strings.Repeat("x", maxTokenBytes+1)},
		"token not ASCII":      {tokenName, "tokén"},
		"password not hex":     {passwordName, strings.Repeat("zz", passwordBytes)},
		"password upper hex":   {passwordName, strings.Repeat("AB", passwordBytes)},
		"password short":       {passwordName, strings.Repeat("ab", passwordBytes-1)},
	}
	for label, c := range cases {
		t.Run(label, func(t *testing.T) {
			w := newWorld(t)
			w.write(c.name, c.content)
			_, err := w.open(w.key())
			notRefused(t, err)
			if !strings.Contains(err.Error(), c.name) {
				t.Errorf("err = %v, want it to name %s", err, c.name)
			}
		})
	}
}

func TestATokenAtTheCapIsTaken(t *testing.T) {
	w := newWorld(t)
	w.write(tokenName, strings.Repeat("x", maxTokenBytes))
	if _, err := w.open(""); err != nil {
		t.Fatalf("Open: %v", err)
	}
}

func TestFilesThatAreNotRegularAreRefused(t *testing.T) {
	for _, name := range []string{tokenName, passwordName} {
		t.Run(name+" link", func(t *testing.T) {
			w := newWorld(t)
			target := filepath.Join(t.TempDir(), "elsewhere")
			if err := os.WriteFile(target, []byte(strings.Repeat("ab", passwordBytes)), 0o600); err != nil {
				t.Fatalf("WriteFile: %v", err)
			}
			if err := os.Symlink(target, w.path(name)); err != nil {
				t.Fatalf("Symlink: %v", err)
			}
			_, err := w.open(w.key())
			notRefused(t, err)
		})
		t.Run(name+" fifo", func(t *testing.T) {
			w := newWorld(t)
			if err := unix.Mkfifo(w.path(name), 0o600); err != nil {
				t.Fatalf("Mkfifo: %v", err)
			}
			_, err := w.open(w.key())
			notRefused(t, err)
			if !strings.Contains(err.Error(), "not a regular file") {
				t.Errorf("err = %v", err)
			}
		})
	}
}

func TestTheTeamDirectoryMustBeTheChildsOwnAnd0700(t *testing.T) {
	cases := map[string]func(w *world){
		"absent": func(w *world) {
			if err := os.Remove(w.dir); err != nil {
				w.t.Fatalf("Remove: %v", err)
			}
		},
		"mode 0750": func(w *world) { w.mkdir(w.dir, 0o750) },
		"mode 0711": func(w *world) { w.mkdir(w.dir, 0o711) },
		"a link": func(w *world) {
			target := w.dir + ".real"
			w.mkdir(target, 0o700)
			if err := os.Remove(w.dir); err != nil {
				w.t.Fatalf("Remove: %v", err)
			}
			if err := os.Symlink(target, w.dir); err != nil {
				w.t.Fatalf("Symlink: %v", err)
			}
		},
		"a file": func(w *world) {
			if err := os.Remove(w.dir); err != nil {
				w.t.Fatalf("Remove: %v", err)
			}
			if err := os.WriteFile(w.dir, nil, 0o600); err != nil {
				w.t.Fatalf("WriteFile: %v", err)
			}
		},
		"another owner": func(w *world) {
			own := geteuid
			geteuid = func() int { return own() + 1 }
			w.t.Cleanup(func() { geteuid = own })
		},
	}
	for label, prepare := range cases {
		t.Run(label, func(t *testing.T) {
			w := newWorld(t)
			prepare(w)
			_, err := w.open(w.key())
			notRefused(t, err)
			if !strings.Contains(err.Error(), w.dir) {
				t.Errorf("err = %v, want it to name the directory", err)
			}
			if len(w.hs.Requests()) != 0 {
				t.Error("the server was called before the directory was judged")
			}
		})
	}
}

func TestAPortOutsideTheRangeIsRefusedBeforeAnyFile(t *testing.T) {
	for _, port := range []int{0, 65536} {
		w := newWorld(t)
		w.port = port
		if _, err := w.open(w.key()); err == nil {
			t.Errorf("port %d: Open succeeded", port)
		}
		if got := w.entries(); len(got) != 0 {
			t.Errorf("port %d: wrote %v", port, got)
		}
	}
}

func TestTheAdminMakerFailingFails(t *testing.T) {
	w := newWorld(t)
	boom := errors.New("no client")
	w.cfg.Admin = func(string) (*matrix.Admin, error) { return nil, boom }
	if _, err := w.open(""); !errors.Is(err, boom) {
		t.Fatalf("err = %v", err)
	}
}

type failingReader struct{}

func (failingReader) Read([]byte) (int, error) { return 0, io.ErrUnexpectedEOF }

func TestARandomSourceThatFailsWritesNothing(t *testing.T) {
	w := newWorld(t)
	w.cfg.Rand = failingReader{}
	if _, err := w.open(w.key()); !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatalf("err = %v", err)
	}
	if got := w.entries(); len(got) != 0 {
		t.Errorf("wrote %v", got)
	}
}

// shortReader gives n bytes, then fails: the password is drawn, the temporary name is not.
type shortReader struct{ n int }

func (r *shortReader) Read(p []byte) (int, error) {
	if r.n == 0 {
		return 0, io.ErrUnexpectedEOF
	}
	k := min(len(p), r.n)
	for i := range k {
		p[i] = 'a'
	}
	r.n -= k
	return k, nil
}

func TestARandomSourceThatFailsForTheTemporaryNameWritesNothing(t *testing.T) {
	w := newWorld(t)
	w.cfg.Rand = &shortReader{n: passwordBytes}
	if _, err := w.open(w.key()); !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatalf("err = %v", err)
	}
	if got := w.entries(); len(got) != 0 {
		t.Errorf("wrote %v", got)
	}
}

func openTestDir(t *testing.T, w *world) *dir {
	t.Helper()
	d, err := openDir(w.dir)
	if err != nil {
		t.Fatalf("openDir: %v", err)
	}
	t.Cleanup(func() {
		if err := d.close(); err != nil {
			t.Errorf("close: %v", err)
		}
	})
	return d
}

func TestLinkNeverReplacesAnExistingName(t *testing.T) {
	w := newWorld(t)
	w.write(tokenName, "original")
	d := openTestDir(t, w)
	err := d.create(tokenName, []byte("replacement"), w.cfg.Rand)
	if !errors.Is(err, unix.EEXIST) {
		t.Fatalf("create over a name: %v, want EEXIST", err)
	}
	if got := w.text(tokenName); got != "original" {
		t.Errorf("admin.token = %q", got)
	}
	if got := w.entries(); !slices.Equal(got, []string{tokenName}) {
		t.Errorf("a temporary file was left: %v", got)
	}
}

func TestATemporaryNameThatIsTakenFails(t *testing.T) {
	w := newWorld(t)
	d := openTestDir(t, w)
	seed := [32]byte{9}
	name, err := tempName(tokenName, rand.NewChaCha8(seed))
	if err != nil {
		t.Fatalf("tempName: %v", err)
	}
	w.write(name, "")
	if err := d.create(tokenName, []byte("x"), rand.NewChaCha8(seed)); !errors.Is(err, unix.EEXIST) {
		t.Fatalf("err = %v, want EEXIST", err)
	}
	if w.has(tokenName) {
		t.Error("the name was linked")
	}
}

func TestTheMemberMakerReachesTheSameServerAsAMember(t *testing.T) {
	w := newWorld(t)
	o := w.mustOpen(w.key())
	bob := w.hs.AddUser("bob", "", false)
	token, err := w.hs.MintToken(bob)
	if err != nil {
		t.Fatalf("MintToken: %v", err)
	}
	c, err := o.Member(token)
	if err != nil {
		t.Fatalf("Member: %v", err)
	}
	id, err := c.Whoami(t.Context())
	if err != nil {
		t.Fatalf("Whoami: %v", err)
	}
	if id.UserID != bob {
		t.Errorf("whoami = %s", id.UserID)
	}
	if _, err := o.Member(""); err == nil {
		t.Error("a member client without a token was made")
	}
}

func TestTheOpenerIsOpenAndTheDefaultsAreTheRealOnes(t *testing.T) {
	w := newWorld(t)
	cfg := Config{Root: w.cfg.Root}
	o, err := cfg.Opener()(t.Context(), w.team, w.port, w.key())
	if err != nil {
		w.errs = append(w.errs, err)
		t.Fatalf("Opener: %v", err)
	}
	w.bootstrapped(o)
	if o.Now == nil {
		t.Error("the default clock is nil")
	}
}
