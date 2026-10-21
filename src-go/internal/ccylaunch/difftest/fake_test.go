package difftest

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"golang.org/x/sys/unix"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
)

// The fakes are this test binary itself, linked into the sandbox's bin directory under each
// program's name: TestMain sees the name it was started as and answers as that program, before
// any test runs. A fake finds its sandbox through sandboxEnv, which the runner puts in the
// launcher's environment and the launcher passes on to whatever it starts.
const (
	sandboxEnv  = "CCY_DIFFTEST_SANDBOX"
	starterName = "ccy-difftest-start"
	dateName    = "date"
	fakesFile   = "fakes.json"
	callsFile   = "calls.jsonl"
	pidFile     = "launcher.pid"
	fakeBroken  = 97 // a fake that cannot read its sandbox: never an answer a rule gives
	logFileMode = 0o600
)

// observedTools are the programs whose every call is recorded in the golden log and answered
// from the scenario's rules: what the launcher asks of the world outside the sandbox. A Go
// launcher must make the same calls.
var observedTools = []string{
	"ccy-teams", "claude", "curl", "docker", "docker-compose", "fzf", "getenforce", "gh", "gum",
	"loginctl", "notify-send", "podman", "podman-compose", "python3", "ssh", "ssh-add", "ssh-agent",
	"ssh-keygen", "stty", "sudo", "systemctl", "systemd-run", "timedatectl", "tmux", "tput",
	"xdg-open",
}

// quietTools are the environment fakes: answered from the rules like an observed tool, but
// not recorded, because they report facts about this machine (its name, its users) that a Go
// launcher reads without starting a program. Every call must be answered by a rule.
var quietTools = []string{"hostname", "id", "uname", "whoami"}

// realTools run for real inside the sandbox: pure text and file tools that touch nothing
// outside it. Every one must exist on the machine; a missing one fails the run (it is a
// dependency, owned by the play that installs it).
var realTools = []string{
	"awk", "base64", "basename", "bash", "cat", "chmod", "cmp", "comm", "cp", "cut", "dirname",
	"du", "env", "expr", "false", "find", "flock", "getent", "git", "grep", "head", "jq", "ln",
	"ls", "md5sum", "mkdir", "mktemp", "mv", "nproc", "numfmt", "od", "printf", "readlink",
	"realpath", "rm", "rmdir", "sed", "seq", "sh", "sha256sum", "sleep", "sort", "stat",
	"systemd-escape", "tail", "tee", "test", "timeout", "touch", "tr", "true", "uniq",
	"wc", "xargs",
}

// bashNoise are the names bash puts in every child's environment itself: not the launcher's
// choice, so not recorded.
var bashNoise = []string{"OLDPWD", "PWD", "SHLVL", "_"}

// rule answers the calls of one program whose arguments start with Args.
type rule struct {
	Tool   string   `json:"tool"`
	Args   []string `json:"args"`
	Stdout string   `json:"stdout"`
	Stderr string   `json:"stderr"`
	Exit   int      `json:"exit"`
}

// fakeConfig is what the runner writes into the sandbox for the fakes to read.
type fakeConfig struct {
	Rules    []rule   `json:"rules"`
	RealDate string   `json:"realDate"`
	Epoch    int64    `json:"epoch"`
	Base     []string `json:"base"` // the environment names the runner set: not recorded
}

// call is one recorded call of an observed program.
type call struct {
	Tool string   `json:"tool"`
	Args []string `json:"args"`
	Env  []string `json:"env"` // names only, never values: what the launcher added
}

func fakeTool() (string, bool) {
	if os.Getenv(sandboxEnv) == "" {
		return "", false
	}
	name := filepath.Base(os.Args[0])
	if name == starterName || name == dateName || slices.Contains(observedTools, name) ||
		slices.Contains(quietTools, name) {
		return name, true
	}
	return "", false
}

func runFake(tool string) int {
	sandbox := os.Getenv(sandboxEnv)
	cfg, err := readFakeConfig(sandbox)
	if err != nil {
		return brokenFake(tool, err)
	}
	switch tool {
	case starterName:
		return brokenFake(tool, startLauncher(sandbox))
	case dateName:
		return brokenFake(tool, execx.OS{}.Exec(execx.Cmd{Name: cfg.RealDate, Args: fixedDateArgs(os.Args[1:], cfg.Epoch)}))
	}
	if slices.Contains(observedTools, tool) {
		if err := recordCall(sandbox, cfg, tool, os.Args[1:]); err != nil {
			return brokenFake(tool, err)
		}
	}
	r, ok := matchRule(cfg.Rules, tool, os.Args[1:])
	switch {
	case !ok && slices.Contains(quietTools, tool):
		// An unanswered quiet call would have to ask the real machine, whose answer differs
		// from one machine to the next: the scenario must answer it.
		return brokenFake(tool, fmt.Errorf("no rule answers %s %s", tool, strings.Join(os.Args[1:], " ")))
	case !ok:
		return 1
	}
	if _, err := os.Stdout.WriteString(r.Stdout); err != nil {
		return brokenFake(tool, err)
	}
	if _, err := os.Stderr.WriteString(r.Stderr); err != nil {
		return brokenFake(tool, err)
	}
	return r.Exit
}

func brokenFake(tool string, err error) int {
	if err == nil {
		return 0
	}
	if _, werr := fmt.Fprintf(os.Stderr, "difftest fake %s: %v\n", tool, err); werr != nil {
		return fakeBroken
	}
	return fakeBroken
}

func readFakeConfig(sandbox string) (fakeConfig, error) {
	var cfg fakeConfig
	raw, err := readIn(sandbox, fakesFile)
	if err != nil {
		return cfg, err
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	return cfg, dec.Decode(&cfg)
}

// startLauncher records its own pid, which exec keeps, and becomes the launcher: so the golden
// can name the launcher's pid wherever it appears ($$ in a file name or an argument).
func startLauncher(sandbox string) error {
	if len(os.Args) < 2 {
		return errors.New("no launcher named")
	}
	pid := []byte(strconv.Itoa(os.Getpid()))
	if err := writeIn(sandbox, pidFile, pid); err != nil {
		return err
	}
	return execx.OS{}.Exec(execx.Cmd{Name: os.Args[1], Args: os.Args[2:]})
}

// ownDateFlags are the date options that name a date or a clock of their own.
var ownDateFlags = []string{"-d", "--date", "-r", "--reference", "-f", "--file", "-s", "--set"}

// fixedDateArgs pins "now" for a date call that asks for it: one that names no date of its
// own gets -d @epoch first, so every run of a scenario sees the same day.
func fixedDateArgs(args []string, epoch int64) []string {
	for _, a := range args {
		for _, own := range ownDateFlags {
			short := len(own) == 2 && strings.HasPrefix(a, own)
			if a == own || strings.HasPrefix(a, own+"=") || short {
				return args
			}
		}
	}
	return append([]string{"-d", "@" + strconv.FormatInt(epoch, 10)}, args...)
}

func recordCall(sandbox string, cfg fakeConfig, tool string, args []string) error {
	var names []string
	for _, kv := range os.Environ() {
		name := envName(kv)
		if slices.Contains(cfg.Base, name) || slices.Contains(bashNoise, name) || strings.HasPrefix(name, "BASH_FUNC_") {
			continue
		}
		names = append(names, name)
	}
	slices.Sort(names)
	line, err := json.Marshal(call{Tool: tool, Args: args, Env: names})
	if err != nil {
		return err
	}
	root, err := os.OpenRoot(sandbox)
	if err != nil {
		return err
	}
	f, err := root.OpenFile(callsFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, logFileMode)
	if err != nil {
		return errors.Join(err, root.Close())
	}
	_, err = f.Write(append(line, '\n'))
	return errors.Join(err, f.Close(), root.Close())
}

// readIn reads one file of the sandbox, and writeIn writes one, through an os.Root: a fake
// touches nothing outside its sandbox.
func readIn(sandbox, name string) ([]byte, error) {
	root, err := os.OpenRoot(sandbox)
	if err != nil {
		return nil, err
	}
	raw, err := root.ReadFile(name)
	return raw, errors.Join(err, root.Close())
}

func writeIn(sandbox, name string, data []byte) error {
	root, err := os.OpenRoot(sandbox)
	if err != nil {
		return err
	}
	return errors.Join(root.WriteFile(name, data, logFileMode), root.Close())
}

// silenceStderr points this process's stderr at /dev/null.
func silenceStderr() error {
	null, err := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
	if err != nil {
		return err
	}
	if err := unix.Dup2(int(null.Fd()), int(os.Stderr.Fd())); err != nil {
		return errors.Join(err, null.Close())
	}
	return null.Close()
}

// envName is the name of one NAME=value environment entry.
func envName(kv string) string {
	return strings.Split(kv, "=")[0]
}

func matchRule(rules []rule, tool string, args []string) (rule, bool) {
	for _, r := range rules {
		if r.Tool == tool && len(args) >= len(r.Args) && slices.Equal(args[:len(r.Args)], r.Args) {
			return r, true
		}
	}
	return rule{}, false
}
