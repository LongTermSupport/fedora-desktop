package host

import (
	"context"
	"errors"
	"fmt"
	"net/netip"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"golang.org/x/sys/unix"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/state"
)

// The health tick probes each applied team's listen addresses through its terminator (X32), and
// the health unit's IP filter allows loopback alone (Plan 00257 HOST-1). Its drop-in, which team
// apply and team remove render, allows every listen address of every applied team (one that has
// a stamp) as a host prefix, the union of their units.json listen sets: the addresses the tick
// dials, and no network. It is rendered under a host-wide lock, after the team's own records, so
// two teams applied at once both end in it; systemd is reloaded for it until a reload of the text
// on disk has been recorded, so a write whose reload failed is finished by the next verb.

// healthLock and healthLoaded are the host-wide lock the health filter's render holds and the
// digest of the drop-in systemd last reloaded, both in the lock directory: a reboot, which empties
// it, loads the drop-in on disk, and the next verb records that by one reload.
const (
	healthLock   = "health-filter.lock"
	healthLoaded = "health-filter.loaded"
)

// HealthDropIn is the health unit's drop-in for the listen addresses listen: each as a host
// prefix, beside the deny of any the drop-in carries itself, as a team's drop-ins do (X18b).
func HealthDropIn(listen []netip.Addr) []byte {
	allow := make([]string, 0, len(listen))
	for _, a := range listen {
		allow = append(allow, netip.PrefixFrom(a, a.BitLen()).String())
	}
	text := "# Rendered by " + paths.AdminBinary + " team apply and team remove; do not edit.\n" +
		"# Every applied team's listen addresses, which the health tick probes through each team's\n" +
		"# TLS terminator; the base unit allows loopback.\n[Service]\nIPAddressDeny=any\n"
	if len(allow) > 0 {
		text += "IPAddressAllow=" + strings.Join(allow, " ") + "\n"
	}
	return []byte(text)
}

// appliedListenAll is every listen address of every team under root that has a stamp, once each,
// in order.
func appliedListenAll(root string) ([]netip.Addr, error) {
	entries, err := os.ReadDir(root + paths.AdminStateRoot)
	if err != nil {
		return nil, err
	}
	var out []netip.Addr
	for _, e := range entries {
		team, perr := paths.ParseTeamID(e.Name())
		if perr != nil || !e.IsDir() {
			continue
		}
		listen, err := stampedListen(root, team)
		if err != nil {
			return nil, err
		}
		for _, addr := range listen {
			if !slices.Contains(out, addr) {
				out = append(out, addr)
			}
		}
	}
	slices.SortFunc(out, netip.Addr.Compare)
	return out, nil
}

// stampedListen is the listen set units.json records for team, none when it has no stamp.
func stampedListen(root string, team paths.TeamID) ([]netip.Addr, error) {
	_, stamped, err := ReadStamp(root, team)
	if err != nil || !stamped {
		return nil, err
	}
	raw, found, err := state.ReadFile(root+paths.UnitsRecord(team), fileMax)
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, fmt.Errorf("team %s has a stamp and no %s", team, paths.UnitsRecord(team))
	}
	u, err := decodeUnits(raw, team)
	if err != nil {
		return nil, err
	}
	out := make([]netip.Addr, 0, len(u.listen))
	for _, l := range u.listen {
		addr, err := netip.ParseAddr(l)
		if err != nil {
			return nil, err
		}
		out = append(out, addr)
	}
	return out, nil
}

// syncHealthFilter renders the health unit's drop-in from every applied team under the host-wide
// lock, writes it when it differs, and reloads systemd until the text on disk is recorded as
// loaded. It answers the CHANGED lines.
func syncHealthFilter(ctx context.Context, d Deps) (lines []string, err error) {
	unlock, err := lockHealthFilter(d.LockDir)
	if err != nil {
		return nil, err
	}
	defer func() { err = errors.Join(err, unlock()) }()
	listen, err := appliedListenAll(d.Root)
	if err != nil {
		return nil, err
	}
	want := HealthDropIn(listen)
	file := d.Root + paths.HealthDropInFile
	have, _, err := state.ReadFile(file, fileMax)
	if err != nil {
		return nil, err
	}
	if string(have) != string(want) {
		if err := MkdirShared(filepath.Dir(file)); err != nil {
			return nil, err
		}
		if err := state.WriteFile(file, want, 0o644, -1); err != nil {
			return nil, err
		}
		lines = append(lines, "health drop-in written")
	}
	record := d.LockDir + "/" + healthLoaded
	loaded, _, err := state.ReadFile(record, fileMax)
	if err != nil {
		return lines, err
	}
	if string(loaded) == digest(want) {
		return lines, nil
	}
	if err := d.Systemd.DaemonReload(ctx); err != nil {
		return lines, err
	}
	return lines, state.WriteFile(record, []byte(digest(want)), 0o600, -1)
}

// lockHealthFilter takes the host-wide lock of the health drop-in, waiting for it, and answers
// its release.
func lockHealthFilter(dir string) (func() error, error) {
	f, err := os.OpenFile(dir+"/"+healthLock, os.O_RDWR|os.O_CREATE|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0o600)
	if err != nil {
		return nil, fmt.Errorf("the health filter's lock: %w", err)
	}
	if err := unix.Flock(int(f.Fd()), unix.LOCK_EX); err != nil {
		return nil, errors.Join(fmt.Errorf("the health filter's lock: %w", err), f.Close())
	}
	return f.Close, nil
}
