package vmtest

import (
	"bytes"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

// E68 and E57's extension: member-up --link gives a member a NIC on a link, records its end and
// its MAC before the boot, and prints one VMTEST-MEMBER-NIC line per link.

// linkedKit is a member kit whose lab already has the named links, made in this order, so the
// first link's ports are a=41000 and b=41001, the second's 41002 and 41003.
func linkedKit(t *testing.T, links ...string) *kit {
	t.Helper()
	k := memberKit(t)
	for _, l := range links {
		k.wantCode(k.run("link-up", l), term.ExitOK)
	}
	return k
}

// networksOf is the value of every --network option, in order.
func networksOf(args []string) []string {
	var out []string
	for i, a := range args {
		if a == "--network" && i+1 < len(args) {
			out = append(out, args[i+1])
		}
	}
	return out
}

// E68: one NIC per link, after the passt NIC, in the order given, each end's own port local and
// the other end's the peer; the record and the printed lines carry the MACs.
func TestE68MemberUpLinks(t *testing.T) {
	k := linkedKit(t, "wan", "lan")
	k.random = bytes.NewReader([]byte{1, 2, 3, 4, 5, 6})
	k.wantCode(k.run("member-up", "m1", "server-fast", "--link", "wan:a", "--link", "lan:b"), term.ExitOK)
	wantOut := "VMTEST-MEMBER-UP m1 domain=vmtest-member-m1\n" +
		"VMTEST-MEMBER-NIC m1 link=wan end=a mac=52:54:00:01:02:03\n" +
		"VMTEST-MEMBER-NIC m1 link=lan end=b mac=52:54:00:04:05:06\n"
	if k.out.String() != wantOut {
		t.Fatalf("stdout %q, want %q", k.out.String(), wantOut)
	}
	nets := networksOf(k.mustCall("virt-install").Args)
	want := []string{
		passtNetwork(40123),
		"type=udp,source.address=127.0.0.1,source.port=41001,source.local.address=127.0.0.1,source.local.port=41000,model.type=virtio,mac.address=52:54:00:01:02:03",
		"type=udp,source.address=127.0.0.1,source.port=41002,source.local.address=127.0.0.1,source.local.port=41003,model.type=virtio,mac.address=52:54:00:04:05:06",
	}
	if strings.Join(nets, "\n") != strings.Join(want, "\n") {
		t.Fatalf("networks:\n%s\nwant:\n%s", strings.Join(nets, "\n"), strings.Join(want, "\n"))
	}
	if got := k.read("members/m1/links"); got != "wan a 52:54:00:01:02:03\nlan b 52:54:00:04:05:06\n" {
		t.Fatalf("the record is %q", got)
	}

	k = memberKit(t)
	k.wantCode(k.run("member-up", "m9", "server-fast"), term.ExitOK)
	if k.exists("members/m9/links") || k.out.String() != "VMTEST-MEMBER-UP m9 domain=vmtest-member-m9\n" {
		t.Fatalf("a member with no link: stdout %q", k.out.String())
	}
	if n := len(networksOf(k.mustCall("virt-install").Args)); n != 1 {
		t.Fatalf("a member with no link got %d NICs", n)
	}
}

// E68: a command line that cannot be run is a usage error that touches nothing, and a link that
// was never made says how to make it.
func TestE68MemberUpLinkUsage(t *testing.T) {
	five := []string{"--link", "l1:a", "--link", "l2:a", "--link", "l3:a", "--link", "l4:a", "--link", "l5:a"}
	for _, tc := range []struct {
		args []string
		msg  string
	}{
		{[]string{"--link"}, "--link wants <link>:<a|b>; got ''"},
		{[]string{"--link", "wan"}, "--link wants <link>:<a|b>; got 'wan'"},
		{[]string{"--link", "wan:c"}, "--link wants <link>:<a|b>; got 'wan:c'"},
		{[]string{"--link", "Bad:a"}, "--link wants <link>:<a|b>; got 'Bad:a'"},
		{[]string{"--link", "wan:a", "--link", "wan:a"}, "the link end wan:a is given twice"},
		{five, "at most 4 --link options; got 5"},
		{[]string{"--bogus"}, "unknown option --bogus"},
		{[]string{"--link", "nolink:a"}, "no link nolink; make it with: vmtest link-up nolink"},
	} {
		k := linkedKit(t, "wan")
		k.wantCode(k.run(append([]string{"member-up", "m1", "server-fast"}, tc.args...)...), term.ExitUsage)
		k.wantErr("ERROR: " + tc.msg)
		if len(k.fake.Calls()) != 0 || k.exists("members/m1") {
			t.Fatalf("%v: a usage error touched the lab: %v", tc.args, k.lines(""))
		}
	}
}

// E68: an end another member records is refused, naming it; the other end of the link is free;
// a member asking again for its own end is told it exists, not that the end is held.
func TestE68MemberUpHeldEnd(t *testing.T) {
	k := linkedKit(t, "wan")
	k.random = bytes.NewReader([]byte{1, 1, 1, 2, 2, 2})
	k.wantCode(k.run("member-up", "m1", "server-fast", "--link", "wan:a"), term.ExitOK)

	k.wantCode(k.run("member-up", "m2", "server-fast", "--link", "wan:a"), term.ExitFailure)
	k.wantErr("ERROR: link wan end a is held by m1; remove it with: vmtest member-down m1")
	if k.count("virt-install") != 1 || k.exists("members/m2") {
		t.Fatalf("a held end was booted: %v", k.lines("virt-install"))
	}

	k.wantCode(k.run("member-up", "m1", "server-fast", "--link", "wan:a"), term.ExitFailure)
	k.wantErr("member m1 already exists")

	k.wantCode(k.run("member-up", "m2", "server-fast", "--link", "wan:b"), term.ExitOK)
	if got := k.read("members/m2/links"); got != "wan b 52:54:00:02:02:02\n" {
		t.Fatalf("the record is %q", got)
	}
}

// E68: a MAC another member's record holds is refused before anything is created.
func TestE68MemberUpMACIsUnique(t *testing.T) {
	k := linkedKit(t, "wan")
	k.write("members/m0/links", "wan b 52:54:00:00:00:01\n")
	k.random = bytes.NewReader([]byte{0, 0, 1})
	k.wantCode(k.run("member-up", "m1", "server-fast", "--link", "wan:a"), term.ExitFailure)
	k.wantErr("ERROR: the MAC 52:54:00:00:00:01 is already recorded by m0")
	if k.count("virt-install") != 0 || k.exists("members/m1") {
		t.Fatal("a member with a taken MAC was created")
	}

	k = linkedKit(t, "wan", "lan")
	k.random = bytes.NewReader([]byte{9, 9, 9, 9, 9, 9})
	k.wantCode(k.run("member-up", "m1", "server-fast", "--link", "wan:a", "--link", "lan:a"), term.ExitFailure)
	k.wantErr("ERROR: the MAC 52:54:00:09:09:09 is already recorded by m1")
	if k.count("virt-install") != 0 || k.exists("members/m1") {
		t.Fatal("a member with two NICs of one MAC was created")
	}

	k = linkedKit(t, "wan")
	k.random = bytes.NewReader([]byte{1, 2})
	k.wantCode(k.run("member-up", "m1", "server-fast", "--link", "wan:a"), term.ExitFailure)
	k.wantErr("ERROR: cannot draw a MAC address:")
	if k.exists("members/m1") {
		t.Fatal("a member was created without a MAC")
	}
}

// E57, extended: a member-up that does not finish leaves no record, so its link is free again.
func TestE57FailedMemberHoldsNoLink(t *testing.T) {
	for _, fail := range []func(*kit){
		func(k *kit) {
			k.lv.state = "shut off"
			k.fake.On([]string{"ssh"}, execx.Result{ExitCode: 255})
		},
		func(k *kit) { k.lv.virtFails = true },
	} {
		k := linkedKit(t, "wan")
		fail(k)
		k.random = bytes.NewReader([]byte{1, 2, 3})
		k.wantCode(k.run("member-up", "m3", "server-fast", "--link", "wan:a"), term.ExitFailure)
		if k.exists("members/m3/links") {
			t.Fatal("a member that never came up still holds its link end")
		}
		if !k.exists("members/m3") {
			t.Fatal("the directory and its console log were not kept")
		}
		k.wantCode(k.run("link-down", "wan"), term.ExitOK)
		if k.exists("links/wan") {
			t.Fatal("the free link was kept")
		}
	}
}
