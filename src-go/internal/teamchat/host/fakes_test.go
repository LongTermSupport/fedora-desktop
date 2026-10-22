package host

import (
	"bytes"
	"context"
	"errors"
	"io/fs"
	"net"
	"net/netip"
	"os"
	"slices"
	"strconv"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/declare"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
)

var errFault = errors.New("injected fault")

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
	boot     func(Bootstrap) []string
	boots    []Bootstrap
	backups  int
	onBackup func()
}

func newWorld(t *testing.T) *world {
	t.Helper()
	w := &world{t: t, root: t.TempDir(), clock: newClock(), faults: map[string]error{}, approved: map[string]*declare.Gated{}}
	for _, dir := range []string{
		"/etc/systemd/system", "/run", paths.AdminStateRoot, paths.CtlStateRoot, paths.StatusRoot, paths.HomeserverPrivateStateRoot,
	} {
		if err := os.MkdirAll(w.at(dir), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	w.write(paths.ServerUnitTemplateFile, "[Unit]\nDescription=v2 team homeserver %i\n")
	w.sd = &fakeSystemd{w: w, units: map[string]*UnitState{}}
	w.fw = &fakeFirewall{w: w, present: map[bool]map[string]bool{true: {}, false: {}}, zone: "public"}
	w.net = &fakeNetwork{w: w, ifaces: []Interface{
		{Name: "lo", Addrs: []netip.Addr{netip.MustParseAddr("127.0.0.1")}},
		{Name: "eth0", Addrs: []netip.Addr{netip.MustParseAddr("192.0.2.10")}},
	}, bridge: netip.MustParsePrefix("198.51.100.0/24")}
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
	return Deps{
		Root: w.root, LockDir: w.at(paths.AdminRunDir), LockWait: PlayWait, Clock: w.clock,
		Rand: bytes.NewReader(bytes.Repeat([]byte{0xab}, 64)), Pin: p, Software: "d1g3st",
		CtlUID: os.Getuid(), CtlGID: os.Getgid(), Approvals: w, Systemd: w.sd, Firewall: w.fw,
		Network: w.net, Ready: w.ready, Status: w.status,
		Backup: func(_ context.Context, held *Lock) error {
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

type fakeSystemd struct {
	w          *world
	units      map[string]*UnitState
	startTo    string
	journal    string
	activating int
}

func (s *fakeSystemd) DaemonReload(context.Context) error { return s.w.call("daemon-reload") }

func (s *fakeSystemd) Start(_ context.Context, unit string) error {
	if err := s.w.call("start"); err != nil {
		return err
	}
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
	s.unit(unit).Active = "active"
	return nil
}

func (s *fakeSystemd) Stop(_ context.Context, unit string) error {
	if err := s.w.call("stop"); err != nil {
		return err
	}
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

type fakeNetwork struct {
	w      *world
	ifaces []Interface
	extra  []Listener
	bound  []Listener
	bridge netip.Prefix
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
			out = append(out, bindings(n.w.cur, unit)...)
		}
	}
	return out, n.w.call("listeners")
}

func listener(addr string, port int, unit string) Listener {
	return Listener{Addr: netip.MustParseAddrPort(net.JoinHostPort(addr, strconv.Itoa(port))), Unit: unit}
}

func bindings(in declare.Input, unit string) []Listener {
	out := []Listener{listener("127.0.0.1", in.Team.Port, unit)}
	for _, a := range in.Team.Listen {
		if a != "127.0.0.1" {
			out = append(out, listener(a, in.Team.Port, unit))
		}
	}
	return out
}

func (n *fakeNetwork) Bridge() (netip.Prefix, error) { return n.bridge, n.w.call("bridge") }

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
}

func (s *fakeStatus) Refresh(_ context.Context, _ paths.TeamID, first bool) error {
	s.refreshed = append(s.refreshed, first)
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
