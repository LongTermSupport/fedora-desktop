package lxc

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"net/netip"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/apply"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/declare"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/host/lxc/guestbus"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/host/lxc/guestroot"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/host/lxc/lxccmd"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/host/lxc/lxcstate"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/guest"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
)

// The fixture guest's facts: one agent user in a guest whose /bin is a link to usr/bin (EL9).
const (
	agentUID  = 1001
	agentGID  = 1001
	checkout  = "/srv/agent1/src/repo"
	kitDigest = "0123456789abcdef"
	oldDigest = "fedcba9876543210"
	runID     = "00112233aabbccdd"
	agentAddr = "192.0.2.5"
	allowNet  = "192.0.2.0/24"
)

func mustTeam(t *testing.T, s string) paths.TeamID {
	t.Helper()
	id, err := paths.ParseTeamID(s)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

// agent1 is the fixture's one guest user.
func agent1(t *testing.T) paths.GuestUser {
	t.Helper()
	u, err := paths.ParseGuestUser("agent1")
	if err != nil {
		t.Fatal(err)
	}
	return u
}

func mustBox(t *testing.T, s string) paths.ContainerName {
	t.Helper()
	c, err := paths.ParseContainerName(s)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func mustDigest(s string) paths.Digest {
	d, err := paths.ParseDigest(s)
	if err != nil {
		panic(err)
	}
	return d
}

// writeTree writes files (path to content) under dir, each directory 0755; a content starting
// "->" is a symbolic link to the rest.
func writeTree(t *testing.T, dir string, files map[string]string) {
	t.Helper()
	for p, content := range files {
		full := filepath.Join(dir, p)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if target, ok := strings.CutPrefix(content, "->"); ok {
			if err := os.Symlink(target, full); err != nil {
				t.Fatal(err)
			}
			continue
		}
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

// newGuest is a fixture guest root tree with the agent user agent1, its checkout, systemd and an
// EL9-shaped tmux.
func newGuest(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.Chmod(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeTree(t, dir, map[string]string{
		"etc/passwd":                      "root:x:0:0:root:/root:/bin/bash\nagent1:x:1001:1001::/srv/agent1:/bin/bash\n",
		"etc/group":                       "root:x:0:\nagents:x:1001:\n",
		"usr/lib/systemd/systemd":         "systemd",
		"usr/bin/tmux":                    "tmux",
		"bin":                             "->usr/bin",
		"srv/agent1/src/repo/.git/config": "[remote \"origin\"]\n\turl = https://github.com/example/repo.git\n",
	})
	return dir
}

// fakeRoot is a fixture guest root whose init is its own systemd, or exe when set.
type fakeRoot struct {
	*guestroot.Root

	exe *guestroot.Info
}

func (r fakeRoot) InitExe() (guestroot.Info, error) {
	if r.exe != nil {
		return *r.exe, nil
	}
	return r.Stat("/usr/lib/systemd/systemd")
}

// box is one fake container.
type box struct {
	state lxccmd.State
	dir   string
	addrs []netip.Addr
	cfg   map[string]string
	exe   *guestroot.Info
}

type guestCall struct {
	Container, User, Verb string
	Args                  []string
	Stdin                 string
}

// fakeLXC is the containers: scripted states, and guest ops that record their calls and answer
// by answer.
type fakeLXC struct {
	mu     sync.Mutex
	boxes  map[string]*box
	calls  []guestCall
	answer func(c guestCall) error
	// onState runs before each State answer, so a test can stop a container mid-phase.
	onState func(c string)
	states  int
	listErr error
	log     *[]string
}

func (f *fakeLXC) Containers(context.Context) ([]string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.listErr != nil {
		return nil, f.listErr
	}
	out := make([]string, 0, len(f.boxes))
	for n := range f.boxes {
		out = append(out, n)
	}
	slices.Sort(out)
	return out, nil
}

func (f *fakeLXC) State(_ context.Context, c string) (lxccmd.State, error) {
	f.mu.Lock()
	f.states++
	hook := f.onState
	f.mu.Unlock()
	if hook != nil {
		hook(c)
	}
	b, err := f.get(c)
	if err != nil {
		return "", err
	}
	return b.state, nil
}

func (f *fakeLXC) InitPID(_ context.Context, c string) (int, error) {
	b, err := f.get(c)
	if err != nil {
		return 0, err
	}
	if b.state != lxccmd.Running {
		return 0, fmt.Errorf("%w: lxc-info printed no pid", lxccmd.ErrOutput)
	}
	return 100, nil
}

func (f *fakeLXC) Addresses(_ context.Context, c string) ([]netip.Addr, error) {
	b, err := f.get(c)
	if err != nil {
		return nil, err
	}
	return b.addrs, nil
}

func (f *fakeLXC) Config(_ context.Context, c string, keys ...string) (map[string]string, error) {
	b, err := f.get(c)
	if err != nil {
		return nil, err
	}
	out := map[string]string{}
	for _, k := range keys {
		out[k] = b.cfg[k]
	}
	return out, nil
}

func (f *fakeLXC) Guest(_ context.Context, a lxccmd.Attach, op guest.Op, args []string, stdin []byte) (lxccmd.GuestResult, error) {
	c := guestCall{Container: a.Container, User: a.User.String(), Verb: op.Verb, Args: args, Stdin: string(stdin)}
	f.mu.Lock()
	f.calls = append(f.calls, c)
	if f.log != nil {
		*f.log = append(*f.log, op.Verb)
	}
	answer := f.answer
	f.mu.Unlock()
	if a.Kit.String() != paths.HostKitDir(mustDigest(kitDigest)).String() || a.UID != agentUID || a.Home != "/srv/agent1" {
		return lxccmd.GuestResult{}, fmt.Errorf("attach with kit %s uid %d home %s", a.Kit, a.UID, a.Home)
	}
	if answer != nil {
		if err := answer(c); err != nil {
			return lxccmd.GuestResult{}, err
		}
	}
	return lxccmd.GuestResult{}, nil
}

func (f *fakeLXC) get(c string) (*box, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	b, ok := f.boxes[c]
	if !ok {
		return nil, fmt.Errorf("lxc-info: container %s does not exist", c)
	}
	return b, nil
}

// verbs is each guest op run, as "<container> <verb>".
func (f *fakeLXC) verbs() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]string, 0, len(f.calls))
	for _, c := range f.calls {
		out = append(out, c.Container+" "+c.Verb)
	}
	return out
}

// fakeRoster is the child: a roster by team, and the joins and parks it was asked for.
type fakeRoster struct {
	mu      sync.Mutex
	roster  map[paths.TeamID]map[string]string
	joins   []string
	parks   []string
	joinErr error
	// membersErr and parkErr fail those calls; rename is a handle Join answers instead.
	membersErr, parkErr error
	rename              string
	log                 *[]string
}

func (f *fakeRoster) Members(_ context.Context, team paths.TeamID) (map[string]string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.membersErr != nil {
		return nil, f.membersErr
	}
	out := map[string]string{}
	maps.Copy(out, f.roster[team])
	return out, nil
}

func (f *fakeRoster) Join(_ context.Context, team paths.TeamID, j Join) (Joined, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.joinErr != nil {
		return Joined{}, f.joinErr
	}
	handle := j.Handle
	if j.Op == apply.OpMemberAdd {
		h, ok := j.Prefix.Numbered(1)
		if !ok {
			return Joined{}, errors.New("no handle")
		}
		handle = h.String()
	}
	if f.rename != "" {
		handle = f.rename
	}
	f.joins = append(f.joins, j.Op+" "+handle)
	if f.roster[team] == nil {
		f.roster[team] = map[string]string{}
	}
	f.roster[team][handle] = apply.MemberActive
	f.note("join " + handle)
	return Joined{Handle: handle, Bundle: []byte(`{"bundle":"` + handle + `"}`)}, nil
}

func (f *fakeRoster) Park(_ context.Context, team paths.TeamID, handle string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.parkErr != nil {
		return f.parkErr
	}
	f.parks = append(f.parks, team.String()+" "+handle)
	f.roster[team][handle] = apply.MemberParked
	f.note("park " + handle)
	return nil
}

func (f *fakeRoster) Reach(_ context.Context, team paths.TeamID) ([]byte, error) {
	return []byte(`{"v":1,"team":"` + team.String() + `"}`), nil
}

func (f *fakeRoster) note(s string) {
	if f.log != nil {
		*f.log = append(*f.log, s)
	}
}

// fakeBus is the guest's systemd, recording its calls.
type fakeBus struct {
	mu    sync.Mutex
	calls []string
	err   error
	log   *[]string
}

func (b *fakeBus) Reload(context.Context) error { return b.call("Reload", paths.GuestUser{}) }

func (b *fakeBus) Start(_ context.Context, u paths.GuestUser) error { return b.call("Start", u) }

func (b *fakeBus) Restart(_ context.Context, u paths.GuestUser) error { return b.call("Restart", u) }

func (b *fakeBus) Stop(_ context.Context, u paths.GuestUser) error { return b.call("Stop", u) }

func (b *fakeBus) ResetFailed(_ context.Context, u paths.GuestUser) error {
	return b.call("ResetFailed", u)
}

func (b *fakeBus) Close() error { return nil }

func (b *fakeBus) call(name string, u paths.GuestUser) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	s := name
	if u.String() != "" {
		s += " " + u.String()
	}
	b.calls = append(b.calls, s)
	if b.log != nil {
		*b.log = append(*b.log, "systemd "+s)
	}
	return b.err
}

var _ guestbus.Systemd = (*fakeBus)(nil)

// fakeClock is a clock whose Sleep advances it, counts, and runs onSleep.
type fakeClock struct {
	mu      sync.Mutex
	now     time.Time
	sleeps  int
	onSleep func()
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *fakeClock) Sleep(_ context.Context, d time.Duration) error {
	c.mu.Lock()
	c.now = c.now.Add(d)
	c.sleeps++
	hook := c.onSleep
	c.mu.Unlock()
	if hook != nil {
		hook()
	}
	return nil
}

type fakeApprovals map[paths.TeamID]*declare.Gated

func (a fakeApprovals) Load(team paths.TeamID) (*declare.Gated, error) { return a[team], nil }

type fakeV1 struct {
	held  bool
	calls int
}

func (v *fakeV1) Holds(context.Context, Root, paths.GuestUser, paths.TeamID) (bool, error) {
	v.calls++
	return v.held, nil
}

// world is one host, its containers and the child, from which Deps is built.
type world struct {
	t         *testing.T
	host      string
	lxc       *fakeLXC
	roster    *fakeRoster
	bus       *fakeBus
	clock     *fakeClock
	v1        *fakeV1
	origin    string
	decl      declare.Declaration
	approvals fakeApprovals
	refresh   []string
	log       []string
	busErr    error
	pinErr    error
	members   []Target
}

func newWorld(t *testing.T) *world {
	t.Helper()
	host := t.TempDir()
	for _, d := range []string{paths.StatusRoot, paths.AdminStateRoot, "lock"} {
		if err := os.MkdirAll(filepath.Join(host, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	writeTree(t, host, map[string]string{"kit/bin/teamchat": "binary", "kit/plugin/hooks.json": "{}"})
	w := &world{
		t: t, host: host, origin: "v2",
		lxc:    &fakeLXC{boxes: map[string]*box{}},
		roster: &fakeRoster{roster: map[paths.TeamID]map[string]string{}},
		clock:  &fakeClock{now: time.UnixMilli(1_700_000_000_000)},
		v1:     &fakeV1{},
	}
	w.bus = &fakeBus{log: &w.log}
	w.lxc.log = &w.log
	w.roster.log = &w.log
	return w
}

// addBox adds a running container with a fresh fixture guest.
func (w *world) addBox(name string) *box {
	b := &box{
		state: lxccmd.Running, dir: newGuest(w.t),
		addrs: []netip.Addr{netip.MustParseAddr(agentAddr)},
		cfg:   map[string]string{"lxc.idmap": "", "lxc.net.0.link": "lxcbr0", "lxc.start.auto": "1"},
	}
	w.lxc.boxes[name] = b
	return b
}

func (w *world) team(id string) declare.Team {
	return declare.Team{
		ID: mustTeam(w.t, id), State: declare.StatePresent, Name: id, Port: 8448,
		URL: "http://198.51.100.1:8448", AllowFrom: []string{allowNet},
	}
}

// member declares agent1 of container c in team, present, with its service on.
func (w *world) member(team, c string) declare.LXCMember {
	return declare.LXCMember{
		Team: mustTeam(w.t, team), Container: c, User: agent1(w.t), Checkout: checkout,
		Service: true, State: declare.StatePresent,
	}
}

// declare sets the declaration: the teams and the members.
func (w *world) declare(teams []string, members ...declare.LXCMember) {
	w.decl = declare.Declaration{LXC: members}
	for _, id := range teams {
		w.decl.Teams = append(w.decl.Teams, w.team(id))
	}
	w.approvals = fakeApprovals{}
	for _, in := range w.decl.Inputs() {
		g := in.Gated()
		w.approvals[in.ID()] = &g
	}
}

func (w *world) deps() Deps {
	return Deps{
		Root:      w.host,
		LockDir:   filepath.Join(w.host, "lock"),
		Clock:     w.clock,
		LXC:       w.lxc,
		Pin:       w.pin,
		Bus:       func(context.Context, Root) (guestbus.Systemd, error) { return w.bus, w.busErr },
		Roster:    w.roster,
		Approvals: w.approvals,
		Origin:    func(context.Context, paths.TeamID) (string, error) { return w.origin, nil },
		V1:        w.v1,
		Kit:       mustDigest(kitDigest),
		HostKit:   filepath.Join(w.host, "kit"),
		HostUIDs:  map[int]bool{0: true, 1000: true},
		OwnerGID:  os.Getgid(),
		Bridge:    "lxcbr0",
		Refresh: func(_ context.Context, team paths.TeamID) error {
			w.refresh = append(w.refresh, team.String())
			return nil
		},
	}
}

func (w *world) pin(_ context.Context, c string) (Root, error) {
	if w.pinErr != nil {
		return nil, w.pinErr
	}
	b, err := w.lxc.get(c)
	if err != nil {
		return nil, err
	}
	if b.state != lxccmd.Running {
		return nil, guestroot.ErrMoved
	}
	r, err := guestroot.OpenDir(b.dir)
	if err != nil {
		return nil, err
	}
	return fakeRoot{Root: r, exe: b.exe}, nil
}

func (w *world) apply(phase Phase, only ...string) (Report, error) {
	w.t.Helper()
	req := Request{Phase: phase, Decl: w.decl, Members: w.members}
	for _, s := range only {
		req.OnlyTeams = append(req.OnlyTeams, mustTeam(w.t, s))
	}
	return Apply(context.Background(), w.deps(), req)
}

// record is team t1's record.
func (w *world) record() lxcstate.Record {
	w.t.Helper()
	r, _, err := lxcstate.ReadRecord(w.host, mustTeam(w.t, "t1"))
	if err != nil {
		w.t.Fatal(err)
	}
	return r
}

// entry is the record entry of agent1 in c, in team t1.
func (w *world) entry(c string) lxcstate.Entry {
	w.t.Helper()
	e, ok := w.record().Find(mustBox(w.t, c), agent1(w.t))
	if !ok {
		w.t.Fatalf("no record entry for %s in t1", c)
	}
	return e
}

// lines is the report's lines as text.
func lines(r Report) []string {
	out := make([]string, 0, len(r.Lines))
	for _, l := range r.Lines {
		out = append(out, l.String())
	}
	return out
}

func hasLine(t *testing.T, r Report, want string) {
	t.Helper()
	if !slices.Contains(lines(r), want) {
		t.Fatalf("no line %q in\n%s", want, strings.Join(lines(r), "\n"))
	}
}

func guestFile(t *testing.T, b *box, p string) (string, bool) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(b.dir, p))
	if errors.Is(err, os.ErrNotExist) {
		return "", false
	}
	if err != nil {
		t.Fatal(err)
	}
	return string(data), true
}
