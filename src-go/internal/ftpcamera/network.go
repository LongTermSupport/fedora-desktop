package ftpcamera

import (
	"context"
	"math"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
)

// hotspotInfo is an active NetworkManager shared connection (E45).
type hotspotInfo struct {
	active        bool
	dev, ip, ssid string
}

func (c *cli) nmcli(ctx context.Context, args ...string) (execx.Result, error) {
	return c.env.Runner.Run(ctx, execx.Cmd{Name: nmcliBin, Args: args})
}

// lines is the non-empty lines of a command's stdout.
func lines(out []byte) []string {
	var got []string
	for l := range strings.SplitSeq(string(out), "\n") {
		if l != "" {
			got = append(got, l)
		}
	}
	return got
}

// connections is the NetworkManager profile names, all or only the active ones.
func (c *cli) connections(ctx context.Context, active bool) ([]string, error) {
	args := []string{"-t", "-f", "NAME", "connection", nmShow}
	if active {
		args = append(args, "--active")
	}
	res, err := c.nmcli(ctx, args...)
	if err != nil {
		return nil, err
	}
	return lines(res.Stdout), nil
}

// detectHotspot finds an active shared connection, if any (E45). Nothing here is an error.
func (c *cli) detectHotspot(ctx context.Context) hotspotInfo {
	if !c.onPath(nmcliBin) {
		return hotspotInfo{}
	}
	active, err := c.connections(ctx, true)
	if err != nil {
		return hotspotInfo{}
	}
	conn := ""
	for _, name := range active {
		method, err := execx.Combined(ctx, c.env.Runner, execx.Cmd{Name: nmcliBin, Args: []string{"-g", "ipv4.method", "connection", nmShow, name}})
		if err != nil {
			c.err.f("WARNING: could not read ipv4.method for '%s': %s\n", name, strings.TrimRight(method, "\n"))
			continue
		}
		if strings.TrimSpace(method) == "shared" {
			conn = name
			break
		}
	}
	if conn == "" {
		return hotspotInfo{}
	}
	h := hotspotInfo{dev: c.nmValue(ctx, "GENERAL.DEVICES", conn)}
	if h.dev == "" {
		return hotspotInfo{}
	}
	if h.ip = c.firstIPv4(ctx, h.dev); h.ip == "" {
		return hotspotInfo{}
	}
	h.ssid = c.nmValue(ctx, "802-11-wireless.ssid", conn)
	h.active = true
	return h
}

// nmValue is one field of a connection, "" when it cannot be read (a wired shared connection
// has no SSID).
func (c *cli) nmValue(ctx context.Context, field, conn string) string {
	res, err := c.nmcli(ctx, "-g", field, "connection", nmShow, conn)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(res.Stdout))
}

// firstIPv4 is a device's first IPv4 address, "" when it has none.
func (c *cli) firstIPv4(ctx context.Context, dev string) string {
	res, err := c.env.Runner.Run(ctx, execx.Cmd{Name: "ip", Args: []string{"-4", "addr", nmShow, "dev", dev}})
	if err != nil {
		return ""
	}
	for _, l := range lines(res.Stdout) {
		if !strings.Contains(l, "inet ") {
			continue
		}
		if f := strings.Fields(l); len(f) >= 2 {
			return strings.SplitN(f[1], "/", 2)[0]
		}
	}
	return ""
}

// wifiDevice is the first WiFi device NetworkManager knows, "" when there is none.
func (c *cli) wifiDevice(ctx context.Context) (string, error) {
	res, err := c.nmcli(ctx, "-t", "-f", "DEVICE,TYPE", "device")
	if err != nil {
		return "", err
	}
	for _, l := range lines(res.Stdout) {
		if dev, kind, ok := strings.Cut(l, ":"); ok && kind == "wifi" {
			return dev, nil
		}
	}
	return "", nil
}

// detectWifi is the laptop's own band and signal (E46); every failure is "unknown".
func (c *cli) detectWifi(ctx context.Context) wifiInfo {
	if !c.onPath("iw") {
		return wifiInfo{}
	}
	dev, err := c.wifiDevice(ctx)
	if err != nil || dev == "" {
		return wifiInfo{}
	}
	res, err := c.env.Runner.Run(ctx, execx.Cmd{Name: "iw", Args: []string{"dev", dev, "link"}})
	if err != nil {
		return wifiInfo{}
	}
	freq, signal := parseLink(res.Stdout)
	w := wifiInfo{signal: signal}
	if freq < 0 {
		return wifiInfo{}
	}
	w.freqMHz = strconv.FormatFloat(freq, 'f', 0, 64)
	switch {
	case freq < 3000:
		w.band = "2.4"
	case freq < 5926:
		w.band = "5"
	default:
		w.band = "6"
	}
	return w
}

// parseLink reads `iw dev <dev> link`: the frequency in whole MHz (-1 when absent) and the
// signal in dBm.
func parseLink(out []byte) (float64, string) {
	freq, signal := -1.0, ""
	for _, l := range lines(out) {
		f := strings.Fields(l)
		if len(f) < 2 {
			continue
		}
		if strings.Contains(l, "freq:") && freq < 0 {
			if v, err := strconv.ParseFloat(f[1], 64); err == nil {
				freq = math.Trunc(v)
			}
		}
		if strings.Contains(l, "signal:") && signal == "" {
			signal = f[1]
		}
	}
	return freq, signal
}

// localIP is the address the camera is told to use (E47).
func (c *cli) localIP(ctx context.Context, h hotspotInfo) (string, error) {
	if h.active {
		return h.ip, nil
	}
	res, err := c.env.Runner.Run(ctx, execx.Cmd{Name: "ip", Args: []string{"-4", "route", nmShow, "default"}})
	dev := ""
	if err == nil {
		if first := lines(res.Stdout); len(first) > 0 {
			f := strings.Fields(first[0])
			if i := slices.Index(f, "dev"); i >= 0 && i+1 < len(f) {
				dev = f[i+1]
			}
		}
	}
	if dev == "" {
		e := c.err
		e.f("ERROR: No default network interface found AND no hotspot active.\n")
		e.f("  Either connect to a network OR turn on a hotspot in GNOME quick\n")
		e.f("  settings (or via 'nmcli device wifi hotspot') and try again.\n")
		return "", errFailed
	}
	ip := c.firstIPv4(ctx, dev)
	if ip == "" {
		return "", c.failErr("%s has no IPv4 address", dev)
	}
	return ip, nil
}

var apWord = regexp.MustCompile(`(^|[^A-Za-z0-9_])AP([^A-Za-z0-9_]|$)`)

// ifaceModesWindow is how many lines after "Supported interface modes" are read for AP: the
// heading and the twelve lines grep -A 12 gave.
const ifaceModesWindow = 13

// The answers of apSupported.
const (
	apYes = "yes"
	apNo  = "no"
)

// apSupported is apYes, apNo or "" (unknown) for dev (E44): nmcli first, then iw.
func (c *cli) apSupported(ctx context.Context, dev string) string {
	if probe, err := execx.Combined(ctx, c.env.Runner, execx.Cmd{Name: nmcliBin, Args: []string{"-g", "WIFI-PROPERTIES.AP", "device", nmShow, dev}}); err == nil {
		switch lower := strings.ToLower(probe); {
		case strings.Contains(lower, apYes):
			return apYes
		case strings.Contains(lower, apNo):
			return apNo
		}
	}
	if !c.onPath("iw") {
		return ""
	}
	probe, err := execx.Combined(ctx, c.env.Runner, execx.Cmd{Name: "iw", Args: []string{"list"}})
	if err != nil {
		return ""
	}
	all := strings.Split(probe, "\n")
	for i, l := range all {
		if !strings.Contains(l, "Supported interface modes") {
			continue
		}
		if slices.ContainsFunc(all[i:min(i+ifaceModesWindow, len(all))], apWord.MatchString) {
			return apYes
		}
	}
	return apNo
}

// assertHotspotSupported refuses hardware that cannot be an access point (E44).
func (c *cli) assertHotspotSupported(ctx context.Context) error {
	dev, err := c.wifiDevice(ctx)
	if err != nil {
		return c.failErr("could not list the network devices: %v", err)
	}
	e := c.err
	if dev == "" {
		e.f("ERROR: --hotspot needs a WiFi device — NetworkManager reports none.\n")
		e.f("  This machine has no radio that could host an access point.\n")
		return errFailed
	}
	switch c.apSupported(ctx, dev) {
	case apYes:
		e.f("%sHotspot capability: '%s' supports AP mode.%s\n", e.g, dev, e.n)
	case apNo:
		e.f("ERROR: WiFi device '%s' does not support AP mode.\n\n", dev)
		e.f("  This is a hardware/driver limitation, not a configuration\n")
		e.f("  problem — the driver does not advertise AP capability, so\n")
		e.f("  --hotspot cannot work on this machine however it is set up.\n")
		e.f("  Options: use the normal network, or a USB WiFi adapter that\n")
		e.f("  does support AP mode.\n")
		return errFailed
	default:
		e.f("  Note: could not determine whether '%s' supports AP mode\n", dev)
		e.f("  (neither nmcli nor iw gave a usable answer). Continuing —\n")
		e.f("  if the hardware cannot do it, activation below will fail.\n")
	}
	e.f("%s  Note: a single-radio card cannot be WiFi client and AP at once,%s\n", e.y, e.n)
	e.f("%s  so this laptop will likely drop its own internet while the%s\n", e.y, e.n)
	e.f("%s  hotspot is up. Uploads still cache locally and drain later.%s\n", e.y, e.n)
	return nil
}
