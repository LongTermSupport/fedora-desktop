// Package ccylaunch is the Go front of the ccy launcher (Plan 00191 Task 4.5, phase 2 of
// ASSESS-ccy-launcher.md; decisions D1 to D3). The play installs it as
// /var/local/claude-yolo/claude-yolo and the bash launcher beside it as claude-yolo.bash, with
// lib/ as before. The front answers a few flags itself and hands every other run to the bash
// launcher, which is the strangler: the bash keeps working and is one exec away for the whole
// port.
//
// Intended behaviour, each held by a test:
//
//   - F1. `--version` or `-v` as the first argument prints `ccy version <V>` and
//     `Hash: <H>`: V is the version the build was given (-X main.Version, from the bash
//     launcher's CCY_VERSION), H the bash launcher's integrity hash (IntegrityHash), so a Quick
//     Launch file the bash wrote holds the hash the front prints. Further arguments are ignored,
//     as the bash ignored them.
//   - F2. Every other run is the bash launcher's: claude-yolo.bash in the front's own directory
//     is exec'd (no shell) with the arguments and the environment as given, plus
//     CCY_LAUNCHER_FRONT naming the front's own path (Self, os.Executable). The bash launcher
//     re-runs and records that path (a tmux pane, a restart, a session record), so a restore
//     runs the front; it unsets the variable before it starts anything, and run directly it
//     names itself.
//   - F3. CCY_LEGACY set to anything but empty or 0 sends every run to the bash launcher,
//     the handled flags too: the way back while the port runs (assessment section 10).
//   - F4. The front answers a flag only where the bash launcher would reach its own answer the
//     way the front models it; anywhere else it hands over, and the bash prints what it prints
//     (an error, as a rule). So it hands over when: CCY_GH_TOKEN_HANDOFF is set (the bash takes
//     that file before anything); BASH_ENV, ENV, SHELLOPTS, BASHOPTS, POSIXLY_CORRECT or
//     GREP_OPTIONS is set, or a function is exported (BASH_FUNC_*), any of which changes what
//     the bash runs; CCY_CONTAINER_ENGINE names neither podman nor docker, or the engine or a
//     tool the bash runs first is not on PATH; the bash launcher or a library it loads
//     (CCY_LIBS, read from the launcher, common-pure and launcher-path) is missing or unreadable, its text
//     holds a NUL or is not UTF-8, or it has no CCY_VERSION or CCY_LIBS line; lib/ holds no
//     .bash file; or an override of lib/host-paths.bash is one ccy_host_dir refuses.
//   - F5. Where the bash would answer, the front does: either engine, an empty variable, an
//     override the library accepts.
//   - F6. A front whose version is not the bash launcher's CCY_VERSION is half of two
//     checkouts: the handled flags refuse (exit 1), naming both versions, the launcher's path and
//     the play that installs both; so does a front built without its version.
//   - F7. A hand-over that cannot exec (the bash launcher missing, not executable) fails with
//     exit 1, naming the path, the error and CCY_LEGACY; it never does nothing.
//   - F8. `--help` or `-h` prints the bash launcher's help text (help.txt, held to the
//     launcher's by a test), then asks the engine for the image (engine E10): absent, how to
//     build it; present, claude --help from the image, or the engine's own words when that
//     fails ("no output" when it said none). Exit 0, as the bash.
//   - F9. Output that could not be written is exit 1.
//   - F10. `--list-tokens` as the only argument prints the token pool as the bash's list_tokens
//     does (tokens.List), expiry coloured whatever the output is, as the bash colours it; exit 1
//     for an empty pool. It hands over wherever the bash could read the pool otherwise
//     (poolWhy: an unclean path, a zone Go reads otherwise, an entry that is not a regular
//     file, a collation that could sort it otherwise, a name the bash dates otherwise (T1), a
//     day the clocks change), and, for this flag and every later one, to a bash of another
//     version (F6 refuses only --version and --help).
//
// The front tells the time by its Clock: the machine's (SystemClock) in the installed program;
// the differential harness's pinned clock only in a build with the difftest tag.
package ccylaunch

import (
	"bytes"
	"context"
	_ "embed" // the help text
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/engine"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

// BashName is the bash launcher's name beside the front.
const BashName = "claude-yolo.bash"

// LegacyEnv is the variable that sends every run to the bash launcher (F3).
const LegacyEnv = "CCY_LEGACY"

// FrontEnv is the variable a hand-over names the front in (F2): the bash launcher re-runs and
// records that path, and unsets the variable before it starts anything.
const FrontEnv = "CCY_LAUNCHER_FRONT"

const (
	imageName    = "claude-yolo:latest"
	handoffEnv   = "CCY_GH_TOKEN_HANDOFF"
	funcEnvStart = "BASH_FUNC_"
)

//go:embed help.txt
var helpText string

// shellEnv are the variables that change what bash or the tools it runs first do.
var shellEnv = []string{"BASH_ENV", "ENV", "SHELLOPTS", "BASHOPTS", "POSIXLY_CORRECT", "GREP_OPTIONS"}

// bashTools are the programs the bash launcher runs before it answers --version or --help:
// dirname for its own directory, and the hash and library count. realpath is added when an
// override is set.
var bashTools = []string{"dirname", "find", "grep", "sort", "xargs", "cat", "md5sum", "cut", "wc"}

var (
	versionLine = regexp.MustCompile(`(?m)^CCY_VERSION="([^"]+)"`)
	libsLine    = regexp.MustCompile(`(?m)^CCY_LIBS=\(([a-z0-9 -]+)\)$`)
)

// extraLibs are loaded by a library rather than by the launcher: common.bash sources
// common-pure.bash, and token-handoff.bash sources launcher-path.bash.
var extraLibs = []string{"common-pure", "launcher-path"}

// Front is one run of the claude-yolo program.
type Front struct {
	Version  string   // the version the build was given, -X main.Version
	Self     string   // this program's path: the bash launcher is beside it
	Args     []string // the arguments after the program's name
	Environ  []string // the environment, KEY=VALUE, handed to the bash as it is
	LookPath func(string) (string, error)
	Runner   execx.Runner
	Execer   execx.Execer
	Stdout   io.Writer
	Stderr   io.Writer
	Clock    Clock  // the time it answers by; the system's when nil
	Dir      string // the directory the run started in; "" is this process's
}

// launcher is what the front read of the bash launcher beside it.
type launcher struct {
	version string
	hash    string
}

// Run answers the run or hands it to the bash launcher.
func (f Front) Run(ctx context.Context) term.ExitCode {
	bash := filepath.Join(filepath.Dir(f.Self), BashName)
	kind, l, flag, why := f.plan(bash)
	if why != "" {
		return f.handOver(bash)
	}
	if flag != flagVersion && flag != flagHelp {
		// F6 for the other flags: the bash beside a front of another version is the one that
		// knows its own answer.
		if f.Version == "" || l.version != f.Version {
			return f.handOver(bash)
		}
		code, why := f.answer(flag)
		if why != "" {
			return f.handOver(bash)
		}
		return code
	}
	if f.Version == "" {
		return f.fail("ccy: this claude-yolo was built without its version (-X main.Version); re-run play-claude-yolo.yml, or set " + LegacyEnv + "=1 to run the bash launcher")
	}
	if l.version != f.Version {
		return f.fail(fmt.Sprintf("ccy: this claude-yolo is version %s but the bash launcher beside it, %s, is %s: they come from different checkouts. Re-run play-claude-yolo.yml, or set %s=1 to run the bash launcher.",
			f.Version, bash, l.version, LegacyEnv))
	}
	var p term.Printer
	if f.Args[0] == "--version" || f.Args[0] == "-v" {
		p.Print(f.Stdout, "ccy version "+f.Version+"\nHash: "+l.hash+"\n")
	} else {
		f.help(ctx, &p, kind)
	}
	if err := p.Err(); err != nil {
		return f.fail("ccy: could not write the answer: " + err.Error())
	}
	return term.ExitOK
}

// flagKind is the run the front answers itself, or flagNone.
type flagKind int

const (
	flagNone flagKind = iota
	flagVersion
	flagHelp
	flagListTokens
)

// flagOf is the run the arguments ask for, among those the front answers.
func flagOf(args []string) flagKind {
	if len(args) == 0 {
		return flagNone
	}
	switch args[0] {
	case "--version", "-v":
		return flagVersion
	case "--help", "-h":
		return flagHelp
	case "--list-tokens":
		if len(args) == 1 {
			return flagListTokens
		}
	}
	return flagNone
}

// flagTools are the programs the bash launcher runs to answer the flag, beyond bashTools.
var flagTools = map[flagKind][]string{
	flagListTokens: {"date", "ls", "basename"},
}

// answer runs a flag the front answers past --version and --help: its exit code, or why the
// bash should answer it instead, decided before anything is written.
func (f Front) answer(flag flagKind) (term.ExitCode, string) {
	switch flag {
	case flagListTokens:
		return f.listTokens()
	case flagNone, flagVersion, flagHelp:
	}
	return term.ExitFailure, "not a flag the front answers"
}

// plan is the engine and the launcher the front answers with, or why it hands over instead.
func (f Front) plan(bash string) (engine.Kind, launcher, flagKind, string) {
	if v := f.getenv(LegacyEnv); v != "" && v != "0" {
		return "", launcher{}, flagNone, LegacyEnv
	}
	flag := flagOf(f.Args)
	if flag == flagNone {
		return "", launcher{}, flagNone, "not a flag the front answers"
	}
	if why := f.shellChanged(); why != "" {
		return "", launcher{}, flag, why
	}
	kind, why := f.engine()
	if why == "" {
		why = f.toolsMissing(flag)
	}
	if why != "" {
		return "", launcher{}, flag, why
	}
	for _, d := range hostDirs {
		if !hostDirOK(f.getenv(d.env), d.def, d.absentOK) {
			return "", launcher{}, flag, d.env + " is refused by the library"
		}
	}
	l, why := readLauncher(bash)
	return kind, l, flag, why
}

// shellChanged names what would make the bash run other than as modelled.
func (f Front) shellChanged() string {
	if f.getenv(handoffEnv) != "" {
		return handoffEnv
	}
	for _, name := range shellEnv {
		if f.getenv(name) != "" {
			return name
		}
	}
	for _, kv := range f.Environ {
		if strings.HasPrefix(kv, funcEnvStart) {
			return "an exported function"
		}
	}
	return ""
}

// engine is the engine common.bash would choose and find.
func (f Front) engine() (engine.Kind, string) {
	kind := engine.Podman
	if v := f.getenv(engine.EnvName); v != "" {
		kind = engine.Kind(v)
	}
	if kind != engine.Podman && kind != engine.Docker {
		return "", engine.EnvName + " names no engine ccy drives"
	}
	if _, err := f.LookPath(string(kind)); err != nil {
		return "", string(kind) + " is not on PATH"
	}
	return kind, ""
}

func (f Front) toolsMissing(flag flagKind) string {
	tools := slices.Concat(bashTools, flagTools[flag])
	if slices.ContainsFunc(hostDirs, func(d hostDir) bool { return f.getenv(d.env) != "" }) {
		tools = append(slices.Clone(tools), "realpath")
	}
	for _, tool := range tools {
		if _, err := f.LookPath(tool); err != nil {
			return tool + " is not on PATH"
		}
	}
	return ""
}

// readLauncher reads the bash launcher's version and hash, or why it cannot be modelled.
func readLauncher(bash string) (launcher, string) {
	text, err := os.ReadFile(bash)
	if err != nil {
		return launcher{}, err.Error()
	}
	if !utf8.Valid(text) || bytes.IndexByte(text, 0) >= 0 {
		return launcher{}, "the launcher's text is not plain UTF-8"
	}
	version := versionLine.FindSubmatch(text)
	libs := libsLine.FindSubmatch(text)
	if version == nil || libs == nil {
		return launcher{}, "the launcher has no CCY_VERSION or CCY_LIBS line"
	}
	libDir := filepath.Join(filepath.Dir(bash), "lib")
	for _, lib := range slices.Concat(strings.Fields(string(libs[1])), extraLibs) {
		if info, statErr := os.Stat(filepath.Join(libDir, lib+".bash")); statErr != nil || !info.Mode().IsRegular() {
			return launcher{}, "library " + lib + " is missing"
		}
	}
	hash, count, err := IntegrityHash(text, libDir)
	if err != nil {
		return launcher{}, err.Error()
	}
	if count == 0 {
		return launcher{}, "lib/ holds no library"
	}
	return launcher{version: string(version[1]), hash: hash}, ""
}

// help is F8.
func (f Front) help(ctx context.Context, p *term.Printer, kind engine.Kind) {
	p.Print(f.Stdout, helpText)
	eng, err := engine.New(kind, f.Runner)
	if err == nil {
		err = eng.Inspect(ctx, imageName)
	}
	if err != nil {
		p.Print(f.Stdout, helpRule+"Claude Code Help:\n"+helpRule+"\n"+
			"Container image not built yet. Run 'ccy --rebuild' first to build the container,\n"+
			"then run 'ccy --help' again to see Claude Code options.\n")
		return
	}
	p.Print(f.Stdout, helpRule+"Claude Code Help (from container):\n"+helpRule+"\n")
	out, err := eng.ClaudeHelp(ctx, imageName)
	// As a shell's $(...) gives it: NUL bytes dropped, trailing newlines removed.
	out = strings.TrimRight(strings.ReplaceAll(out, "\x00", ""), "\n")
	if err == nil {
		p.Print(f.Stdout, out+"\n")
		return
	}
	if out == "" {
		out = "no output"
	}
	p.Print(f.Stdout, "Note: the image exists, but 'claude --help' would not run inside it.\n  "+string(kind)+" said: "+out+"\n")
}

const helpRule = "════════════════════════════════════════════════════════════════════════════════\n"

// handOver is F2 and F7: the bash launcher, in place of this process.
func (f Front) handOver(bash string) term.ExitCode {
	err := f.Execer.Exec(execx.Cmd{Name: bash, Args: f.Args, Env: []string{FrontEnv + "=" + f.Self}})
	if err == nil {
		// Only a test's Execer returns from a successful exec.
		return term.ExitOK
	}
	return f.fail(fmt.Sprintf("ccy: cannot run the bash launcher %s: %v. Re-run play-claude-yolo.yml to install it (%s=1 runs it too, once it is there).", bash, err, LegacyEnv))
}

func (f Front) fail(msg string) term.ExitCode {
	var p term.Printer
	p.Print(f.Stderr, msg+"\n")
	return term.ExitFailure
}

// getenv is the first value of name in the environment, as getenv(3) reads it.
func (f Front) getenv(name string) string {
	for _, kv := range f.Environ {
		if value, ok := strings.CutPrefix(kv, name+"="); ok {
			return value
		}
	}
	return ""
}
