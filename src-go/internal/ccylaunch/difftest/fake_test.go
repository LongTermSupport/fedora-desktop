package difftest

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

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
	statName    = "stat"
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
	"realpath", "rm", "rmdir", "sed", "seq", "sh", "sha256sum", "sleep", "sort",
	"systemd-escape", "tail", "tee", "test", "timeout", "touch", "tr", "true", "uniq",
	"wc", "xargs",
}

// bashNoise are the names bash puts in every child's environment itself: not the launcher's
// choice, so not recorded.
var bashNoise = []string{"OLDPWD", "PWD", "SHLVL", "_"}

// rule answers the calls of one program whose arguments start with Args. ArgFiles writes
// files the way the program would: for each option, the content goes to the path that follows
// the option in the call (curl's --dump-header), which must be inside the sandbox.
type rule struct {
	Tool     string            `json:"tool"`
	Args     []string          `json:"args"`
	Stdout   string            `json:"stdout"`
	Stderr   string            `json:"stderr"`
	Exit     int               `json:"exit"`
	ArgFiles map[string]string `json:"argFiles"`
}

// fakeConfig is what the runner writes into the sandbox for the fakes to read.
type fakeConfig struct {
	Rules []rule            `json:"rules"`
	Real  map[string]string `json:"real"` // the real date and stat the clock fakes run
	Epoch int64             `json:"epoch"`
	// Shift is how far the machine's clock is ahead of the pinned one: a file the launcher
	// writes is stamped by the machine, and stat reports its time shifted back by this much.
	Shift int64    `json:"shift"`
	Base  []string `json:"base"` // the environment names the runner set: not recorded
}

// call is one recorded call of an observed program.
type call struct {
	Tool string   `json:"tool"`
	Args []string `json:"args"`
	Env  []string `json:"env"` // names only, never values: those the program reads (readsEnv)
}

func fakeTool() (string, bool) {
	if os.Getenv(sandboxEnv) == "" {
		return "", false
	}
	name := filepath.Base(os.Args[0])
	if name == starterName || slices.Contains(clockTools, name) || slices.Contains(observedTools, name) ||
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
		return brokenFake(tool, execx.OS{}.Exec(execx.Cmd{Name: cfg.Real[dateName], Args: fixedDateArgs(os.Args[1:], cfg.Epoch)}))
	case statName:
		return shiftedStat(cfg, os.Args[1:])
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
	if err := writeArgFiles(sandbox, r, os.Args[1:]); err != nil {
		return brokenFake(tool, err)
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

// clockTools are the real programs that tell the time, run with the pinned clock.
var clockTools = []string{dateName, statName}

// mtimeFormats are the stat arguments that ask for a modification time in seconds alone.
var mtimeFormats = []string{"%Y", "-c%Y", "--format=%Y", "--printf=%Y"}

// shiftedStat is the real stat; a call that asks for %Y alone gets each time on the pinned
// clock, so a file's age is the same whichever machine and day the scenario runs on.
func shiftedStat(cfg fakeConfig, args []string) int {
	if !slices.ContainsFunc(args, func(a string) bool { return slices.Contains(mtimeFormats, a) }) {
		return brokenFake(statName, execx.OS{}.Exec(execx.Cmd{Name: cfg.Real[statName], Args: args}))
	}
	res, err := execx.OS{}.Run(context.Background(), execx.Cmd{Name: cfg.Real[statName], Args: args})
	var exitErr *execx.ExitError
	if err != nil && !errors.As(err, &exitErr) {
		return brokenFake(statName, err)
	}
	var out strings.Builder
	for line := range strings.Lines(string(res.Stdout)) {
		text := strings.TrimSuffix(line, "\n")
		if n, perr := strconv.ParseInt(text, 10, 64); perr == nil {
			text = strconv.FormatInt(pinnedTime(n, cfg), 10)
		}
		out.WriteString(text + line[len(strings.TrimSuffix(line, "\n")):])
	}
	if _, werr := os.Stdout.WriteString(out.String()); werr != nil {
		return brokenFake(statName, werr)
	}
	if _, werr := os.Stderr.Write(res.Stderr); werr != nil {
		return brokenFake(statName, werr)
	}
	return res.ExitCode
}

// runWindow bounds one run: a time the machine gave a file during it reads as the pinned now,
// so how long the run took never shows.
const runWindow = 300

// pinnedTime is a machine time on the pinned clock.
func pinnedTime(machine int64, cfg fakeConfig) int64 {
	pinned := machine - cfg.Shift
	if pinned >= cfg.Epoch && pinned < cfg.Epoch+runWindow {
		return cfg.Epoch
	}
	return pinned
}

// relativeDate is a -d value counted from now ("+90 days", "3 days ago", "tomorrow").
var relativeDate = regexp.MustCompile(`^\s*([+-]?\d+\s*[a-z]+(\s+ago)?|now|today|tomorrow|yesterday)\s*$`)

// fixedDateArgs pins "now" for a date call that asks for it: one that names no date of its
// own gets -d @epoch first, and a -d counted from now is counted from the epoch, so every
// run of a scenario sees the same day.
func fixedDateArgs(args []string, epoch int64) []string {
	anchor := time.Unix(epoch, 0).UTC().Format("2006-01-02 15:04:05 UTC")
	for i, a := range args {
		if a == "-d" && i+1 < len(args) && relativeDate.MatchString(args[i+1]) {
			out := slices.Clone(args)
			out[i+1] = anchor + " " + args[i+1]
			return out
		}
	}
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

// Which environment names a call records. A name is recorded only where the program the call
// starts reads it, so a golden pins what the launcher hands on and not what bash happens to
// leave exported: bash exports GH_TOKEN, CLAUDE_CODE_OAUTH_TOKEN and its CCY_* settings to
// every child (getenforce, curl, timedatectl, ...), which a port must not copy (Plan 00191
// Task 4.4). Recorded names are the ones set in the call's environment, whoever set them (the
// launcher or the scenario), less the runner's own (fakeConfig.Base) and bash's (bashNoise).
//
//   - wholeEnvTools hand their whole environment on: systemd-run --scope runs tmux as its own
//     child, and the tmux server it starts gives that environment to every session and pane,
//     where the relaunched launcher reads its CCY_* hand-over. Every name is recorded.
//   - passByNameTools (the engines and compose) see the environment only through the names a
//     call passes by name (-e NAME, --env NAME, --build-arg NAME): the container gets those,
//     and whether each was set decides whether it gets it. Those names alone are recorded.
//     The token setup (an engine run of claude setup-token) passes none, so records none.
//   - readEnvNames are the names a tool reads for itself: gh its token, the host's claude its
//     token, ssh and ssh-add their agent and askpass.
//
// Every other call records no environment.
var (
	wholeEnvTools   = []string{"systemd-run", "tmux"}
	passByNameTools = []string{"docker", "docker-compose", "podman", "podman-compose"}
	sshEnvNames     = []string{"DISPLAY", "SSH_ASKPASS", "SSH_ASKPASS_REQUIRE", "SSH_AUTH_SOCK"}
	readEnvNames    = map[string][]string{
		"claude":  {"ANTHROPIC_API_KEY", "CLAUDE_CODE_OAUTH_TOKEN"},
		"gh":      {"GH_HOST", "GH_TOKEN", "GITHUB_TOKEN"},
		"ssh":     sshEnvNames,
		"ssh-add": sshEnvNames,
	}
	// passFlags are the options whose value, when it has no "=", names a variable the engine
	// copies from its own environment.
	passFlags = []string{"-e", "--env", "--build-arg"}
)

// readsEnv reports whether tool, called with args, reads the variable name.
func readsEnv(tool string, args []string, name string) bool {
	switch {
	case slices.Contains(wholeEnvTools, tool):
		return true
	case slices.Contains(passByNameTools, tool):
		return slices.Contains(passedByName(args), name)
	default:
		return slices.Contains(readEnvNames[tool], name)
	}
}

// passedByName are the variables an engine call passes by name: "-e NAME", "--env NAME",
// "--env=NAME", and the same for --build-arg; "-e NAME=value" carries its own value.
func passedByName(args []string) []string {
	var names []string
	for i, a := range args {
		for _, f := range passFlags {
			value, ok := strings.CutPrefix(a, f+"=")
			if a == f && i+1 < len(args) {
				value, ok = args[i+1], true
			}
			if ok && value != "" && !strings.Contains(value, "=") {
				names = append(names, value)
			}
		}
	}
	return names
}

func recordCall(sandbox string, cfg fakeConfig, tool string, args []string) error {
	var names []string
	for _, kv := range os.Environ() {
		name := envName(kv)
		if slices.Contains(cfg.Base, name) || slices.Contains(bashNoise, name) || strings.HasPrefix(name, "BASH_FUNC_") {
			continue
		}
		if readsEnv(tool, args, name) {
			names = append(names, name)
		}
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

func writeArgFiles(sandbox string, r rule, args []string) error {
	for _, opt := range slices.Sorted(maps.Keys(r.ArgFiles)) {
		i := slices.Index(args, opt)
		if i < 0 || i+1 >= len(args) {
			return fmt.Errorf("the call has no %s PATH to write", opt)
		}
		rel, err := filepath.Rel(sandbox, args[i+1])
		if err != nil || !filepath.IsLocal(rel) {
			return fmt.Errorf("%s %s is outside the sandbox", opt, args[i+1])
		}
		if err := writeIn(sandbox, rel, []byte(r.ArgFiles[opt])); err != nil {
			return err
		}
	}
	return nil
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
