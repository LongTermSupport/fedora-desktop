package host

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"io"
	"io/fs"
	"net"
	"net/netip"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/declare"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/teamlock"
)

var errFault = errors.New("injected fault")

// t0 is the fake clock's start: a fixed instant, never the wall clock.
var t0 = time.UnixMilli(1_760_000_000_000)

// fakeClock is a clock whose Sleep only moves its time forward.
type fakeClock struct {
	now   time.Time
	slept []time.Duration
}

func newClock() *fakeClock { return &fakeClock{now: t0} }

func (c *fakeClock) Now() time.Time { return c.now }

func (c *fakeClock) Sleep(ctx context.Context, d time.Duration) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	c.now = c.now.Add(d)
	c.slept = append(c.slept, d)
	return nil
}

func mustTeam(t *testing.T, s string) paths.TeamID {
	t.Helper()
	team, err := paths.ParseTeamID(s)
	if err != nil {
		t.Fatal(err)
	}
	return team
}

func readFile(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func releaseOrFail(t *testing.T, l *teamlock.Lock) {
	t.Helper()
	if err := l.Release(); err != nil {
		t.Error(err)
	}
}

// world is one host under a temporary root: the installed directories, the base unit, and a
// fake for every seam, sharing one call log.
type world struct {
	t        *testing.T
	root     string
	clock    *fakeClock
	log      []string
	faults   map[string]error
	sd       *fakeSystemd
	fw       *fakeFirewall
	net      *fakeNetwork
	ready    *fakeReady
	status   *fakeStatus
	approved map[string]*declare.Gated
	cur      declare.Input
	version  string
	// memory is the pin's memory ceiling when a test changes it, a reload-set change.
	memory   string
	boot     func(Bootstrap) []string
	boots    []Bootstrap
	backups  int
	onBackup func()
	guarded  []paths.TeamID
	guardErr error
	// rand is the randomness every apply of this host draws on: the registration key's 64 bytes,
	// then an endless stream that never repeats, so each key and serial number a later step makes
	// is its own.
	rand io.Reader
}

// stream is an endless deterministic byte source that never repeats: the SHA-256 of a counter
// that starts at seed.
type stream struct {
	seed, n uint64
	buf     []byte
}

func (s *stream) Read(p []byte) (int, error) {
	for len(s.buf) < len(p) {
		sum := sha256.Sum256(binary.BigEndian.AppendUint64(binary.BigEndian.AppendUint64(nil, s.seed), s.n))
		s.n++
		s.buf = append(s.buf, sum[:]...)
	}
	n := copy(p, s.buf)
	s.buf = s.buf[n:]
	return n, nil
}

func newWorld(t *testing.T) *world {
	t.Helper()
	w := &world{t: t, root: t.TempDir(), clock: newClock(), faults: map[string]error{}, approved: map[string]*declare.Gated{}}
	w.rand = io.MultiReader(bytes.NewReader(bytes.Repeat([]byte{0xab}, 64)), &stream{})
	for _, dir := range []string{
		"/etc/systemd/system", "/run", paths.AdminStateRoot, paths.CtlStateRoot, paths.TLSStateRoot, paths.StatusRoot, paths.HomeserverPrivateStateRoot,
	} {
		if err := os.MkdirAll(w.at(dir), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	w.write(paths.ServerUnitTemplateFile, "[Unit]\nDescription=v2 team homeserver %i\n")
	w.write(paths.TLSUnitTemplateFile, "[Unit]\nDescription=v2 team TLS terminator %i\n")
	w.sd = &fakeSystemd{w: w, units: map[string]*UnitState{}}
	w.fw = &fakeFirewall{w: w, present: map[bool]map[string]bool{true: {}, false: {}}, zone: "public"}
	w.net = &fakeNetwork{w: w, ifaces: []Interface{
		{Name: "lo", Addrs: []netip.Addr{netip.MustParseAddr("127.0.0.1")}},
		{Name: "eth0", Addrs: []netip.Addr{netip.MustParseAddr("192.0.2.10")}},
		{Name: "lxcbr0", Addrs: []netip.Addr{netip.MustParseAddr("198.51.100.1")}},
	}, bridge: netip.MustParsePrefix("198.51.100.0/24"), routes: []fakeRoute{
		{netip.MustParsePrefix("192.0.2.0/24"), "eth0"},
		{netip.MustParsePrefix("203.0.113.0/24"), "eth0"},
		{netip.MustParsePrefix("198.51.100.0/24"), "lxcbr0"},
		{netip.MustParsePrefix("0.0.0.0/0"), "eth0"},
	}}
	w.ready = &fakeReady{w: w}
	w.status = &fakeStatus{w: w}
	w.boot = func(Bootstrap) []string { return nil }
	return w
}

func (w *world) Load(team paths.TeamID) (*declare.Gated, error) {
	if err := w.call("approvals.load"); err != nil {
		return nil, err
	}
	return w.approved[team.String()], nil
}

func (w *world) at(p string) string { return w.root + p }

func (w *world) write(p, content string) {
	w.t.Helper()
	if err := os.WriteFile(w.at(p), []byte(content), 0o600); err != nil {
		w.t.Fatal(err)
	}
}

func (w *world) read(p string) string {
	w.t.Helper()
	return string(readFile(w.t, w.at(p)))
}

func (w *world) exists(p string) bool {
	w.t.Helper()
	_, err := os.Lstat(w.at(p))
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		w.t.Fatal(err)
	}
	return err == nil
}

// call logs one seam call and answers its injected fault.
func (w *world) call(name string) error {
	w.log = append(w.log, name)
	return w.faults[name]
}

func (w *world) called(name string) bool { return slices.Contains(w.log, name) }

// approve approves the input's security fields, as the owner would.
func (w *world) approve(in declare.Input) {
	g := in.Gated()
	w.approved[in.ID().String()] = &g
}

// apply runs Apply over in with a fresh log.
func (w *world) apply(in declare.Input) ([]string, error) {
	w.cur, w.log = in, nil
	return Apply(w.t.Context(), w.deps(), in)
}

func (w *world) deps() Deps {
	p := pin(w.t)
	if w.version != "" {
		p.Version = mustVersion(w.t, w.version)
	}
	if w.memory != "" {
		p.MemoryHigh = w.memory
	}
	return Deps{
		Root: w.root, LockDir: w.at(paths.AdminRunDir), LockWait: PlayWait, Clock: w.clock,
		Rand: w.rand, Pin: p, Software: "d1g3st",
		CtlUID: os.Getuid(), CtlGID: os.Getgid(), StateOwnerUID: self(w.t), Approvals: w, Systemd: w.sd, Firewall: w.fw,
		Network: w.net, Ready: w.ready, Status: w.status,
		Guard: func(team paths.TeamID) error {
			w.guarded = append(w.guarded, team)
			return w.guardErr
		},
		Backup: func(_ context.Context, held *teamlock.Lock) error {
			if held == nil {
				w.t.Error("the backup was not given the held lock")
			}
			w.backups++
			if w.onBackup != nil {
				w.onBackup()
			}
			return w.call("backup")
		},
		Bootstrap: func(_ context.Context, b Bootstrap) ([]string, error) {
			w.boots = append(w.boots, b)
			lines := w.boot(b)
			return lines, w.call("bootstrap")
		},
	}
}

// fakeSystemd keeps, as systemd does, the drop-in it loaded at its last daemon-reload ("" for
// none) apart from the one on disk, and runs the loaded one on a start or restart.
type fakeSystemd struct {
	w               *world
	units           map[string]*UnitState
	startTo         string
	journal         string
	activating      int
	loaded, running string
	// started and stopped are the units each Start or Restart, and each Stop, named, in order.
	started, stopped []string
}

func (s *fakeSystemd) DaemonReload(context.Context) error {
	if err := s.w.call("daemon-reload"); err != nil {
		return err
	}
	raw, err := os.ReadFile(s.w.at(paths.ServerDropInFile(s.w.cur.ID())))
	if errors.Is(err, fs.ErrNotExist) {
		s.loaded = ""
		return nil
	}
	s.loaded = string(raw)
	return err
}

func (s *fakeSystemd) Start(_ context.Context, unit string) error {
	if err := s.w.call("start"); err != nil {
		return err
	}
	s.started = append(s.started, unit)
	s.running = s.loaded
	s.unit(unit).Active = "active"
	if s.startTo != "" {
		s.unit(unit).Active = s.startTo
	}
	return nil
}

func (s *fakeSystemd) Restart(_ context.Context, unit string) error {
	if err := s.w.call("restart"); err != nil {
		return err
	}
	s.started = append(s.started, unit)
	s.running = s.loaded
	s.unit(unit).Active = "active"
	return nil
}

func (s *fakeSystemd) Stop(_ context.Context, unit string) error {
	if err := s.w.call("stop"); err != nil {
		return err
	}
	s.stopped = append(s.stopped, unit)
	s.unit(unit).Active = "inactive"
	return nil
}

func (s *fakeSystemd) Enable(_ context.Context, unit string) error {
	if err := s.w.call("enable"); err != nil {
		return err
	}
	s.unit(unit).Enabled = true
	return nil
}

func (s *fakeSystemd) Disable(_ context.Context, unit string) error {
	if err := s.w.call("disable"); err != nil {
		return err
	}
	s.unit(unit).Enabled = false
	return nil
}

func (s *fakeSystemd) State(_ context.Context, unit string) (UnitState, error) {
	if err := s.w.call("state"); err != nil {
		return UnitState{}, err
	}
	u := s.unit(unit)
	if u.Active == "active" && s.activating > 0 {
		s.activating--
		return UnitState{Active: "activating", Enabled: u.Enabled}, nil
	}
	return *u, nil
}

func (s *fakeSystemd) JournalTail(context.Context, string) (string, error) {
	return s.journal, s.w.call("journal")
}

func (s *fakeSystemd) CleanState(context.Context, string) error { return s.w.call("clean") }

func (s *fakeSystemd) unit(name string) *UnitState {
	if s.units[name] == nil {
		s.units[name] = &UnitState{Active: "inactive"}
	}
	return s.units[name]
}

type fakeFirewall struct {
	w       *world
	present map[bool]map[string]bool
	zone    string
	onAdd   func(Rule)
	onDrop  func(Rule)
}

func (f *fakeFirewall) Zone(context.Context, netip.Prefix) (string, error) {
	return f.zone, f.w.call("zone")
}

func (f *fakeFirewall) Query(_ context.Context, permanent bool, r Rule) (bool, error) {
	if err := f.w.call("query"); err != nil {
		return false, err
	}
	return f.present[permanent][r.String()], nil
}

func (f *fakeFirewall) Add(_ context.Context, permanent bool, r Rule) error {
	if f.onAdd != nil {
		f.onAdd(r)
	}
	if err := f.w.call("add"); err != nil {
		return err
	}
	f.present[permanent][r.String()] = true
	return nil
}

func (f *fakeFirewall) Remove(_ context.Context, permanent bool, r Rule) error {
	if f.onDrop != nil {
		f.onDrop(r)
	}
	if err := f.w.call("remove"); err != nil {
		return err
	}
	delete(f.present[permanent], r.String())
	return nil
}

// fakeRoute is a route of the fake's main table: the prefix and the interface that carries it.
type fakeRoute struct {
	prefix netip.Prefix
	iface  string
}

type fakeNetwork struct {
	w      *world
	ifaces []Interface
	extra  []Listener
	bound  []Listener
	// boundTLS is what the terminator's unit binds when a test sets it.
	boundTLS []Listener
	bridge   netip.Prefix
	routes   []fakeRoute
}

// Route answers as the production adapter does: the interface that holds a host prefix, else
// the longest route that covers the network, the route of length 0 being the default.
func (n *fakeNetwork) Route(network netip.Prefix) (Route, bool, error) {
	if err := n.w.call("route"); err != nil {
		return Route{}, false, err
	}
	if network.IsSingleIP() {
		for _, i := range n.ifaces {
			if slices.Contains(i.Addrs, network.Addr()) {
				return Route{Interface: i.Name}, true, nil
			}
		}
	}
	best, found := fakeRoute{}, false
	for _, r := range n.routes {
		if r.prefix.Bits() <= network.Bits() && r.prefix.Contains(network.Addr()) && (!found || r.prefix.Bits() > best.prefix.Bits()) {
			best, found = r, true
		}
	}
	return Route{Interface: best.iface, Default: found && best.prefix.Bits() == 0}, found, nil
}

func (n *fakeNetwork) Interfaces() ([]Interface, error) { return n.ifaces, n.w.call("interfaces") }

// Listeners are the extra listeners, and while the current team's unit is active the addresses
// it binds: loopback and its listen addresses, or bound when a test sets it.
func (n *fakeNetwork) Listeners() ([]Listener, error) {
	out := slices.Clone(n.extra)
	unit := paths.ServerUnit(n.w.cur.ID())
	if st := n.w.sd.units[unit]; st != nil && st.Active == "active" {
		if n.bound != nil {
			out = append(out, n.bound...)
		} else {
			out = append(out, n.bindings(unit)...)
		}
	}
	tls := paths.TLSUnit(n.w.cur.ID())
	if st := n.w.sd.units[tls]; st != nil && st.Active == "active" {
		if n.boundTLS != nil {
			out = append(out, n.boundTLS...)
		} else {
			out = append(out, n.terminated(tls)...)
		}
	}
	return out, n.w.call("listeners")
}

func listener(addr string, port int, unit string) Listener {
	return Listener{Addr: netip.MustParseAddrPort(net.JoinHostPort(addr, strconv.Itoa(port))), Unit: unit}
}

func (n *fakeNetwork) Bridge() (netip.Prefix, error) { return n.bridge, n.w.call("bridge") }

var execListen = regexp.MustCompile(`--listen (\S+)`)

// terminated are what the terminator's unit binds: every --listen address of its installed
// drop-in's ExecStart, on the team's port, as the verb binds them.
func (n *fakeNetwork) terminated(unit string) []Listener {
	found := execListen.FindAllSubmatch(readFile(n.w.t, n.w.at(paths.TLSDropInFile(n.w.cur.ID()))), -1)
	out := make([]Listener, 0, len(found))
	for _, m := range found {
		out = append(out, listener(string(m[1]), n.w.cur.Team.Port, unit))
	}
	return out
}

var configAddress = regexp.MustCompile(`(?m)^address = \[(.*)\]$`)

// bindings are what the current team's unit binds: every address of the installed config's
// `address` line, as Tuwunel binds them.
func (n *fakeNetwork) bindings(unit string) []Listener {
	m := configAddress.FindSubmatch(readFile(n.w.t, n.w.at(paths.ServerConfig(n.w.cur.ID()))))
	if m == nil {
		n.w.t.Fatal("the installed config has no address line")
	}
	out := make([]Listener, 0, bytes.Count(m[1], []byte(","))+1)
	for quoted := range strings.SplitSeq(string(m[1]), ", ") {
		out = append(out, listener(strings.Trim(quoted, `"`), n.w.cur.Team.Port, unit))
	}
	return out
}

type fakeReady struct {
	w     *world
	fails int
}

func (r *fakeReady) ServerVersion(context.Context, int) error {
	if err := r.w.call("ready"); err != nil {
		return err
	}
	if r.fails > 0 {
		r.fails--
		return errors.New("connection refused")
	}
	return nil
}

type fakeStatus struct {
	w          *world
	refreshed  []bool
	removeLine string
	events     []string
}

func (s *fakeStatus) Refresh(_ context.Context, _ paths.TeamID, first bool, events []string) error {
	s.refreshed = append(s.refreshed, first)
	s.events = append(s.events, events...)
	return s.w.call("status.refresh")
}

// Remove answers removeLine once, as the record's writer does: a second remove finds nothing.
func (s *fakeStatus) Remove(context.Context, paths.TeamID) (string, error) {
	if err := s.w.call("status.remove"); err != nil {
		return "", err
	}
	line := s.removeLine
	s.removeLine = ""
	return line, nil
}
