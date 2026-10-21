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
	"maps"
	"os"
	"os/user"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"time"
	"unicode/utf8"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/pingbus/pyjson"
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
	notRun      = "(status line could not be run)"
	cyan        = "\x1b[36m"
	red         = "\x1b[31m"
	reset       = "\x1b[0m"
)

// sessionIDPattern is what a session id must match to name a record (S18).
var sessionIDPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,128}$`)

// Input is one render: the environment the status line sees, its working directory, the
// session JSON Claude Code wrote on stdin, the clock, and the passwd home of a uid. A nil Now
// is time.Now and a nil UserHome is the passwd database.
type Input struct {
	Env      map[string]string
	Cwd      string
	Stdin    []byte
	Now      func() time.Time
	UserHome func(uid int) (string, error)
}

// passwdHome is the passwd database's home of uid.
func passwdHome(uid int) (string, error) {
	u, err := user.LookupId(strconv.Itoa(uid))
	if err != nil {
		return "", err
	}
	return u.HomeDir, nil
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
	if in.Now == nil {
		in.Now = time.Now
	}
	if in.UserHome == nil {
		in.UserHome = passwdHome
	}
	text, err := render(ctx, in, stderr)
	if c, ok := errors.AsType[cancelled](err); ok {
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
	seg, err := segmentText(ctx, in, payload, stderr)
	if err != nil {
		return "", err
	}
	switch {
	case seg == "":
		return inner, nil
	case inner == "":
		return seg, nil
	}
	return inner + "\n" + seg, nil
}

// Cancel is the cause a caller cancels the render's context with when a signal arrives, so
// that Run ends with 128 plus that signal.
func Cancel(sig syscall.Signal) error { return cancelled(sig) }

// CancelStatus is the exit status of a render whose context was cancelled with Cancel: 128 plus
// the signal. It is 0 when the context carries no such cancel.
func CancelStatus(ctx context.Context) int {
	if c, ok := errors.AsType[cancelled](cancelOf(ctx)); ok {
		return 128 + int(c)
	}
	return 0
}

// cancelOf reports the cancel a context carries, if any.
func cancelOf(ctx context.Context) error {
	if ctx.Err() == nil {
		return nil
	}
	if c, ok := errors.AsType[cancelled](context.Cause(ctx)); ok {
		return c
	}
	return ctx.Err()
}

// parsePayload is the session JSON as an object. Anything else reads as an empty object (S18).
func parsePayload(raw []byte) pyjson.Object {
	if len(raw) > inputMaxBytes {
		return pyjson.Object{}
	}
	data, err := pyjson.DecodeUTF8(raw, pyjson.Options{})
	if err != nil {
		return pyjson.Object{}
	}
	if object, ok := data.(pyjson.Object); ok {
		return object
	}
	return pyjson.Object{}
}

// projectDir is the first absolute path among the payload's workspace.project_dir, the
// CLAUDE_PROJECT_DIR variable and the payload's cwd (S6).
func projectDir(payload pyjson.Object, env map[string]string) (string, bool) {
	var candidates []any
	if workspace, ok := payload["workspace"].(pyjson.Object); ok {
		candidates = append(candidates, workspace["project_dir"])
	}
	if value, ok := env["CLAUDE_PROJECT_DIR"]; ok {
		candidates = append(candidates, value)
	}
	candidates = append(candidates, payload["cwd"])
	for _, value := range candidates {
		if s, ok := value.(string); ok && strings.HasPrefix(s, "/") {
			return s, true
		}
	}
	return "", false
}

// innerText is the inner status line's text, with a red note when it could not be had cleanly.
func innerText(ctx context.Context, in Input, payload pyjson.Object, stderr io.Writer) (string, error) {
	project, hasProject := projectDir(payload, in.Env)
	line, err := innerCommand(project, hasProject, in.Env, in.Cwd)
	if err != nil {
		if se, ok := errors.AsType[*settingsError](err); ok {
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
	switch status {
	case timedOut:
		note = "(status line timed out after 10 s)"
	case notStarted:
		note = notRun
	}
	if text == "" {
		return red + note + reset, nil
	}
	return text + " " + red + note + reset, nil
}

// notStarted is the status of an inner command that could not be started at all.
const notStarted = -1 << 16

// runInner runs the inner command as `/bin/sh -c`, in its own session, with the payload on
// stdin and this process's environment plus the loop guard (S9 to S14). Its stdout is decoded
// as UTF-8 with replacement; its stderr goes to stderr as it is written. A command that could
// not be started has the status notStarted; only a cancel is an error.
func runInner(ctx context.Context, line string, in Input, dir string, stderr io.Writer) (string, int, error) {
	if err := cancelOf(ctx); err != nil {
		return "", 0, err
	}
	env := make(map[string]string, len(in.Env)+1)
	maps.Copy(env, in.Env)
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
	exitErr, isExit := errors.AsType[*execx.ExitError](err)
	switch {
	case err == nil:
		return decodeLossy(res.Stdout), 0, nil
	case !isExit:
		return "", notStarted, nil
	case exitErr.Signal != 0:
		return decodeLossy(res.Stdout), -int(exitErr.Signal), nil
	}
	return decodeLossy(res.Stdout), exitErr.Code, nil
}

// envPairs is env as NAME=value pairs, sorted by name so a run is the same every time.
func envPairs(env map[string]string) []string {
	pairs := make([]string, 0, len(env))
	for _, k := range slices.Sorted(maps.Keys(env)) {
		pairs = append(pairs, k+"="+env[k])
	}
	return pairs
}

// unsetFor names every variable of this process that the child must not inherit, so the
// child's environment is exactly env.
func unsetFor(env map[string]string) []string {
	var names []string
	for _, kv := range os.Environ() {
		name, _, found := strings.Cut(kv, "=")
		if _, ok := env[name]; found && !ok {
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

// segmentText is the bus segment, from the cache when it holds a good entry (S32 to S36). Its
// role diagnostic goes to stderr on both paths, before the segment (S24, S35).
func segmentText(ctx context.Context, in Input, payload pyjson.Object, stderr io.Writer) (string, error) {
	if err := cancelOf(ctx); err != nil {
		return "", err
	}
	w := newWorld(in.Cwd, in.Now(), in.UserHome)
	id := sessionID(payload)
	seg, err := cachedSegment(ctx, w, in.Env, id, in.Cwd, func(w *world) (segment, error) {
		return w.segment(ctx, in.Env, id)
	})
	if err != nil {
		return "", err
	}
	if seg.problem != "" {
		if _, err := io.WriteString(stderr, seg.problem+"\n"); err != nil {
			return "", err
		}
	}
	return seg.text, nil
}

// segment is the teams the session is on, with the roles, or a red note.
func (w *world) segment(ctx context.Context, env map[string]string, id string) (segment, error) {
	marker, has := env[membershipVar]
	if has && marker != membershipOff && strings.HasPrefix(marker, "/") {
		found, ok := w.membershipTeams(env)
		if !ok {
			return segment{text: note(env, noMembership)}, nil
		}
		if len(found) == 0 {
			return segment{}, nil
		}
		return w.teamsText(ctx, env, found)
	}
	if teams, ok := w.sessionTeams(env, id); ok {
		return w.teamsText(ctx, env, teams)
	}
	if env["PINGBUS_TEAMS"] != "" {
		return segment{text: note(env, noRecord)}, nil
	}
	return segment{}, nil
}

// sessionID is the payload's session_id when it is a name a record can be filed under.
func sessionID(payload pyjson.Object) string {
	id, ok := payload["session_id"].(string)
	if !ok || !sessionIDPattern.MatchString(id) {
		return ""
	}
	return id
}

// teamsText is the teams, each with its role and marks, wrapped to COLUMNS (S26 to S29), and
// the role diagnostic (S24).
func (w *world) teamsText(ctx context.Context, env map[string]string, teams []string) (segment, error) {
	roles, problem := w.readRoles(env, teams)
	seg := segment{problem: problem, teamsRead: true}
	pieces := make([]string, 0, len(teams))
	for i, team := range teams {
		role := roles[team]
		if role == "" {
			role = roleUnknown
		}
		piece := team + " (" + role + ")"
		if until := w.downUntil(env, team); !until.IsZero() {
			piece += " (down)"
			if seg.expires.IsZero() || until.Before(seg.expires) {
				seg.expires = until
			}
		}
		if i < len(teams)-1 {
			piece += ","
		}
		pieces = append(pieces, piece)
	}
	if err := cancelOf(ctx); err != nil {
		return segment{}, err
	}
	seg.text = wrapped(cyan, pieces, terminalColumns(env))
	return seg, nil
}

func note(env map[string]string, text string) string {
	return wrapped(red, strings.Split(text, " "), terminalColumns(env))
}

// wrapped is `label pieces` in rows no wider than columns (unbroken when columns is 0). A row
// breaks only between pieces, joined by one space, and a later row is indented under the first.
func wrapped(colour string, pieces []string, columns int) string {
	indent := strings.Repeat(" ", displayWidth(label+" "))
	var rows []string
	current := label + " " + pieces[0]
	for _, piece := range pieces[1:] {
		candidate := current + " " + piece
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
