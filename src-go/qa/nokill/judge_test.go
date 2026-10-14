package nokill

import "testing"

// ruleCase is one line of source and the rule that must flag it ("" means it must pass).
type ruleCase struct {
	name string
	text string
	rule string
}

func checkCases(t *testing.T, exception bool, cases []ruleCase) {
	t.Helper()
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := Judge("f.py", c.text, exception)
			if c.rule == "" {
				if len(got) != 0 {
					t.Fatalf("%q was flagged: %+v", c.text, got)
				}
				return
			}
			if len(got) != 1 || got[0].Rule != c.rule {
				t.Fatalf("%q: want one finding of rule %s, got %+v", c.text, c.rule, got)
			}
		})
	}
}

// E9: the signal rules.
func TestE9SignalRules(t *testing.T) {
	checkCases(t, false, []ruleCase{
		{"os.kill", "    os.kill(pid, 9)", "os-kill"},
		{"os.kill spaced", "os.kill (pid, 9)", "os-kill"},
		{"os.killpg", "os.killpg(pgid, 15)", "os-kill"},
		{"os.abort", "os.abort()", "os-kill"},
		{"from os import kill", "from os import getpid, kill", "os-import-kill"},
		{"from signal import raise_signal", "from signal import raise_signal", "os-import-kill"},
		{"signal const", "x = signal.SIGTERM", "signal-const"},

		{"raise_signal", "signal.raise_signal(2)", "signal-const"},
		{"pthread_kill", "signal.pthread_kill(t, 2)", "signal-const"},
		{"send_signal", "proc.send_signal(15)", "send-signal"},
		{"pidfd_send_signal", "os.pidfd_send_signal(fd, 9)", "send-signal"},
		{"force_exit", "subproc.force_exit();", "force-exit"},
		{"method kill", "child.kill()", "proc-kill"},
		{"method terminate", "await proc.terminate()", "proc-kill"},
		{"argv kill list", `subprocess.run(["kill", "-9", str(pid)])`, "argv-kill"},
		{"argv pkill tuple single quotes", `Popen(('pkill', '-f', name))`, "argv-kill"},
		{"argv killall", `run(["killall", name])`, "argv-kill"},
		{"argv sudo kill", `run(["sudo", "kill", pid])`, "argv-kill"},
		{"argv kill on its own line", `    "kill",`, "argv-kill"},
		{"shell pkill", "    pkill -f watchdog", "shell-kill"},
		{"shell killall", "killall python3", "shell-kill"},
		{"shell kill -9", "kill -9 $pid", "shell-kill"},
		{"js argv", `new Gio.Subprocess({argv: ['kill', '-9', pid]})`, "argv-kill"},
	})
}

// E9, the other half: what must not be flagged.
func TestE9GuidanceAndLookalikesPass(t *testing.T) {
	checkCases(t, false, []ruleCase{
		{"exec_hint prose", `exec_hint = "podman exec -it box ps -o pid,args  # in-container: kill <pid>"`, ""},
		{"skill", "run skill -h to learn", ""},
		{"dbus --signal", `"--signal", f"{IFACE}.{SIG}",`, ""},
		{"killed word", "the container was killed by oom", ""},
		{"kill identifier", "self.kill_count += 1", ""},
		{"quoted kill in a sentence", `msg = "do not kill the process"`, ""},
		{"termination word", "the terminal terminates here", ""},
		{"os.path", "os.path.join(a, b)", ""},
		{"SIG_IGN is a disposition, not a delivery", "signal.signal(signal.SIG_IGN, h)", ""},
	})
}

// E10: the lifecycle rules.
func TestE10LifecycleRules(t *testing.T) {
	checkCases(t, false, []ruleCase{
		{"podman stop", `subprocess.run(["podman", "stop", cid])`, "engine-literal"},
		{"docker rm", `run(['docker', 'rm', cid])`, "engine-literal"},
		{"space before comma", `run(["podman" , "stop", cid])`, "engine-literal"},
		{"container subcommand", `run(["podman", "container", "stop", cid])`, "engine-literal"},
		{"unpause", `run(["docker", "unpause", cid])`, "engine-literal"},
		{"program path", `run(["/usr/bin/podman", "restart", cid])`, "engine-literal"},
		{"variable bracket", `subprocess.run([engine, "stop", cid], check=True)`, "engine-variable"},
		{"variable paren", `build(engine, "stop")`, "engine-variable"},
		{"variable attribute", `[self.engine, "pause", cid]`, "engine-variable"},
		{"variable suffix", `[container_engine, "rm", cid]`, "engine-variable"},
		{"report advice prose", `return "  Stop it, or fix why it exits:  podman stop <container>"`, ""},
		{"podman ps is fine", `run(["podman", "ps", "--format", "json"])`, ""},
		{"docker inspect", `run(["docker", "inspect", cid])`, ""},
		{"global flag", `subprocess.run(["podman", "--remote", "stop", cid])`, "engine-literal"},
		{"global flag with value", `run(["podman", "--url", "ssh://h", "stop", cid])`, "engine-literal"},
		{"flag=value", `run(["docker", "--context=x", "rm", cid])`, "engine-literal"},
		{"flags then container", `run(["podman", "--remote", "container", "rm", cid])`, "engine-literal"},
		{"variable with flag", `run([engine, "--remote", "stop", cid])`, "engine-variable"},
		{"upper-case constant", `run([ENGINE, "stop", cid])`, "engine-variable"},
		{"mixed-case suffix", `run([self.containerEngine, "rm", cid])`, "engine-variable"},
		{"flag only, no verb", `run(["podman", "--remote", "ps"])`, ""},
		{"engines is another name", `x = [engines, "stopped"]`, ""},
	})
}

// E9 and E10: a call written across lines is seen, and the finding is the line it starts on.
func TestE9E10MultiLineCallsAreSeen(t *testing.T) {
	text := "import subprocess\nsubprocess.run(\n    [\n        \"podman\",\n        \"stop\",\n        cid,\n    ]\n)\n"
	got := Judge("f.py", text, false)
	if len(got) != 1 || got[0].Rule != "engine-literal" || got[0].Line != 4 {
		t.Fatalf("got %+v", got)
	}
	got = Judge("f.py", "run([\n    \"kill\",\n    pid,\n])\n", false)
	if len(got) != 1 || got[0].Rule != "argv-kill" || got[0].Line != 2 {
		t.Fatalf("got %+v", got)
	}
}

// E11: the exception.
func TestE11ContainmentException(t *testing.T) {
	checkCases(t, true, []ruleCase{
		{"stop argv builds", `    return [engine, "stop", "--time", str(10), container_id]`, ""},
		{"literal stop", `run(["podman", "stop", cid])`, ""},
		{"kill verb is an argv kill first", `return [engine, "kill", cid]`, "argv-kill"},
		{"rm verb", `return [engine, "rm", cid]`, "containment-verb"},
		{"pause verb", `return [engine, 'pause', cid]`, "containment-verb"},
		{"unpause verb", `return [engine, "unpause", cid]`, "containment-verb"},
		{"restart verb", `return [engine, "restart", cid]`, "containment-verb"},
		{"force", `args.append("--force")`, "containment-force"},
		{"short force", `args = ["-f", cid]`, "containment-force"},
		{"signal flag", `args = ["--signal", "KILL"]`, "containment-signal"},
		{"os.kill still bars", `os.kill(pid, signal.SIGKILL)`, "os-kill"},
		{"pkill still bars", `subprocess.run(["pkill", "-f", name], check=False)`, "argv-kill"},
		{"signal methods still bar", `proc.terminate()`, "proc-kill"},
		{"time 0 as two elements", `return [engine, "stop", "--time", "0", cid]`, "containment-timeout"},
		{"short time 0", `return [engine, "stop", "-t", "0", cid]`, "containment-timeout"},
		{"time=0", `return [engine, "stop", "--time=0", cid]`, "containment-timeout"},
		{"short t=0", `return [engine, "stop", "-t=0", cid]`, "containment-timeout"},
		{"time integer zero", `return [engine, "stop", "--time", 0, cid]`, "containment-timeout"},
		{"time 00", `return [engine, "stop", "--time", "00", cid]`, "containment-timeout"},
		{"time 10 passes", `return [engine, "stop", "--time", "10", cid]`, ""},
		{"time=30 passes", `return [engine, "stop", "--time=30", cid]`, ""},
		{"t 0.5 is not zero", `return [engine, "stop", "-t", "0.5", cid]`, ""},
		{"prose naming stop", `"""one file may stop a container, every other file still may not."""`, ""},
	})
}

// E9, review F2: a wrapped parenthesised import is seen, and the finding is its first line.
func TestE9ParenthesisedImportOfKillIsSeen(t *testing.T) {
	text := "x = 1\nfrom os import (\n    getpid,\n    kill,\n)\nkill(pid, 9)\n"
	got := Judge("f.py", text, false)
	if len(got) != 1 || got[0].Rule != "os-import-kill" || got[0].Line != 2 {
		t.Fatalf("got %+v", got)
	}
	checkCases(t, false, []ruleCase{
		{"wrapped import of signal names", "from signal import (\n    SIGTERM,\n)", "os-import-kill"},
		{"wrapped import without kill", "from os import (\n    getpid,\n    path,\n)\nx = (kill_count)", ""},
	})
}

// E9, review F6: ending a unit or a machine is a termination.
func TestE9UnitAndMachineTermination(t *testing.T) {
	checkCases(t, false, []ruleCase{
		{"unit stop", `run(["systemctl", "stop", unit])`, "unit-kill"},
		{"unit user kill", `run(["systemctl", "--user", "kill", unit])`, "unit-kill"},
		{"machine terminate", `run(["machinectl", "terminate", m])`, "unit-kill"},
		{"machine kill", `run(['machinectl', 'kill', m])`, "unit-kill"},
		{"unit status is fine", `run(["systemctl", "status", unit])`, ""},
		{"unit show is fine", `run(["systemctl", "--user", "show", unit])`, ""},
		{"machine list is fine", `run(["machinectl", "list"])`, ""},
	})
	checkCases(t, true, []ruleCase{
		{"containment.py may not end a unit either", `run(["systemctl", "stop", unit])`, "unit-kill"},
	})
}

// E10, review F8: a comment inside a wrapped argv does not hide it.
func TestE10CommentsInsideAWrappedArgvAreSkipped(t *testing.T) {
	got := Judge("f.py", "run([\"podman\",   # the engine\n    \"stop\", cid])\n", false)
	if len(got) != 1 || got[0].Rule != "engine-literal" || got[0].Line != 1 {
		t.Fatalf("got %+v", got)
	}
	got = Judge("f.py", "run([  # argv\n    \"kill\", pid])\n", false)
	if len(got) != 1 || got[0].Rule != "argv-kill" || got[0].Line != 2 {
		t.Fatalf("got %+v", got)
	}
}

// E11: the containment rules belong to the exception alone.
func TestE11ContainmentRulesOnlyApplyToTheException(t *testing.T) {
	checkCases(t, false, []ruleCase{
		{"-f is fine elsewhere", `args = ["-f", path]`, ""},
		{"--force is fine elsewhere", `parser.add_argument("--force")`, ""},
	})
}

// E12: comments are judged like code.
func TestE12CommentsAreJudged(t *testing.T) {
	checkCases(t, false, []ruleCase{
		{"commented call", "# os.kill(pid, 9) was removed", "os-kill"},
		{"comment without a call shape", "# we never kill anything", ""},
	})
}

// E8: line splitting.
func TestE8CarriageReturnNulAndNoTrailingNewline(t *testing.T) {
	got := Judge("f.py", "ok\r\nos.kill(pid, 9)\r\nx", false)
	if len(got) != 1 || got[0].Line != 2 || got[0].Text != "os.kill(pid, 9)\r" {
		t.Fatalf("got %+v", got)
	}
	got = Judge("f.py", "a\x00b\nos.kill(1, 2)", false)
	if len(got) != 1 || got[0].Line != 2 {
		t.Fatalf("a NUL byte hid a finding: %+v", got)
	}
	got = Judge("f.py", "ok\nos.kill(1, 2)", false)
	if len(got) != 1 || got[0].Line != 2 {
		t.Fatalf("a last line without a newline was missed: %+v", got)
	}
}

// E14: a line matched by several rules is reported once, under the first rule; order is by line.
func TestE14OneFindingPerLineInLineOrder(t *testing.T) {
	text := "os.kill(p, signal.SIGKILL)\nok\nproc.kill()\n"
	got := Judge("f.py", text, false)
	if len(got) != 2 || got[0].Line != 1 || got[0].Rule != "os-kill" || got[1].Line != 3 || got[1].Rule != "proc-kill" {
		t.Fatalf("got %+v", got)
	}
	if got[0].Path != "f.py" {
		t.Fatalf("path %q", got[0].Path)
	}
}
