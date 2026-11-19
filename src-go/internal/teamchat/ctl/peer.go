package ctl

import (
	"bufio"
	"bytes"
	"context"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"net"
	"net/netip"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/state"
)

// The admin token, the registration key and the admin password go to the homeserver over plain
// HTTP on loopback, where any local process may hold the team's port while the homeserver is
// down (Plan 00257 ADMIN-1). Before a byte is sent on a connection, the child holds the other end
// to the team's own homeserver: the kernel's table of the child's network names the socket that
// accepted the connection and the uid that owns it, which must be the uid of a process in the
// system cgroup systemd gives the team's unit, by its exact path (serverCgroup), which only
// systemd can make. A socket the child cannot judge is refused, so nothing is sent to a
// listener it could not name.

// PeerCheck holds that conn, which the child opened to the homeserver of team on loopback, is
// accepted by that homeserver; any error refuses the connection before it carries a byte.
type PeerCheck func(ctx context.Context, team paths.TeamID, conn net.Conn) error

// The wait for the accepted socket: the kernel lists it once the handshake's last packet is
// processed, which on loopback follows the connect at once or within a few scheduler ticks.
const (
	peerPoll  = 10 * time.Millisecond
	peerTries = 100
	// procCap caps one file the check reads under /proc.
	procCap = 1 << 20
)

// ProcPeer is the production PeerCheck: /proc (Proc) read for the socket's owner and the unit's
// processes, Sleep the wait between reads.
type ProcPeer struct {
	Proc  string
	Sleep func(ctx context.Context, d time.Duration) error
}

// Check implements PeerCheck.
func (p ProcPeer) Check(ctx context.Context, team paths.TeamID, conn net.Conn) error {
	local, lerr := addrPort(conn.LocalAddr())
	remote, rerr := addrPort(conn.RemoteAddr())
	if err := errors.Join(lerr, rerr); err != nil {
		return fmt.Errorf("the connection to the homeserver: %w", err)
	}
	unit := paths.ServerUnit(team)
	for try := 0; ; try++ {
		uid, found, err := acceptedBy(p.Proc, remote, local)
		if err != nil {
			return err
		}
		if found {
			runs, err := unitRunsAs(p.Proc, serverCgroup(team), uid)
			if err != nil {
				return err
			}
			if !runs {
				return fmt.Errorf("the listener on %s is not %s (its socket is uid %d's): nothing is sent to it", remote, unit, uid)
			}
			return nil
		}
		if try+1 >= peerTries {
			return fmt.Errorf("the socket that accepted the connection to %s is not in %s/net/tcp: nothing is sent to it", remote, p.Proc)
		}
		if err := p.Sleep(ctx, peerPoll); err != nil {
			return err
		}
	}
}

func addrPort(a net.Addr) (netip.AddrPort, error) {
	tcp, ok := a.(*net.TCPAddr)
	if !ok {
		return netip.AddrPort{}, fmt.Errorf("%v is not a TCP address", a)
	}
	ap := tcp.AddrPort()
	return netip.AddrPortFrom(ap.Addr().Unmap(), ap.Port()), nil
}

// acceptedBy is the uid owning the IPv4 TCP socket whose local end is server and remote end is
// client, from proc's net/tcp, and whether there is one.
func acceptedBy(proc string, server, client netip.AddrPort) (int, bool, error) {
	if !server.Addr().Is4() || !client.Addr().Is4() {
		return 0, false, fmt.Errorf("the connection %s to %s is not IPv4 loopback", client, server)
	}
	table := proc + "/net/tcp"
	raw, found, err := state.ReadFile(table, procCap)
	if err == nil && !found {
		err = errors.New("it is not there")
	}
	if err != nil {
		return 0, false, fmt.Errorf("%s: %w", table, err)
	}
	want := [2]string{tcpAddr(server), tcpAddr(client)}
	sc := bufio.NewScanner(bytes.NewReader(raw))
	sc.Buffer(make([]byte, 0, 4096), procCap)
	for first := true; sc.Scan(); first = false {
		fields := strings.Fields(sc.Text())
		if first {
			continue
		}
		if len(fields) < 8 {
			return 0, false, fmt.Errorf("%s: a line has %d fields", table, len(fields))
		}
		if fields[1] != want[0] || fields[2] != want[1] {
			continue
		}
		uid, err := strconv.Atoi(fields[7])
		if err != nil || uid < 0 {
			return 0, false, fmt.Errorf("%s: the uid %q is not a number", table, fields[7])
		}
		return uid, true, nil
	}
	return 0, false, sc.Err()
}

// tcpAddr is an IPv4 address and port as /proc/net/tcp writes them: the address's four bytes as
// one host-order word in hex, then the port in hex.
func tcpAddr(ap netip.AddrPort) string {
	b := ap.Addr().As4()
	word := binary.NativeEndian.Uint32(b[:])
	var out [4]byte
	binary.BigEndian.PutUint32(out[:], word)
	return strings.ToUpper(hex.EncodeToString(out[:])) + ":" + fmt.Sprintf("%04X", ap.Port())
}

// unitRunsAs is whether a process in the cgroup unitCgroup runs with effective uid uid. A process
// that ends while it is read is passed by.
func unitRunsAs(proc, unitCgroup string, uid int) (bool, error) {
	entries, err := os.ReadDir(proc)
	if err != nil {
		return false, err
	}
	for _, e := range entries {
		if _, perr := strconv.Atoi(e.Name()); perr != nil {
			continue
		}
		dir := proc + "/" + e.Name()
		inUnit, err := inCgroupOf(dir, unitCgroup)
		if gone(err) || err == nil && !inUnit {
			continue
		}
		if err != nil {
			return false, err
		}
		euid, err := effectiveUID(dir)
		if gone(err) {
			continue
		}
		if err != nil {
			return false, err
		}
		if euid == uid {
			return true, nil
		}
	}
	return false, nil
}

// gone is a process that ended while it was read.
func gone(err error) bool {
	return errors.Is(err, fs.ErrNotExist) || errors.Is(err, errGone)
}

var errGone = errors.New("the process ended")

// inCgroupOf is whether the process at dir sits in the cgroup want itself: its cgroup v2 line
// (`0::<path>`) is exactly want. Only systemd, as root, makes cgroups under /system.slice; a
// unit of the same name a user starts sits under its user@<uid>.service, which a match on the
// name alone would take for the team's.
func inCgroupOf(dir, want string) (bool, error) {
	raw, err := procFile(dir + "/cgroup")
	if err != nil {
		return false, err
	}
	for line := range strings.SplitSeq(string(raw), "\n") {
		if path, ok := strings.CutPrefix(line, "0::"); ok {
			return path == want, nil
		}
	}
	return false, nil
}

// serverCgroup is the cgroup v2 path systemd gives team's homeserver unit, an instance of the
// system template teamchat-server@.service: the default slice of a template's instance is
// system-<template prefix, escaped as a unit name>.slice under system.slice (systemd's
// unit_set_default_slice, src/core/unit.c; the escaping is systemd-escape's, systemd.unit(5)
// "String Escaping for Inclusion in Unit Names", so "-" is \x2d). The unit sets no Slice=.
func serverCgroup(team paths.TeamID) string {
	unit := paths.ServerUnit(team)
	prefix := unit[:strings.IndexByte(unit, '@')]
	return "/system.slice/system-" + unitEscape(prefix) + ".slice/" + unit
}

// unitEscape escapes s as systemd-escape does a unit name's text: every byte but an ASCII
// letter, digit, ":", "_" or a "." that does not lead becomes \xNN.
func unitEscape(s string) string {
	var b strings.Builder
	for i := range len(s) {
		c := s[i]
		keep := c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == ':' || c == '_' || c == '.' && i > 0
		if keep {
			b.WriteByte(c)
		} else {
			fmt.Fprintf(&b, `\x%02x`, c)
		}
	}
	return b.String()
}

// effectiveUID is the effective uid of the process at dir, its status file's second Uid.
func effectiveUID(dir string) (int, error) {
	raw, err := procFile(dir + "/status")
	if err != nil {
		return 0, err
	}
	for line := range strings.SplitSeq(string(raw), "\n") {
		rest, ok := strings.CutPrefix(line, "Uid:")
		if !ok {
			continue
		}
		fields := strings.Fields(rest)
		if len(fields) < 2 {
			break
		}
		uid, err := strconv.Atoi(fields[1])
		if err != nil {
			break
		}
		return uid, nil
	}
	return 0, fmt.Errorf("%s/status has no effective uid", dir)
}

// procFile is a file under a process's directory; one that is not there is errGone.
func procFile(path string) ([]byte, error) {
	raw, found, err := state.ReadFile(path, procCap)
	if err == nil && !found {
		err = errGone
	}
	return raw, err
}
