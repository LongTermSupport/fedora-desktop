package host

import (
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
)

// X4, X18: the team's TLS terminator is a second unit, teamchat-tls@<team>. The homeserver binds
// loopback alone and stays plain there; everything that is a function of the effective networks
// (the listen addresses, the device units they wait for, the unit's IP filter and its socket
// bind) is the terminator's drop-in, and the leaf and its key reach it as credentials.

// wantTLSDropIn is the terminator's drop-in for the sample team.
const wantTLSDropIn = `# Rendered by teamchat-admin team apply for team ops; do not edit.
[Unit]
After=sys-subsystem-net-devices-eth0.device
Wants=sys-subsystem-net-devices-eth0.device

[Service]
ExecStart=/usr/local/bin/teamchat tls --team ops --port 8448 --listen 192.0.2.10
LoadCredential=leaf.crt:/var/lib/teamchat-admin/ops/tls/leaf.crt
LoadCredential=leaf.key:/var/lib/teamchat-admin/ops/tls/leaf.key
IPAddressDeny=any
IPAddressAllow=127.0.0.1/32 ::1/128 192.0.2.10/32 192.0.2.0/24
SocketBindDeny=any
SocketBindAllow=tcp:8448
`

func TestTheTerminatorsDropInIsGolden(t *testing.T) {
	r, err := render(spec(t, input(t, teamDoc())))
	if err != nil {
		t.Fatal(err)
	}
	if got := string(r.tlsDropIn); got != wantTLSDropIn {
		t.Errorf("drop-in:\n%s\nwant:\n%s", got, wantTLSDropIn)
	}
}

// The terminator is given every listen address, in the order they are derived, and holds the
// CA's key nowhere: only the leaf and its key are credentials.
func TestTheTerminatorIsGivenEveryListenAddressAndOnlyTheLeaf(t *testing.T) {
	s := spec(t, input(t, teamDoc()))
	s.listen = addrs("192.0.2.10", "203.0.113.9")
	s.devices = []string{deviceUnit("eth0"), deviceUnit("eth1")}
	r, err := render(s)
	if err != nil {
		t.Fatal(err)
	}
	text := string(r.tlsDropIn)
	if line := lineWith(t, text, "ExecStart="); line != "ExecStart=/usr/local/bin/teamchat tls --team ops --port 8448 --listen 192.0.2.10 --listen 203.0.113.9" {
		t.Errorf("ExecStart: %s", line)
	}
	if got := strings.Count(text, "LoadCredential="); got != 2 || strings.Contains(text, "ca.key") || strings.Contains(text, "ca.crt") {
		t.Errorf("credentials:\n%s", text)
	}
}

// X18: the homeserver binds loopback alone, whatever the networks are, and its unit's filter
// allows loopback alone: it has nothing to say to a network.
func TestTheHomeserverIsLoopbackOnly(t *testing.T) {
	s := spec(t, input(t, teamDoc()))
	s.listen = addrs("192.0.2.10", "203.0.113.9")
	s.networks = prefixes("192.0.2.0/24", "203.0.113.0/24")
	r, err := render(s)
	if err != nil {
		t.Fatal(err)
	}
	if got := lineWith(t, string(r.config), "address = "); got != `address = ["127.0.0.1"]` {
		t.Errorf("config: %s", got)
	}
	drop := string(r.dropIn)
	if got := lineWith(t, drop, "IPAddressAllow="); got != "IPAddressAllow=127.0.0.1/32 ::1/128" {
		t.Errorf("drop-in filter: %s", got)
	}
	if strings.Contains(drop, "After=") || strings.Contains(drop, "Wants=") {
		t.Errorf("the homeserver waits for a device it does not bind:\n%s", drop)
	}
}

// X4: the homeserver's own files are the same for any networks, and only the terminator's drop-in
// follows them; a network changes no digest of the homeserver's.
func TestOnlyTheTerminatorFollowsTheNetworks(t *testing.T) {
	one := spec(t, input(t, teamDoc()))
	two := spec(t, input(t, teamDoc()))
	two.listen = addrs("192.0.2.10", "203.0.113.9")
	two.networks = prefixes("192.0.2.0/24", "203.0.113.0/24")
	two.devices = []string{deviceUnit("eth0"), deviceUnit("eth1")}
	a, err := render(one)
	if err != nil {
		t.Fatal(err)
	}
	b, err := render(two)
	if err != nil {
		t.Fatal(err)
	}
	if string(a.config) != string(b.config) || string(a.dropIn) != string(b.dropIn) || !a.digests.sameRestart(b.digests) || !a.digests.sameReload(b.digests) {
		t.Error("the homeserver's files or digests follow the networks")
	}
	if string(a.tlsDropIn) == string(b.tlsDropIn) {
		t.Error("the terminator's drop-in ignores a new network")
	}
}

// startedUnits are the units each Start and Restart of a world named, in order.
func startedUnits(w *world) []string { return slices.Clone(w.sd.started) }

// X18: a first apply starts the terminator after the homeserver is ready and bound, enables it,
// and leaves its drop-in where systemd reads it.
func TestFirstApplyStartsTheTerminatorAfterTheHomeserver(t *testing.T) {
	w := newWorld(t)
	lines, err := w.apply(approved(w, teamDoc()))
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range []string{"tls drop-in written", "tls unit started", "tls unit enabled"} {
		if !slices.Contains(lines, line) {
			t.Errorf("lines %q lack %q", lines, line)
		}
	}
	team := ops(t)
	server, terminator := paths.ServerUnit(team), paths.TLSUnit(team)
	if got := startedUnits(w); !slices.Equal(got, []string{server, terminator}) {
		t.Errorf("started %q, want the homeserver then the terminator", got)
	}
	if got := w.read(paths.TLSDropInFile(team)); got != wantTLSDropIn {
		t.Errorf("installed drop-in:\n%s", got)
	}
	if mode(t, w.at(paths.TLSDropInFile(team))) != 0o644 {
		t.Error("the terminator's drop-in is not 0644")
	}
	ready := slices.Index(w.log, "ready")
	var starts []int
	for i, call := range w.log {
		if call == "start" {
			starts = append(starts, i)
		}
	}
	if len(starts) != 2 || ready < starts[0] || ready > starts[1] {
		t.Errorf("the homeserver is started, ready, then the terminator is started, not %q", w.log)
	}
}

// A second apply of the same declaration changes nothing, the terminator's unit included.
func TestSecondApplyLeavesTheTerminatorAlone(t *testing.T) {
	w := applied(t)
	was := startedUnits(w)
	lines, err := w.apply(w.cur)
	if err != nil {
		t.Fatal(err)
	}
	if len(lines) != 0 || !slices.Equal(startedUnits(w), was) {
		t.Errorf("a second apply changed %q and started %q after %q", lines, startedUnits(w), was)
	}
}

// X4, X24: a second network restarts the terminator, which has to listen there, and nothing of
// the homeserver: not its config, its drop-in or its unit.
func TestANewNetworkRestartsOnlyTheTerminator(t *testing.T) {
	w := newWorld(t)
	withSecondNIC(w)
	if _, err := w.apply(approved(w, teamDoc())); err != nil {
		t.Fatal(err)
	}
	was := len(startedUnits(w))
	wide := teamDoc()
	docTeam(wide)["networks"] = []any{"192.0.2.0/24", "203.0.113.0/24"}
	lines, err := w.apply(approved(w, wide))
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(lines, "tls drop-in written") || !slices.Contains(lines, "tls unit restarted") {
		t.Errorf("lines %q", lines)
	}
	for _, line := range lines {
		if line == "config written" || line == "drop-in written" || line == "unit restarted" {
			t.Errorf("the homeserver was touched: %q", lines)
		}
	}
	if got := startedUnits(w)[was:]; !slices.Equal(got, []string{paths.TLSUnit(ops(t))}) {
		t.Errorf("restarted %q", got)
	}
}

// X14: a leaf made again is read by the terminator only at its start, so the unit is restarted
// by the apply that made it, though its drop-in did not change.
func TestAnExpiringLeafRestartsTheTerminator(t *testing.T) {
	w := applied(t)
	end := theLeaf(t, w).NotAfter()
	w.clock.now = end.Add(-29 * 24 * time.Hour)
	was := len(startedUnits(w))
	lines, err := w.apply(w.cur)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(lines, "leaf made again: fewer than 30 days remain") || !slices.Contains(lines, "tls unit restarted") {
		t.Errorf("lines %q", lines)
	}
	if got := startedUnits(w)[was:]; !slices.Equal(got, []string{paths.TLSUnit(ops(t))}) {
		t.Errorf("restarted %q", got)
	}
}

// A stopped terminator is started again, as a stopped homeserver is.
func TestAStoppedTerminatorIsStarted(t *testing.T) {
	w := applied(t)
	w.sd.units[paths.TLSUnit(ops(t))].Active = "inactive"
	lines, err := w.apply(w.cur)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(lines, []string{"tls unit started"}) {
		t.Errorf("lines %q", lines)
	}
}

// X18, H 7.21: the terminator's bound addresses are exactly the listen set, as the homeserver's
// are loopback alone; any other is the apply's failure, at the step that names it.
func TestTheBoundAddressesOfBothUnitsAreAsserted(t *testing.T) {
	team := ops(t)
	for name, c := range map[string]struct {
		bound func(w *world)
		step  string
	}{
		"homeserver also on the network": {func(w *world) {
			w.net.bound = []Listener{listener("127.0.0.1", 8448, paths.ServerUnit(team)), listener("192.0.2.10", 8448, paths.ServerUnit(team))}
		}, StepAddresses},
		"terminator also on loopback": {func(w *world) {
			w.net.boundTLS = []Listener{listener("127.0.0.1", 8448, paths.TLSUnit(team)), listener("192.0.2.10", 8448, paths.TLSUnit(team))}
		}, StepTerminator},
		"terminator on the wrong address": {func(w *world) {
			w.net.boundTLS = []Listener{listener("198.51.100.1", 8448, paths.TLSUnit(team))}
		}, StepTerminator},
	} {
		t.Run(name, func(t *testing.T) {
			w := newWorld(t)
			c.bound(w)
			_, err := w.apply(approved(w, teamDoc()))
			if got := stepOf(t, err); got.Step != c.step {
				t.Errorf("failed at %s: %v", got.Step, err)
			}
		})
	}
}

// A host with no base unit for the terminator is refused at the render, before anything changes,
// as one with no base unit for the homeserver is.
func TestNoTerminatorBaseUnitIsRefusedBeforeAnyChange(t *testing.T) {
	w := newWorld(t)
	if gone, err := removeFile(w.at(paths.TLSUnitTemplateFile)); err != nil || !gone {
		t.Fatalf("the base unit was not removed: %v", err)
	}
	_, err := w.apply(approved(w, teamDoc()))
	if got := stepOf(t, err); got.Step != StepRender || !strings.Contains(got.Error(), paths.TLSUnitTemplateFile) {
		t.Errorf("got %v", err)
	}
	if w.exists(paths.TLSDropInFile(ops(t))) || w.exists(paths.ServerDropInFile(ops(t))) || len(startedUnits(w)) != 0 {
		t.Error("something changed before the refusal")
	}
}

// The port the terminator listens on is the team's own, so a listener of the terminator on it is
// not another process's, which the apply refuses.
func TestThePortMayBeHeldByTheTeamsTerminator(t *testing.T) {
	w := applied(t)
	w.net.extra = []Listener{listener("192.0.2.10", 8448, paths.TLSUnit(ops(t)))}
	if _, err := w.apply(w.cur); err != nil {
		t.Errorf("the team's own terminator on its port was refused: %v", err)
	}
}

// Serve starts the terminator after the homeserver and holds its bound addresses to the listen set
// units.json records.
func TestServeStartsTheTerminatorToo(t *testing.T) {
	w := applied(t)
	team := ops(t)
	for _, unit := range []string{paths.ServerUnit(team), paths.TLSUnit(team)} {
		w.sd.units[unit].Active = "inactive"
	}
	w.sd.started = nil
	if err := Serve(t.Context(), w.deps(), team); err != nil {
		t.Fatal(err)
	}
	if got := startedUnits(w); !slices.Equal(got, []string{paths.ServerUnit(team), paths.TLSUnit(team)}) {
		t.Errorf("started %q", got)
	}
	w.net.boundTLS = []Listener{listener("198.51.100.1", 8448, paths.TLSUnit(team))}
	err := Serve(t.Context(), w.deps(), team)
	if got := stepOf(t, err); got.Step != StepTerminator {
		t.Errorf("a terminator on the wrong address: %v", err)
	}
}

// X24: team remove stops and disables the terminator before the homeserver, and deletes its
// drop-in with the homeserver's.
func TestRemoveStopsAndDeletesTheTerminator(t *testing.T) {
	w := applied(t)
	team := ops(t)
	lines, err := Remove(t.Context(), w.deps(), team, false)
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range []string{"tls unit stopped", "tls unit disabled", "tls drop-in removed"} {
		if !slices.Contains(lines, line) {
			t.Errorf("lines %q lack %q", lines, line)
		}
	}
	if w.exists(paths.TLSDropInFile(team)) {
		t.Error("the terminator's drop-in is still there")
	}
	if got := w.sd.stopped; !slices.Equal(got, []string{paths.TLSUnit(team), paths.ServerUnit(team)}) {
		t.Errorf("stopped %q", got)
	}
}
