// Package statusline is `pingbus status-line` (Plan 00213, SPEC-status-line.md): the session's
// Claude Code status line, with the agent-bus teams the session is on and this member's role
// in each. It is the Go port of helpers/pingbus/statusline.py, which stays the reference for
// every behaviour the spec states.
//
// Run never turns a failure into an exit status (S3). Its only non-zero status is a cancel
// (S12): 128 plus the signal, with nothing on stdout.
package statusline

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"time"
	"unicode/utf8"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
)

const (
	innerVar       = "PINGBUS_STATUS_LINE_INNER"
	sessionsDirVar = "PINGBUS_SESSIONS_DIR"
	inputMaxBytes  = 1 << 20
	maxRecord      = 4096
	innerTimeout   = 10 * time.Second
	// timedOut is the status of an inner command killed at its timeout (Python's TIMED_OUT).
	timedOut    = -1
	label       = "👥"
	roleUnknown = "role?"
	noRecord    = "no record of this session's teams (pingbus status)"
	cyan        = "\x1b[36m"
	red         = "\x1b[31m"
	reset       = "\x1b[0m"
)

// sessionIDPattern is what a session id must match to name a record (S18).
var sessionIDPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,128}$`)

// Input is one render: the environment the status line sees, its working directory, and the
// session JSON Claude Code wrote on stdin.
type Input struct {
	Env   map[string]string
	Cwd   string
	Stdin []byte
}

// cancelled is the error a render ends with when a signal cancelled it.
type cancelled syscall.Signal

func (c cancelled) Error() string { return "cancelled by " + syscall.Signal(c).String() }

// errInnerTimeout is the cause of the inner command's own deadline, so it is told apart from a
// cancel of the whole render.
var errInnerTimeout = errors.New("the inner status line timed out")

// Run renders the status line. It returns the stdout bytes (the line and one final newline)
// and the exit status. A status other than 0 means the render was cancelled: the bytes are nil,
// and the caller prints nothing. Diagnostics go to stderr; the inner command's own stderr is
// written there too.
func Run(ctx context.Context, in Input, stderr io.Writer) ([]byte, int) {
	text, err := render(ctx, in, stderr)
	var c cancelled
	if errors.As(err, &c) {
		return nil, 128 + int(c)
	}
	return []byte(text + "\n"), 0
}

// render is the status line text without its final newline. It fails only when cancelled.
func render(ctx context.Context, in Input, stderr io.Writer) (string, error) {
	payload := parsePayload(in.Stdin)
	inner := ""
	if in.Env[innerVar] == "" {
		var err error
		inner, err = innerText(ctx, in, payload, stderr)
		if err != nil {
			return "", err
		}
	}
	segment, err := segmentText(ctx, in, payload, stderr)
	if err != nil {
		return "", err
	}
	switch {
	case segment == "":
		return inner, nil
	case inner == "":
		return segment, nil
	}
	return inner + "\n" + segment, nil
}

// Cancel is the cause a caller cancels the render's context with when a signal arrives, so
// that Run ends with 128 plus that signal.
func Cancel(sig syscall.Signal) error { return cancelled(sig) }

// cancelOf reports the cancel a context carries, if any.
func cancelOf(ctx context.Context) error {
	if ctx.Err() == nil {
		return nil
	}
	if c, ok := context.Cause(ctx).(cancelled); ok {
		return c
	}
	return ctx.Err()
}

// parsePayload is the session JSON as an object. Anything else reads as an empty object (S18).
func parsePayload(raw []byte) map[string]any {
	if len(raw) > inputMaxBytes || !utf8.Valid(raw) {
		return map[string]any{}
	}
	data, err := decodeJSON(raw)
	if err != nil {
		return map[string]any{}
	}
	if object, ok := data.(map[string]any); ok {
		return object
	}
	return map[string]any{}
}

// projectDir is the first absolute path among the payload's workspace.project_dir, the
// CLAUDE_PROJECT_DIR variable and the payload's cwd (S6).
func projectDir(payload map[string]any, env map[string]string) (string, bool) {
	var candidates []any
	if workspace, ok := payload["workspace"].(map[string]any); ok {
		candidates = append(candidates, workspace["project_dir"])
	}
	candidates = append(candidates, env["CLAUDE_PROJECT_DIR"], payload["cwd"])
	for _, value := range candidates {
		if s, ok := value.(string); ok && strings.HasPrefix(s, "/") {
			return s, true
		}
	}
	return "", false
}

// innerText is the inner status line's text, with a red note when it could not be had cleanly.
func innerText(ctx context.Context, in Input, payload map[string]any, stderr io.Writer) (string, error) {
	project, hasProject := projectDir(payload, in.Env)
	line, err := innerCommand(project, hasProject, in.Env)
	if err != nil {
		var se *settingsError
		if errors.As(err, &se) {
			return red + "status line: " + se.Error() + reset, nil
		}
		return "", err
	}
	if line == "" {
		return "", nil
	}
	dir := in.Cwd
	if hasProject && isDir(project) {
		dir = project
	}
	text, status, err := runInner(ctx, line, in, dir, stderr)
	if err != nil {
		return "", err
	}
	text = strings.TrimRight(text, "\n")
	if status == 0 {
		return text, nil
	}
	note := "(status line exit " + strconv.Itoa(status) + ")"
	if status == timedOut {
		note = "(status line timed out after 10 s)"
	}
	if text == "" {
		return red + note + reset, nil
	}
	return text + " " + red + note + reset, nil
}

// runInner runs the inner command as `/bin/sh -c`, in its own session, with the payload on
// stdin and this process's environment plus the loop guard (S9 to S14). Its stdout is decoded
// as UTF-8 with replacement; its stderr goes to stderr as it is written.
func runInner(ctx context.Context, line string, in Input, dir string, stderr io.Writer) (string, int, error) {
	if err := cancelOf(ctx); err != nil {
		return "", 0, err
	}
	env := make(map[string]string, len(in.Env)+1)
	for k, v := range in.Env {
		env[k] = v
	}
	env[innerVar] = "1"
	inner, stop := context.WithTimeoutCause(ctx, innerTimeout, errInnerTimeout)
	defer stop()
	cmd := execx.Cmd{
		Name:       "/bin/sh",
		Args:       []string{"-c", line},
		Env:        envPairs(env),
		Unset:      unsetFor(env),
		Dir:        dir,
		Stdin:      bytes.NewReader(in.Stdin),
		Stderr:     stderr,
		NewSession: true,
	}
	res, err := (execx.OS{}).Run(inner, cmd)
	if cancelErr := cancelOf(ctx); cancelErr != nil {
		return "", 0, cancelErr
	}
	if errors.Is(context.Cause(inner), errInnerTimeout) {
		return "", timedOut, nil
	}
	if err != nil && !isExit(err) {
		return "", 0, err
	}
	return decodeLossy(res.Stdout), statusOf(res, err), nil
}

func isExit(err error) bool {
	var exitErr *execx.ExitError
	return errors.As(err, &exitErr)
}

// statusOf is the process's status as Python's returncode: its exit code, or the negative
// number of the signal that ended it.
func statusOf(res execx.Result, err error) int {
	var exitErr *execx.ExitError
	switch {
	case err == nil:
		return 0
	case errors.As(err, &exitErr) && exitErr.Signal != 0:
		return -int(exitErr.Signal)
	case errors.As(err, &exitErr):
		return exitErr.Code
	}
	return res.ExitCode
}

func envPairs(env map[string]string) []string {
	pairs := make([]string, 0, len(env))
	for k, v := range env {
		pairs = append(pairs, k+"="+v)
	}
	return pairs
}

// unsetFor names every variable of this process that the child must not inherit, so the
// child's environment is exactly env.
func unsetFor(env map[string]string) []string {
	var names []string
	for _, kv := range os.Environ() {
		name := strings.SplitN(kv, "=", 2)[0]
		if _, ok := env[name]; !ok {
			names = append(names, name)
		}
	}
	return names
}

// decodeLossy is Python's decode with errors="replace": each invalid byte becomes U+FFFD.
func decodeLossy(b []byte) string {
	var out strings.Builder
	out.Grow(len(b))
	for len(b) > 0 {
		r, size := utf8.DecodeRune(b)
		out.WriteRune(r)
		b = b[size:]
	}
	return out.String()
}

func isDir(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

// segmentText is the bus segment: the teams the session is on, with the roles, or a red note.
func segmentText(ctx context.Context, in Input, payload map[string]any, stderr io.Writer) (string, error) {
	if err := cancelOf(ctx); err != nil {
		return "", err
	}
	marker, has := in.Env[membershipVar]
	if has && marker != membershipOff && strings.HasPrefix(marker, "/") {
		found, ok := membershipTeams(in.Env)
		if !ok {
			return note(in.Env, noMembership), nil
		}
		if len(found) == 0 {
			return "", nil
		}
		return teamsText(ctx, in, found, stderr)
	}
	if teams, ok := sessionTeams(in.Env, sessionID(payload)); ok {
		return teamsText(ctx, in, teams, stderr)
	}
	if in.Env["PINGBUS_TEAMS"] != "" {
		return note(in.Env, noRecord), nil
	}
	return "", nil
}

// sessionID is the payload's session_id when it is a name a record can be filed under.
func sessionID(payload map[string]any) string {
	id, ok := payload["session_id"].(string)
	if !ok || !sessionIDPattern.MatchString(id) {
		return ""
	}
	return id
}

// teamsText is the teams, each with its role and marks, wrapped to COLUMNS (S26 to S29). A
// role diagnostic goes to stderr once, before the segment (S24).
func teamsText(ctx context.Context, in Input, teams []string, stderr io.Writer) (string, error) {
	roles, problem := readRoles(in.Env, teams)
	if problem != "" {
		if _, err := io.WriteString(stderr, problem+"\n"); err != nil {
			return "", err
		}
	}
	pieces := make([]string, 0, len(teams))
	for i, team := range teams {
		piece := teamPiece(in.Env, team, roles[team])
		if i < len(teams)-1 {
			piece += ","
		}
		pieces = append(pieces, piece)
	}
	if err := cancelOf(ctx); err != nil {
		return "", err
	}
	return wrapped(cyan, pieces, " ", terminalColumns(in.Env)), nil
}

// teamPiece is `team (role)`, then its marks: the one order every team is shown in.
func teamPiece(env map[string]string, team, role string) string {
	if role == "" {
		role = roleUnknown
	}
	marks := ""
	if isDown(env, team) {
		marks = " (down)"
	}
	return team + " (" + role + ")" + marks
}

func note(env map[string]string, text string) string {
	return wrapped(red, strings.Split(text, " "), " ", terminalColumns(env))
}

// wrapped is `label pieces` in rows no wider than columns (unbroken when columns is 0). A row
// breaks only between pieces, and a later row is indented under the first.
func wrapped(colour string, pieces []string, joiner string, columns int) string {
	indent := strings.Repeat(" ", displayWidth(label+" "))
	var rows []string
	current := label + " " + pieces[0]
	for _, piece := range pieces[1:] {
		candidate := current + joiner + piece
		if columns == 0 || displayWidth(candidate) <= columns {
			current = candidate
			continue
		}
		rows = append(rows, current)
		current = indent + piece
	}
	rows = append(rows, current)
	for i, row := range rows {
		rows[i] = colour + row + reset
	}
	return strings.Join(rows, "\n")
}
