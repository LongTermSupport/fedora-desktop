package ccystart

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
)

// The parity oracle for the port of entrypoint.sh's end (Plan 00209 Task 4.2): every v1 launch
// gets the command line the bash built, byte for byte, with the same refusals and messages.
// testdata/entrypoint-tail-v1.bash.txt is entrypoint.sh from its AGENT-BUS marker to the end, as
// it was at the commit before the port. Each scenario runs that bash, its fixed paths pointed
// into a scratch tree and its exec replaced by a recorder, and then Start over the same tree
// and environment.

const tailFile = "testdata/entrypoint-tail-v1.bash.txt"

// fixture is one scenario's scratch tree: the image's kits, the membership mounts, the
// supervisor plugin directory and some projects' .claude/ccy.
type fixture struct {
	dir   string
	paths Paths
}

func newFixture(t *testing.T) fixture {
	t.Helper()
	dir := t.TempDir()
	f := fixture{dir: dir, paths: Paths{
		AgentBus:          filepath.Join(dir, "image/agent-bus"),
		PingbusLink:       filepath.Join(dir, "bin/pingbus"),
		Membership:        filepath.Join(dir, "run-ccy-membership"),
		SessionHome:       filepath.Join(dir, "pingbus-home"),
		DefaultSupervisor: filepath.Join(dir, "ws/claude-supervise.py"),
		SupervisorPlugins: filepath.Join(dir, "plugins"),
		Teamchat:          filepath.Join(dir, "image/teamchat"),
		TeamchatLink:      filepath.Join(dir, "bin/teamchat"),
		TeamchatDesired:   filepath.Join(dir, "run-teamchat-membership"),
	}}
	files := map[string]string{
		"image/agent-bus/pingbus":                                   "#!/bin/sh\n",
		"image/agent-bus/pingbus-status-line":                       "#!/bin/sh\n",
		"image/agent-bus/plugin/pingbus/.claude-plugin/plugin.json": "{}\n",
		"image/agent-bus/plugin/pingbus/hooks/hooks.json":           "{}\n",
		"run-ccy-membership/membership.json":                        "{}\n",
		"plugins/ccy_lifecycle.py":                                  "# plugin\nPLUGIN_API = 1\n",
		"ws/claude-supervise.py":                                    "_PLUGIN_API_MAJOR = 1\n",
		"ws/claude-supervise":                                       "#!/bin/sh\nexec python3 \"$0.py\" \"$@\"\n",
		"old/claude-supervise.py":                                   "import argparse\n",
		"old/claude-supervise":                                      "#!/bin/sh\n",
		"api2/claude-supervise.py":                                  "_PLUGIN_API_MAJOR = 2\n",
		"sh/claude-supervise":                                       "#!/bin/sh\nMIN_MAJOR=3\nrun_unsupervised() {\n    :\n}\n",
		"sh/claude-supervise.py":                                    "_PLUGIN_API_MAJOR = 1\n",
		"sh/other-script":                                           "#!/usr/bin/env bash\necho hi\n",
		"py/claude-supervise":                                       "#!/usr/bin/env python3\nMIN_MAJOR = 3\n",
		"py/claude-supervise.py":                                    "_PLUGIN_API_MAJOR = 1\n",
		"image/teamchat/bin/teamchat":                               "#!/bin/sh\n",
		"run-teamchat-membership/desired.json":                      "{}\n",
	}
	for name, body := range files {
		writeFile(t, filepath.Join(dir, name), body)
	}
	if err := os.MkdirAll(filepath.Join(dir, "bin"), 0o750); err != nil {
		t.Fatal(err)
	}
	return f
}

func writeFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		t.Fatal(err)
	}
	root, err := os.OpenRoot(filepath.Dir(path))
	if err != nil {
		t.Fatal(err)
	}
	if err := errors.Join(root.WriteFile(filepath.Base(path), []byte(body), 0o600), root.Close()); err != nil {
		t.Fatal(err)
	}
}

// expand puts the fixture's directory in for {d}.
func (f fixture) expand(s string) string { return strings.ReplaceAll(s, "{d}", f.dir) }

// outcome is what a launch did: the exit status, its stderr, the command it ran, the
// CCY_CLAUDE_WRAPPER that command inherited and where the pingbus link points. execs is the
// Go side's recorded exec, for the tests that read its environment.
type outcome struct {
	code    int
	stderr  string
	argv    []string
	wrapper string
	link    string
	execs   []execx.Cmd
}

func readLink(path string) string {
	target, err := os.Readlink(path)
	if err != nil {
		return "none"
	}
	return target
}

func removeAll(t *testing.T, paths ...string) {
	t.Helper()
	for _, p := range paths {
		if err := os.Remove(p); err != nil && !errors.Is(err, os.ErrNotExist) {
			t.Fatal(err)
		}
	}
}

// runBash runs the frozen tail with its paths in the fixture and its exec recorded.
func (f fixture) runBash(t *testing.T, env map[string]string, args []string) outcome {
	t.Helper()
	argvFile := filepath.Join(f.dir, "argv.bin")
	wrapperFile := filepath.Join(f.dir, "wrapper.txt")
	removeAll(t, f.paths.PingbusLink, argvFile, wrapperFile)
	raw, err := os.ReadFile(tailFile)
	if err != nil {
		t.Fatal(err)
	}
	tail := strings.NewReplacer(
		"/opt/claude-yolo/optional/agent-bus", f.paths.AgentBus,
		"/usr/local/bin/pingbus", f.paths.PingbusLink,
		"/run/ccy/membership", f.paths.Membership,
		"/tmp/pingbus-home", f.paths.SessionHome,
		"/workspace/.claude/ccy/claude-supervise.py", f.paths.DefaultSupervisor,
		"/opt/claude-yolo/supervisor-plugins", f.paths.SupervisorPlugins,
		"/tmp/ccy-relaunch-plugin.why", filepath.Join(f.dir, "why"),
	).Replace(string(raw))
	prelude := "_ccy_bus_snapshot() { :; }\n_ccy_bus_before=\n" +
		"exec() { printf '%s\\0' \"$@\" >" + argvFile + "; " +
		"bash -c 'printf %s \"${CCY_CLAUDE_WRAPPER-unset}\"' >" + wrapperFile + "; builtin exit 0; }\n"
	script := filepath.Join(f.dir, "tail.bash")
	writeFile(t, script, prelude+tail)
	var stderr bytes.Buffer
	code := 0
	_, runErr := envRunner{env: env, dir: f.dir}.Run(context.Background(),
		execx.Cmd{Name: "bash", Args: append([]string{"-e", script}, args...), Stderr: &stderr})
	if runErr != nil {
		exitErr, ok := errors.AsType[*execx.ExitError](runErr)
		if !ok {
			t.Fatal(runErr)
		}
		code = exitErr.Code
	}
	o := outcome{code: code, stderr: stderr.String(), link: readLink(f.paths.PingbusLink), wrapper: "unset"}
	data, err := os.ReadFile(filepath.Clean(argvFile))
	if errors.Is(err, os.ErrNotExist) {
		return o
	}
	if err != nil {
		t.Fatal(err)
	}
	if len(data) <= 1 {
		return o // exec with no command: printf wrote one NUL
	}
	o.argv = strings.Split(strings.TrimSuffix(string(data), "\x00"), "\x00")
	w, err := os.ReadFile(filepath.Clean(wrapperFile))
	if err != nil {
		t.Fatal(err)
	}
	o.wrapper = string(w)
	return o
}

// runGo runs Start over the same tree and environment, with the real python3 for the
// supervisor's syntax probe and a recorder for the exec.
func (f fixture) runGo(t *testing.T, env map[string]string, args []string) outcome {
	t.Helper()
	removeAll(t, f.paths.PingbusLink, f.paths.TeamchatLink)
	var stderr bytes.Buffer
	fake := &execx.Fake{}
	s := Session{
		Env: func(k string) (string, bool) {
			v, ok := env[k]
			return v, ok
		},
		Args:   args,
		Paths:  f.paths,
		Stderr: &stderr,
		Runner: envRunner{env: env, dir: f.dir},
		Execer: fake,
	}
	code := Start(context.Background(), s)
	o := outcome{code: int(code), stderr: stderr.String(), link: readLink(f.paths.PingbusLink), wrapper: "unset", execs: fake.Execs()}
	switch {
	case len(o.execs) > 1:
		t.Fatalf("Start ran %d execs", len(o.execs))
	case len(o.execs) == 1:
		c := o.execs[0]
		o.argv = append([]string{c.Name}, c.Args...)
		o.wrapper = childValue(env, c, "CCY_CLAUDE_WRAPPER")
	}
	return o
}

// childValue is name's value in the environment c gives its program, "unset" when absent.
func childValue(parent map[string]string, c execx.Cmd, name string) string {
	base := make([]string, 0, len(parent))
	for k, v := range parent {
		base = append(base, k+"="+v)
	}
	for _, kv := range execx.ChildEnv(base, c.Env, c.Unset) {
		if k, v, found := strings.Cut(kv, "="); found && k == name {
			return v
		}
	}
	return "unset"
}

type scenario struct {
	name  string
	env   map[string]string
	args  []string
	setup func(t *testing.T, f fixture)
}

func remove(rel string) func(*testing.T, fixture) {
	return func(t *testing.T, f fixture) {
		t.Helper()
		removeAll(t, filepath.Join(f.dir, rel))
	}
}

// with is an environment of PINGBUS_MEMBERSHIP=off and the given pairs.
func with(kv ...string) map[string]string {
	m := map[string]string{"PINGBUS_MEMBERSHIP": "off"}
	for i := 0; i+1 < len(kv); i += 2 {
		m[kv[i]] = kv[i+1]
	}
	return m
}

// seated is with, on the v1 membership mount.
func seated(kv ...string) map[string]string {
	return with(append([]string{"PINGBUS_MEMBERSHIP", "{d}/run-ccy-membership"}, kv...)...)
}

const (
	envWrapper = "CCY_CLAUDE_WRAPPER"
	envNoSup   = "CCY_NO_SUPERVISOR"
	envAge     = "CCY_LIFECYCLE_MAX_AGE_SECONDS"
	sup        = "python3 {d}/ws/claude-supervise.py --"
	supArmed   = "python3 {d}/ws/claude-supervise.py --arm --"
)

func v1Scenarios() []scenario {
	claude := []string{"claude", "--dangerously-skip-permissions", "hi"}
	one := []string{"claude"}
	return []scenario{
		{name: "off, a wrapper", env: with(envWrapper, sup), args: claude},
		{name: "off, --no-supervise", env: with(envNoSup, "1"), args: []string{"claude", "--x"}},
		{name: "off, --no-supervise sets the project's wrapper aside", env: with(envNoSup, "1", envWrapper, supArmed), args: one},
		{name: "off, the default supervisor", env: with(), args: claude},
		{name: "off, a supervisor path from the host", env: with("CCY_SUPERVISOR_PATH", "{d}/py/claude-supervise.py"), args: claude},
		{name: "off, an empty wrapper takes the default", env: with(envWrapper, ""), args: claude},
		{name: "off, no supervisor present", env: with(), args: claude, setup: remove("ws/claude-supervise.py")},
		{name: "off, a supervisor that does not parse", env: with(), args: claude, setup: func(t *testing.T, f fixture) {
			t.Helper()
			writeFile(t, f.paths.DefaultSupervisor, "def broken(:\n")
		}},
		{name: "off, a command other than claude", env: with(envNoSup, "1"), args: []string{"bash", "-l"}},
		{name: "off, no arguments", env: with(envNoSup, "1"), args: nil},
		{name: "off, a blank wrapper", env: with(envWrapper, "   "), args: one},
		{name: "seated, a wrapper", env: seated(envWrapper, sup), args: claude},
		{name: "seated, an armed wrapper", env: seated(envWrapper, supArmed), args: one},
		{name: "seated, the daemon's launcher", env: seated(envWrapper, "{d}/ws/claude-supervise --arm --"), args: one},
		{name: "seated, --no-supervise", env: seated(envNoSup, "1"), args: []string{"claude", "--x"}},
		{name: "seated, a wrapper that is not the supervisor", env: seated(envWrapper, "env FOO=1 --"), args: one},
		{name: "seated, a supervisor on another API", env: seated(envWrapper, "python3 {d}/api2/claude-supervise.py --"), args: one},
		{name: "seated, no plugin in the image", env: seated(envWrapper, sup), args: one, setup: remove("plugins/ccy_lifecycle.py")},
		{name: "seated, a command other than claude", env: seated(envNoSup, "1"), args: []string{"bash", "-l"}},
		{name: "seated, no hooks.json", env: seated(envNoSup, "1"), args: one, setup: remove("image/agent-bus/plugin/pingbus/hooks/hooks.json")},
		{name: "seated, no plugin.json", env: seated(envNoSup, "1"), args: one, setup: remove("image/agent-bus/plugin/pingbus/.claude-plugin/plugin.json")},
		{name: "seated, no status line", env: seated(envNoSup, "1"), args: one, setup: remove("image/agent-bus/pingbus-status-line")},
		{name: "seated, no zipapp", env: seated(envNoSup, "1"), args: one, setup: remove("image/agent-bus/pingbus")},
		{name: "seated, no membership file", env: seated(envNoSup, "1"), args: one, setup: remove("run-ccy-membership/membership.json")},
		{name: "membership unset", env: map[string]string{envNoSup: "1"}, args: one},
		{name: "membership elsewhere", env: with("PINGBUS_MEMBERSHIP", "/elsewhere"), args: one},
		{name: "membership empty", env: with("PINGBUS_MEMBERSHIP", ""), args: one},
		{name: "leftover PINGBUS_SEATS beside the mount", env: seated("PINGBUS_SEATS", "dev1@team-a"), args: one},
		{name: "leftover PINGBUS_SEATS beside off", env: with("PINGBUS_SEATS", "dev1@team-a"), args: one},
		{name: "leftover PINGBUS_TEAMS", env: with("PINGBUS_TEAMS", "team-a"), args: one},
		{name: "leftover PINGBUS_HOME, empty", env: seated("PINGBUS_HOME", ""), args: one},
		{name: "lifecycle, max age", env: with(envWrapper, sup, envAge, "7200"), args: one},
		{name: "lifecycle, deadline", env: with(envWrapper, sup, "CCY_LIFECYCLE_DEADLINE_EPOCH", "1790007200"), args: one},
		{name: "lifecycle, armed", env: with(envWrapper, supArmed, envAge, "7200"), args: one},
		{name: "lifecycle, seated", env: seated(envWrapper, sup, envAge, "7200"), args: one},
		{name: "daemon launcher, no option", env: with(envWrapper, "{d}/ws/claude-supervise --arm --"), args: one},
		{name: "daemon launcher, max age", env: with(envWrapper, "{d}/ws/claude-supervise --arm --", envAge, "7200"), args: one},
		{name: "launcher with no sibling", env: with(envWrapper, "{d}/nowhere/claude-supervise --arm --", envAge, "7200"), args: one},
		{name: "launcher beside an old supervisor", env: with(envWrapper, "{d}/old/claude-supervise --arm --", envAge, "7200"), args: one},
		{name: "an old supervisor", env: with(envWrapper, "python3 {d}/old/claude-supervise.py --", envAge, "7200"), args: one},
		{name: "another API major", env: with(envWrapper, "python3 {d}/api2/claude-supervise.py --", envAge, "7200"), args: one},
		{name: "a supervisor not there", env: with(envWrapper, "python3 {d}/nowhere/claude-supervise.py --", envAge, "7200"), args: one},
		{name: "a wrapper that is not the supervisor", env: with(envWrapper, "env FOO=1 --", envAge, "7200"), args: one},
		{name: "a supervisor line with no final --", env: with(envWrapper, "python3 {d}/ws/claude-supervise.py --arm", envAge, "7200"), args: one},
		{name: "a wrapper of one word", env: with(envWrapper, "--", envAge, "7200"), args: one},
		{name: "a feature with no supervisor", env: with(envNoSup, "1", envAge, "7200"), args: one},
		{name: "a feature with --no-supervise over a wrapper", env: with(envNoSup, "1", envWrapper, sup, envAge, "7200"), args: one},
		{name: "a missing plugin file", env: with(envWrapper, sup, envAge, "7200"), args: one, setup: remove("plugins/ccy_lifecycle.py")},
		{name: "a plugin with no API", env: with(envWrapper, sup, envAge, "7200"), args: one, setup: func(t *testing.T, f fixture) {
			t.Helper()
			writeFile(t, filepath.Join(f.paths.SupervisorPlugins, "ccy_lifecycle.py"), "# none\n")
		}},
		{name: "python3 on the sh launcher", env: with(envWrapper, "python3 {d}/sh/claude-supervise --arm --"), args: one},
		{name: "an interpreter by path on the sh launcher", env: with(envWrapper, "/usr/bin/python3.12 {d}/sh/claude-supervise --"), args: one},
		{name: "python3 on a bash script", env: with(envWrapper, "python3 {d}/sh/other-script --"), args: one},
		{name: "python3 on a python-shebang launcher", env: with(envWrapper, "python3 {d}/py/claude-supervise --"), args: one},
		{name: "python3 on the .py", env: with(envWrapper, "python3 {d}/sh/claude-supervise.py --arm --"), args: one},
		{name: "the sh launcher run directly", env: with(envWrapper, "{d}/sh/claude-supervise --arm --"), args: one},
		{name: "python3 alone", env: with(envWrapper, "python3"), args: one},
		{name: "tabs and newlines in the wrapper", env: with(envWrapper, "python3\t{d}/ws/claude-supervise.py\n--"), args: []string{"claude", "a b"}},
	}
}

func TestV1CommandIsTheBashCommand(t *testing.T) {
	if _, err := exec.LookPath("python3"); err != nil {
		t.Fatal("python3 is needed: the supervisor probe runs it")
	}
	for _, sc := range v1Scenarios() {
		t.Run(sc.name, func(t *testing.T) {
			f := newFixture(t)
			if sc.setup != nil {
				sc.setup(t, f)
			}
			env := map[string]string{}
			for k, v := range sc.env {
				env[k] = f.expand(v)
			}
			want := f.runBash(t, env, sc.args)
			got := f.runGo(t, env, sc.args)
			if got.code != want.code || got.stderr != want.stderr || !slices.Equal(got.argv, want.argv) ||
				got.wrapper != want.wrapper || got.link != want.link {
				t.Errorf("Go and bash differ\n Go:   %+v\n bash: %+v", got, want)
			}
		})
	}
}

// envRunner runs a command in the scenario's environment alone, as the bash's children ran.
type envRunner struct {
	env map[string]string
	dir string
}

func (r envRunner) Run(ctx context.Context, c execx.Cmd) (execx.Result, error) {
	c.CleanEnv = true
	c.Env = []string{"PATH=" + os.Getenv("PATH"), "HOME=" + r.dir}
	for k, v := range r.env {
		c.Env = append(c.Env, k+"="+v)
	}
	res, err := execx.OS{}.Run(ctx, c)
	if err != nil {
		return res, fmt.Errorf("run %s: %w", c.Name, err)
	}
	return res, nil
}
