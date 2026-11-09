package vmtest

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"regexp"
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
	ports, err := c.env.FreeUDPPorts(2)
	if err != nil {
		return failf("%v", err)
	}
	if len(ports) != 2 {
		return failf("find a free UDP port: wanted 2, got %d", len(ports))
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

// linkHolders is the members that record an end of the link, in name order.
func (c *cli) linkHolders(link string) ([]string, error) {
	entries, err := os.ReadDir(c.paths.at(membersDir))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, failf("cannot read %s: %v", c.paths.at(membersDir), err)
	}
	var holders []string
	for _, e := range entries {
		data, rerr := os.ReadFile(c.paths.at(membersDir, e.Name(), memberLinksFile))
		if errors.Is(rerr, os.ErrNotExist) {
			continue
		}
		if rerr != nil {
			return nil, failf("cannot read the links of member %s: %v", e.Name(), rerr)
		}
		for line := range strings.SplitSeq(string(data), "\n") {
			if fields := strings.Fields(line); len(fields) > 0 && fields[0] == link {
				holders = append(holders, e.Name())
				break
			}
		}
	}
	return holders, nil
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
