package ftpcamera

import (
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
)

// The addresses are RFC 5737 documentation addresses (CLAUDE/ExampleValues.md).
const (
	hotspotIP = "198.51.100.1"
	lanIP     = "192.0.2.20"
)

func onNM(k *kit, args string, out string, code int) {
	k.fake.On(append([]string{"nmcli"}, strings.Fields(args)...), execx.Result{Stdout: []byte(out), ExitCode: code})
}

func ipAddr(k *kit, dev, addr string) {
	out := "3: " + dev + ": <UP>\n"
	if addr != "" {
		out += "    inet " + addr + "/24 scope global " + dev + "\n"
	}
	k.fake.On([]string{"ip", "-4", "addr", "show", "dev", dev}, execx.Result{Stdout: []byte(out)})
}

// E45: the first active connection whose method is shared, its device, address and SSID.
func TestDetectHotspot(t *testing.T) {
	k := newKit(t)
	k.path("nmcli")
	onNM(k, "-t -f NAME connection show --active", "Wired\nBroken\nHotspot\n", 0)
	onNM(k, "-g ipv4.method connection show Wired", "auto\n", 0)
	onNM(k, "-g ipv4.method connection show Broken", "Error: no such connection\n", 10)
	onNM(k, "-g ipv4.method connection show Hotspot", "shared\n", 0)
	onNM(k, "-g GENERAL.DEVICES connection show Hotspot", "wlp1\n", 0)
	onNM(k, "-g 802-11-wireless.ssid connection show Hotspot", "ftp-camera\n", 0)
	ipAddr(k, "wlp1", hotspotIP)
	c := newCLI(k.app().Env)
	got := c.detectHotspot(t.Context())
	if got != (hotspotInfo{active: true, dev: "wlp1", ip: hotspotIP, ssid: "ftp-camera"}) {
		t.Fatalf("%+v", got)
	}
	k.wantErr("WARNING: could not read ipv4.method for 'Broken': Error: no such connection\n")
}

func TestDetectHotspotAbsent(t *testing.T) {
	k := newKit(t)
	c := newCLI(k.app().Env)
	if got := c.detectHotspot(t.Context()); got.active {
		t.Fatal("no nmcli, no hotspot")
	}
	k.path("nmcli")
	onNM(k, "-t -f NAME connection show --active", "Hotspot\n", 0)
	onNM(k, "-g ipv4.method connection show Hotspot", "shared\n", 0)
	onNM(k, "-g GENERAL.DEVICES connection show Hotspot", "", 0)
	if got := c.detectHotspot(t.Context()); got.active {
		t.Fatal("no device, no hotspot")
	}
	onNM(k, "-g GENERAL.DEVICES connection show Hotspot", "wlp1\n", 0)
	ipAddr(k, "wlp1", "")
	if got := c.detectHotspot(t.Context()); got.active {
		t.Fatal("no address, no hotspot")
	}
	onNM(k, "-t -f NAME connection show --active", "", 1)
	if got := c.detectHotspot(t.Context()); got.active {
		t.Fatal("nmcli failing, no hotspot")
	}
}

// E46.
func TestDetectWifi(t *testing.T) {
	cases := []struct {
		link string
		want wifiInfo
	}{
		{"Connected to x\n\tfreq: 5180.0\n\tsignal: -61 dBm\n", wifiInfo{band: "5", freqMHz: "5180", signal: "-61"}},
		{"\tfreq: 2412\n", wifiInfo{band: "2.4", freqMHz: "2412"}},
		{"\tfreq: 5955\n\tsignal: -70 dBm\n", wifiInfo{band: "6", freqMHz: "5955", signal: "-70"}},
		{"Not connected.\n", wifiInfo{}},
	}
	for _, tc := range cases {
		k := newKit(t)
		k.path("iw")
		onNM(k, "-t -f DEVICE,TYPE device", "enp0s1:ethernet\nwlp1:wifi\n", 0)
		k.fake.On([]string{"iw", "dev", "wlp1", "link"}, execx.Result{Stdout: []byte(tc.link)})
		if got := newCLI(k.app().Env).detectWifi(t.Context()); got != tc.want {
			t.Errorf("%q: %+v, want %+v", tc.link, got, tc.want)
		}
	}
	k := newKit(t)
	if got := newCLI(k.app().Env).detectWifi(t.Context()); got != (wifiInfo{}) {
		t.Errorf("no iw: %+v", got)
	}
	k.path("iw")
	onNM(k, "-t -f DEVICE,TYPE device", "enp0s1:ethernet\n", 0)
	if got := newCLI(k.app().Env).detectWifi(t.Context()); got != (wifiInfo{}) {
		t.Errorf("no wifi device: %+v", got)
	}
}

// E47, B7, B8.
func TestLocalIP(t *testing.T) {
	k := newKit(t)
	c := newCLI(k.app().Env)
	if ip, err := c.localIP(t.Context(), hotspotInfo{active: true, ip: hotspotIP}); err != nil || ip != hotspotIP {
		t.Fatalf("hotspot: %q %v", ip, err)
	}
	k.fake.On([]string{"ip", "-4", "route", "show", "default"}, execx.Result{Stdout: []byte("default dev wg0 scope link\ndefault via 192.0.2.1 dev wlp1\n")})
	ipAddr(k, "wg0", lanIP)
	if ip, err := c.localIP(t.Context(), hotspotInfo{}); err != nil || ip != lanIP {
		t.Fatalf("default route: %q %v", ip, err)
	}
	ipAddr(k, "wg0", "")
	if _, err := c.localIP(t.Context(), hotspotInfo{}); err == nil {
		t.Fatal("no address accepted")
	}
	k.wantErr("ERROR: wg0 has no IPv4 address\n")
	k.fake.On([]string{"ip", "-4", "route", "show", "default"}, execx.Result{})
	if _, err := c.localIP(t.Context(), hotspotInfo{}); err == nil {
		t.Fatal("no route accepted")
	}
	k.wantErr("ERROR: No default network interface found AND no hotspot active.\n  Either connect to a network OR turn on a hotspot in GNOME quick\n")
}

// E44: AP support from nmcli, else iw.
func TestHotspotSupport(t *testing.T) {
	cases := []struct {
		name, nm string
		nmCode   int
		iw       string
		iwOn     bool
		wantErr  bool
		want     string
	}{
		{name: "nmcli yes", nm: "yes\n", want: "Hotspot capability: 'wlp1' supports AP mode."},
		{name: "nmcli no", nm: "no\n", wantErr: true, want: "ERROR: WiFi device 'wlp1' does not support AP mode."},
		{name: "iw has AP", nmCode: 1, iwOn: true, iw: "Wiphy phy0\n\tSupported interface modes:\n\t\t * managed\n\t\t * AP\n", want: "supports AP mode."},
		{name: "iw lacks AP", nm: "unknown\n", iwOn: true, iw: "\tSupported interface modes:\n\t\t * managed\n\t\t * APX\n", wantErr: true, want: "does not support AP mode"},
		{name: "nobody knows", nmCode: 1, want: "Note: could not determine whether 'wlp1' supports AP mode"},
	}
	for _, tc := range cases {
		k := newKit(t)
		onNM(k, "-t -f DEVICE,TYPE device", "wlp1:wifi\n", 0)
		onNM(k, "-g WIFI-PROPERTIES.AP device show wlp1", tc.nm, tc.nmCode)
		if tc.iwOn {
			k.path("iw")
			k.fake.On([]string{"iw", "list"}, execx.Result{Stdout: []byte(tc.iw)})
		}
		err := newCLI(k.app().Env).assertHotspotSupported(t.Context())
		if (err != nil) != tc.wantErr {
			t.Errorf("%s: error %v", tc.name, err)
		}
		k.wantErr(tc.want)
		if !tc.wantErr && !strings.Contains(k.err.String(), "single-radio card cannot be WiFi client and AP at once") {
			t.Errorf("%s: no single-radio note", tc.name)
		}
	}
	k := newKit(t)
	onNM(k, "-t -f DEVICE,TYPE device", "enp0s1:ethernet\n", 0)
	if err := newCLI(k.app().Env).assertHotspotSupported(t.Context()); err == nil {
		t.Fatal("no wifi device accepted")
	}
	k.wantErr("ERROR: --hotspot needs a WiFi device — NetworkManager reports none.\n")
}
