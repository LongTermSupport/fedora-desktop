// Package wsitoken is wsi-claude-token: the one place speech-to-text chooses the Claude
// account its post-processing runs as (Plan 00208 Phase 4). wsi-claude-process runs it and
// exports the token file it names, so every recorder and menu item chooses the same way.
//
// What it does, numbered as its tests are:
//
//   - K1. The `claude-token-auto` setting is read through `<dir>/wsi-setting` (dir: this
//     binary's). Anything but `true` or `false`, or a failed read: `cannot read the
//     claude-token-auto setting (re-run play-speech-to-text.yml)`, exit 1.
//   - K2. Off: the `claude-token` setting names the token. Empty is the desktop login: nothing
//     on stdout, exit 0.
//   - K3. Off, a name: the newest file NAME.YYYY-MM-DD.token in ~/.claude-tokens/ccy/tokens
//     dated today or later; its path on stdout. None: `no unexpired token named 'NAME' in
//     DIR (...)`, exit 1. A name of anything but letters, digits, `.`, `_`, `-` is refused.
//   - K4. The chosen file must hold a Claude Code OAuth token (`sk-ant-oat01-`, 90-120
//     characters), else `the token file F is not a Claude Code OAuth token`, exit 1. The
//     value is never printed.
//   - K5. On: every token's usage is read with `<ccy> --token-usage --json` (Plan 00101: one
//     small Haiku request per token unless read in the last 15 minutes). Exit 0 or 1 with a
//     schema 1 document are figures; anything else: `cannot read the tokens' usage (...: exit
//     N: <ccy's first stderr line>; re-run play-claude-yolo.yml)`, exit 1, the warning file
//     left as it was (a broken reader says nothing about the tokens). Past the 60 s deadline:
//     `... did not answer within 60 s (...; try again, or untick ...)`.
//   - K6. A token has room when its status is `ok`, at least one of its 5-hour and 7-day
//     percentages is reported, and each reported one is below 100; its room is 100 less the
//     larger. The most room wins; a tie goes to the lower 7-day figure, then the name. Its
//     file (a name with no `/`, ending `.token`) is checked by K4 and printed, exit 0, and
//     the warning file is removed.
//   - K7. No token has room: `no Claude token has allowance left: ` and each token's state
//     (`NAME: 5-hour 12%, 7-day 100% (resets Sun 18 Oct 09:00)`, local time, or `NAME:
//     unavailable (<ccy's reason>)`), or ccy's error when it listed none; exit 1. The same,
//     a token a line, goes to ~/.local/share/speech-to-text/claude-tokens-exhausted (0600),
//     the panel's warning until it is cleared there or K6 removes it.
//   - K8. Any argument is a usage error (exit 64, nothing run); -h/--help prints the usage.
//
// stdout is only the token file's path. The choice is one line on stderr (`using token
// 'NAME', ...`) that wsi-claude-process logs; every other message is `wsi-claude-token: ...`.
package wsitoken

import (
	"bytes"
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/fsx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

const (
	usage        = "usage: wsi-claude-token   (prints the Claude token file post-processing runs as)\n"
	tool         = "wsi-claude-token"
	tokenDir     = ".claude-tokens/ccy/tokens"
	warningFile  = ".local/share/speech-to-text/claude-tokens-exhausted"
	usageTimeout = 60 * time.Second // ccy gives each fetch 20 s; claude gets the rest of 170 s
	full         = 100.0
	oatPrefix    = "sk-ant-oat01-"
	minTokenLen  = 90
	maxTokenLen  = 120
)

var (
	tokenName = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)
	datedFile = regexp.MustCompile(`^(.+)\.([0-9]{4}-[0-9]{2}-[0-9]{2})\.token$`)
)

// Env is everything a run touches outside its own memory.
type Env struct {
	Runner execx.Runner
	Stdout io.Writer
	Stderr io.Writer
	Home   string
	// BinDir holds wsi-setting, beside this binary.
	BinDir string
	// CCY is ccy's launcher, which reads every token's usage.
	CCY string
	// Now is the clock; reset times are shown in its location.
	Now func() time.Time
	// UsageTimeout bounds the usage read; zero is the production 60 s.
	UsageTimeout time.Duration
}

// DefaultCCY is where play-claude-yolo.yml installs ccy's launcher.
const DefaultCCY = "/var/local/claude-yolo/claude-yolo"

// NewEnv is the production environment.
func NewEnv() (Env, error) {
	home := os.Getenv("HOME")
	if home == "" {
		return Env{}, errors.New("HOME is not set")
	}
	self, err := os.Executable()
	if err != nil {
		return Env{}, fmt.Errorf("where is this binary: %w", err)
	}
	return Env{
		Runner: execx.OS{}, Stdout: os.Stdout, Stderr: os.Stderr, Home: home, BinDir: filepath.Dir(self),
		CCY: DefaultCCY, Now: time.Now,
	}, nil
}

// App is one wsi-claude-token invocation.
type App struct {
	Env Env
}

// errFailed is a failure already reported.
var errFailed = errors.New("failed")

// Run is K1-K8.
func (a App) Run(ctx context.Context, args []string) term.ExitCode {
	if len(args) > 0 {
		if args[0] == "-h" || args[0] == "--help" {
			return a.write(a.Env.Stdout, usage, term.ExitOK)
		}
		a.say("unknown argument: " + args[0])
		return a.write(a.Env.Stderr, usage, term.ExitUsage)
	}
	path, err := a.choose(ctx)
	if err != nil {
		return term.ExitFailure
	}
	if path == "" {
		return term.ExitOK
	}
	return a.write(a.Env.Stdout, path+"\n", term.ExitOK)
}

func (a App) choose(ctx context.Context) (string, error) {
	auto, err := a.setting(ctx, "claude-token-auto")
	if err != nil || (auto != "true" && auto != "false") {
		a.say("cannot read the claude-token-auto setting (re-run play-speech-to-text.yml)")
		return "", errFailed
	}
	if auto == "true" {
		return a.mostRoom(ctx)
	}
	return a.named(ctx)
}

// named is K2-K4.
func (a App) named(ctx context.Context) (string, error) {
	name, err := a.setting(ctx, "claude-token")
	switch {
	case err != nil:
		a.say("cannot read the claude-token setting (re-run play-speech-to-text.yml)")
		return "", errFailed
	case name == "":
		a.info("using the desktop login (claude-token is empty)")
		return "", nil
	case !tokenName.MatchString(name):
		a.say(fmt.Sprintf("the claude-token setting '%s' is not a token name (Settings → Claude token)", name))
		return "", errFailed
	}
	dir := filepath.Join(a.Env.Home, tokenDir)
	entries, err := os.ReadDir(dir)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		a.say("cannot list " + dir + ": " + err.Error())
		return "", errFailed
	}
	today := a.Env.Now().Format(time.DateOnly)
	newest := ""
	for _, e := range entries {
		m := datedFile.FindStringSubmatch(e.Name())
		if m == nil || m[1] != name || m[2] < today || e.IsDir() {
			continue
		}
		newest = max(newest, e.Name())
	}
	if newest == "" {
		a.say(fmt.Sprintf("no unexpired token named '%s' in %s (Settings → Claude token; make one with ccy --create-token)", name, dir))
		return "", errFailed
	}
	path := filepath.Join(dir, newest)
	if err := a.check(path); err != nil {
		return "", err
	}
	a.info(fmt.Sprintf("using token '%s' (%s)", name, newest))
	return path, nil
}

// check is K4.
func (a App) check(path string) error {
	data, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		a.say("cannot read the token file " + path + ": " + err.Error())
		return errFailed
	}
	value := strings.TrimRight(string(data), "\n")
	if !strings.HasPrefix(value, oatPrefix) || len(value) < minTokenLen || len(value) > maxTokenLen {
		a.say("the token file " + path + " is not a Claude Code OAuth token")
		return errFailed
	}
	return nil
}

// mostRoom is K5-K7.
func (a App) mostRoom(ctx context.Context) (string, error) {
	rep, err := a.readUsage(ctx)
	if timeout, ok := errors.AsType[*timedOutError](err); ok {
		a.say(fmt.Sprintf("cannot read the tokens' usage: %s --token-usage --json did not answer within %s "+
			"(the API or the network is slow; try again, or untick Use the token with the most allowance left)",
			a.Env.CCY, timeout.spoken()))
		return "", errFailed
	}
	if err != nil {
		a.say(fmt.Sprintf("cannot read the tokens' usage (%s --token-usage --json: %v; re-run play-claude-yolo.yml)", a.Env.CCY, err))
		return "", errFailed
	}
	var roomy []entry
	for _, e := range rep.Tokens {
		if hasRoom(e) {
			roomy = append(roomy, e)
		}
	}
	if len(roomy) == 0 {
		return "", a.exhausted(rep)
	}
	slices.SortFunc(roomy, func(x, y entry) int {
		return cmp.Or(cmp.Compare(used(x), used(y)), cmp.Compare(orZero(x.SevenDay), orZero(y.SevenDay)), cmp.Compare(x.Name, y.Name))
	})
	best := roomy[0]
	if strings.Contains(best.File, "/") || !strings.HasSuffix(best.File, ".token") {
		a.say(fmt.Sprintf("ccy --token-usage named the file '%s' for token '%s', which is not a token file name", best.File, best.Name))
		return "", errFailed
	}
	path := filepath.Join(a.Env.Home, tokenDir, best.File)
	if err := a.check(path); err != nil {
		return "", err
	}
	if err := fsx.RemoveIfPresent(filepath.Join(a.Env.Home, warningFile)); err != nil {
		a.say("cannot clear the panel's token warning: " + err.Error())
		return "", errFailed
	}
	a.info(fmt.Sprintf("using token '%s', the most allowance left (%s, %s)", best.Name,
		bucket("5-hour", best.FiveHour, nil, a.Env.Now().Location()), bucket("7-day", best.SevenDay, nil, a.Env.Now().Location())))
	return path, nil
}

func (a App) readUsage(ctx context.Context) (report, error) {
	limit := cmp.Or(a.Env.UsageTimeout, usageTimeout)
	ctx, cancel := context.WithTimeout(ctx, limit)
	defer cancel()
	var stdout bytes.Buffer
	res, err := a.Env.Runner.Run(ctx, execx.Cmd{
		Name: a.Env.CCY, Args: []string{"--token-usage", "--json"}, Stdout: &stdout,
	})
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return report{}, &timedOutError{limit: limit} // ccy was killed: its exit says nothing
	}
	exit, isExit := errors.AsType[*execx.ExitError](err)
	if isExit && exit.Code == 1 {
		err = nil // no token's usage was read: still a document, and K7's to say why
	}
	if err != nil {
		reason := err.Error()
		if isExit {
			reason = fmt.Sprintf("exit %d", exit.Code)
		}
		// ccy's own first line says why; the rest would not fit one notification
		if line := firstLine(string(res.Stderr)); line != "" {
			reason += ": " + line
		}
		return report{}, errors.New(reason)
	}
	var rep report
	if err := json.Unmarshal(stdout.Bytes(), &rep); err != nil {
		return report{}, fmt.Errorf("not the JSON report: %w", err)
	}
	if rep.Schema == nil || *rep.Schema != 1 {
		return report{}, errors.New("not a schema 1 report")
	}
	return rep, nil
}

// timedOutError is a usage read the deadline ended.
type timedOutError struct {
	limit time.Duration
}

func (e *timedOutError) Error() string { return "no answer within " + e.spoken() }

// spoken is the limit as a person reads it: "60 s", or Go's form below a second.
func (e *timedOutError) spoken() string {
	if e.limit >= time.Second {
		return fmt.Sprintf("%d s", int(e.limit.Seconds()))
	}
	return e.limit.String()
}

// firstLine is the first line of text that is not blank, trimmed; "" when there is none.
func firstLine(text string) string {
	for line := range strings.Lines(text) {
		if trimmed := strings.TrimSpace(line); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

// exhausted is K7.
func (a App) exhausted(rep report) error {
	var states []string
	for _, e := range rep.Tokens {
		states = append(states, e.Name+": "+a.state(e))
	}
	if len(states) == 0 {
		why := "ccy --token-usage listed no token"
		if rep.Error != nil {
			why = *rep.Error
		}
		states = append(states, why)
	}
	a.say("no Claude token has allowance left: " + strings.Join(states, "; "))
	text := "No Claude token has allowance left\n" + strings.Join(states, "\n") + "\n"
	path := filepath.Join(a.Env.Home, warningFile)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		a.say("cannot raise the panel's token warning: " + err.Error())
		return errFailed
	}
	if err := fsx.WriteAtomic(path, "token warning", []byte(text), 0o600); err != nil {
		a.say("cannot raise the panel's token warning: " + err.Error())
	}
	return errFailed
}

func (a App) state(e entry) string {
	if e.Status != "ok" {
		reason := "no reason given"
		if e.Reason != nil {
			reason = *e.Reason
		}
		return "unavailable (" + reason + ")"
	}
	return bucket("5-hour", e.FiveHour, e.Resets.FiveHour, a.Env.Now().Location()) + ", " +
		bucket("7-day", e.SevenDay, e.Resets.SevenDay, a.Env.Now().Location())
}

// bucket is "7-day 100% (resets Sun 18 Oct 09:00)": the reset only for a bucket at its limit.
func bucket(label string, pct *float64, reset *string, loc *time.Location) string {
	if pct == nil {
		return label + " not reported"
	}
	text := label + " " + strconv.FormatFloat(*pct, 'f', -1, 64) + "%"
	if *pct < full || reset == nil {
		return text
	}
	at, err := time.Parse(time.RFC3339, *reset)
	if err != nil {
		return text + " (resets " + *reset + ")"
	}
	return text + " (resets " + at.In(loc).Format("Mon 2 Jan 15:04") + ")"
}

func hasRoom(e entry) bool {
	if e.Status != "ok" || (e.FiveHour == nil && e.SevenDay == nil) {
		return false
	}
	return used(e) < full
}

// used is the larger reported percentage.
func used(e entry) float64 {
	return max(orZero(e.FiveHour), orZero(e.SevenDay))
}

func orZero(p *float64) float64 {
	if p == nil {
		return 0
	}
	return *p
}

func (a App) setting(ctx context.Context, key string) (string, error) {
	var stdout bytes.Buffer
	_, err := a.Env.Runner.Run(ctx, execx.Cmd{
		Name: filepath.Join(a.Env.BinDir, "wsi-setting"), Args: []string{key}, Stdout: &stdout, Stderr: a.Env.Stderr,
	})
	return strings.TrimRight(stdout.String(), "\n"), err
}

func (a App) write(w io.Writer, text string, code term.ExitCode) term.ExitCode {
	if _, err := io.WriteString(w, text); err != nil {
		return term.ExitFailure
	}
	return code
}

// info is the one line saying what was chosen: wsi-claude-process logs it.
func (a App) info(message string) {
	if _, err := fmt.Fprintln(a.Env.Stderr, message); err != nil {
		return // stderr itself is gone: the choice still stands, and stdout carries it
	}
}

func (a App) say(message string) {
	if _, err := fmt.Fprintf(a.Env.Stderr, "%s: %s\n", tool, message); err != nil {
		return // stderr itself is gone: nowhere left to say it, and the exit code still does
	}
}
