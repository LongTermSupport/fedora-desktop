package vmtest

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

const runID = "20261015T120000Z-server-fast-provision"

// runKit is a lab ready to run server-fast-provision: the scenario on the allowlist, its base
// built and current, the profile's checker deployed, the branch tip on the remote, and a
// judge that answers verdict.
func runKit(t *testing.T, verdict string) *kit {
	t.Helper()
	k := buildKit(t)
	k.scenario("server-fast-provision", nil)
	k.write(allowlistFile, "server-fast-provision\n")
	k.write("guest-acceptance-server.bash", "checker\n")
	k.builtBase("server-fast-44")
	k.gate(0, "current")
	k.fake.On([]string{"git", "ls-remote", testRepoURL, "refs/heads/" + testBranch}, execx.Result{Stdout: []byte(testCommit + "\trefs/heads/" + testBranch + "\n")})
	k.fake.On(helperArgv("judge_run"), execx.Result{Stdout: []byte("{\n  \"schema\": 1,\n  \"verdict\": \"" + verdict + "\"\n}\n")})
	k.fake.On(append(helperArgv("refresh"), "certify"), execx.Result{Stdout: []byte("VMTEST-BASE-CERTIFIED server-fast-44 revision=1 mirror=m\n")})
	return k
}

// E28-E38, E42, E46, E47: an ordinary server scenario, end to end, passing.
func TestE28ServerRunPasses(t *testing.T) {
	k := runKit(t, "pass")
	k.wantCode(k.run("run", "server-fast-provision"), term.ExitOK)
	dir := k.path("runs/" + runID)
	if k.out.String() != "VMTEST-RUN "+runID+" verdict=pass response="+dir+"/response.json\n" {
		t.Fatalf("stdout %q", k.out.String())
	}
	if k.exists("runs/"+runID+"/overlay.qcow2") || !strings.Contains(k.read("runs/"+runID+"/response.json"), "\"verdict\": \"pass\"") {
		t.Fatal("a passing run kept its overlay, or lost its response")
	}
	transcript := k.read("runs/" + runID + "/transcript.log")
	if !strings.HasPrefix(transcript, "# vmtest run "+runID+"\n# scenario server-fast-provision on base server-fast-44 (fast, server) freshness=current commit="+testCommit+"\n") ||
		!strings.Contains(transcript, "RUN-BASH-EXIT 0\n") {
		t.Fatalf("transcript %q", transcript)
	}
	assertInOrder(t, k, []string{
		"git ls-remote " + testRepoURL + " refs/heads/" + testBranch,
		"helpers.vmtest.probe_upstream",
		"retention floor --path " + k.path("runs") + " --base-size 4 --step run --guest-mib 4096",
		"qemu-img create -q -f qcow2 -F qcow2 -b " + k.path("bases/server-fast-44/base.qcow2") + " " + dir + "/overlay.qcow2",
		"--name vmtest-run-" + runID,
		"fedora@127.0.0.1 curl -fsSL -o ~/run.bash 'https://raw.githubusercontent.com/example/fedora-desktop/" + testCommit + "/run.bash' && chmod 0755 ~/run.bash",
		"umask 077 && mkdir -p ~/.vmtest && head -c 32 /dev/urandom | base64 > ~/.vmtest/throwaway-pw",
		"-o ServerAliveInterval=30 -i " + k.path(sshKeyFile) + " -p 40123 fedora@127.0.0.1 env RUN_BASH_HEADLESS=1 RUN_BASH_USER_EMAIL='vmtest@example.com' RUN_BASH_GITHUB_ACCOUNTS=none RUN_BASH_VAULT_PASSWORD_FILE=$HOME/.vmtest/throwaway-pw RUN_BASH_GIT_REF='" + testCommit + "' RUN_BASH_PROVISIONING_PROFILE='server' TERM=dumb bash ~/run.bash",
		k.path("guest-acceptance-server.bash") + " fedora@127.0.0.1:/tmp/guest-acceptance.bash",
		"fedora@127.0.0.1 env VMTEST_COMMIT='" + testCommit + "' VMTEST_USER_EMAIL='vmtest@example.com' bash /tmp/guest-acceptance.bash",
		"virsh -c qemu:///session destroy vmtest-run-" + runID,
		"virsh -c qemu:///session undefine --nvram vmtest-run-" + runID,
		"helpers.vmtest.judge_run",
		"refresh certify --response " + dir + "/response.json --base-json " + k.path("bases/server-fast-44/base.json"),
	})
	install := k.mustCall("--name vmtest-run-").Line()
	if !strings.Contains(install, "cache=unsafe") || !strings.Contains(install, "--graphics none --serial file,path="+dir+"/console.log") {
		t.Fatalf("got %s", install)
	}
	judge := k.mustCall("judge_run").Line()
	for _, want := range []string{
		"--transcript " + dir + "/transcript.log --transcript-path runs/" + runID + "/transcript.log",
		"--scenario server-fast-provision --planned 5 --max-skipped 0 --run-id " + runID + " --commit " + testCommit,
		"--branch " + testBranch + " --freshness-decision current --freshness-degraded false --freshness-divergences ",
		"--divergences throwaway-vault-password,lab-ssh-key-baked --started-at 1792065600 --finished-at 1792065600",
		"--probe-revision " + testProbeRev,
	} {
		if !strings.Contains(judge, want) {
			t.Fatalf("judge lacks %q:\n%s", want, judge)
		}
	}
	for _, want := range []string{
		"==> freshness: current (degraded=false)", "==> run " + runID + ": booting vmtest-run-" + runID,
		"==> fetching run.bash at 0123456789ab into the guest", "==> provisioning (headless run.bash; this is the long part — up to 5400s)\n",
		"RUN-BASH-EXIT 0\n", "==> VMTEST-BASE-CERTIFIED server-fast-44", "==> verdict: pass; response " + dir + "/response.json; transcript " + dir + "/transcript.log",
	} {
		k.wantErr(want)
	}
	if k.count("guest-prepare") != 0 || k.count("boot_id") != 0 {
		t.Fatal("a scenario without reboot_before_checks prepared or rebooted")
	}
	assertNoProbeFiles(t, k)
}

// E46, E47: a failing verdict exits 1 and keeps the overlay; a broken judge is a failure.
func TestE46VerdictAndJudge(t *testing.T) {
	k := runKit(t, "fail")
	k.wantCode(k.run("run", "server-fast-provision"), term.ExitFailure)
	if !strings.HasSuffix(k.out.String(), "verdict=fail response="+k.path("runs/"+runID)+"/response.json\n") || !k.exists("runs/"+runID+"/overlay.qcow2") {
		t.Fatalf("stdout %q", k.out.String())
	}
	k.wantErr("==> overlay and console kept for diagnosis: " + k.path("runs/"+runID))

	k = runKit(t, "pass")
	k.fake.On(helperArgv("judge_run"), execx.Result{ExitCode: 2})
	k.wantCode(k.run("run", "server-fast-provision"), term.ExitFailure)
	k.wantErr("ERROR: could not judge the run; see " + k.path("runs/"+runID))
	if k.out.Len() != 0 {
		t.Fatal("an unjudged run printed a verdict")
	}

	k = runKit(t, "pass")
	k.fake.On(helperArgv("judge_run"), execx.Result{Stdout: []byte("{\"verdict\": 1}\n"), ExitCode: 1})
	k.wantCode(k.run("run", "server-fast-provision"), term.ExitFailure)
	k.wantErr("ERROR: could not judge the run; see ")

	k = runKit(t, "pass")
	k.fake.On(append(helperArgv("refresh"), "certify"), execx.Result{Stdout: []byte("VMTEST-BASE-NOT-CERTIFIED server-fast-44 reason=refresh outcome is 'stale'\n")})
	k.wantCode(k.run("run", "server-fast-provision"), term.ExitOK)
	k.wantErr("==> base server-fast-44 is stale: refresh it with: vmtest refresh-base server-fast")

	k = runKit(t, "pass")
	k.fake.On(append(helperArgv("refresh"), "certify"), execx.Result{ExitCode: 2})
	k.wantCode(k.run("run", "server-fast-provision"), term.ExitFailure)
	k.wantErr("ERROR: could not apply the run's refresh outcome to " + k.path("bases/server-fast-44/base.json"))
}

// E28-E33: every refusal comes before anything boots.
func TestE28RunRefusals(t *testing.T) {
	cases := []struct {
		name  string
		setup func(k *kit)
		args  []string
		want  string
	}{
		{
			"not runnable", func(k *kit) {
				k.scenario("server-fast-provision", map[string]string{"runnable": "false", "planned": "-"})
			}, nil,
			"server-fast-provision is not runnable: its guest script has not declared a planned check count",
		},
		{
			"github options on an ordinary scenario", func(*kit) {},
			[]string{"--github-account", "someone"},
			"the --github-* options apply only to a host_only scenario, and server-fast-provision is not one",
		},
		{
			"not on the allowlist", func(k *kit) { k.write(allowlistFile, "server-fast-provision-x\n") }, nil,
			"server-fast-provision is not in the deployed allowlist " + "ALLOWLIST" + "; re-run play-vm-test-lab.yml on the host",
		},
		{"no allowlist", func(k *kit) {
			if err := os.Remove(k.path(allowlistFile)); err != nil {
				k.t.Fatal(err)
			}
		}, nil, "server-fast-provision is not in the deployed allowlist ALLOWLIST; re-run play-vm-test-lab.yml on the host"},
		{"base absent", func(k *kit) {
			if err := os.Remove(k.path("bases/server-fast-44/base.json")); err != nil {
				k.t.Fatal(err)
			}
		}, nil, "base server-fast-44 is absent (stage: base); build it with: vmtest build-base server-fast"},
		{"no checker", func(k *kit) {
			if err := os.Remove(k.path("guest-acceptance-server.bash")); err != nil {
				k.t.Fatal(err)
			}
		}, nil, "no checker for server-fast-provision: neither guest-acceptance-server-fast-provision.bash nor guest-acceptance-server.bash is readable under "},
		{"a short commit", func(*kit) {}, []string{"--commit", "0123abc"}, "--commit must be a 40-hex commit, got 0123abc"},
		{
			"no tip on the remote", func(k *kit) { k.fake.On([]string{"git"}, execx.Result{}) }, nil,
			"could not read the tip of " + testBranch + " from " + testRepoURL,
		},
		{
			"a refused base", func(k *kit) { k.gate(1, "reinstall") }, nil,
			"base server-fast-44 may not be used (freshness: reinstall); rebuild it with: vmtest build-base server-fast",
		},
		{
			"no room for the clone", func(k *kit) { k.fake.On(helperArgv("retention"), execx.Result{ExitCode: 1}) }, nil,
			"the host cannot carry a run on server-fast-44 (stage: clone); see the VMTEST-DISK/VMTEST-RAM lines above",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			k := runKit(t, "pass")
			tc.setup(k)
			k.wantCode(k.run(append([]string{"run", "server-fast-provision"}, tc.args...)...), term.ExitFailure)
			k.wantErr("ERROR: " + strings.ReplaceAll(tc.want, "ALLOWLIST", k.path(allowlistFile)))
			if k.count("virt-install") != 0 || k.exists("runs/"+runID) {
				t.Fatal("booted past a refusal")
			}
		})
	}
}

// E31, E32: a scenario's own checker wins over its profile's; --commit pins the commit.
func TestE31CheckerAndCommit(t *testing.T) {
	k := runKit(t, "pass")
	k.write("guest-acceptance-server-fast-provision.bash", "own checker\n")
	other := strings.Repeat("f", 40)
	k.wantCode(k.run("run", "server-fast-provision", "--commit", other), term.ExitOK)
	if k.count(k.path("guest-acceptance-server-fast-provision.bash")+" fedora@127.0.0.1:/tmp/guest-acceptance.bash") != 1 || k.count("ls-remote") != 0 {
		t.Fatalf("calls %v", k.lines("scp"))
	}
	if k.count("RUN_BASH_GIT_REF='"+other+"'") != 1 {
		t.Fatal("the pinned commit was not provisioned")
	}
}

// E11, E36: run_env overrides come last on the provisioning line.
func TestE36RunEnvOverridesComeLast(t *testing.T) {
	k := runKit(t, "pass")
	k.scenario("server-fast-provision", map[string]string{"run_env": "RUN_BASH_PROVISIONING_PROFILE=odd,RUN_BASH_PLAY=x.yml"})
	k.wantCode(k.run("run", "server-fast-provision"), term.ExitOK)
	if k.count("RUN_BASH_PROVISIONING_PROFILE='server' RUN_BASH_PROVISIONING_PROFILE='odd' RUN_BASH_PLAY='x.yml' TERM=dumb bash ~/run.bash") != 1 {
		t.Fatalf("calls %v", k.lines("run.bash"))
	}
	k.wantErr("==> provisioning (headless run.bash; this is the long part — up to 5400s); overrides: RUN_BASH_PROVISIONING_PROFILE='odd' RUN_BASH_PLAY='x.yml'")
}

// E36, E38: a run.bash that fails or hits its bound is recorded, and the verdict decides.
func TestE38RunBashStatusIsRecorded(t *testing.T) {
	k := runKit(t, "fail")
	k.gw.remote = func(remote string) (execx.Result, bool) {
		if strings.HasSuffix(remote, "bash ~/run.bash") {
			return execx.Result{Stdout: []byte("PLAY RECAP failed=1\n"), ExitCode: 2}, true
		}
		return execx.Result{}, false
	}
	k.wantCode(k.run("run", "server-fast-provision"), term.ExitFailure)
	if !strings.Contains(k.read("runs/"+runID+"/transcript.log"), "PLAY RECAP failed=1\nRUN-BASH-EXIT 2\n") || k.count("judge_run") != 1 {
		t.Fatal("a failed run.bash was not recorded and judged")
	}

	k = runKit(t, "fail")
	k.tweak = func(c *cli) { c.bound = expiredBound }
	k.gw.remote = func(remote string) (execx.Result, bool) {
		if strings.HasSuffix(remote, "bash ~/run.bash") {
			return execx.Result{}, true
		}
		return execx.Result{}, false
	}
	k.fake.OnFunc([]string{"ssh"}, func(c execx.Cmd) (execx.Result, error) {
		if strings.HasSuffix(c.Line(), "bash ~/run.bash") {
			return execx.Result{}, context.DeadlineExceeded
		}
		return execx.Result{}, nil
	})
	k.wantCode(k.run("run", "server-fast-provision"), term.ExitFailure)
	if !strings.Contains(k.read("runs/"+runID+"/transcript.log"), "RUN-BASH-EXIT 124\n") {
		t.Fatal("the bound's 124 was not recorded")
	}
}

// E39-E41: reboot_before_checks runs the fixture and the reboot before the checker.
func TestE39RebootBeforeChecks(t *testing.T) {
	k := runKit(t, "pass")
	k.scenario("server-fast-provision", map[string]string{"reboot_before_checks": "true"})
	ids := []string{"one", "two"}
	k.gw.remote = func(remote string) (execx.Result, bool) {
		if remote == bootIDCommand {
			id := ids[0]
			if len(ids) > 1 {
				ids = ids[1:]
			}
			return execx.Result{Stdout: []byte(id + "\n")}, true
		}
		return execx.Result{}, false
	}
	k.wantCode(k.run("run", "server-fast-provision"), term.ExitOK)
	assertInOrder(t, k, []string{"bash ~/run.bash", bootIDCommand, rebootCommand, bootIDCommand, "/tmp/guest-acceptance.bash", "bash /tmp/guest-acceptance.bash"})
	if !strings.Contains(k.read("runs/"+runID+"/transcript.log"), "RUN-BASH-EXIT 0\n# harness: no guest-prepare-server-fast-provision.bash under "+k.home+"; this scenario prepares nothing\n# harness: rebooting the guest;") {
		t.Fatalf("transcript %q", k.read("runs/"+runID+"/transcript.log"))
	}
}

// E30, E36, E42, E44, E45: a host-only run delivers its secrets by path and scrubs after.
func TestE44HostOnlyRun(t *testing.T) {
	k := runKit(t, "pass")
	k.scenario(hostOnlyID, map[string]string{"host_only": "true"})
	k.write(hostOnlyFile, hostOnlyID+"\n")
	k.write(scrubLibrary, "lib\n")
	k.write(scrubFront, "front\n")
	dir := t.TempDir()
	token, pp := filepath.Join(dir, "token"), filepath.Join(dir, "pp")
	k.writeAbs(token, plantedToken+"\n")
	k.writeAbs(pp, passphraseValue+"\n\n")
	k.fake.On([]string{"bash"}, execx.Result{})
	id := "20261015T120000Z-" + hostOnlyID
	k.wantCode(k.run("run", hostOnlyID, "--github-account", "throwaway-lab-user", "--github-token-file", token, "--github-ssh-passphrase-file", pp), term.ExitOK)
	for _, c := range k.fake.Calls() {
		if strings.Contains(c.Line(), plantedToken) || strings.Contains(c.Line(), passphraseValue) {
			t.Fatalf("a secret on a command line: %s", c.Line())
		}
	}
	for stream, text := range map[string]string{"stdout": k.out.String(), "stderr": k.err.String()} {
		if strings.Contains(text, plantedToken) || strings.Contains(text, passphraseValue) {
			t.Fatalf("a secret on %s:\n%s", stream, text)
		}
	}
	runDir := k.path("runs/" + id)
	assertInOrder(t, k, []string{
		"umask 077 && mkdir -p /dev/shm/vmtest-secrets && chmod 0700 /dev/shm/vmtest-secrets",
		token + " fedora@127.0.0.1:/dev/shm/vmtest-secrets/gh-token",
		pp + " fedora@127.0.0.1:/dev/shm/vmtest-secrets/ssh-passphrase",
		"chmod 0600 /dev/shm/vmtest-secrets/gh-token /dev/shm/vmtest-secrets/ssh-passphrase",
		"RUN_BASH_GITHUB_ACCOUNTS='throwaway-lab-user' RUN_BASH_GITHUB_TOKEN_FILE=/dev/shm/vmtest-secrets/gh-token RUN_BASH_GITHUB_SSH_PASSPHRASE_FILE=/dev/shm/vmtest-secrets/ssh-passphrase RUN_BASH_VAULT_PASSWORD_FILE",
		"rm -f /dev/shm/vmtest-secrets/gh-token /dev/shm/vmtest-secrets/ssh-passphrase",
		"umask 077 && cat > /dev/shm/vmtest-secrets/needles",
		"env VMTEST_GITHUB_USER='throwaway-lab-user' VMTEST_NEEDLES_FILE=/dev/shm/vmtest-secrets/needles VMTEST_COMMIT=",
		"bash " + k.path(scrubFront) + " redact " + runDir + "/transcript.log " + token + " github-token",
		"bash " + k.path(scrubFront) + " redact " + runDir + "/transcript.log " + pp + " ssh-passphrase",
		"bash " + k.path(scrubFront) + " verify " + runDir + "/transcript.log " + token + " " + pp,
		"helpers.vmtest.judge_run",
	})
	if got := stdinText(t, k.mustCall("cat > /dev/shm/vmtest-secrets/needles")); got != plantedToken+"\n"+passphraseValue+"\n" {
		t.Fatalf("needles %q", got)
	}
	k.wantErr("==> no " + runDir + "/console.log to scrub")

	k = runKit(t, "pass")
	k.scenario(hostOnlyID, map[string]string{"host_only": "true"})
	k.write(hostOnlyFile, hostOnlyID+"\n")
	k.write(scrubLibrary, "lib\n")
	k.writeAbs(token, plantedToken+"\n")
	k.wantCode(k.run("run", hostOnlyID, "--github-account", "throwaway-lab-user", "--github-token-file", token, "--github-ssh-passphrase-file", pp), term.ExitFailure)
	k.wantErr("ERROR: no " + k.path(scrubFront) + ": this run handled a real credential and its artefacts cannot be checked for residue; re-run play-vm-test-lab.yml on the host")
	if k.count("judge_run") != 0 {
		t.Fatal("an unscrubbed run was judged")
	}

	k = runKit(t, "pass")
	k.scenario(hostOnlyID, map[string]string{"host_only": "true"})
	k.write(hostOnlyFile, hostOnlyID+"\n")
	k.write(scrubLibrary, "lib\n")
	k.write(scrubFront, "front\n")
	k.fake.OnFunc([]string{"bash"}, func(c execx.Cmd) (execx.Result, error) {
		if c.Args[1] == "verify" {
			return execx.Result{ExitCode: 1}, nil
		}
		return execx.Result{}, nil
	})
	k.wantCode(k.run("run", hostOnlyID, "--github-account", "throwaway-lab-user", "--github-token-file", token, "--github-ssh-passphrase-file", pp), term.ExitFailure)
	k.wantErr("ERROR: a supplied secret survived redaction in " + k.path("runs/"+id) + "/transcript.log; the run's artefacts are NOT safe to share (they remain under " + k.path("runs/"+id) + ")")
}

// E45: the redaction replaces the transcript's inode; the run's transcript is reopened on the
// new file, so what is written after the scrub is in the file that is judged and archived.
func TestE45ScrubReopensTheTranscript(t *testing.T) {
	k := newKit(t)
	k.write(scrubLibrary, "lib\n")
	k.write(scrubFront, "front\n")
	r := newRunning(t, k, profileServer)
	r.s.HostOnly = true
	r.cr = credentials{account: "a", tokenFile: "/host/token", ppFile: "/host/pp"}
	r.note("before\n")
	k.fake.OnFunc([]string{"bash"}, func(c execx.Cmd) (execx.Result, error) {
		if c.Args[1] != "redact" || c.Args[2] != r.dir+"/transcript.log" {
			return execx.Result{}, nil
		}
		next := c.Args[2] + ".next"
		if err := os.WriteFile(next, []byte("redacted\n"), 0o600); err != nil {
			return execx.Result{}, err
		}
		return execx.Result{}, os.Rename(next, c.Args[2])
	})
	if err := r.scrubArtefacts(context.Background()); err != nil {
		t.Fatal(err)
	}
	r.note("after\n")
	if got := transcriptOf(t, r); got != "redacted\nafter\n" {
		t.Fatalf("transcript %q", got)
	}
}

// E34, E37, E42, E43: a desktop run boots with a display and a socket, unlocks, and provisions
// in the session.
func TestE34DesktopRun(t *testing.T) {
	k := runKit(t, "pass")
	k.scenario("desktop-provision", map[string]string{"base": "desktop", "base_name": "desktop-44", "profile": "desktop"})
	k.write(allowlistFile, "desktop-provision\n")
	k.write("guest-acceptance-desktop.bash", "checker\n")
	k.builtBase("desktop-44")
	k.write("bases/desktop-44/luks-passphrase", "pp\n")
	k.gw.remote = func(remote string) (execx.Result, bool) {
		if remote == "cat /var/lib/vmtest/run.exit" {
			return execx.Result{Stdout: []byte("0\n")}, true
		}
		return execx.Result{}, false
	}
	id := "20261015T120000Z-desktop-provision"
	k.wantCode(k.run("run", "desktop-provision"), term.ExitOK)
	install := k.mustCall("--name vmtest-run-").Line()
	if !strings.Contains(install, "--graphics vnc,listen=127.0.0.1 --video virtio --serial unix,path="+k.path("runs/"+id)+"/serial.sock,mode=bind") {
		t.Fatalf("got %s", install)
	}
	if k.count("--unlock-with "+k.path("bases/desktop-44/luks-passphrase")) != 1 || k.count("cat >/var/lib/vmtest/run.env.tmp") != 1 {
		t.Fatal("no unlock or no session provisioning")
	}
	if k.count("env XDG_RUNTIME_DIR=/run/user/$(id -u) DBUS_SESSION_BUS_ADDRESS=unix:path=/run/user/$(id -u)/bus VMTEST_COMMIT=") != 1 {
		t.Fatalf("the checker did not get the session bus: %v", k.lines("guest-acceptance"))
	}
	if !strings.Contains(k.mustCall("judge_run").Line(), "--divergences throwaway-vault-password,lab-ssh-key-baked,luks-unlock-automated,plymouth-disabled,gdm-autologin-enabled-by-harness,session-runner-autostart") {
		t.Fatal("the desktop divergences")
	}
	if k.count("screenshot") != 1 {
		t.Fatal("no evidence collected")
	}

	k = runKit(t, "pass")
	k.scenario("desktop-provision", map[string]string{"base": "desktop", "base_name": "desktop-44", "profile": "desktop"})
	k.write(allowlistFile, "desktop-provision\n")
	k.write("guest-acceptance-desktop.bash", "checker\n")
	k.builtBase("desktop-44")
	k.wantCode(k.run("run", "desktop-provision"), term.ExitFailure)
	k.wantErr("ERROR: no luks-passphrase beside desktop-44; rebuild it (stage: boot)")
	if k.count("destroy vmtest-run-") != 1 {
		t.Fatal("the booted guest was not torn down")
	}
}

// E4, E47: a signal mid-run ends it 130 with the guest torn down and the overlay kept.
func TestE47RunSignal(t *testing.T) {
	k := runKit(t, "pass")
	ctx, cancel := context.WithCancelCause(context.Background())
	k.gw.remote = func(remote string) (execx.Result, bool) {
		if strings.HasSuffix(remote, "bash ~/run.bash") {
			cancel(InterruptError{Signal: syscall.SIGINT})
			return execx.Result{ExitCode: 255}, true
		}
		return execx.Result{}, false
	}
	k.wantCode(k.runCtx(ctx, "run", "server-fast-provision"), 130)
	if k.count("destroy vmtest-run-"+runID) != 1 || !k.exists("runs/"+runID+"/overlay.qcow2") || k.count("judge_run") != 0 {
		t.Fatalf("calls %v", k.lines("virsh"))
	}
	k.wantErr("ERROR: interrupted by interrupt")
	assertNoProbeFiles(t, k)
}
