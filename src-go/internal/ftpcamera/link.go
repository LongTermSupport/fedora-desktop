package ftpcamera

import "sync"

// wifiInfo is the laptop's own WiFi link (E46): band "2.4", "5", "6" or "" when unknown. It
// says nothing about the camera's link unless this laptop is the access point.
type wifiInfo struct {
	band, freqMHz, signal string
}

// linkState is the weak-link advisory's memory: whether it has been shown, and how often
// each file name has arrived (E58, E59).
type linkState struct {
	mu     sync.Mutex
	warned bool
	seen   map[string]int
	wifi   wifiInfo
}

// arrived counts one arrival of name and returns how many there have been.
func (l *linkState) arrived(name string) int {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.seen == nil {
		l.seen = map[string]int{}
	}
	l.seen[name]++
	return l.seen[name]
}

// warnWeakLink prints the advisory for reason, once per run (E58).
func (c *cli) warnWeakLink(reason string) {
	c.link.mu.Lock()
	if c.link.warned {
		c.link.mu.Unlock()
		return
	}
	c.link.warned = true
	w := c.link.wifi
	c.link.mu.Unlock()

	e := c.err
	e.f("\n")
	e.f("  %s%s⚠  Weak camera link — move the camera closer to the router%s\n", e.y, e.b, e.n)
	e.f("     %sPlace the camera as physically close to the router as you can.%s\n", e.b, e.n)
	e.f("     Ideally right next to it, line of sight, no walls between.\n\n")
	e.f("     Detected: %s%s%s\n\n", e.c, reason, e.n)
	e.f("     The camera's transmitter is far weaker than the laptop's, so a\n")
	e.f("     link the laptop finds fine can still be too weak for the camera.\n")
	e.f("     When it is, the data connection drops mid-transfer, the file\n")
	e.f("     arrives truncated, and the camera re-sends the same frame\n")
	e.f("     forever without ever reaching the next one.\n\n")
	if w.band != "" {
		signal := w.signal
		if signal == "" {
			signal = "?"
		}
		e.f("     This laptop is on %s%sGHz%s (%s MHz), signal %s%s dBm%s.\n", e.c, w.band, e.n, w.freqMHz, e.c, signal, e.n)
		e.f("     %sThat is the LAPTOP's link, not the camera's%s — the camera talks to\n", e.y, e.n)
		e.f("     the router, not to us, and band-steering can put the two on\n")
		e.f("     different bands under the same SSID.\n\n")
	}
	e.f("     If you cannot move it closer:\n")
	switch w.band {
	case "5", "6":
		e.f("       • %sTry the camera on 2.4GHz.%s %sGHz is faster but has\n", e.c, e.n, w.band)
		e.f("         much shorter range and poorer wall penetration, so for a\n")
		e.f("         weak transmitter it is usually the worse trade — a slower\n")
		e.f("         link that completes beats a faster one that truncates.\n")
	case "2.4":
		e.f("       • You are already on 2.4GHz, which is the better band for\n")
		e.f("         range, so a band change will not help. Distance or\n")
		e.f("         interference is the remaining variable.\n")
	}
	e.f("       • %s--hotspot%s — the camera connects straight to this laptop,\n", e.c, e.n)
	e.f("         which also lets ftp-camera read the camera's REAL signal\n")
	e.f("         strength, since this machine becomes the access point.\n")
	e.f("       • A card reader, if the backlog is large.\n\n")
}
