package sshkey

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

func TestE10TheToolsOptionsComeFirst(t *testing.T) {
	cases := []struct {
		tool Tool
		args []string
		want func(key string) []string
	}{
		{SSH, []string{"-p", "2222", "admin@host.example.com", "uptime"}, func(k string) []string {
			return []string{
				"ssh", "-i", k, "-o", "IdentitiesOnly=yes", "-o", "PreferredAuthentications=publickey",
				"-o", "IdentityAgent=none", "-p", "2222", "admin@host.example.com", "uptime",
			}
		}},
		{SCP, []string{"f", "host.example.com:/tmp/"}, func(k string) []string {
			return []string{
				"scp", "-i", k, "-o", "IdentitiesOnly=yes", "-o", "PreferredAuthentications=publickey",
				"-o", "IdentityAgent=none", "f", "host.example.com:/tmp/",
			}
		}},
		{CopyID, []string{"-f", "-i", "new.pub", "host.example.com"}, func(k string) []string {
			return []string{
				"ssh-copy-id", "-o", "IdentityFile=" + k, "-o", "IdentitiesOnly=yes",
				"-o", "IdentityAgent=none", "-o", "PreferredAuthentications=publickey", "-f", "-i", "new.pub", "host.example.com",
			}
		}},
	}
	for _, c := range cases {
		w := newWorld(t)
		key := w.key("id_a")
		w.probeSays()
		wantCode(t, w.run(c.tool, append([]string{c.tool.KeyFlag, key}, c.args...)...), term.ExitOK)
		got := w.execd()
		if want := c.want(key); !slices.Equal(append([]string{got.Name}, got.Args...), want) {
			t.Errorf("%s: exec\n %q\nwant\n %q", c.tool.Name, got.Line(), strings.Join(want, " "))
		}
	}
}

func TestE11TheProbeAsksSSHWithTheToolsOptions(t *testing.T) {
	w := newWorld(t)
	key := w.key("id_a")
	w.probeSays()
	wantCode(t, w.run(SSH, "-i", key, "-p", "2222", "host.example.com"), term.ExitOK)
	calls := w.fake.Calls()
	if len(calls) != 1 {
		t.Fatalf("calls %v", w.ran())
	}
	want := []string{
		"-G", "-T", "-i", key, "-o", "IdentitiesOnly=yes", "-o", "PreferredAuthentications=publickey",
		"-o", "IdentityAgent=none", "-p", "2222", "host.example.com",
	}
	if !slices.Equal(calls[0].Args, want) {
		t.Errorf("probe %q", calls[0].Line())
	}
}

func TestE11AForwardedRunsProbeLeavesTheAgentToTheConfiguration(t *testing.T) {
	w := newWorld(t)
	key := w.key("id_a")
	w.probeSays()
	w.tty = "1\n"
	wantCode(t, w.run(SSH, "-i", key, "-A", "host.example.com"), term.ExitOK)
	if probe := w.fake.Calls()[0]; slices.Contains(probe.Args, "IdentityAgent=none") {
		t.Errorf("the forwarded run's probe hides the configured agent: %s", probe.Line())
	}
}

func TestE11AConfiguredAgentStopsAForwardedRun(t *testing.T) {
	for _, line := range []string{"identityagent /run/user.sock", "forwardagent /run/user.sock", "identityagent none"} {
		w := newWorld(t)
		key := w.key("id_a")
		w.probeSays()
		w.probeAlsoSays(line)
		wantCode(t, w.run(SSH, "-i", key, "-A", "host.example.com"), term.ExitFailure)
		w.stderrHas("ssh-with-key: ssh's configuration gives host.example.com " + line + ", so -A would forward that agent, " +
			"not the one this run starts. Remove that line for this host, or pass -F with a configuration that has none. Nothing was run.")
		w.noExec()
	}
	for _, line := range []string{"identityagent SSH_AUTH_SOCK", "forwardagent yes"} {
		w := newWorld(t)
		key := w.key("id_a")
		w.probeSays()
		w.probeAlsoSays(line)
		wantCode(t, w.run(SSH, "-i", key, "-A", "host.example.com"), term.ExitOK)
		w.execd()
	}
	// Not forwarding, the tool's IdentityAgent=none wins, so a configured agent is not asked about.
	w := newWorld(t)
	key := w.key("id_a")
	w.probeSays()
	w.probeAlsoSays("identityagent /run/user.sock")
	wantCode(t, w.run(SSH, "-i", key, "host.example.com"), term.ExitOK)
}

func TestE11AnotherKeyInTheConfigurationStopsTheRun(t *testing.T) {
	w := newWorld(t)
	key := w.key("id_a")
	w.key("id_work")
	w.probeSays("~/.ssh/id_work", "~/.ssh/id_gone", "%d/.ssh/id_a", "/etc/ssh/%C_key", "none")
	wantCode(t, w.run(SSH, "-i", key, "host.example.com"), term.ExitFailure)
	w.stderrHas("ssh-with-key: ssh's configuration also gives host.example.com the key(s) ~/.ssh/id_work, /etc/ssh/%C_key, " +
		"which ssh would offer too; this tool offers only ~/.ssh/id_a.")
	w.stderrHas("Nothing was run.")
	w.noExec()
	if strings.Contains(w.stderr.String(), "id_gone") {
		t.Error("a configured key that does not exist is not offered")
	}
}

func TestE11TheConnectionsTokensAreExpandedAsSSHDoes(t *testing.T) {
	w := newWorld(t)
	key := w.key("id_a")
	w.key("host.example.com_someone_22_host.example.com")
	w.probeSays("~/.ssh/%h_%r_%p_%n", "~/.ssh/%h_gone", "~/.ssh/%%h", "~/.ssh/%u_key")
	w.probeAlsoSays("port 22")
	wantCode(t, w.run(SSH, "-i", key, "host.example.com"), term.ExitFailure)
	w.stderrHas("the key(s) ~/.ssh/host.example.com_someone_22_host.example.com, ~/.ssh/%u_key, which")
	if strings.Contains(w.stderr.String(), "gone") || strings.Contains(w.stderr.String(), "%%h") {
		t.Errorf("a token path that names no file is offered:\n%s", w.stderr.String())
	}
}

func TestE11AConfiguredPublicKeyAloneCounts(t *testing.T) {
	w := newWorld(t)
	key := w.key("id_a")
	w.file("id_pubonly.pub", "ssh-ed25519 AAAA x\n")
	w.probeSays(filepath.Join(w.sshDir(), "id_pubonly"))
	wantCode(t, w.run(SSH, "-i", key, "host.example.com"), term.ExitFailure)
	w.stderrHas("~/.ssh/id_pubonly")
}

func TestE11TheSameKeyInTheConfigurationIsFine(t *testing.T) {
	w := newWorld(t)
	key := w.key("id_a")
	w.probeSays("~/.ssh/id_a", "%d/.ssh/id_a")
	wantCode(t, w.run(SSH, "-i", key, "host.example.com"), term.ExitOK)
	w.execd()
}

func TestE11AFailedProbeStops(t *testing.T) {
	w := newWorld(t)
	key := w.key("id_a")
	w.fake.On([]string{"ssh", "-G"}, execx.Result{Stderr: []byte("command-line line 0: Bad key types 'x'.\n"), ExitCode: 255})
	wantCode(t, w.run(SSH, "-i", key, "-o", "Bogus=x", "host.example.com"), term.ExitFailure)
	w.stderrHas("Bad key types")
	w.noExec()
}

func TestE11EachSCPHostIsProbedAndALocalCopyIsNot(t *testing.T) {
	w := newWorld(t)
	key := w.key("id_a")
	w.probeSays()
	wantCode(t, w.run(SCP, "-i", key, "a.example.com:x", "b.example.com:y"), term.ExitOK)
	if n := len(w.fake.Calls()); n != 2 {
		t.Errorf("probes %v", w.ran())
	}
	w2 := newWorld(t)
	key2 := w2.key("id_a")
	wantCode(t, w2.run(SCP, "-i", key2, "a", "b"), term.ExitOK)
	if n := len(w2.fake.Calls()); n != 0 {
		t.Errorf("probes %v", w2.ran())
	}
	w2.execd()
}

func TestE12AMissingProgram(t *testing.T) {
	w := newWorld(t)
	key := w.key("id_a")
	w.probeSays()
	w.fake.ExecError(&execx.NotInstalledError{Name: "ssh-copy-id"})
	wantCode(t, w.run(CopyID, "--auth-key", key, "-f", "-i", "new.pub", "host.example.com"), term.ExitFailure)
	w.stderrHas("ssh-copy-id-with-key: ssh-copy-id is not installed.")
}

func TestE14AForwardedRunReExecsUnderItsOwnAgent(t *testing.T) {
	w := newWorld(t)
	w.key("id_a")
	b := w.key("id_b")
	w.probeSays()
	w.tty = "2\n"
	wantCode(t, w.run(SSH, "-A", "host.example.com"), term.ExitOK)
	got := w.execd()
	want := []string{"ssh-agent", "--", testSelf, "-i", b, "-A", "host.example.com"}
	if !slices.Equal(append([]string{got.Name}, got.Args...), want) {
		t.Errorf("exec %q", got.Line())
	}
	if !slices.Equal(got.Env, []string{markerVar + "=4242"}) {
		t.Errorf("env %q", got.Env)
	}
	if len(w.fake.Calls()) != 1 {
		t.Errorf("want the probe before the agent: %v", w.ran())
	}
}

func TestE14AStaleMarkerIsNamed(t *testing.T) {
	w := newWorld(t)
	key := w.key("id_a")
	w.insideAgent()
	w.env[markerVar] = "1"
	wantCode(t, w.run(SSH, "-i", key, "-A", "host.example.com"), term.ExitFailure)
	w.stderrHas("ssh-with-key: SSH_WITH_KEY_AGENT is set (to 1) in the environment, but not by this run; " +
		"unset it and run again. Nothing was run.")
	if len(w.ran()) != 0 {
		t.Errorf("ran %v", w.ran())
	}
}

func TestE14AnAgentThatIsNotTheRunsOwnIsRefused(t *testing.T) {
	breaks := map[string]func(w *world){
		"no socket":         func(w *world) { delete(w.env, "SSH_AUTH_SOCK") },
		"no agent pid":      func(w *world) { delete(w.env, "SSH_AGENT_PID") },
		"another's agent":   func(w *world) { w.parents[testAgent] = 1 },
		"an agent gone":     func(w *world) { delete(w.parents, testAgent) },
		"a pid not numeric": func(w *world) { w.env["SSH_AGENT_PID"] = "x" },
	}
	for name, breakIt := range breaks {
		w := newWorld(t)
		key := w.key("id_a")
		w.insideAgent()
		breakIt(w)
		wantCode(t, w.run(SSH, "-i", key, "-A", "host.example.com"), term.ExitFailure)
		if !strings.Contains(w.stderr.String(), "the agent in SSH_AUTH_SOCK is not one this run started; nothing was run.") {
			t.Errorf("%s: stderr\n%s", name, w.stderr.String())
		}
		if len(w.fake.Foregrounds()) != 0 || len(w.fake.Execs()) != 0 {
			t.Errorf("%s: ran %v", name, w.ran())
		}
	}
}

func TestE15KeysAreAddedOneMenuAtATimeThenTheLoginRuns(t *testing.T) {
	w := newWorld(t)
	a := w.key("id_a")
	b := w.key("id_b")
	w.key("id_c")
	w.insideAgent()
	w.tty = "2\n1\nd\n"
	wantCode(t, w.run(SSH, "-i", a, "-A", "host.example.com"), term.ExitOK)
	want := []string{
		"fg ssh-add " + b,
		"fg ssh-add " + a,
		"fg ssh -t -i " + a + " -o IdentitiesOnly=yes -o PreferredAuthentications=publickey -A host.example.com " + remoteInit,
		"ssh-agent -k",
	}
	got := w.agentRun()
	if !slices.Equal(got, want) {
		t.Errorf("ran\n%s", strings.Join(got, "\n"))
	}
	for _, c := range w.fake.Foregrounds() {
		if !slices.Contains(c.Unset, markerVar) {
			t.Errorf("%s keeps %s", c.Line(), markerVar)
		}
	}
	if len(w.fake.Calls()) != 1 || w.fake.Calls()[0].Line() != "ssh-agent -k" {
		t.Errorf("the run inside the agent probes again: %v", w.ran())
	}
	w.stderrHas("ssh-with-key › Keys to forward")
	w.stderrHas("added: id_b")
	w.stderrHas("d) Done: forward the keys added")
	if strings.Count(w.stderr.String(), "  a) All the keys not yet added") != 2 {
		t.Errorf("all offered while more than one key is left:\n%s", w.stderr.String())
	}
}

func TestE15AllAddsEveryKeyLeft(t *testing.T) {
	w := newWorld(t)
	a := w.key("id_a")
	b := w.key("id_b")
	w.insideAgent()
	w.tty = "a\n"
	wantCode(t, w.run(SCP, "-i", a, "-A", "f", "host.example.com:/tmp/"), term.ExitOK)
	want := []string{
		"fg ssh-add " + a,
		"fg ssh-add " + b,
		"fg scp -i " + a + " -o IdentitiesOnly=yes -o PreferredAuthentications=publickey -A f host.example.com:/tmp/",
		"ssh-agent -k",
	}
	if got := w.fake.Calls(); len(got) != 1 {
		t.Errorf("calls %v", w.ran())
	}
	got := w.agentRun()
	if !slices.Equal(got, want) {
		t.Errorf("ran\n%s", strings.Join(got, "\n"))
	}
}

func TestE16AFailedAddIsSaidAndOfferedAgain(t *testing.T) {
	w := newWorld(t)
	a := w.key("id_a")
	w.insideAgent()
	w.fake.On([]string{"ssh-add"}, execx.Result{ExitCode: 1})
	w.tty = "1\n1\n1\n"
	wantCode(t, w.run(SSH, "-i", a, "-A", "host.example.com", "true"), term.ExitFailure)
	w.stderrHas("ssh-add could not add id_a (exit 1).")
	w.stderrHas("ssh-with-key: three keys could not be added; nothing was run.")
	if n := len(w.fake.Foregrounds()); n != 3 {
		t.Errorf("foregrounds %v", w.ran())
	}
	if !w.fake.Called("ssh-agent -k") {
		t.Error("the agent is left running")
	}
}

func TestE16CancelEndsTheAgent(t *testing.T) {
	cases := map[string]func(w *world){
		"q":      func(w *world) { w.tty = "q\n" },
		"ctrl-d": func(w *world) { w.tty = "" },
		"ctrl-c in ssh-add": func(w *world) {
			w.tty = "1\n"
			w.fore.signals["ssh-add"] = 2
		},
	}
	for name, set := range cases {
		w := newWorld(t)
		a := w.key("id_a")
		w.insideAgent()
		set(w)
		wantCode(t, w.run(SSH, "-i", a, "-A", "host.example.com"), term.ExitCancelled)
		w.stderrHas("Cancelled; nothing was run.")
		if !w.fake.Called("ssh-agent -k") {
			t.Errorf("%s: the agent is left running", name)
		}
		for _, c := range w.fake.Foregrounds() {
			if c.Name == "ssh" {
				t.Errorf("%s: ssh ran", name)
			}
		}
	}
}

func TestE17NothingToForward(t *testing.T) {
	w := newWorld(t)
	a := w.file("elsewhere", privateKeyText("OPENSSH "))
	if err := os.Rename(a, filepath.Join(w.home, "login_key")); err != nil {
		t.Fatal(err)
	}
	w.insideAgent()
	wantCode(t, w.run(SSH, "-i", filepath.Join(w.home, "login_key"), "-A", "host.example.com"), term.ExitFailure)
	w.stderrHas("ssh-with-key: agent forwarding (-A) was asked, but ~/.ssh has no private key to forward; nothing was run.")
	if !w.fake.Called("ssh-agent -k") {
		t.Error("the agent is left running")
	}
}

func TestE18ARemoteCommandOrAModeWithoutOneGetsNoInit(t *testing.T) {
	for _, args := range [][]string{{"host.example.com", "uptime"}, {"-N", "host.example.com"}} {
		w := newWorld(t)
		a := w.key("id_a")
		w.insideAgent()
		w.tty = "1\n"
		wantCode(t, w.run(SSH, append([]string{"-i", a, "-A"}, args...)...), term.ExitOK)
		fg := w.fake.Foregrounds()
		last := fg[len(fg)-1]
		if last.Name != "ssh" || slices.Contains(last.Args, "-t") || slices.Contains(last.Args, remoteInit) {
			t.Errorf("%v: %s", args, last.Line())
		}
	}
}

func TestE18TheProgramsStatusIsTheRuns(t *testing.T) {
	w := newWorld(t)
	a := w.key("id_a")
	w.insideAgent()
	w.fake.On([]string{"ssh", "-i"}, execx.Result{ExitCode: 7})
	w.tty = "1\n"
	wantCode(t, w.run(SSH, "-i", a, "-A", "host.example.com", "exit 7"), 7)
	if !w.fake.Called("ssh-agent -k") {
		t.Error("the agent is left running")
	}
	w2 := newWorld(t)
	a2 := w2.key("id_a")
	w2.insideAgent()
	w2.tty = "1\n"
	w2.fore.signals["ssh"] = 15
	wantCode(t, w2.run(SSH, "-i", a2, "-A", "host.example.com", "sleep 9"), 143)
	if !w2.fake.Called("ssh-agent -k") {
		t.Error("the agent is left running")
	}
}

func TestE18AProgramThatCannotStart(t *testing.T) {
	w := newWorld(t)
	a := w.key("id_a")
	w.insideAgent()
	w.fake.OnFunc([]string{"scp"}, func(execx.Cmd) (execx.Result, error) {
		return execx.Result{}, &execx.NotInstalledError{Name: "scp"}
	})
	w.tty = "1\n"
	wantCode(t, w.run(SCP, "-i", a, "-A", "f", "host.example.com:/tmp/"), term.ExitFailure)
	w.stderrHas("scp-with-key: scp is not installed.")
	if !w.fake.Called("ssh-agent -k") {
		t.Error("the agent is left running")
	}
}

func TestE19AFailedAgentEndIsSaid(t *testing.T) {
	w := newWorld(t)
	a := w.key("id_a")
	w.insideAgent()
	w.fake.On([]string{"ssh-agent", "-k"}, execx.Result{Stderr: []byte("kill: No such process\n"), ExitCode: 1})
	w.tty = "1\n"
	wantCode(t, w.run(SSH, "-i", a, "-A", "host.example.com", "true"), term.ExitFailure)
	w.stderrHas("could not end the forwarding agent")
	w.stderrHas("No such process")

	w2 := newWorld(t)
	a2 := w2.key("id_a")
	w2.insideAgent()
	w2.fake.On([]string{"ssh-agent", "-k"}, execx.Result{ExitCode: 1})
	w2.fake.On([]string{"ssh", "-i"}, execx.Result{ExitCode: 5})
	w2.tty = "1\n"
	wantCode(t, w2.run(SSH, "-i", a2, "-A", "host.example.com", "true"), 5)
}

func TestAFailedExecIsSaid(t *testing.T) {
	w := newWorld(t)
	a := w.key("id_a")
	w.probeSays()
	w.fake.ExecError(errors.New("exec ssh: permission denied"))
	wantCode(t, w.run(SSH, "-i", a, "host.example.com"), term.ExitFailure)
	w.stderrHas("ssh-with-key: exec ssh: permission denied")
}

func TestASignalAtTheMenuIsTheSignalsStatus(t *testing.T) {
	w := newWorld(t)
	w.key("id_a")
	w.tty = "1\n"
	ctx, cancel := context.WithCancelCause(context.Background())
	cancel(&execx.SignalError{Signal: syscall.SIGTERM})
	wantCode(t, w.runCtx(ctx, SSH, "host.example.com"), 143)
	w.stderrHas("Cancelled; nothing was run.")
	w.noExec()
}

func TestTheProgramCannotBeFoundToRunInsideTheAgent(t *testing.T) {
	w := newWorld(t)
	a := w.key("id_a")
	w.probeSays()
	env := w.environment()
	env.Self = func() (string, error) { return "", errors.New("readlink /proc/self/exe: gone") }
	wantCode(t, App{Tool: SSH, Env: env}.Run(context.Background(), []string{"-i", a, "-A", "host.example.com"}), term.ExitFailure)
	w.stderrHas("could not find this program to run it inside the agent")
	w.noExec()
}

func TestForwardingNeedsATerminal(t *testing.T) {
	w := newWorld(t)
	a := w.key("id_a")
	w.insideAgent()
	w.noTTY = true
	wantCode(t, w.run(SSH, "-i", a, "-A", "host.example.com"), term.ExitUsage)
	w.stderrHas("agent forwarding (-A) needs a terminal to choose the keys on; nothing was run.")
	if !w.fake.Called("ssh-agent -k") {
		t.Error("the agent is left running")
	}
}

func TestSSHAddMissing(t *testing.T) {
	w := newWorld(t)
	a := w.key("id_a")
	w.insideAgent()
	w.fake.OnFunc([]string{"ssh-add"}, func(execx.Cmd) (execx.Result, error) {
		return execx.Result{}, &execx.NotInstalledError{Name: "ssh-add"}
	})
	w.tty = "1\n"
	wantCode(t, w.run(SSH, "-i", a, "-A", "host.example.com"), term.ExitFailure)
	w.stderrHas("ssh-with-key: ssh-add is not installed.")
}

func TestSSHMissingForTheProbe(t *testing.T) {
	w := newWorld(t)
	a := w.key("id_a")
	w.fake.OnFunc([]string{"ssh", "-G"}, func(execx.Cmd) (execx.Result, error) {
		return execx.Result{}, &execx.NotInstalledError{Name: "ssh"}
	})
	wantCode(t, w.run(SSH, "-i", a, "host.example.com"), term.ExitFailure)
	w.stderrHas("ssh-with-key: ssh is not installed.")
}

func TestAnUnwritableStderrIsAFailure(t *testing.T) {
	w := newWorld(t)
	a := w.key("id_a")
	w.probeSays()
	env := w.environment()
	env.Stdout = failingWriter{}
	wantCode(t, App{Tool: SSH, Env: env}.Run(context.Background(), []string{"--help"}), term.ExitFailure)
	wantCode(t, App{Tool: SSH, Env: env}.Run(context.Background(), []string{"-i", a, "host.example.com"}), term.ExitOK)
}

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) { return 0, errors.New("closed") }
