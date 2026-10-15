package vmtest

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
)

// The reboot, fixture and session steps (E23, E24, E37, E40, E41; the cases numbered below are
// those SPEC-vmtest.md section 6 lists), on the steps of a run driven directly: a guest that
// answers, a transcript on disk.

// newRunning is a run in progress on a guest of the profile, its transcript open.
func newRunning(t *testing.T, k *kit, profile string) *running {
	t.Helper()
	c := newCLI(k.environment())
	dir := k.path("runs/r1")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	f, err := os.Create(filepath.Join(dir, "transcript.log"))
	if err != nil {
		t.Fatal(err)
	}
	l := &lab{paths: c.paths, fedora: testFedora, repoURL: testRepoURL, branch: testBranch}
	r := &running{
		c: c, l: l, id: "r1", dir: dir, baseDir: k.path("bases/b-44"), commit: testCommit, transcript: f,
		s: scenarioFacts{ID: "scen", Profile: profile, Planned: "5", MaxSkipped: "0"},
		b: baseFacts{Name: "b-44", Key: "b", Kind: kindFast, Profile: profile, VCPUs: 2, RAMMiB: 4096},
		g: &guest{c: c, key: k.path(sshKeyFile), domain: "vmtest-run-r1", port: 4022, dir: dir},
	}
	// The run's transcript at the end, which a scrub may have reopened (E45).
	t.Cleanup(func() {
		if err := r.transcript.Close(); err != nil {
			t.Error(err)
		}
	})
	return r
}

func transcriptOf(t *testing.T, r *running) string {
	t.Helper()
	body, err := os.ReadFile(r.transcript.Name())
	if err != nil {
		t.Fatal(err)
	}
	return string(body)
}

// stepKit is a guest that is up, answers SSH, and runs what its remote function says.
func stepKit(t *testing.T, remote func(string) execx.Result) *kit {
	t.Helper()
	k := newKit(t)
	k.withLibvirt().state = "running"
	k.fake.On([]string{"scp"}, execx.Result{})
	k.fake.OnFunc([]string{"ssh"}, func(c execx.Cmd) (execx.Result, error) {
		return remote(c.Args[len(c.Args)-1]), nil
	})
	k.fake.On(helperArgv("serial_console"), execx.Result{})
	return k
}

func bootIDs(ids ...string) func(string) execx.Result {
	return func(remote string) execx.Result {
		if remote == "cat /proc/sys/kernel/random/boot_id" && len(ids) > 0 {
			id := ids[0]
			if len(ids) > 1 {
				ids = ids[1:]
			}
			return execx.Result{Stdout: []byte(id + "\n")}
		}
		return execx.Result{}
	}
}

// Case 1 and E41: a server reboots by the server's mechanics, and the boot must change.
func TestE41ServerReboot(t *testing.T) {
	k := stepKit(t, bootIDs("aaa", "aaa", "bbb"))
	r := newRunning(t, k, profileServer)
	if err := r.reboot(context.Background()); err != nil {
		t.Fatal(err)
	}
	assertInOrder(t, k, []string{"cat /proc/sys/kernel/random/boot_id", "sudo systemd-run --quiet --on-active=2 systemctl reboot", "fedora@127.0.0.1 true", "boot_id", "boot_id"})
	if got := transcriptOf(t, r); got != "# harness: rebooting the guest; the checks judge the boot AFTER this one\n# harness: boot id aaa -> bbb\n" {
		t.Fatalf("transcript %q", got)
	}
	if k.count("loginctl") != 0 || k.count("serial_console") != 0 {
		t.Fatal("a server reboot used the desktop's mechanics")
	}
	if k.slept[0] != 10*time.Second {
		t.Fatalf("slept %v", k.slept)
	}

	k = stepKit(t, bootIDs("same"))
	r = newRunning(t, k, profileServer)
	err := r.reboot(context.Background())
	if err == nil || err.Error() != "the guest still reports boot id same 180s after the reboot request; it never rebooted (stage: boot)" {
		t.Fatalf("got %v", err)
	}

	k = stepKit(t, bootIDs(""))
	r = newRunning(t, k, profileServer)
	if err := r.reboot(context.Background()); err == nil || !strings.Contains(err.Error(), "boot id") {
		t.Fatalf("an unreadable boot id was accepted: %v", err)
	}
}

// Case 2 and E41: a desktop reboots into its session, by unlocking and waiting for seat0.
func TestE41DesktopReboot(t *testing.T) {
	sessions := 0
	k := stepKit(t, func(remote string) execx.Result {
		if remote == "loginctl list-sessions --no-legend" {
			sessions++
			if sessions < 3 {
				return execx.Result{Stdout: []byte("   2 1000 fedora  -     tty2\n")}
			}
			return execx.Result{Stdout: []byte("   3 1000 fedora  seat0 tty2\n")}
		}
		return execx.Result{}
	})
	r := newRunning(t, k, profileDesktop)
	if err := r.reboot(context.Background()); err != nil {
		t.Fatal(err)
	}
	if k.count("--unlock-with "+k.path("bases/b-44/luks-passphrase")) != 1 || sessions != 3 {
		t.Fatalf("unlock %d, polls %d", k.count("--unlock-with"), sessions)
	}
	if got := transcriptOf(t, r); got != "# harness: rebooting the guest after run.bash, as run.bash asks; checks run in the new session\n" {
		t.Fatalf("transcript %q", got)
	}
	if last := k.slept[len(k.slept)-1]; last != 15*time.Second {
		t.Fatalf("no settle after the session appeared: %v", k.slept)
	}
	r.follower.stop()

	k = stepKit(t, func(string) execx.Result { return execx.Result{} })
	r = newRunning(t, k, profileDesktop)
	err := r.reboot(context.Background())
	if err == nil || err.Error() != "no autologin session on seat0 within 180s after the reboot (stage: boot)" {
		t.Fatalf("got %v", err)
	}
	r.follower.stop()
}

// Case 3 and E41: a profile with no mechanics refuses, naming it.
func TestE41UnknownProfileRefuses(t *testing.T) {
	k := stepKit(t, func(string) execx.Result { return execx.Result{} })
	r := newRunning(t, k, "appliance")
	err := r.reboot(context.Background())
	if err == nil || err.Error() != "no reboot mechanics for profile appliance (stage: boot)" {
		t.Fatalf("got %v", err)
	}
	if k.count("ssh") != 0 {
		t.Fatal("an unknown profile reached the guest")
	}
}

// Cases 4-7 and E40: the fixture, if the scenario has one.
func TestE40GuestPrepare(t *testing.T) {
	k := stepKit(t, func(string) execx.Result { return execx.Result{} })
	r := newRunning(t, k, profileServer)
	if err := r.prepare(context.Background()); err != nil {
		t.Fatal(err)
	}
	if k.count("ssh") != 0 || k.count("scp") != 0 {
		t.Fatal("a scenario with no fixture ran one")
	}
	want := "# harness: no guest-prepare-scen.bash under " + k.home + "; this scenario prepares nothing\n"
	if transcriptOf(t, r) != want {
		t.Fatalf("transcript %q", transcriptOf(t, r))
	}
	k.wantErr(want)

	k, r = prepareFixture(t, "==> prepare: step\nVMTEST-GUEST-PREPARE-DONE\n", 0)
	if err := r.prepare(context.Background()); err != nil {
		t.Fatal(err)
	}
	c := k.mustCall("guest-prepare-scen.bash fedora@")
	if !strings.Contains(c.Line(), k.path("guest-prepare-scen.bash")+" fedora@127.0.0.1:/tmp/guest-prepare.bash") {
		t.Fatalf("got %s", c.Line())
	}
	if !strings.Contains(k.mustCall("env TERM=dumb bash /tmp/guest-prepare.bash").Line(), "-o ServerAliveInterval=30") {
		t.Fatal("the fixture ran without keepalives")
	}
	if !strings.Contains(transcriptOf(t, r), "# harness: running the scen fixture in the guest, before the reboot\n==> prepare: step\nVMTEST-GUEST-PREPARE-DONE\n") {
		t.Fatalf("transcript %q", transcriptOf(t, r))
	}
}

// prepareFixture is a server run whose scenario ships a fixture that prints out and exits code.
func prepareFixture(t *testing.T, out string, code int) (*kit, *running) {
	t.Helper()
	k := stepKit(t, func(remote string) execx.Result {
		if remote == "env TERM=dumb bash /tmp/guest-prepare.bash" {
			return execx.Result{Stdout: []byte(out), ExitCode: code}
		}
		return execx.Result{}
	})
	k.write("guest-prepare-scen.bash", "#!/usr/bin/bash\n")
	return k, newRunning(t, k, profileServer)
}

// E40: a fixture that fails, or does not reach its last step, fails the run and names why.
func TestE40GuestPrepareFailures(t *testing.T) {
	_, r := prepareFixture(t, "ERROR: prepare: first\nnoise\nERROR: prepare: the last one\n", 7)
	err := r.prepare(context.Background())
	if err == nil || err.Error() != "the scen fixture exited 7 (stage: prepare): ERROR: prepare: the last one; nothing after it would be judging the scenario it names" {
		t.Fatalf("got %v", err)
	}

	_, r = prepareFixture(t, "guest said ERROR: prepare: not at line start\n", 1)
	err = r.prepare(context.Background())
	if err == nil || !strings.Contains(err.Error(), "(stage: prepare): the fixture printed no ERROR: prepare: line;") {
		t.Fatalf("got %v", err)
	}

	_, r = prepareFixture(t, "==> prepare: returned early\n", 0)
	if _, werr := r.transcript.WriteString("VMTEST-GUEST-PREPARE-DONE\n"); werr != nil {
		t.Fatal(werr)
	}
	err = r.prepare(context.Background())
	if err == nil || err.Error() != "the scen fixture exited 0 without printing VMTEST-GUEST-PREPARE-DONE (stage: prepare); it did not reach its last step" {
		t.Fatalf("a marker earlier in the transcript satisfied the fixture's check (B8): %v", err)
	}
}

// E37: a desktop run hands run.bash to the session runner and waits for its exit.
func TestE37SessionProvision(t *testing.T) {
	polls := 0
	k := stepKit(t, func(remote string) execx.Result {
		switch remote {
		case "test -r /var/lib/vmtest/run.exit":
			polls++
			if polls < 3 {
				return execx.Result{ExitCode: 1}
			}
		case "cat /var/lib/vmtest/run.log":
			return execx.Result{Stdout: []byte("PLAY RECAP\n")}
		case "cat /var/lib/vmtest/run.exit":
			return execx.Result{Stdout: []byte("3\n")}
		}
		return execx.Result{}
	})
	r := newRunning(t, k, profileDesktop)
	r.s.RunEnv = []envPair{{"RUN_BASH_PLAY", "x.yml"}}
	rc, err := r.provision(context.Background())
	if err != nil || rc != 3 {
		t.Fatalf("got %d %v", rc, err)
	}
	env := stdinText(t, k.mustCall("cat >/var/lib/vmtest/run.env.tmp"))
	want := "RUN_BASH_HEADLESS=1\nRUN_BASH_USER_EMAIL=vmtest@example.com\nRUN_BASH_GITHUB_ACCOUNTS=none\n" +
		"RUN_BASH_VAULT_PASSWORD_FILE=/home/" + cloudUser + "/.vmtest/throwaway-pw\nRUN_BASH_GIT_REF=" + testCommit + "\n" +
		"RUN_BASH_PROVISIONING_PROFILE=desktop\nTERM=dumb\nRUN_BASH_PLAY=x.yml\n"
	if env != want {
		t.Fatalf("run.env:\n%s", env)
	}
	if !strings.Contains(k.mustCall("run.env.tmp").Line(), "umask 077 && cat >/var/lib/vmtest/run.env.tmp && mv -f /var/lib/vmtest/run.env.tmp /var/lib/vmtest/run.env") {
		t.Fatal("run.env not written through a temporary and a rename")
	}
	if transcriptOf(t, r) != "PLAY RECAP\n" || polls != 3 {
		t.Fatalf("transcript %q polls %d", transcriptOf(t, r), polls)
	}
}

// B16, E36, E37: a desktop host-only run hands the session runner the throwaway account and
// the guest paths of its delivered secrets, as a server run passes them to run.bash.
func TestB16DesktopHostOnlySessionCarriesTheIdentity(t *testing.T) {
	k := stepKit(t, func(remote string) execx.Result {
		if remote == "cat /var/lib/vmtest/run.exit" {
			return execx.Result{Stdout: []byte("0\n")}
		}
		return execx.Result{}
	})
	r := newRunning(t, k, profileDesktop)
	r.s.HostOnly = true
	r.cr = credentials{account: "throwaway-lab-user", tokenFile: "/host/token", ppFile: "/host/pp"}
	if rc, err := r.provision(context.Background()); err != nil || rc != 0 {
		t.Fatalf("got %d %v", rc, err)
	}
	env := stdinText(t, k.mustCall("cat >/var/lib/vmtest/run.env.tmp"))
	for _, want := range []string{
		"\nRUN_BASH_GITHUB_ACCOUNTS=throwaway-lab-user\n",
		"\nRUN_BASH_GITHUB_TOKEN_FILE=/dev/shm/vmtest-secrets/gh-token\n",
		"\nRUN_BASH_GITHUB_SSH_PASSPHRASE_FILE=/dev/shm/vmtest-secrets/ssh-passphrase\n",
	} {
		if !strings.Contains(env, want) {
			t.Fatalf("run.env lacks %q:\n%s", want, env)
		}
	}
	if strings.Contains(env, "=none\n") || strings.Contains(env, "/host/") {
		t.Fatalf("run.env names none or a host path:\n%s", env)
	}
}

// E37: a malformed run.exit fails the run; a session that never finishes is a timeout with
// the run log so far in the transcript.
func TestE37SessionProvisionFailures(t *testing.T) {
	k := stepKit(t, func(remote string) execx.Result {
		if remote == "cat /var/lib/vmtest/run.exit" {
			return execx.Result{Stdout: []byte("done\n")}
		}
		return execx.Result{}
	})
	r := newRunning(t, k, profileDesktop)
	rc, err := r.provision(context.Background())
	if err == nil || err.Error() != "the session runner wrote a malformed run.exit: done" {
		t.Fatalf("got %d %v", rc, err)
	}

	k = stepKit(t, func(remote string) execx.Result {
		switch remote {
		case "test -r /var/lib/vmtest/run.exit":
			return execx.Result{ExitCode: 1}
		case "cat /var/lib/vmtest/run.log":
			return execx.Result{Stdout: []byte("half a run\n")}
		}
		return execx.Result{}
	})
	r = newRunning(t, k, profileDesktop)
	rc, err = r.provision(context.Background())
	if err != nil || rc != timedOut {
		t.Fatalf("got %d %v", rc, err)
	}
	if transcriptOf(t, r) != "half a run\nERROR: the session runner did not finish run.bash within 5400s\n" {
		t.Fatalf("transcript %q", transcriptOf(t, r))
	}
	k.wantErr("==> still provisioning in the session after 300s")
}

// E43: a desktop's evidence.
func TestE43DesktopEvidence(t *testing.T) {
	k := stepKit(t, func(remote string) execx.Result {
		switch remote {
		case "cat /var/lib/vmtest/session.env":
			return execx.Result{Stdout: []byte("HOME=/home/" + cloudUser + "\nWAYLAND_DISPLAY=wayland-0\nXDG_SESSION_TYPE=wayland\nDISPLAY=:0\n")}
		case "systemd-run --user --pipe --quiet --wait env | sort":
			return execx.Result{Stdout: []byte("HOME=/home/" + cloudUser + "\nDISPLAY=:0\n")}
		}
		return execx.Result{}
	})
	r := newRunning(t, k, profileDesktop)
	r.evidence(context.Background())
	want := "VMTEST-EVIDENCE session_only_vars=WAYLAND_DISPLAY,XDG_SESSION_TYPE,\n" +
		"VMTEST-EVIDENCE screenshot=runs/r1/screenshot.png (evidence only: proves the compositor produced a frame, not that the frame is correct)\n"
	if transcriptOf(t, r) != want {
		t.Fatalf("transcript %q", transcriptOf(t, r))
	}
	if k.count("screenshot vmtest-run-r1 "+filepath.Join(r.dir, "screenshot.png")) != 1 {
		t.Fatal("no screenshot")
	}

	k = stepKit(t, func(string) execx.Result { return execx.Result{ExitCode: 255} })
	k.fake.On([]string{"virsh", "-c", libvirtURI, "screenshot"}, execx.Result{ExitCode: 1})
	r = newRunning(t, k, profileDesktop)
	r.evidence(context.Background())
	if transcriptOf(t, r) != "" {
		t.Fatalf("transcript %q", transcriptOf(t, r))
	}
	k.wantErr("==> no session.env in the guest")
	k.wantErr("==> screenshot failed (recorded as absent)")
}
