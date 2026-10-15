package vmtest

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
)

func testGuest(k *kit) *guest {
	c := newCLI(k.environment())
	return &guest{c: c, key: k.path(sshKeyFile), domain: "vmtest-stub", port: 4022, dir: "/stub/build"}
}

// E22: the options every guest ssh and scp carries.
func TestE22GuestSSHAndSCPOptions(t *testing.T) {
	k := newKit(t)
	k.fake.On([]string{"ssh"}, execx.Result{Stdout: []byte("out\n")})
	k.fake.On([]string{"scp"}, execx.Result{})
	g := testGuest(k)
	out, err := g.output(context.Background(), "uname -r")
	if err != nil || out != "out\n" {
		t.Fatalf("got %q %v", out, err)
	}
	if err := g.scp(context.Background(), "/local/f", "/tmp/f"); err != nil {
		t.Fatal(err)
	}
	key := k.path(sshKeyFile)
	want := []string{
		"ssh -o BatchMode=yes -o IdentitiesOnly=yes -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null -o LogLevel=ERROR -o ConnectTimeout=5 -i " + key + " -p 4022 fedora@127.0.0.1 uname -r",
		"scp -o BatchMode=yes -o IdentitiesOnly=yes -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null -o LogLevel=ERROR -i " + key + " -P 4022 /local/f fedora@127.0.0.1:/tmp/f",
	}
	for i, c := range k.fake.Calls() {
		if c.Line() != want[i] {
			t.Fatalf("call %d:\n got %s\nwant %s", i, c.Line(), want[i])
		}
	}
}

// E24 (reboot-dispatch case 8): a guest that answers SSH ends the wait at once, no sleep.
func TestE24AnsweringGuestEndsTheWait(t *testing.T) {
	k := newKit(t)
	k.fake.On([]string{"virsh", "-c", libvirtURI, "domstate"}, execx.Result{Stdout: []byte("running\n\n")})
	k.fake.On([]string{"ssh"}, execx.Result{})
	if err := testGuest(k).waitSSH(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(k.slept) != 0 {
		t.Fatalf("slept %v", k.slept)
	}
	k.wantErr("==> guest answered SSH after ~0s")
}

// E23 (reboot-dispatch case 9): a stopped guest is named at once, not waited out.
func TestE23StoppedGuestIsNamedAtOnce(t *testing.T) {
	for _, state := range []string{"shut off", "crashed"} {
		k := newKit(t)
		k.fake.On([]string{"virsh", "-c", libvirtURI, "domstate"}, execx.Result{Stdout: []byte(state + "\n")})
		k.fake.On([]string{"ssh"}, execx.Result{ExitCode: 255})
		err := testGuest(k).waitSSH(context.Background())
		if err == nil || err.Error() != "guest vmtest-stub is "+state+" while waiting for SSH; see /stub/build/console.log" {
			t.Fatalf("got %v", err)
		}
		if len(k.slept) != 0 || k.count("ssh ") != 0 {
			t.Fatalf("waited on a stopped guest: slept %v", k.slept)
		}
	}
	k := newKit(t)
	k.fake.On([]string{"virsh"}, execx.Result{ExitCode: 1})
	err := testGuest(k).waitSSH(context.Background())
	if err == nil || !strings.Contains(err.Error(), "cannot read the state of guest vmtest-stub while waiting for SSH") {
		t.Fatalf("got %v", err)
	}
}

// E24, B12: the wait is bounded by the clock, not by counting its own sleeps.
func TestE24SSHWaitIsBoundedByTheClock(t *testing.T) {
	k := newKit(t)
	k.fake.On([]string{"virsh", "-c", libvirtURI, "domstate"}, execx.Result{Stdout: []byte("running\n")})
	k.fake.OnFunc([]string{"ssh"}, func(execx.Cmd) (execx.Result, error) {
		k.now = k.now.Add(5 * time.Second) // each attempt spends its ConnectTimeout
		return execx.Result{ExitCode: 255}, nil
	})
	err := testGuest(k).waitSSH(context.Background())
	if err == nil || err.Error() != "guest did not answer SSH within 600s; see /stub/build/console.log" {
		t.Fatalf("got %v", err)
	}
	if n := len(k.slept); n != 60 {
		t.Fatalf("slept %d times; the clock moved 10s per try, so 60 tries fill 600s", n)
	}
	k.wantErr("==> waiting for SSH on 127.0.0.1:4022 (up to 600s)")
}

// E25: the power-off wait.
func TestE25ShutoffWait(t *testing.T) {
	k := newKit(t)
	states := []string{"running", "in shutdown", "shut off"}
	k.fake.OnFunc([]string{"virsh", "-c", libvirtURI, "domstate"}, func(execx.Cmd) (execx.Result, error) {
		s := states[0]
		states = states[1:]
		return execx.Result{Stdout: []byte(s + "\n")}, nil
	})
	if err := testGuest(k).waitShutoff(context.Background()); err != nil || len(k.slept) != 2 {
		t.Fatalf("got %v, slept %v", err, k.slept)
	}
	k = newKit(t)
	k.fake.On([]string{"virsh", "-c", libvirtURI, "domstate"}, execx.Result{Stdout: []byte("running\n")})
	err := testGuest(k).waitShutoff(context.Background())
	if err == nil || err.Error() != "guest did not shut off within 300s (state: running)" {
		t.Fatalf("got %v", err)
	}
	k = newKit(t)
	k.fake.On([]string{"virsh"}, execx.Result{ExitCode: 1})
	if err := testGuest(k).waitShutoff(context.Background()); err == nil {
		t.Fatal("a failed domstate was taken as a shut-off guest")
	}
}

// E26: the LUKS unlock reports its console lines and names the stall; the follower starts.
func TestE26ConsoleUnlock(t *testing.T) {
	k := newKit(t)
	k.fake.OnFunc(helperArgv("serial_console"), func(c execx.Cmd) (execx.Result, error) {
		if strings.Contains(c.Line(), "--unlock-with") {
			return execx.Result{Stdout: []byte("noise\nVMTEST-CONSOLE stalled at=luks reason=no prompt in 600s\n"), ExitCode: 1}, nil
		}
		return execx.Result{}, nil
	})
	g := testGuest(k)
	f, err := g.unlock(context.Background(), "/run/dir", "/base/luks-passphrase")
	if err == nil || f != nil || err.Error() != "the guest did not reach userspace (stage: boot): reason=no prompt in 600s" {
		t.Fatalf("got %v", err)
	}
	k.wantErr("VMTEST-CONSOLE stalled at=luks reason=no prompt in 600s\n")
	if strings.Contains(k.err.String(), "noise") || k.count("--follow") != 0 {
		t.Fatalf("stderr %q", k.err.String())
	}
	c := k.mustCall("--unlock-with")
	if !strings.Contains(c.Line(), "--socket /run/dir/serial.sock --log /run/dir/console.log --unlock-with /base/luks-passphrase --until login:  --timeout 600 --connect-wait 60 --domain vmtest-stub --libvirt-uri qemu:///session") {
		t.Fatalf("got %s", c.Line())
	}

	k = newKit(t)
	k.fake.On(helperArgv("serial_console"), execx.Result{Stdout: []byte("VMTEST-CONSOLE userspace\n")})
	f, err = testGuest(k).unlock(context.Background(), t.TempDir(), "/pp")
	if err != nil || f == nil {
		t.Fatalf("got %v", err)
	}
	f.stop()
	if k.count("--follow --timeout 86400 --connect-wait 120") != 1 {
		t.Fatalf("no follower: %v", k.lines("serial_console"))
	}
}

// E26: the follower's own end is judged: 0 and SIGTERM (143) are its normal ends.
func TestE26FollowerStop(t *testing.T) {
	for code, logged := range map[int]bool{0: false, 143: false, 1: true} {
		k := newKit(t)
		k.fake.On(helperArgv("serial_console"), execx.Result{ExitCode: code})
		f := newCLI(k.environment()).follow(context.Background(), t.TempDir())
		f.stop()
		if got := strings.Contains(k.err.String(), "console follower exited"); got != logged {
			t.Fatalf("exit %d: logged %v: %q", code, got, k.err.String())
		}
	}
	var none *follower
	none.stop()
	k := newKit(t)
	k.fake.OnFunc(helperArgv("serial_console"), func(execx.Cmd) (execx.Result, error) { return execx.Result{}, context.Canceled })
	newCLI(k.environment()).follow(context.Background(), t.TempDir()).stop()
	if k.err.Len() != 0 {
		t.Fatalf("a cancelled follower was logged: %q", k.err.String())
	}
	k.fake.OnFunc(helperArgv("serial_console"), func(execx.Cmd) (execx.Result, error) { return execx.Result{}, errors.New("boom") })
	newCLI(k.environment()).follow(context.Background(), t.TempDir()).stop()
	k.wantErr("console follower")
}
