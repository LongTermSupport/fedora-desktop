package vmtest

import (
	"os"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

// E72: member-address prints how to reach a member: its ssh port on the loopback, the guest user
// and the lab key, then a line for each NIC the member's record holds. It reads files only.
func TestE72MemberAddress(t *testing.T) {
	k := newKit(t)
	k.write("members/m5/ssh-port", "40123\n")
	k.write("members/m5/links", "wan a 52:54:00:01:02:03\nlan b 52:54:00:04:05:06\n")
	k.wantCode(k.run("member-address", "m5"), term.ExitOK)
	want := "VMTEST-MEMBER-ADDRESS m5 host=127.0.0.1 port=40123 user=fedora key=" + k.path(sshKeyFile) + "\n" +
		"VMTEST-MEMBER-NIC m5 link=wan end=a mac=52:54:00:01:02:03\n" +
		"VMTEST-MEMBER-NIC m5 link=lan end=b mac=52:54:00:04:05:06\n"
	if k.out.String() != want {
		t.Fatalf("stdout %q, want %q", k.out.String(), want)
	}
	if len(k.fake.Calls()) != 0 {
		t.Fatalf("member-address ran %v", k.lines(""))
	}

	k = newKit(t)
	k.write("members/m5/ssh-port", "40123\n")
	k.wantCode(k.run("member-address", "m5"), term.ExitOK)
	if k.out.String() != "VMTEST-MEMBER-ADDRESS m5 host=127.0.0.1 port=40123 user=fedora key="+k.path(sshKeyFile)+"\n" {
		t.Fatalf("a member with no link: stdout %q", k.out.String())
	}
}

// E72: what member-ssh refuses, member-address refuses: a bad name, a member that is not up, a
// port file that holds no port, no lab key; and the wrong number of arguments is usage.
func TestE72MemberAddressRefusals(t *testing.T) {
	k := newKit(t)
	k.wantCode(k.run("member-address", "Bad"), term.ExitFailure)
	k.wantErr("a member name is 1 to 40 of a-z, 0-9 and '-', not starting with '-'; got 'Bad'")

	k = newKit(t)
	k.wantCode(k.run("member-address", "nobody"), term.ExitFailure)
	k.wantErr("no member nobody is up (no " + k.path("members/nobody/ssh-port") + "); start one with: vmtest member-up nobody <base-key>")

	k = newKit(t)
	k.write("members/m5/ssh-port", "not-a-port\n")
	k.wantCode(k.run("member-address", "m5"), term.ExitFailure)
	k.wantErr("does not hold a port")

	k = newKit(t)
	k.write("members/m5/ssh-port", "40123\n")
	if err := os.Remove(k.path(sshKeyFile)); err != nil {
		t.Fatal(err)
	}
	k.wantCode(k.run("member-address", "m5"), term.ExitFailure)
	k.wantErr("no lab SSH key at")

	k = newKit(t)
	k.write("members/m5/ssh-port", "40123\n")
	k.write("members/m5/links", "wan a\n")
	k.wantCode(k.run("member-address", "m5"), term.ExitFailure)
	k.wantErr("holds a line that is not '<link> <end> <mac>'")

	k = newKit(t)
	k.wantCode(k.run("member-address"), term.ExitUsage)
	k.wantCode(k.run("member-address", "a", "b"), term.ExitUsage)
	k.wantErr("usage: vmtest member-address <name>")
}

// E73: member-ssh --as <user> connects as that user; without it, as the guest user.
func TestE73MemberSSHAs(t *testing.T) {
	k := newKit(t)
	k.write("members/m5/ssh-port", "40123\n")
	k.fake.On([]string{"ssh"}, execx.Result{})
	k.wantCode(k.run("member-ssh", "--as", "root", "m5", "id", "-un"), term.ExitOK)
	if c := k.mustCall("ssh "); !strings.HasSuffix(c.Line(), "-i "+k.path(sshKeyFile)+" -p 40123 root@127.0.0.1 id -un") {
		t.Fatalf("got %s", c.Line())
	}

	k = newKit(t)
	k.write("members/m5/ssh-port", "40123\n")
	k.fake.On([]string{"ssh"}, execx.Result{})
	k.wantCode(k.run("member-ssh", "--as", "_svc-1", "m5", "true"), term.ExitOK)
	if c := k.mustCall("ssh "); !strings.Contains(c.Line(), " _svc-1@127.0.0.1 true") {
		t.Fatalf("got %s", c.Line())
	}

	// the option is read before the name only: after it, it is part of the command
	k = newKit(t)
	k.write("members/m5/ssh-port", "40123\n")
	k.fake.On([]string{"ssh"}, execx.Result{})
	k.wantCode(k.run("member-ssh", "m5", "--as", "root", "true"), term.ExitOK)
	if c := k.mustCall("ssh "); !strings.HasSuffix(c.Line(), " fedora@127.0.0.1 --as root true") {
		t.Fatalf("got %s", c.Line())
	}
}

// E73: a user that is not a login name is a usage error before anything runs, so an option cannot
// ride in as a user.
func TestE73MemberSSHAsUsage(t *testing.T) {
	for _, bad := range []string{"Root", "1root", "a b", "", "-oProxyCommand=x", "a@b", strings.Repeat("a", 33), "root:x"} {
		k := newKit(t)
		k.write("members/m5/ssh-port", "40123\n")
		k.fake.On([]string{"ssh"}, execx.Result{})
		k.wantCode(k.run("member-ssh", "--as", bad, "m5", "true"), term.ExitUsage)
		k.wantErr("ERROR: --as wants a login name of 1 to 32 of a-z, 0-9, '_' and '-', not starting with a digit or '-'; got '" + bad + "'")
		if k.count("ssh ") != 0 {
			t.Fatalf("%q reached ssh", bad)
		}
	}

	k := newKit(t)
	k.wantCode(k.run("member-ssh", "--as", "root", "m5"), term.ExitUsage)
	k.wantCode(k.run("member-ssh", "--as"), term.ExitUsage)
	k.wantErr("usage: vmtest member-ssh [--as <user>] <name> <command>")
}
