package vmtest

import (
	"context"
	"os"
	"slices"
	"strings"
	"syscall"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

// Case accounting for the port of scripts/test-vmtest-member.bash (its 44 checks): every one
// is here, by its label. Two change on purpose: a missing base key is a usage error, exit 64
// rather than 1 (B4); and SIGTERM's 143 comes from the cancelled context, as it now does for
// every command (E4).

// libvirt is a fake libvirt: the domains it knows, what domstate answers, and whether
// virt-install fails. Every virsh and virt-install call goes through it.
type libvirt struct {
	domains   []string
	state     string
	virtFails bool
}

func (k *kit) withLibvirt() *libvirt {
	lv := &libvirt{state: "running"}
	k.lv = lv
	k.fake.OnFunc([]string{"virsh"}, func(c execx.Cmd) (execx.Result, error) {
		args := c.Args[2:] // -c <uri>
		switch args[0] {
		case "list":
			return execx.Result{Stdout: []byte(strings.Join(lv.domains, "\n") + "\n")}, nil
		case "domstate":
			return execx.Result{Stdout: []byte(lv.state + "\n")}, nil
		case "dominfo":
			if slices.Contains(lv.domains, args[1]) {
				return execx.Result{}, nil
			}
			return execx.Result{ExitCode: 1}, nil
		case "undefine":
			lv.domains = slices.DeleteFunc(lv.domains, func(d string) bool { return d == args[len(args)-1] })
		}
		return execx.Result{}, nil
	})
	k.fake.OnFunc([]string{"virt-install"}, func(c execx.Cmd) (execx.Result, error) {
		if lv.virtFails {
			return execx.Result{ExitCode: 1}, nil
		}
		lv.domains = append(lv.domains, argAfter(c.Args, "--name"))
		return execx.Result{}, nil
	})
	k.fake.OnFunc([]string{"qemu-img"}, func(c execx.Cmd) (execx.Result, error) {
		if c.Args[0] == "create" {
			k.writeAbs(createdImage(c.Args), "")
		}
		return execx.Result{}, nil
	})
	k.fake.On(helperArgv("retention"), execx.Result{})
	return lv
}

// createdImage is the file `qemu-img create` makes: the last argument of an overlay (which
// names its backing file with -b), the one before the size otherwise.
func createdImage(args []string) string {
	if slices.Contains(args, "-b") {
		return args[len(args)-1]
	}
	return args[len(args)-2]
}

func memberKit(t *testing.T) *kit {
	t.Helper()
	k := newKit(t)
	k.builtBase("server-fast-44")
	k.gate(0, "current")
	k.withLibvirt()
	k.fake.On([]string{"ssh"}, execx.Result{})
	return k
}

// E55: a name is checked before anything runs.
func TestE55MemberNames(t *testing.T) {
	for _, bad := range []string{"Bad", "../x", "-x", "a/b", "", "a_b", strings.Repeat("a", 41)} {
		k := memberKit(t)
		k.wantCode(k.run("member-down", bad), term.ExitFailure)
		k.wantErr("a member name is 1 to 40 of a-z, 0-9 and '-', not starting with '-'; got '" + bad + "'")
		if k.count("virsh") != 0 {
			t.Fatalf("%q reached virsh", bad)
		}
	}
	k := memberKit(t)
	k.wantCode(k.run("member-up", "Bad", "server-fast"), term.ExitFailure)
	if len(k.fake.Calls()) != 0 {
		t.Fatal("a bad name reached the lab")
	}
}

// E59: member-down on nothing, a running member, a stopped one; a failed list is a failure.
func TestE59MemberDown(t *testing.T) {
	k := memberKit(t)
	k.wantCode(k.run("member-down", "agent-bus-acceptance-u23"), term.ExitOK)
	if k.count("destroy") != 0 || k.count("undefine") != 0 || k.out.String() != "VMTEST-MEMBER-DOWN agent-bus-acceptance-u23\n" {
		t.Fatalf("calls %v stdout %q", k.lines("virsh"), k.out.String())
	}

	k = memberKit(t)
	k.lv.domains = []string{"vmtest-member-agent-bus-acceptance-u23"}
	k.write("members/agent-bus-acceptance-u23/overlay.qcow2", "")
	k.wantCode(k.run("member-down", "agent-bus-acceptance-u23"), term.ExitOK)
	if k.count("destroy vmtest-member-agent-bus-acceptance-u23") != 1 || k.count("undefine --nvram vmtest-member-agent-bus-acceptance-u23") != 1 {
		t.Fatalf("calls %v", k.lines("virsh"))
	}
	if k.exists("members/agent-bus-acceptance-u23") {
		t.Fatal("its directory was kept")
	}
	k.wantErr("==> removed guest vmtest-member-agent-bus-acceptance-u23")

	k = memberKit(t)
	k.lv.domains, k.lv.state = []string{"vmtest-member-m0"}, "shut off"
	k.wantCode(k.run("member-down", "m0"), term.ExitOK)
	if k.count("destroy") != 0 || k.count("undefine --nvram vmtest-member-m0") != 1 {
		t.Fatalf("calls %v", k.lines("virsh"))
	}

	k = newKit(t)
	k.fake.On([]string{"virsh"}, execx.Result{ExitCode: 1})
	k.wantCode(k.run("member-down", "m0"), term.ExitFailure)
	k.wantErr("ERROR: virsh -c qemu:///session list failed")
	if k.out.Len() != 0 {
		t.Fatal("a failed list was reported as a member removed")
	}

	k = memberKit(t)
	k.lv.domains = []string{"vmtest-member-m0-extra"}
	k.wantCode(k.run("member-down", "m0"), term.ExitOK)
	if k.count("undefine") != 0 {
		t.Fatal("a domain whose name only starts with the member's was removed")
	}
}

// E56: member-up boots and leaves the guest running; the refusals touch nothing.
func TestE56MemberUp(t *testing.T) {
	k := memberKit(t)
	k.wantCode(k.run("member-up", "m1", "server-fast"), term.ExitOK)
	if k.out.String() != "VMTEST-MEMBER-UP m1 domain=vmtest-member-m1\n" || !slices.Equal(k.lv.domains, []string{"vmtest-member-m1"}) {
		t.Fatalf("stdout %q domains %v", k.out.String(), k.lv.domains)
	}
	if !k.exists("members/m1/overlay.qcow2") || k.read("members/m1/ssh-port") != "40123\n" {
		t.Fatal("no overlay or no port in the member's directory")
	}
	if k.count("backend.type=passt") != 1 || k.count("retention floor --path "+k.path("members")+" --base-size 4 --step run --guest-mib 4096") != 1 {
		t.Fatalf("calls %v", k.lines(""))
	}
	assertNoProbeFiles(t, k)

	k.wantCode(k.run("member-up", "m1", "server-fast"), term.ExitFailure)
	k.wantErr("member m1 already exists (" + k.path("members/m1") + " or guest vmtest-member-m1); remove it with: vmtest member-down m1")
	if k.count("virt-install") != 1 {
		t.Fatal("a second boot of an existing member")
	}

	k = memberKit(t)
	k.wantCode(k.run("member-up", "m1"), term.ExitUsage)
	if k.count("virt-install") != 0 {
		t.Fatal("booted without a base key")
	}

	k = memberKit(t)
	k.gate(1, "reinstall")
	k.wantCode(k.run("member-up", "m2", "server-fast"), term.ExitFailure)
	k.wantErr("ERROR: base server-fast-44 may not be used (freshness: reinstall); refresh it with: vmtest ensure-base server-fast")
	k.wantErr("VMTEST-FRESHNESS-VERDICT decision=reinstall")
	if k.count("virt-install") != 0 || k.exists("members/m2") {
		t.Fatal("a refused base was booted")
	}

	k = memberKit(t)
	k.base("desktop", "desktop-44", "full", "desktop", "everything")
	k.wantCode(k.run("member-up", "m2", "desktop"), term.ExitFailure)
	k.wantErr("a member boots a server base; desktop-44 is desktop")

	k = memberKit(t)
	k.base("server-other", "server-other-44", "fast", "server", "-")
	k.wantCode(k.run("member-up", "m2", "server-other"), term.ExitFailure)
	k.wantErr("base server-other-44 is absent; build it with: vmtest ensure-base server-other")
}

// E57: a member-up that does not finish leaves no guest, overlay or port, and keeps the log.
func TestE57MemberUpTeardown(t *testing.T) {
	k := memberKit(t)
	k.lv.state = "shut off"
	k.fake.On([]string{"ssh"}, execx.Result{ExitCode: 255})
	k.wantCode(k.run("member-up", "m3", "server-fast"), term.ExitFailure)
	if k.count("undefine --nvram vmtest-member-m3") != 1 || k.exists("members/m3/overlay.qcow2") || k.exists("members/m3/ssh-port") {
		t.Fatalf("left behind: calls %v", k.lines("virsh"))
	}
	if !k.exists("members/m3") {
		t.Fatal("the directory and its console log were not kept")
	}
	k.wantErr("==> member not started; its console log is kept in " + k.path("members/m3") + " until: vmtest member-down m3")
	k.wantCode(k.run("member-ssh", "m3", "true"), term.ExitFailure)
	if k.count("fedora@127.0.0.1 true") != 0 {
		t.Fatal("member-ssh reached a member that never came up")
	}

	k = memberKit(t)
	k.lv.virtFails = true
	k.wantCode(k.run("member-up", "m4", "server-fast"), term.ExitFailure)
	if k.exists("members/m4/overlay.qcow2") {
		t.Fatal("virt-install failed and the overlay was kept")
	}
}

// E4, E57: SIGTERM in the SSH wait: 143, never 0; the guest, overlay and port are removed.
func TestE57MemberUpSignal(t *testing.T) {
	k := memberKit(t)
	ctx, cancel := context.WithCancelCause(context.Background())
	k.fake.OnFunc([]string{"ssh"}, func(execx.Cmd) (execx.Result, error) {
		cancel(InterruptError{Signal: syscall.SIGTERM})
		return execx.Result{}, context.Canceled
	})
	k.wantCode(k.runCtx(ctx, "member-up", "m6", "server-fast"), 143)
	if k.count("destroy vmtest-member-m6") != 1 || k.count("undefine --nvram vmtest-member-m6") != 1 {
		t.Fatalf("calls %v", k.lines("virsh"))
	}
	if k.exists("members/m6/overlay.qcow2") || k.exists("members/m6/ssh-port") {
		t.Fatal("overlay or port left behind")
	}
	assertNoProbeFiles(t, k)
}

// E58: member-ssh is bounded, keeps the remote status and passes the streams through.
func TestE58MemberSSH(t *testing.T) {
	k := newKit(t)
	k.wantCode(k.run("member-ssh", "nobody", "true"), term.ExitFailure)
	k.wantErr("no member nobody is up (no " + k.path("members/nobody/ssh-port") + "); start one with: vmtest member-up nobody <base-key>")
	if k.count("ssh") != 0 {
		t.Fatal("ssh ran for no member")
	}

	k = newKit(t)
	k.write("members/m5/ssh-port", "40123\n")
	k.stdin = "piped input"
	k.fake.On([]string{"ssh"}, execx.Result{Stdout: []byte("remote out\n"), Stderr: []byte("remote err\n"), ExitCode: 7})
	k.wantCode(k.run("member-ssh", "m5", "sudo", "-n", "true"), 7)
	c := k.mustCall("ssh ")
	if !strings.HasSuffix(c.Line(), "-o ServerAliveInterval=15 -o ServerAliveCountMax=4 -i "+k.path(sshKeyFile)+" -p 40123 fedora@127.0.0.1 sudo -n true") {
		t.Fatalf("got %s", c.Line())
	}
	if stdinText(t, c) != "piped input" || k.out.String() != "remote out\n" || k.err.String() != "remote err\n" {
		t.Fatalf("streams: out %q err %q", k.out.String(), k.err.String())
	}

	k = newKit(t)
	k.write("members/m5/ssh-port", "40123\n")
	k.env["VMTEST_MEMBER_SSH_TIMEOUT"] = "1"
	k.fake.OnFunc([]string{"ssh"}, func(execx.Cmd) (execx.Result, error) {
		return execx.Result{}, context.DeadlineExceeded
	})
	k.tweak = func(c *cli) { c.bound = expiredBound }
	k.wantCode(k.run("member-ssh", "m5", "dnf", "-y", "install", "git-core"), 124)
	k.wantErr("ERROR: member-ssh m5: the command did not finish within 1s (VMTEST_MEMBER_SSH_TIMEOUT)")

	for _, bad := range []string{"soon", "0", "-5", "1.5"} {
		k = newKit(t)
		k.write("members/m5/ssh-port", "40123\n")
		k.env["VMTEST_MEMBER_SSH_TIMEOUT"] = bad
		k.wantCode(k.run("member-ssh", "m5", "true"), term.ExitFailure)
		k.wantErr("VMTEST_MEMBER_SSH_TIMEOUT must be a positive number of seconds, got '" + bad + "'")
	}

	k = newKit(t)
	k.write("members/m5/ssh-port", "not-a-port\n")
	k.wantCode(k.run("member-ssh", "m5", "true"), term.ExitFailure)
	k.wantErr("does not hold a port")

	k = newKit(t)
	k.write("members/m5/ssh-port", "40123\n")
	if err := os.Remove(k.path(sshKeyFile)); err != nil {
		t.Fatal(err)
	}
	k.wantCode(k.run("member-ssh", "m5", "true"), term.ExitFailure)
	k.wantErr("no lab SSH key at")
}
