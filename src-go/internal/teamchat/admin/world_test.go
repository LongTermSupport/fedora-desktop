package admin

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/netip"
	"os"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/apply"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/declare"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/host"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/matrix"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/matrix/fakehs"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/proto"
)

const (
	server = "alpha.teamchat.internal"
	coord  = "myrepo.1+desk.podman"
	other  = "myrepo.2+desk.podman"
	prefix = "myrepo+desk.podman"
	t0ms   = 1_760_000_000_000
)

// alphaDoc is alpha's one-team input: humans alice and bob, coordinator coord.
func alphaDoc() map[string]any {
	return map[string]any{
		"team": map[string]any{
			"team": "alpha", "state": "present", "port": 8448, "description": "The team that builds the thing.",
			"listen": []any{"192.0.2.10"}, "allow_from": []any{"192.0.2.0/24"}, "humans": []any{"alice", "bob"},
			"coordinator": coord,
			"trust": map[string]any{
				"kind": "github", "org": "example-org",
				"repos": []any{map[string]any{"repo": "example-org/thing", "branches": []any{"main"}}},
			},
		},
		"lxc_members": []any{},
		"address":     "192.0.2.10",
	}
}

// docTeam is the doc's team object, for a test to change.
func docTeam(doc map[string]any) map[string]any {
	m, ok := doc["team"].(map[string]any)
	if !ok {
		panic("fixture: no team")
	}
	return m
}

func encodeDoc(t *testing.T, doc map[string]any) string {
	t.Helper()
	raw, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

func alpha(t *testing.T) paths.TeamID {
	t.Helper()
	id, err := paths.ParseTeamID("alpha")
	if err != nil {
		t.Fatal(err)
	}
	return id
}

// world is one host under a temporary root with alpha's homeserver on fakehs: a fake for every
// host seam, the real child verb served in-process, and a crash to inject into the child's
// admin client.
type world struct {
	t       *testing.T
	root    string
	srv     *fakehs.Server
	admin   *matrix.Admin
	mu      sync.Mutex
	at      time.Time
	units   map[string]*host.UnitState
	rules   map[bool]map[string]bool
	status  []bool
	statErr error
	bundled []string
	crash   *crashing
	regKeys []string
	gidErr  error
}

func newWorld(t *testing.T) *world {
	t.Helper()
	w := &world{
		t: t, root: t.TempDir(), at: time.UnixMilli(t0ms).UTC(), units: map[string]*host.UnitState{},
		rules: map[bool]map[string]bool{true: {}, false: {}},
	}
	for _, dir := range []string{
		"/etc/systemd/system", "/run", paths.AdminStateRoot, paths.CtlStateRoot, paths.StatusRoot,
		paths.HomeserverPrivateStateRoot,
	} {
		if err := os.MkdirAll(w.root+dir, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(w.root+paths.ServerUnitTemplateFile, []byte("[Unit]\nDescription=v2 team homeserver %i\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	w.srv = fakehs.New(fakehs.Options{ServerName: server, Now: w.now, Quirks: fakehs.TuwunelQuirks()})
	if err := w.srv.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := w.srv.CheckModelled(); err != nil {
			t.Errorf("fakehs: %v", err)
		}
		if err := w.srv.Close(); err != nil {
			t.Errorf("close: %v", err)
		}
	})
	w.srv.AddUser(proto.AdminLocalpart, "pw", true)
	for _, h := range []string{coord, other} {
		w.srv.AddUser(h, "pw", false)
	}
	token, err := w.srv.MintToken(proto.AdminID(server))
	if err != nil {
		t.Fatal(err)
	}
	if w.admin, err = matrix.NewAdmin(matrix.Config{BaseURL: w.srv.URL(), Token: token, Clock: w}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(w.admin.Close)
	return w
}

// Now and Sleep are the host's clock and the matrix client's: a sleep passes no time.
func (w *world) Now() time.Time { return w.now() }

func (w *world) Sleep(ctx context.Context, _ time.Duration) error { return ctx.Err() }

// inProcessChild is the child start's test form (n2a-D6): the request through the real codecs
// and the real ctl-child verb, run as teamchat-ctl in this process.
type inProcessChild struct{ w *world }

func (c inProcessChild) Ask(ctx context.Context, req Request) (Answer, error) {
	raw, err := EncodeRequest(req)
	if err != nil {
		return Answer{}, err
	}
	var stdout, stderr bytes.Buffer
	Run(ctx, Env{
		Args: []string{"ctl-child"}, Stdin: bytes.NewReader(raw), Stdout: &stdout, Stderr: &stderr,
		Proc: childProc(), Sys: &System{Open: c.w.open},
	})
	if stderr.Len() > 0 {
		return Answer{}, errors.New(stderr.String())
	}
	return DecodeAnswer(bytes.TrimSuffix(stdout.Bytes(), []byte("\n")))
}

// Bundle is the seat side's bundle as a test reads it: the handle and the credential.
func (w *world) Bundle(_ context.Context, team declare.Team, handle string, c apply.Credential) (proto.Value, error) {
	w.bundled = append(w.bundled, handle)
	return proto.Obj(
		proto.M("team", proto.Str(team.ID.String())), proto.M("handle", proto.Str(handle)),
		proto.M("kind", proto.Str(c.Kind)), proto.M("secret", proto.Str(c.Secret)),
	), nil
}

func (w *world) Interfaces() ([]host.Interface, error) {
	return []host.Interface{
		{Name: "lo", Addrs: []netip.Addr{netip.MustParseAddr("127.0.0.1")}},
		{Name: "eth0", Addrs: []netip.Addr{netip.MustParseAddr("192.0.2.10")}},
	}, nil
}

// Listeners are alpha's loopback and listen address while its unit is active.
func (w *world) Listeners() ([]host.Listener, error) {
	unit := paths.ServerUnit(alpha(w.t))
	if u := w.units[unit]; u == nil || u.Active != "active" {
		return nil, nil
	}
	var out []host.Listener
	for _, a := range []string{"127.0.0.1", "192.0.2.10"} {
		out = append(out, host.Listener{Addr: netip.MustParseAddrPort(net.JoinHostPort(a, strconv.Itoa(8448))), Unit: unit})
	}
	return out, nil
}

func (w *world) Bridge() (netip.Prefix, error) { return netip.MustParsePrefix("198.51.100.0/24"), nil }

func (w *world) ServerVersion(context.Context, int) error { return nil }

// now moves one millisecond on every read, so each operation's since_ms is its own.
func (w *world) now() time.Time {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.at = w.at.Add(time.Millisecond)
	return w.at
}

func (w *world) sys() *System {
	return &System{
		Host: host.Deps{
			Root: w.root, LockDir: w.root + paths.AdminRunDir, Clock: w,
			Rand: bytes.NewReader(bytes.Repeat([]byte{0xab}, 64)), Pin: w.pin(), Software: digest,
			CtlUID: os.Getuid(), CtlGID: os.Getgid(),
			Approvals: declare.FileApprovals{Path: func(id paths.TeamID) string { return w.root + paths.ApprovedFile(id) }, GID: -1},
			Systemd:   &fakeSystemd{w: w}, Firewall: &fakeFirewall{w: w}, Network: w, Ready: w, Status: &fakeStatus{w: w},
			Backup: func(context.Context, *host.Lock) error { return nil },
		},
		Child:      inProcessChild{w: w},
		Bundles:    w,
		OwnerGroup: w.ownerGroup,
		Open:       w.open,
	}
}

func (w *world) pin() host.Pin {
	v, err := paths.ParseServerVersion("1.9.4")
	if err != nil {
		w.t.Fatal(err)
	}
	return host.Pin{Version: v, MemoryHigh: "512M", CacheCapacityModifier: "0.5"}
}

func (w *world) ownerGroup() (int, error) { return -1, w.gidErr }

// open is the child's reach to alpha: the admin client over fakehs, wrapped by the crash the
// test set, and the team's files under ctl/.
func (w *world) open(_ context.Context, team paths.TeamID, regKey string) (apply.Deps, error) {
	w.regKeys = append(w.regKeys, regKey)
	var a apply.Admin = w.admin
	if w.crash != nil {
		w.crash.Admin = w.admin
		a = w.crash
	}
	return apply.Deps{
		Admin: a, Now: w.now, RosterPath: w.root + paths.RosterFile(team), RoomsPath: w.root + paths.RoomsFile(team),
	}, nil
}

type fakeSystemd struct{ w *world }

func (s *fakeSystemd) DaemonReload(context.Context) error { return nil }

func (s *fakeSystemd) Start(_ context.Context, unit string) error {
	s.unit(unit).Active = "active"
	return nil
}

func (s *fakeSystemd) Restart(ctx context.Context, unit string) error { return s.Start(ctx, unit) }

func (s *fakeSystemd) Stop(_ context.Context, unit string) error {
	s.unit(unit).Active = "inactive"
	return nil
}

func (s *fakeSystemd) Enable(_ context.Context, unit string) error {
	s.unit(unit).Enabled = true
	return nil
}

func (s *fakeSystemd) Disable(_ context.Context, unit string) error {
	s.unit(unit).Enabled = false
	return nil
}

func (s *fakeSystemd) State(_ context.Context, unit string) (host.UnitState, error) {
	return *s.unit(unit), nil
}

func (s *fakeSystemd) JournalTail(context.Context, string) (string, error) { return "", nil }

func (s *fakeSystemd) CleanState(context.Context, string) error { return nil }

func (s *fakeSystemd) unit(name string) *host.UnitState {
	if s.w.units[name] == nil {
		s.w.units[name] = &host.UnitState{Active: "inactive"}
	}
	return s.w.units[name]
}

type fakeFirewall struct{ w *world }

func (f *fakeFirewall) Zone(context.Context, netip.Prefix) (string, error) { return "public", nil }

func (f *fakeFirewall) Query(_ context.Context, permanent bool, r host.Rule) (bool, error) {
	return f.w.rules[permanent][r.String()], nil
}

func (f *fakeFirewall) Add(_ context.Context, permanent bool, r host.Rule) error {
	f.w.rules[permanent][r.String()] = true
	return nil
}

func (f *fakeFirewall) Remove(_ context.Context, permanent bool, r host.Rule) error {
	delete(f.w.rules[permanent], r.String())
	return nil
}

type fakeStatus struct{ w *world }

func (s *fakeStatus) Refresh(_ context.Context, _ paths.TeamID, first bool) error {
	s.w.status = append(s.w.status, first)
	return s.w.statErr
}

func (s *fakeStatus) Remove(context.Context, paths.TeamID) (string, error) { return "", nil }

// verb runs teamchat-admin as root over the world.
func (w *world) verb(stdin string, args ...string) outcome {
	w.t.Helper()
	var stdout, stderr bytes.Buffer
	code := Run(w.t.Context(), Env{
		Args: args, Stdin: strings.NewReader(stdin), Stdout: &stdout, Stderr: &stderr,
		Proc: rootProc(), Digest: digest, Sys: w.sys(),
	})
	return outcome{code, stdout.String(), stderr.String()}
}

// crashing is the child's admin client with one call failing: the n-th call of method, its
// effect landed first when landed (the answer lost).
type crashing struct {
	apply.Admin

	method   string
	n, calls int
	landed   bool
}

var errCrash = errors.New("the process died here")

func (c *crashing) Account(ctx context.Context, id string) (matrix.Account, bool, error) {
	type found struct {
		a  matrix.Account
		ok bool
	}
	got, err := hit(c, "Account", func() (found, error) {
		a, ok, err := c.Admin.Account(ctx, id)
		return found{a, ok}, err
	})
	return got.a, got.ok, err
}

func (c *crashing) CreateAccount(ctx context.Context, id, pw, name string) (matrix.Account, error) {
	return hit(c, "CreateAccount", func() (matrix.Account, error) { return c.Admin.CreateAccount(ctx, id, pw, name) })
}

func (c *crashing) ResetPassword(ctx context.Context, id, pw string) error {
	return hitErr(c, "ResetPassword", func() error { return c.Admin.ResetPassword(ctx, id, pw) })
}

func (c *crashing) LoginDevice(ctx context.Context, name, pw, device string) (matrix.Session, error) {
	return hit(c, "LoginDevice", func() (matrix.Session, error) { return c.Admin.LoginDevice(ctx, name, pw, device) })
}

func (c *crashing) SetDeactivated(ctx context.Context, id string, off bool) error {
	return hitErr(c, "SetDeactivated", func() error { return c.Admin.SetDeactivated(ctx, id, off) })
}

func (c *crashing) Invite(ctx context.Context, room, id string) error {
	return hitErr(c, "Invite", func() error { return c.Admin.Invite(ctx, room, id) })
}

func (c *crashing) Kick(ctx context.Context, room, id, reason string) error {
	return hitErr(c, "Kick", func() error { return c.Admin.Kick(ctx, room, id, reason) })
}

func (c *crashing) PutState(ctx context.Context, room, typ, key string, content json.RawMessage) (string, error) {
	return hit(c, "PutState", func() (string, error) { return c.Admin.PutState(ctx, room, typ, key, content) })
}

func (c *crashing) Send(ctx context.Context, room, typ, txn string, content json.RawMessage) (string, error) {
	return hit(c, "Send", func() (string, error) { return c.Admin.Send(ctx, room, typ, txn, content) })
}

// hit makes call, failing it with errCrash when it is the n-th call of the crashing method.
func hit[T any](c *crashing, method string, call func() (T, error)) (T, error) {
	if method != c.method {
		return call()
	}
	c.calls++
	if c.calls != c.n {
		return call()
	}
	var zero T
	if c.landed {
		if _, err := call(); err != nil {
			return zero, err
		}
	}
	return zero, errCrash
}

func hitErr(c *crashing, method string, call func() error) error {
	_, err := hit(c, method, func() (struct{}, error) { return struct{}{}, call() })
	return err
}
