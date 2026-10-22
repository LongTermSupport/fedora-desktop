// Package hostnet is the host's network, firewalld and loopback probe adapters (Plan 00209
// SPEC-n2f section 7, NW1-NW4, FW1-FW2, RD1-RD2): the production side of host.Network,
// host.Firewall, host.Readiness and health.Prober.
//
// Everything root reads from /proc is read in-process, through state.ReadFile (no link, a
// regular file, capped), and no text of another program is parsed; the one external program,
// firewall-cmd, runs through execx with the rule as one argument. The probes are plain HTTP to
// 127.0.0.1: no proxy, no redirect, five seconds, at most 64 KiB of body read.
package hostnet

import (
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"net"
	"net/netip"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"syscall"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/host"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/state"
)

// The caps of what is read from /proc: a socket table can be long, a cgroup file is one line
// per hierarchy.
const (
	tableCap  = 32 << 20
	cgroupCap = 64 << 10
	listening = 0x0A
)

// link is a network interface as the host reports it: its name and each address with its
// prefix length.
type link struct {
	Name     string
	Prefixes []netip.Prefix
}

// Network implements host.Network. Proc is /proc and BridgeName the LXC bridge's interface name
// (lxcbr0; not Bridge, which is the method); the interfaces come from the kernel through net.Interfaces unless a test supplies
// them.
type Network struct {
	Proc       string
	BridgeName string
	links      func() ([]link, error)
}

var _ host.Network = Network{}

// Interfaces is the host's interfaces and their addresses, none with a zone (NW1).
func (n Network) Interfaces() ([]host.Interface, error) {
	links, err := n.read()
	if err != nil {
		return nil, err
	}
	out := make([]host.Interface, 0, len(links))
	for _, l := range links {
		iface := host.Interface{Name: l.Name}
		for _, p := range l.Prefixes {
			iface.Addrs = append(iface.Addrs, p.Addr())
		}
		out = append(out, iface)
	}
	return out, nil
}

// Bridge is the bridge's first IPv4 address and prefix, masked; no such interface, or no IPv4
// address on it, is an error naming it (NW4).
func (n Network) Bridge() (netip.Prefix, error) {
	links, err := n.read()
	if err != nil {
		return netip.Prefix{}, err
	}
	for _, l := range links {
		if l.Name != n.BridgeName {
			continue
		}
		for _, p := range l.Prefixes {
			if p.Addr().Is4() {
				return p.Masked(), nil
			}
		}
		return netip.Prefix{}, fmt.Errorf("hostnet: the bridge %s has no IPv4 address", n.BridgeName)
	}
	return netip.Prefix{}, fmt.Errorf("hostnet: the bridge %s does not exist", n.BridgeName)
}

// systemLinks is net.Interfaces with each interface's addresses.
func systemLinks() ([]link, error) {
	ifaces, err := net.Interfaces()
	if err != nil {
		return nil, fmt.Errorf("hostnet: listing the interfaces: %w", err)
	}
	links := make([]link, 0, len(ifaces))
	for _, iface := range ifaces {
		addrs, addrErr := iface.Addrs()
		if addrErr != nil {
			return nil, fmt.Errorf("hostnet: the addresses of %s: %w", iface.Name, addrErr)
		}
		l := link{Name: iface.Name}
		for _, a := range addrs {
			p, prefixErr := prefixOf(a)
			if prefixErr != nil {
				return nil, fmt.Errorf("hostnet: an address of %s: %w", iface.Name, prefixErr)
			}
			l.Prefixes = append(l.Prefixes, p)
		}
		links = append(links, l)
	}
	return links, nil
}

var errAddrKind = errors.New("not an address with a usable prefix")

// prefixOf is an interface address as a prefix with its zone dropped and IPv4 unmapped.
func prefixOf(a net.Addr) (netip.Prefix, error) {
	var ip net.IP
	var mask net.IPMask
	switch v := a.(type) {
	case *net.IPNet:
		ip, mask = v.IP, v.Mask
	case *net.IPAddr:
		ip = v.IP
	default:
		return netip.Prefix{}, fmt.Errorf("%w: %v", errAddrKind, a)
	}
	addr, ok := netip.AddrFromSlice(ip)
	if !ok {
		return netip.Prefix{}, fmt.Errorf("%w: %v", errAddrKind, a)
	}
	addr = addr.Unmap().WithZone("")
	ones := addr.BitLen()
	if mask != nil {
		o, bits := mask.Size()
		if bits == 0 {
			return netip.Prefix{}, fmt.Errorf("%w: %v has a mask that is not a prefix", errAddrKind, a)
		}
		ones = o - (bits - addr.BitLen()) // a 16-byte mask over an IPv4 address
	}
	if ones < 0 || ones > addr.BitLen() {
		return netip.Prefix{}, fmt.Errorf("%w: %v", errAddrKind, a)
	}
	return netip.PrefixFrom(addr, ones), nil
}

// socket is one listening line of a socket table.
type socket struct {
	addr  netip.AddrPort
	inode string
}

// Listeners is every listening TCP socket from /proc/net/tcp then tcp6, with the unit whose
// process holds it (NW2, NW3). A host without IPv6 has no tcp6 table, which is no listeners.
func (n Network) Listeners() ([]host.Listener, error) {
	v4, found, err := readSockets(n.Proc+"/net/tcp", 4)
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, fmt.Errorf("hostnet: %s/net/tcp does not exist", n.Proc)
	}
	v6, _, err := readSockets(n.Proc+"/net/tcp6", 16)
	if err != nil {
		return nil, err
	}
	sockets := slices.Concat(v4, v6)
	units, err := n.units(sockets)
	if err != nil {
		return nil, err
	}
	out := make([]host.Listener, 0, len(sockets))
	for _, s := range sockets {
		out = append(out, host.Listener{Addr: s.addr, Unit: units[s.inode]})
	}
	return out, nil
}

// readSockets is the LISTEN lines of one table; a line that does not parse fails the read.
func readSockets(path string, size int) ([]socket, bool, error) {
	raw, found, err := state.ReadFile(path, tableCap)
	if err != nil || !found {
		return nil, found, err
	}
	var out []socket
	for i, line := range strings.Split(string(raw), "\n") {
		if i == 0 || strings.TrimSpace(line) == "" {
			continue // the header
		}
		s, isListener, parseErr := parseSocket(line, size)
		if parseErr != nil {
			return nil, true, fmt.Errorf("hostnet: %s line %d: %w", path, i+1, parseErr)
		}
		if isListener {
			out = append(out, s)
		}
	}
	return out, true, nil
}

var errSocketLine = errors.New("not a socket table line")

// parseSocket reads the local address, the state and the inode of one line.
func parseSocket(line string, size int) (socket, bool, error) {
	fields := strings.Fields(line)
	const inodeColumn = 9
	if len(fields) <= inodeColumn {
		return socket{}, false, fmt.Errorf("%w: %d columns", errSocketLine, len(fields))
	}
	st, err := strconv.ParseUint(fields[3], 16, 8)
	if err != nil {
		return socket{}, false, fmt.Errorf("the state %q: %w", fields[3], err)
	}
	if _, err = strconv.ParseUint(fields[inodeColumn], 10, 64); err != nil {
		return socket{}, false, fmt.Errorf("the inode %q: %w", fields[inodeColumn], err)
	}
	text, port, ok := strings.Cut(fields[1], ":")
	if !ok {
		return socket{}, false, fmt.Errorf("%w: local address %q", errSocketLine, fields[1])
	}
	addr, err := socketAddr(text, size)
	if err != nil {
		return socket{}, false, err
	}
	p, err := strconv.ParseUint(port, 16, 16)
	if err != nil {
		return socket{}, false, fmt.Errorf("the port %q: %w", port, err)
	}
	return socket{addr: netip.AddrPortFrom(addr, uint16(p)), inode: fields[inodeColumn]}, st == listening, nil
}

// socketAddr decodes the kernel's hex address: each four-byte word is printed in host
// (little-endian) order.
func socketAddr(text string, size int) (netip.Addr, error) {
	raw, err := hex.DecodeString(text)
	if err != nil || len(raw) != size {
		return netip.Addr{}, fmt.Errorf("%w: the address %q is not %d bytes of hex", errSocketLine, text, size)
	}
	for i := 0; i < size; i += 4 {
		raw[i], raw[i+1], raw[i+2], raw[i+3] = raw[i+3], raw[i+2], raw[i+1], raw[i]
	}
	addr, ok := netip.AddrFromSlice(raw)
	if !ok {
		return netip.Addr{}, fmt.Errorf("%w: the address %q", errSocketLine, text)
	}
	return addr, nil
}

func (n Network) read() ([]link, error) {
	if n.links != nil {
		return n.links()
	}
	return systemLinks()
}

// units maps each socket's inode to the unit whose process holds it, walking /proc/<pid>/fd
// once. A process that ends during the walk is skipped (NW3).
func (n Network) units(sockets []socket) (map[string]string, error) {
	wanted := make(map[string]bool, len(sockets))
	for _, s := range sockets {
		wanted[s.inode] = true
	}
	units := map[string]string{}
	if len(wanted) == 0 {
		return units, nil
	}
	procs, err := os.ReadDir(n.Proc)
	if err != nil {
		return nil, fmt.Errorf("hostnet: reading %s: %w", n.Proc, err)
	}
	for _, p := range procs {
		if !p.IsDir() || !isPID(p.Name()) {
			continue
		}
		if err = n.holder(p.Name(), wanted, units); err != nil {
			return nil, err
		}
	}
	return units, nil
}

func isPID(name string) bool {
	_, err := strconv.ParseUint(name, 10, 32)
	return err == nil
}

// holder records, for each wanted socket the process holds, the process's unit.
func (n Network) holder(pid string, wanted map[string]bool, units map[string]string) error {
	fds := filepath.Join(n.Proc, pid, "fd")
	entries, err := os.ReadDir(fds)
	if vanished(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("hostnet: reading %s: %w", fds, err)
	}
	unit, read := "", false
	for _, e := range entries {
		target, linkErr := os.Readlink(filepath.Join(fds, e.Name()))
		if vanished(linkErr) {
			continue
		}
		if linkErr != nil {
			return fmt.Errorf("hostnet: reading the link %s: %w", filepath.Join(fds, e.Name()), linkErr)
		}
		inode, isSocket := strings.CutPrefix(target, "socket:[")
		inode, closed := strings.CutSuffix(inode, "]")
		if !isSocket || !closed || !wanted[inode] {
			continue
		}
		if !read {
			if unit, read, err = n.unitOf(pid); err != nil {
				return err
			}
			if !read {
				return nil // the process ended between its fd directory and its cgroup file
			}
		}
		if unit != "" {
			units[inode] = unit
		}
	}
	return nil
}

// unitOf is the unit of a process; read is false when its cgroup file is gone.
func (n Network) unitOf(pid string) (string, bool, error) {
	raw, found, err := state.ReadFile(filepath.Join(n.Proc, pid, "cgroup"), cgroupCap)
	if err != nil || !found {
		return "", false, err
	}
	return cgroupUnit(raw), true, nil
}

// vanished is an error of a process that is gone.
func vanished(err error) bool {
	return errors.Is(err, fs.ErrNotExist) || errors.Is(err, syscall.ESRCH)
}

// cgroupUnit is the last element of the cgroup v2 path (`0::<path>`) when it is a service,
// and "" otherwise.
func cgroupUnit(raw []byte) string {
	for line := range strings.SplitSeq(string(raw), "\n") {
		path, ok := strings.CutPrefix(line, "0::")
		if !ok {
			continue
		}
		last := path[strings.LastIndex(path, "/")+1:]
		if strings.HasSuffix(last, ".service") {
			return last
		}
		return ""
	}
	return ""
}
