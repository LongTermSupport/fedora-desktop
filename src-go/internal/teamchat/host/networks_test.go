package host

import (
	"net/netip"
	"slices"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
)

// X3: the address an interface serves a network with.
func TestListenAddress(t *testing.T) {
	for name, c := range map[string]struct {
		network string
		own     bool
		addrs   []string
		want    string
		refusal string
	}{
		"the one address inside":         {"192.0.2.0/24", false, []string{"192.0.2.10"}, "192.0.2.10", ""},
		"the one inside of several":      {"203.0.113.0/24", false, []string{"192.0.2.10", "203.0.113.20"}, "203.0.113.20", ""},
		"the lowest of several inside":   {"192.0.2.0/24", false, []string{"192.0.2.30", "192.0.2.20", "203.0.113.1"}, "192.0.2.20", ""},
		"none inside, the lowest":        {"203.0.113.0/24", false, []string{"198.51.100.7", "192.0.2.10"}, "192.0.2.10", ""},
		"none inside, own network":       {"203.0.113.0/24", true, []string{"192.0.2.10"}, "", "eth0 has no address inside it"},
		"own network, one inside":        {"192.0.2.10/32", true, []string{"192.0.2.10", "192.0.2.11"}, "192.0.2.10", ""},
		"no address of the family":       {"2001:db8::/32", false, []string{"192.0.2.10"}, "", "eth0 has no IPv6 address"},
		"no IPv4 address":                {"192.0.2.0/24", false, []string{"2001:db8::10"}, "", "eth0 has no IPv4 address"},
		"IPv6 inside":                    {"2001:db8::/32", false, []string{"192.0.2.10", "2001:db8::10"}, "2001:db8::10", ""},
		"link-local is never chosen":     {"2001:db8::/32", false, []string{"fe80::1", "2001:db8::10"}, "2001:db8::10", ""},
		"only link-local":                {"2001:db8::/32", false, []string{"fe80::1"}, "", "eth0 has no IPv6 address"},
		"link-local is no lowest choice": {"2001:db8::/32", false, []string{"fe80::1", "2001:db8:1::10"}, "2001:db8:1::10", ""},
	} {
		t.Run(name, func(t *testing.T) {
			iface := Interface{Name: "eth0", Addrs: addrs(c.addrs...)}
			got, err := listenAddress(netip.MustParsePrefix(c.network), c.own, iface)
			if c.refusal != "" {
				if err == nil || !strings.Contains(err.Error(), c.refusal) {
					t.Fatalf("got %v, %v, want a refusal saying %q", got, err, c.refusal)
				}
				return
			}
			if err != nil || got != netip.MustParseAddr(c.want) {
				t.Fatalf("got %v, %v, want %s", got, err, c.want)
			}
		})
	}
}

// snapshot is every file and rule the effective networks of a team decide (X4).
type snapshot struct {
	config, dropIn, tlsDropIn, firewall string
	units                               []string
}

func snapshotOf(t *testing.T, w *world) snapshot {
	t.Helper()
	team := ops(t)
	raw := []byte(w.read(paths.UnitsRecord(team)))
	units, err := decodeUnits(raw, team)
	if err != nil {
		t.Fatal(err)
	}
	return snapshot{
		config: w.read(paths.ServerConfig(team)), dropIn: w.read(paths.ServerDropInFile(team)),
		tlsDropIn: w.read(paths.TLSDropInFile(team)), firewall: w.read(paths.FirewallRecord(team)), units: units.listen,
	}
}

// withSecondNIC is a host with a second interface, which routes 203.0.113.0/24.
func withSecondNIC(w *world) {
	w.net.ifaces = append(w.net.ifaces, Interface{Name: "eth1", Addrs: addrs("203.0.113.9")})
	w.net.routes[1] = fakeRoute{netip.MustParsePrefix("203.0.113.0/24"), "eth1"}
}

// applyFresh applies the sample team, with edit applied to its declaration, in a new host.
func applyFresh(t *testing.T, edit func(doc map[string]any)) snapshot {
	t.Helper()
	w := newWorld(t)
	withSecondNIC(w)
	doc := teamDoc()
	edit(doc)
	if _, err := w.apply(approved(w, doc)); err != nil {
		t.Fatal(err)
	}
	return snapshotOf(t, w)
}

// same is whether o holds exactly the files and rules s does.
func (s snapshot) same(o snapshot) bool {
	return s.config == o.config && s.dropIn == o.dropIn && s.tlsDropIn == o.tlsDropIn && s.firewall == o.firewall &&
		slices.Equal(s.units, o.units)
}

// baseSnapshot is the sample team's files in a host with a second interface, applied once.
func baseSnapshot(t *testing.T) snapshot {
	t.Helper()
	base := applyFresh(t, func(map[string]any) {})
	if base.config == "" || base.dropIn == "" || base.firewall == "" || len(base.units) != 1 {
		t.Fatalf("the base snapshot is empty: %+v", base)
	}
	return base
}

// linesWithout is text's lines that do not start with any of prefixes.
func linesWithout(text string, prefixes ...string) []string {
	var out []string
	for l := range strings.SplitSeq(text, "\n") {
		if !slices.ContainsFunc(prefixes, func(p string) bool { return strings.HasPrefix(l, p) }) {
			out = append(out, l)
		}
	}
	return out
}

// lineWith is the one line of text that starts with prefix.
func lineWith(t *testing.T, text, prefix string) string {
	t.Helper()
	var found []string
	for l := range strings.SplitSeq(text, "\n") {
		if strings.HasPrefix(l, prefix) {
			found = append(found, l)
		}
	}
	if len(found) != 1 {
		t.Fatalf("%d lines start with %q in:\n%s", len(found), prefix, text)
	}
	return found[0]
}

// X4: the terminator's drop-in (its listen, device and filter lines), the firewall rules and
// units.json's listen set are a function of the effective networks alone: no other field of the
// declaration changes any of them, and none of them is in the homeserver's config or drop-in.
func TestNetworksAloneDecideTheListenFilterAndRules(t *testing.T) {
	base := baseSnapshot(t)
	for name, edit := range map[string]func(map[string]any){
		"description": func(d map[string]any) { docTeam(d)["description"] = "The team that runs other things." },
		"name":        func(d map[string]any) { docTeam(d)["name"] = "Ops team" },
		"humans":      func(d map[string]any) { docTeam(d)["humans"] = []any{"alice", "bob"} },
		"trust": func(d map[string]any) {
			docTeam(d)["trust"] = map[string]any{
				"kind": "github", "org": "example-org",
				"repos": []any{map[string]any{"repo": "example-org/other", "branches": []any{"main"}}},
			}
		},
	} {
		if got := applyFresh(t, edit); !got.same(base) {
			t.Errorf("%s changed the networks' files:\n%+v\nwas\n%+v", name, got, base)
		}
	}
}

// X4: the order of the declared networks, and one the host derives itself, change nothing.
func TestOrderAndRepeatsOfTheEffectiveNetworksChangeNothing(t *testing.T) {
	base := baseSnapshot(t)
	got := applyFresh(t, func(d map[string]any) {
		// The bus address's own /32 is already effective; declaring it too is still one network.
		docTeam(d)["networks"] = []any{"192.0.2.10/32", "192.0.2.0/24"}
	})
	if !got.same(base) {
		t.Errorf("a reordered, repeated declaration changed the files:\n%+v\nwas\n%+v", got, base)
	}
}

// X4: one more network changes the terminator's listen, device and filter lines, the rule set and
// units.json's listen set, and nothing else: not the homeserver's config or drop-in.
func TestASecondNetworkChangesOnlyWhatItShould(t *testing.T) {
	base := baseSnapshot(t)
	got := applyFresh(t, func(d map[string]any) { docTeam(d)["networks"] = []any{"192.0.2.0/24", "203.0.113.0/24"} })
	if got.config != base.config || got.dropIn != base.dropIn {
		t.Errorf("the homeserver's files follow the networks:\n%s\n%s", got.config, got.dropIn)
	}
	skip := []string{"After=", "Wants=", "ExecStart=", "IPAddressAllow="}
	if !slices.Equal(linesWithout(got.tlsDropIn, skip...), linesWithout(base.tlsDropIn, skip...)) {
		t.Errorf("more than the device, listen and filter lines of the terminator's drop-in changed:\n%s", got.tlsDropIn)
	}
	devices := deviceUnit("eth0") + " " + deviceUnit("eth1")
	for prefix, want := range map[string]string{
		"After=":          "After=" + devices,
		"Wants=":          "Wants=" + devices,
		"ExecStart=":      "ExecStart=/usr/local/bin/teamchat tls --team ops --port 8448 --listen 192.0.2.10 --listen 203.0.113.9",
		"IPAddressAllow=": "IPAddressAllow=127.0.0.1/32 ::1/128 192.0.2.10/32 203.0.113.9/32 192.0.2.0/24 203.0.113.0/24",
	} {
		if line := lineWith(t, got.tlsDropIn, prefix); line != want {
			t.Errorf("terminator drop-in: %s, want %s", line, want)
		}
	}
	if line := lineWith(t, got.tlsDropIn, "SocketBindAllow="); line != "SocketBindAllow=tcp:8448" {
		t.Errorf("the socket bind depends on more than the port: %s", line)
	}
	wantRules := `[{"zone":"public","source":"192.0.2.0/24","port":8448},{"zone":"public","source":"192.0.2.10/32","port":8448},{"zone":"public","source":"203.0.113.0/24","port":8448}]`
	if !strings.Contains(got.firewall, wantRules) || strings.Count(base.firewall, `"source"`) != 2 {
		t.Errorf("firewall.json:\n%s\nwas\n%s", got.firewall, base.firewall)
	}
	if !slices.Equal(got.units, []string{"192.0.2.10", "203.0.113.9"}) {
		t.Errorf("units.json listen %v", got.units)
	}
}

// X4, X24: removing the network and applying again reverses all of it.
func TestRemovingANetworkReversesIt(t *testing.T) {
	base := baseSnapshot(t)
	w := newWorld(t)
	withSecondNIC(w)
	wide := teamDoc()
	docTeam(wide)["networks"] = []any{"192.0.2.0/24", "203.0.113.0/24"}
	if _, err := w.apply(approved(w, wide)); err != nil {
		t.Fatal(err)
	}
	if _, err := w.apply(approved(w, teamDoc())); err != nil {
		t.Fatal(err)
	}
	if got := snapshotOf(t, w); !got.same(base) {
		t.Errorf("the files after removing the network:\n%+v\nwant\n%+v", got, base)
	}
	for _, permanent := range []bool{true, false} {
		if w.fw.present[permanent]["public 203.0.113.0/24 tcp:8448"] {
			t.Errorf("permanent %v: the removed network's rule is still in place: %v", permanent, w.fw.present[permanent])
		}
	}
}

// X3: what the derivation answers for several networks, and where it puts them.
func TestListenAddressesAreDerivedFromTheNetworks(t *testing.T) {
	for name, c := range map[string]struct {
		setup    func(w *world)
		networks []any
		listen   []string
		devices  []string
	}{
		"two networks on one interface give one address": {
			setup:    func(*world) {},
			networks: []any{"192.0.2.0/24", "203.0.113.0/24"},
			listen:   []string{"192.0.2.10"},
			devices:  []string{"eth0"},
		},
		"two networks on two interfaces give both, sorted": {
			setup:    withSecondNIC,
			networks: []any{"203.0.113.0/24", "192.0.2.0/24"},
			listen:   []string{"192.0.2.10", "203.0.113.9"},
			devices:  []string{"eth0", "eth1"},
		},
		"an interface with several addresses gives the one inside": {
			setup:    func(w *world) { w.net.ifaces[1].Addrs = addrs("192.0.2.10", "203.0.113.20") },
			networks: []any{"203.0.113.0/24"},
			listen:   []string{"192.0.2.10", "203.0.113.20"},
			devices:  []string{"eth0"},
		},
		"an IPv6 network gives an IPv6 address": {
			setup: func(w *world) {
				w.net.ifaces[1].Addrs = addrs("192.0.2.10", "fe80::1", "2001:db8::10")
				w.net.routes = append(w.net.routes, fakeRoute{netip.MustParsePrefix("2001:db8::/32"), "eth0"})
			},
			networks: []any{"192.0.2.0/24", "2001:db8::/32"},
			listen:   []string{"192.0.2.10", "2001:db8::10"},
			devices:  []string{"eth0"},
		},
	} {
		t.Run(name, func(t *testing.T) {
			w := newWorld(t)
			c.setup(w)
			doc := teamDoc()
			docTeam(doc)["networks"] = c.networks
			if _, err := w.apply(approved(w, doc)); err != nil {
				t.Fatal(err)
			}
			team := ops(t)
			if got := lineWith(t, w.read(paths.ServerConfig(team)), "address = "); got != `address = ["127.0.0.1"]` {
				t.Errorf("the homeserver is not loopback alone: %s", got)
			}
			var devices []string
			for _, d := range c.devices {
				devices = append(devices, deviceUnit(d))
			}
			tls := w.read(paths.TLSDropInFile(team))
			if got := lineWith(t, tls, "After="); got != "After="+strings.Join(devices, " ") {
				t.Errorf("terminator drop-in: %s", got)
			}
			want := "ExecStart=/usr/local/bin/teamchat tls --team ops --port 8448 --listen " + strings.Join(c.listen, " --listen ")
			if got := lineWith(t, tls, "ExecStart="); got != want {
				t.Errorf("terminator drop-in: %s, want %s", got, want)
			}
		})
	}
}

// X6: a team's url host is a derived listen address when it is an IP literal; a DNS name is free.
func TestUrlHostIsAListenAddressOrAName(t *testing.T) {
	for name, c := range map[string]struct {
		url    string
		setup  func(w *world)
		doc    func(d map[string]any)
		refuse string
	}{
		"a listen address":                {"http://192.0.2.10:8448", nil, nil, ""},
		"a DNS name":                      {"https://chat.example.com:8448", nil, nil, ""},
		"an address that is not listened": {"http://192.0.2.77:8448", nil, nil, "url host 192.0.2.77 is not a listen address"},
		"loopback is bound but not derived": {
			"http://127.0.0.1:8448", nil, nil, "url host 127.0.0.1 is not a listen address",
		},
		"an IPv6 listen address": {
			"http://[2001:db8::10]:8448",
			func(w *world) {
				w.net.ifaces[1].Addrs = addrs("192.0.2.10", "2001:db8::10")
				w.net.routes = append(w.net.routes, fakeRoute{netip.MustParsePrefix("2001:db8::/32"), "eth0"})
			},
			func(d map[string]any) { docTeam(d)["networks"] = []any{"192.0.2.0/24", "2001:db8::/32"} }, "",
		},
		"the second interface's address": {
			"http://203.0.113.9:8448", withSecondNIC,
			func(d map[string]any) { docTeam(d)["networks"] = []any{"192.0.2.0/24", "203.0.113.0/24"} }, "",
		},
		"the second interface's address, its network undeclared": {
			"http://203.0.113.9:8448", withSecondNIC, nil, "url host 203.0.113.9 is not a listen address",
		},
	} {
		t.Run(name, func(t *testing.T) {
			w := newWorld(t)
			if c.setup != nil {
				c.setup(w)
			}
			doc := teamDoc()
			docTeam(doc)["url"] = c.url
			if c.doc != nil {
				c.doc(doc)
			}
			lines, err := w.apply(approved(w, doc))
			if c.refuse == "" {
				if err != nil {
					t.Fatalf("got %v", err)
				}
				return
			}
			e := stepOf(t, err)
			if e.Step != StepRefusals || !e.Drift || !strings.Contains(err.Error(), c.refuse) || len(lines) != 0 || w.called("start") {
				t.Errorf("got %v (drift %v), %q; want a refusal saying %q", err, e.Drift, lines, c.refuse)
			}
		})
	}
}

// X3: an own network (the bridge) may be routed by the default route, a declared one may not.
func TestOwnNetworkOnTheDefaultRouteIsAccepted(t *testing.T) {
	w := newWorld(t)
	w.net.routes = []fakeRoute{
		{netip.MustParsePrefix("192.0.2.0/24"), "eth0"},
		{netip.MustParsePrefix("0.0.0.0/0"), "lxcbr0"},
	}
	doc := teamDoc()
	doc["lxc_members"] = []any{map[string]any{"team": "ops", "container": "c1", "user": "agent", "checkout": "/srv/checkouts/thing", "seat": 1}}
	if _, err := w.apply(approved(w, doc)); err != nil {
		t.Fatalf("the bridge on the default route: %v", err)
	}
	if got := lineWith(t, w.read(paths.TLSDropInFile(ops(t))), "ExecStart="); !strings.HasSuffix(got, " --listen 192.0.2.10 --listen 198.51.100.1") {
		t.Errorf("terminator drop-in: %s", got)
	}
}
