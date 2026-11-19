package host

import (
	"net/netip"
	"slices"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
)

// Plan 00257 HOST-1: the health unit's IP filter allows loopback alone, and the tick probes each
// applied team's listen addresses through its terminator (X32). team apply and team remove render
// the unit's drop-in with every applied team's listen addresses, so every address the tick
// probes is one the filter allows.

// healthAllowed is the drop-in's allowed prefixes, none when it allows nothing beyond the base.
func healthAllowed(t *testing.T, w *world) []netip.Prefix {
	t.Helper()
	var out []netip.Prefix
	for line := range strings.SplitSeq(w.read(paths.HealthDropInFile), "\n") {
		rest, ok := strings.CutPrefix(line, "IPAddressAllow=")
		if !ok {
			continue
		}
		for field := range strings.FieldsSeq(rest) {
			out = append(out, netip.MustParsePrefix(field))
		}
	}
	return out
}

// devDoc is a second team on the second interface's network, on its own port.
func devDoc() map[string]any {
	doc := teamDoc()
	team := docTeam(doc)
	team["team"], team["port"], team["networks"] = "dev", 8449, []any{"203.0.113.0/24"}
	doc["address"] = "203.0.113.9"
	return doc
}

// probed asserts every address the tick probes for team is allowed by the drop-in.
func probed(t *testing.T, w *world, team paths.TeamID) {
	t.Helper()
	reach, err := AppliedReach(w.root)(team)
	if err != nil {
		t.Fatal(err)
	}
	allowed := healthAllowed(t, w)
	for _, a := range reach.Addrs {
		if !slices.ContainsFunc(allowed, func(p netip.Prefix) bool { return p.Contains(a.Addr()) }) {
			t.Errorf("team %s's probe of %s is outside the health filter %v", team, a, allowed)
		}
	}
}

func TestHOST1TheHealthFilterAllowsEveryAddressTheTickProbes(t *testing.T) {
	w := newWorld(t)
	withSecondNIC(w)
	lines, err := w.apply(approved(w, teamDoc()))
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(lines, "health drop-in written") {
		t.Errorf("lines %q", lines)
	}
	if _, err := w.apply(approved(w, devDoc())); err != nil {
		t.Fatal(err)
	}
	dev := mustTeam(t, "dev")
	probed(t, w, ops(t))
	probed(t, w, dev)
	if got := healthAllowed(t, w); !slices.Equal(got, prefixes("192.0.2.10/32", "203.0.113.9/32")) {
		t.Errorf("allowed %v, want each listen address and no network", got)
	}
	if mode(t, w.at(paths.HealthDropInFile)) != 0o644 {
		t.Error("the health drop-in is not 0644")
	}
	w.log = nil
	if _, err := Remove(t.Context(), w.deps(), dev, false); err != nil {
		t.Fatal(err)
	}
	if got := healthAllowed(t, w); !slices.Equal(got, prefixes("192.0.2.10/32")) {
		t.Errorf("after dev's remove, allowed %v", got)
	}
	if !w.called("daemon-reload") {
		t.Errorf("the remove did not reload for the narrower filter: %q", w.log)
	}
}

// A second apply that changes nothing leaves the drop-in and systemd alone.
func TestHOST1AnUnchangedFilterIsNotReloaded(t *testing.T) {
	w := applied(t)
	lines, err := w.apply(w.cur)
	if err != nil {
		t.Fatal(err)
	}
	if len(lines) != 0 || w.called("daemon-reload") {
		t.Errorf("lines %q, calls %q", lines, w.log)
	}
}

// A drop-in written whose reload is not recorded (a failed reload, a crash) is reloaded by the
// next verb, whatever its bytes say.
func TestHOST1AFilterWrittenButNotReloadedIsReloadedByTheNextApply(t *testing.T) {
	w := applied(t)
	if gone, err := removeFile(w.at(paths.AdminRunDir) + "/" + healthLoaded); err != nil || !gone {
		t.Fatalf("no reload was recorded: %v", err)
	}
	lines, err := w.apply(w.cur)
	if err != nil {
		t.Fatal(err)
	}
	if len(lines) != 0 || !w.called("daemon-reload") {
		t.Errorf("lines %q, calls %q", lines, w.log)
	}
	w.log = nil
	if _, err := w.apply(w.cur); err != nil || w.called("daemon-reload") {
		t.Errorf("the reload was not recorded: %q, %v", w.log, err)
	}
}
