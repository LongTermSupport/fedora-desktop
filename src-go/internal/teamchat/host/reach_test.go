package host

import (
	"net/netip"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
)

// X32: a probe through the terminator reaches each listen address the last apply recorded, on the
// team's declared port, trusting the team's CA certificate as apply wrote it.
func TestTheAppliedReachIsTheListenSetOnThePortWithTheTeamCA(t *testing.T) {
	w := applied(t)
	got, err := AppliedReach(w.root)(ops(t))
	if err != nil {
		t.Fatal(err)
	}
	ca, err := os.ReadFile(filepath.Clean(w.at(paths.CACertFile(ops(t)))))
	if err != nil {
		t.Fatal(err)
	}
	if string(got.CAPEM) != string(ca) {
		t.Fatal("the CA is not the team's CA certificate")
	}
	if want := []netip.AddrPort{netip.MustParseAddrPort("192.0.2.10:8448")}; !slices.Equal(got.Addrs, want) {
		t.Fatalf("addresses %v, want %v", got.Addrs, want)
	}
}

// A team never applied has nothing to reach, and that is an error rather than no address.
func TestATeamNeverAppliedHasNoReach(t *testing.T) {
	w := applied(t)
	if _, err := AppliedReach(w.root)(mustTeam(t, "other")); err == nil {
		t.Fatal("a team never applied has a reach")
	}
}

// A team whose CA certificate is gone cannot be probed: an error, not an empty CA.
func TestAReachWithNoCACertificateIsAnError(t *testing.T) {
	w := applied(t)
	if err := os.Remove(w.at(paths.CACertFile(ops(t)))); err != nil {
		t.Fatal(err)
	}
	if _, err := AppliedReach(w.root)(ops(t)); err == nil {
		t.Fatal("a reach without its CA")
	}
}
