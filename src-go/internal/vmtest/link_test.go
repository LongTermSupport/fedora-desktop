package vmtest

import (
	"errors"
	"net"
	"strconv"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/fifotest"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

// E67: link-up chooses two distinct loopback UDP ports, records them, and starts nothing.
func TestE67LinkUp(t *testing.T) {
	k := newKit(t)
	k.wantCode(k.run("link-up", "x30-n1"), term.ExitOK)
	if got := k.read("links/x30-n1/ports"); got != "a=41000\nb=41001\n" {
		t.Fatalf("ports file %q", got)
	}
	if got := k.out.String(); got != "VMTEST-LINK-UP x30-n1 a=41000 b=41001\n" {
		t.Fatalf("stdout %q", got)
	}
	if calls := k.fake.Calls(); len(calls) != 0 {
		t.Fatalf("link-up ran %v: a link is no libvirt object", calls)
	}

	k = newKit(t)
	k.write("links/x30-n1/ports", "a=1\nb=2\n")
	k.wantCode(k.run("link-up", "x30-n1"), term.ExitFailure)
	k.wantErr("link x30-n1 exists; remove it with: vmtest link-down x30-n1")
	if got := k.read("links/x30-n1/ports"); got != "a=1\nb=2\n" {
		t.Fatalf("an existing link was rewritten: %q", got)
	}

	k = newKit(t)
	k.udpErr = errors.New("no ports")
	k.wantCode(k.run("link-up", "x30-n1"), term.ExitFailure)
	k.wantErr("no ports")
	if k.exists("links/x30-n1") {
		t.Fatal("a failed link-up left its directory")
	}
}

// E67: the name is E55's, checked before anything reaches a path; the arguments are exactly one.
func TestE67LinkNamesAndUsage(t *testing.T) {
	for _, bad := range []string{"Bad", "../x", "-x", "a/b", "a_b", strings.Repeat("a", 41)} {
		k := newKit(t)
		k.wantCode(k.run("link-up", bad), term.ExitFailure)
		k.wantErr("a link name is 1 to 40 of a-z, 0-9 and '-', not starting with '-'; got '" + bad + "'")
		if k.exists("links") {
			t.Fatalf("%q reached the lab's disk", bad)
		}
	}
	for _, args := range [][]string{{"link-up"}, {"link-up", "a", "b"}, {"link-down"}, {"link-down", "a", "b"}} {
		k := newKit(t)
		k.wantCode(k.run(args...), term.ExitUsage)
		k.wantErr("usage: vmtest " + args[0] + " <link>")
	}
}

// E71: link-down on nothing is no error, removes the directory, and is refused while a member
// records an end of it, naming the members.
func TestE71LinkDown(t *testing.T) {
	k := newKit(t)
	k.wantCode(k.run("link-down", "x30-n1"), term.ExitOK)
	if got := k.out.String(); got != "VMTEST-LINK-DOWN x30-n1\n" {
		t.Fatalf("stdout %q", got)
	}

	k = newKit(t)
	k.write("links/x30-n1/ports", "a=1\nb=2\n")
	k.wantCode(k.run("link-down", "x30-n1"), term.ExitOK)
	if k.exists("links/x30-n1") {
		t.Fatal("the link's directory was kept")
	}

	k = newKit(t)
	k.write("links/x30-n1/ports", "a=1\nb=2\n")
	k.write("members/bus/links", "x30-n1 a 52:54:00:00:00:01\nx30-n2 a 52:54:00:00:00:02\n")
	k.write("members/member/links", "x30-n1 b 52:54:00:00:00:03\n")
	k.write("members/other/links", "x30-n3 a 52:54:00:00:00:04\n")
	k.wantCode(k.run("link-down", "x30-n1"), term.ExitFailure)
	k.wantErr("link x30-n1 is held by bus, member; remove them with: vmtest member-down <name>")
	if !k.exists("links/x30-n1/ports") || k.out.Len() != 0 {
		t.Fatalf("a held link was removed or reported: stdout %q", k.out.String())
	}
}

// E71: a member's links record that is not a regular file is refused, never opened: a named pipe
// there would block link-down for ever (project_walk_open).
func TestE71LinkDownRefusesANonRegularRecord(t *testing.T) {
	k := newKit(t)
	k.write("links/x30-n1/ports", "a=1\nb=2\n")
	k.write("members/bus/overlay.qcow2", "")
	opened := fifotest.Plant(t, k.path("members/bus/links"))
	k.wantCode(k.run("link-down", "x30-n1"), term.ExitFailure)
	k.wantErr("is not a regular file")
	if opened() {
		t.Fatal("link-down opened a named pipe")
	}
	if !k.exists("links/x30-n1/ports") {
		t.Fatal("a link was removed while a member's record could not be read")
	}
}

// E67: link-up never records a port another link holds: a link's ports are bound only when its
// members boot, so the free-port probe alone cannot see them. A drawn port another link holds
// is drawn again, a bounded number of times.
func TestE67LinkUpSkipsPortsAnotherLinkHolds(t *testing.T) {
	k := newKit(t)
	k.write("links/x30-n0/ports", "a=41000\nb=41003\n")
	k.wantCode(k.run("link-up", "x30-n1"), term.ExitOK)
	if got := k.read("links/x30-n1/ports"); got != "a=41004\nb=41005\n" {
		t.Fatalf("ports file %q: a port of link x30-n0 was taken again", got)
	}

	k = newKit(t)
	k.write("links/x30-n0/ports", "a=41000\nb=41001\n")
	k.udpRepeat = true
	k.wantCode(k.run("link-up", "x30-n1"), term.ExitFailure)
	k.wantErr("no two free UDP ports another link does not hold")
	if k.exists("links/x30-n1") {
		t.Fatal("a failed link-up left its directory")
	}
}

// The real source of ports gives distinct ones a UDP socket can bind at once.
func TestFreeLoopbackUDPPorts(t *testing.T) {
	ports, err := freeLoopbackUDPPorts(2)
	if err != nil {
		t.Fatal(err)
	}
	if len(ports) != 2 || ports[0] == ports[1] || ports[0] < 1 || ports[1] < 1 {
		t.Fatalf("ports %v", ports)
	}
	for _, p := range ports {
		c, err := net.ListenPacket("udp", net.JoinHostPort("127.0.0.1", strconv.Itoa(p)))
		if err != nil {
			t.Fatalf("port %d is not free: %v", p, err)
		}
		if err := c.Close(); err != nil {
			t.Fatal(err)
		}
	}
}
