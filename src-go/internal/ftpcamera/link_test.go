package ftpcamera

import (
	"strings"
	"testing"
)

// E58: once per run, the reason, the laptop caveat, the band advice.
func TestWeakLinkAdvisoryOncePerRun(t *testing.T) {
	k := newKit(t)
	c := newCLI(k.app().Env)
	c.link.wifi = wifiInfo{band: "5", freqMHz: "5180", signal: "-61"}
	c.warnWeakLink("DSC1.ARW arrived truncated")
	c.warnWeakLink("second reason")
	got := k.err.String()
	for _, want := range []string{
		"\n  ⚠  Weak camera link — move the camera closer to the router\n",
		"     Detected: DSC1.ARW arrived truncated\n",
		"     This laptop is on 5GHz (5180 MHz), signal -61 dBm.\n",
		"     That is the LAPTOP's link, not the camera's — the camera talks to\n",
		"       • Try the camera on 2.4GHz. 5GHz is faster but has\n",
		"       • --hotspot — the camera connects straight to this laptop,\n",
		"       • A card reader, if the backlog is large.\n",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("advisory lacks %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "second reason") {
		t.Error("the advisory was shown twice")
	}
	if k.out.Len() != 0 {
		t.Errorf("advisory on stdout: %q", k.out.String())
	}
}

func TestWeakLinkAdvisoryBands(t *testing.T) {
	k := newKit(t)
	c := newCLI(k.app().Env)
	c.link.wifi = wifiInfo{band: "2.4", freqMHz: "2412"}
	c.warnWeakLink("x")
	if !strings.Contains(k.err.String(), "You are already on 2.4GHz") || !strings.Contains(k.err.String(), "signal ? dBm") {
		t.Errorf("2.4 GHz advice:\n%s", k.err.String())
	}
	k2 := newKit(t)
	c2 := newCLI(k2.app().Env)
	c2.warnWeakLink("x")
	if strings.Contains(k2.err.String(), "This laptop is on") || strings.Contains(k2.err.String(), "2.4GHz") {
		t.Errorf("unknown band:\n%s", k2.err.String())
	}
}

func TestArrivalsAreCountedByName(t *testing.T) {
	var l linkState
	for i, want := range []int{1, 2, 3} {
		if got := l.arrived("A.JPG"); got != want {
			t.Errorf("arrival %d: %d", i, got)
		}
	}
	if l.arrived("B.JPG") != 1 {
		t.Error("names are counted apart")
	}
}
