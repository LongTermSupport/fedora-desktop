package vmtest

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/fsx"
)

// A link is a point-to-point network between exactly two member NICs: a UDP socket pair on
// 127.0.0.1 between two rootless guests, so it is a directory holding the two ports and nothing
// libvirt knows (E67). Members record the ends they hold in members/<name>/links (E68); a link
// is removed only when none does (E71).

const (
	linkPortsFile   = "ports"
	memberLinksFile = "links"
	linkPortsFormat = "a=%d\nb=%d\n"
)

var linkNameRE = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,39}$`)

// linkName checks a link's name before it reaches a path (E67).
func linkName(name string) error {
	if !linkNameRE.MatchString(name) {
		return failf("a link name is 1 to 40 of a-z, 0-9 and '-', not starting with '-'; got '%s'", name)
	}
	return nil
}

// freeLoopbackUDPPorts is n distinct free loopback UDP ports: all n are bound at once, so no two
// are the same, then released for virt-install to take. The race between the release and the
// take is the free TCP port's.
func freeLoopbackUDPPorts(n int) ([]int, error) {
	conns := make([]net.PacketConn, 0, n)
	ports := make([]int, 0, n)
	var failure error
	for range n {
		c, err := net.ListenPacket("udp", "127.0.0.1:0")
		if err != nil {
			failure = fmt.Errorf("find a free UDP port: %w", err)
			break
		}
		conns = append(conns, c)
		addr, ok := c.LocalAddr().(*net.UDPAddr)
		if !ok {
			failure = errors.New("find a free UDP port: not a UDP address")
			break
		}
		ports = append(ports, addr.Port)
	}
	for _, c := range conns {
		if err := c.Close(); err != nil {
			failure = errors.Join(failure, fmt.Errorf("release a free UDP port: %w", err))
		}
	}
	if failure != nil {
		return nil, failure
	}
	return ports, nil
}

// cmdLinkUp makes a link: two ports recorded, nothing started (E67).
func (c *cli) cmdLinkUp(_ context.Context, args []string) error {
	if len(args) != 1 {
		return usagef("usage: vmtest link-up <link>")
	}
	name := args[0]
	if err := linkName(name); err != nil {
		return err
	}
	dir := c.paths.at(linksDir, name)
	if _, err := os.Lstat(dir); err == nil {
		return failf("link %s exists; remove it with: vmtest link-down %s", name, name)
	}
	ports, err := c.drawLinkPorts()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return failf("cannot create %s: %v", dir, err)
	}
	body := fmt.Sprintf(linkPortsFormat, ports[0], ports[1])
	if err := fsx.WriteAtomic(filepath.Join(dir, linkPortsFile), "link ports", []byte(body), 0o644); err != nil {
		return failf("%v", err)
	}
	c.sayf("VMTEST-LINK-UP %s a=%d b=%d", name, ports[0], ports[1])
	return nil
}

// linkPortDraws bounds how often link-up asks again for ports when a drawn one is another link's.
const linkPortDraws = 8

// drawLinkPorts is two free loopback UDP ports no other link records (E67): a link's ports are
// bound only when its members boot, so the free-port probe alone does not see them.
func (c *cli) drawLinkPorts() ([]int, error) {
	held, err := c.heldLinkPorts()
	if err != nil {
		return nil, err
	}
	for range linkPortDraws {
		ports, ferr := c.env.FreeUDPPorts(2)
		if ferr != nil {
			return nil, failf("%v", ferr)
		}
		if len(ports) != 2 {
			return nil, failf("find a free UDP port: wanted 2, got %d", len(ports))
		}
		if !held[ports[0]] && !held[ports[1]] {
			return ports, nil
		}
	}
	return nil, failf("no two free UDP ports another link does not hold after %d draws; remove an unused link with: vmtest link-down <link>", linkPortDraws)
}

// heldLinkPorts is every port a link of the lab records.
func (c *cli) heldLinkPorts() (map[int]bool, error) {
	entries, err := os.ReadDir(c.paths.at(linksDir))
	if errors.Is(err, os.ErrNotExist) {
		return map[int]bool{}, nil
	}
	if err != nil {
		return nil, failf("cannot read %s: %v", c.paths.at(linksDir), err)
	}
	held := map[int]bool{}
	for _, e := range entries {
		if !e.IsDir() || linkName(e.Name()) != nil {
			continue
		}
		// A link directory with no ports file holds no port (a link-up that failed half way).
		present, perr := regularOrAbsent(c.paths.at(linksDir, e.Name(), linkPortsFile))
		if perr != nil {
			return nil, perr
		}
		if !present {
			continue
		}
		p, perr := c.readLinkPorts(e.Name())
		if perr != nil {
			return nil, perr
		}
		held[p.a], held[p.b] = true, true
	}
	return held, nil
}

// regularOrAbsent says whether path is a regular file, refusing anything else that is there: a
// named pipe opened for reading would block for ever (project_walk_open).
func regularOrAbsent(path string) (bool, error) {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, failf("cannot read %s: %v", path, err)
	}
	if !info.Mode().IsRegular() {
		return false, failf("%s is not a regular file", path)
	}
	return true, nil
}

// nicRecord is one line of a member's links file: an end of a link and the MAC of the NIC on it.
type nicRecord struct{ member, link, end, mac string }

// recordedNICs is every NIC any member's record holds, in member name order and then record order.
func (c *cli) recordedNICs() ([]nicRecord, error) {
	entries, err := os.ReadDir(c.paths.at(membersDir))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, failf("cannot read %s: %v", c.paths.at(membersDir), err)
	}
	var out []nicRecord
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		nics, rerr := c.memberRecord(e.Name())
		if rerr != nil {
			return nil, rerr
		}
		out = append(out, nics...)
	}
	return out, nil
}

// memberRecord is the NICs one member's links file holds, in the order recorded; a member with no
// file holds none.
func (c *cli) memberRecord(member string) ([]nicRecord, error) {
	path := c.paths.at(membersDir, member, memberLinksFile)
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, failf("cannot read the links of member %s: %v", member, err)
	}
	if !info.Mode().IsRegular() {
		return nil, failf("%s is not a regular file", path)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, failf("cannot read the links of member %s: %v", member, err)
	}
	var out []nicRecord
	for line := range strings.SplitSeq(string(data), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		f := strings.Fields(line)
		if len(f) != 3 {
			return nil, failf("%s holds a line that is not '<link> <end> <mac>': '%s'", path, line)
		}
		out = append(out, nicRecord{member: member, link: f[0], end: f[1], mac: f[2]})
	}
	return out, nil
}

// linkHolders is the members that record an end of the link, in name order.
func (c *cli) linkHolders(link string) ([]string, error) {
	recorded, err := c.recordedNICs()
	if err != nil {
		return nil, err
	}
	var holders []string
	for _, r := range recorded {
		if r.link == link && !slices.Contains(holders, r.member) {
			holders = append(holders, r.member)
		}
	}
	return holders, nil
}

// maxMemberLinks is how many links one member may join: its NICs after the passt one.
const maxMemberLinks = 4

// linkRef is one --link option: an end of a link.
type linkRef struct{ link, end string }

// parseLinkOption reads <link>:<a|b> (E68).
func parseLinkOption(v string) (linkRef, error) {
	link, end, ok := strings.Cut(v, ":")
	if !ok || linkName(link) != nil || (end != "a" && end != "b") {
		return linkRef{}, usagef("--link wants <link>:<a|b>; got '%s'", v)
	}
	return linkRef{link: link, end: end}, nil
}

// memberNIC is one NIC of a member on a link (E68): the end it holds, its socket pair's ports,
// its own and the other end's, and its MAC.
type memberNIC struct {
	link, end string
	own, peer int
	mac       string
}

// network is the virt-install --network value of the NIC: libvirt's udp interface, a socket
// pair on the loopback. The option names are the design's and unverified until a host boots one.
func (n memberNIC) network() string {
	return fmt.Sprintf("type=udp,source.address=127.0.0.1,source.port=%d,source.local.address=127.0.0.1,source.local.port=%d,model.type=virtio,mac.address=%s",
		n.peer, n.own, n.mac)
}

func networksOfNICs(nics []memberNIC) []string {
	out := make([]string, 0, len(nics))
	for _, n := range nics {
		out = append(out, n.network())
	}
	return out
}

// linkNICs is a NIC for each link end asked for; a link nobody made is a usage error (E68).
func (c *cli) linkNICs(refs []linkRef) ([]memberNIC, error) {
	nics := make([]memberNIC, 0, len(refs))
	for _, r := range refs {
		ports, err := c.readLinkPorts(r.link)
		if err != nil {
			return nil, err
		}
		own, peer := ports.a, ports.b
		if r.end == "b" {
			own, peer = ports.b, ports.a
		}
		nics = append(nics, memberNIC{link: r.link, end: r.end, own: own, peer: peer})
	}
	return nics, nil
}

// linkPorts are the two ports of a link: the sockets of its ends a and b.
type linkPorts struct{ a, b int }

// readLinkPorts is the two ports link-up recorded for the link.
func (c *cli) readLinkPorts(link string) (linkPorts, error) {
	path := c.paths.at(linksDir, link, linkPortsFile)
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return linkPorts{}, usagef("no link %s; make it with: vmtest link-up %s", link, link)
	}
	if err != nil {
		return linkPorts{}, failf("cannot read %s: %v", path, err)
	}
	if !info.Mode().IsRegular() {
		return linkPorts{}, failf("%s is not a regular file", path)
	}
	body, err := os.ReadFile(path)
	if err != nil {
		return linkPorts{}, failf("cannot read %s: %v", path, err)
	}
	var p linkPorts
	if _, err = fmt.Sscanf(string(body), linkPortsFormat, &p.a, &p.b); err != nil || p.a <= 0 || p.b <= 0 {
		return linkPorts{}, failf("%s does not hold the two ports of a link", path)
	}
	return p, nil
}

// planNICs refuses an end another member holds and gives each NIC a MAC no record holds (E68).
// A member asking again for its own end is not refused here: that it exists is the better answer.
func (c *cli) planNICs(name string, nics []memberNIC) error {
	if len(nics) == 0 {
		return nil
	}
	recorded, err := c.recordedNICs()
	if err != nil {
		return err
	}
	taken := map[string]string{}
	for _, r := range recorded {
		taken[r.mac] = r.member
		for _, n := range nics {
			if r.link == n.link && r.end == n.end && r.member != name {
				return failf("link %s end %s is held by %s; remove it with: vmtest member-down %s", r.link, r.end, r.member, r.member)
			}
		}
	}
	for i := range nics {
		var b [3]byte
		if _, rerr := io.ReadFull(c.env.Random, b[:]); rerr != nil {
			return failf("cannot draw a MAC address: %v", rerr)
		}
		mac := fmt.Sprintf("52:54:00:%02x:%02x:%02x", b[0], b[1], b[2])
		if owner, dup := taken[mac]; dup {
			return failf("the MAC %s is already recorded by %s", mac, owner)
		}
		taken[mac] = name
		nics[i].mac = mac
	}
	return nil
}

// recordNICs writes the member's links file, one line per NIC in the order given: the record
// that holds the link ends and the MACs until member-down (E68).
func (c *cli) recordNICs(dir string, nics []memberNIC) error {
	if len(nics) == 0 {
		return nil
	}
	var body strings.Builder
	for _, n := range nics {
		fmt.Fprintf(&body, "%s %s %s\n", n.link, n.end, n.mac)
	}
	if err := fsx.WriteAtomic(filepath.Join(dir, memberLinksFile), "member links", []byte(body.String()), 0o644); err != nil {
		return failf("%v", err)
	}
	return nil
}

// cmdLinkDown removes a link no member holds; nothing there is no error (E71).
func (c *cli) cmdLinkDown(_ context.Context, args []string) error {
	if len(args) != 1 {
		return usagef("usage: vmtest link-down <link>")
	}
	name := args[0]
	if err := linkName(name); err != nil {
		return err
	}
	holders, err := c.linkHolders(name)
	if err != nil {
		return err
	}
	if len(holders) > 0 {
		return failf("link %s is held by %s; remove them with: vmtest member-down <name>", name, strings.Join(holders, ", "))
	}
	dir := c.paths.at(linksDir, name)
	if _, err := os.Lstat(dir); err == nil {
		if err := os.RemoveAll(dir); err != nil {
			return failf("cannot remove %s: %v", dir, err)
		}
		c.logf("removed %s", dir)
	}
	c.sayf("VMTEST-LINK-DOWN %s", name)
	return nil
}
