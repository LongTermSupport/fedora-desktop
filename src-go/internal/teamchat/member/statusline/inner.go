package statusline

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"golang.org/x/sys/unix"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/cli"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
)

const (
	innerTimeout = 10 * time.Second
	// maxInner is the most of the inner command's stdout kept; the rest is read and dropped.
	maxInner = 64 << 10
	// maxSettings bounds a settings file.
	maxSettings = 1 << 20

	red   = "\x1b[31m"
	reset = "\x1b[0m"

	noteTimedOut = "(status line timed out after 10 s)"
	noteNotRun   = "(status line could not be run)"
	// self is this command, which a settings file may name without the line drawing itself.
	self = "teamchat status-line"
)

// errTimedOut is the cause of the inner command's own deadline, told apart from a cancel.
var errTimedOut = errors.New("the inner status line timed out")

// inner is the inner status line's text, with a red note when it could not be had cleanly; ""
// when there is none or the render was cancelled.
func (d deps) inner(ctx context.Context, e cli.Env, raw []byte) string {
	payload := parsePayload(raw)
	project, hasProject := projectDir(payload, e.Getenv)
	line, err := innerCommand(project, hasProject, e.Getenv)
	if err != nil {
		return red + "status line: " + err.Error() + reset
	}
	if line == "" {
		return ""
	}
	dir := ""
	if hasProject && isDir(project) {
		dir = project
	}
	return d.runInner(ctx, line, raw, dir, e.Stderr)
}

// parsePayload is the session JSON as an object; anything else is none.
func parsePayload(raw []byte) map[string]any {
	if len(raw) > maxPayload {
		return nil
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		return nil
	}
	object, ok := v.(map[string]any)
	if !ok {
		return nil
	}
	return object
}

// projectDir is the first absolute of the payload's workspace.project_dir,
// CLAUDE_PROJECT_DIR and the payload's cwd (S6).
func projectDir(payload map[string]any, getenv func(string) string) (string, bool) {
	var candidates []any
	if workspace, ok := payload["workspace"].(map[string]any); ok {
		candidates = append(candidates, workspace["project_dir"])
	}
	candidates = append(candidates, getenv("CLAUDE_PROJECT_DIR"), payload["cwd"])
	for _, c := range candidates {
		if s, ok := c.(string); ok && filepath.IsAbs(s) {
			return s, true
		}
	}
	return "", false
}

// settingsFiles are the files the inner command is looked for in, in precedence order (S7).
func settingsFiles(project string, hasProject bool, getenv func(string) string) []string {
	var files []string
	if hasProject {
		files = append(files,
			filepath.Join(project, ".claude", "settings.local.json"),
			filepath.Join(project, ".claude", "settings.json"))
	}
	user := getenv("CLAUDE_CONFIG_DIR")
	if home := getenv("HOME"); user == "" && filepath.IsAbs(home) {
		user = filepath.Join(home, ".claude")
	}
	if user != "" {
		files = append(files, filepath.Join(user, "settings.json"))
	}
	return files
}

// settingsError is a settings file that exists but cannot be read or decoded (S8); its text
// is drawn in the line, and names no exception (n3e-D16).
type settingsError struct{ msg string }

func (e *settingsError) Error() string { return e.msg }

// innerCommand is the first usable statusLine command, or "" for none; a settings file that
// exists but cannot be read or decoded stops the search.
func innerCommand(project string, hasProject bool, getenv func(string) string) (string, error) {
	for _, path := range settingsFiles(project, hasProject, getenv) {
		raw, err := readSettings(path)
		if errors.Is(err, unix.ENOENT) {
			continue
		}
		if err != nil || !utf8.Valid(raw) {
			return "", &settingsError{path + " cannot be read"}
		}
		dec := json.NewDecoder(bytes.NewReader(raw))
		dec.UseNumber()
		var doc any
		if err := dec.Decode(&doc); err != nil || dec.More() || trailing(dec) {
			return "", &settingsError{path + " is not valid JSON"}
		}
		if command := commandOf(doc); command != "" {
			return command, nil
		}
	}
	return "", nil
}

// trailing is whether anything but whitespace follows the decoded document.
func trailing(dec *json.Decoder) bool {
	rest, err := io.ReadAll(dec.Buffered())
	return err != nil || len(bytes.TrimSpace(rest)) > 0
}

// readSettings is a settings file's bytes. A link is followed, as Claude Code follows it; the
// open never blocks on a FIFO, and only a regular file within the bound is read.
func readSettings(path string) ([]byte, error) {
	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_NONBLOCK|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, err
	}
	f := os.NewFile(uintptr(fd), path)
	data, readErr := readRegular(f)
	return data, errors.Join(readErr, f.Close())
}

var errNotRegular = errors.New("not a regular file within the bound")

func readRegular(f *os.File) ([]byte, error) {
	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, errNotRegular
	}
	data, err := io.ReadAll(io.LimitReader(f, maxSettings+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxSettings {
		return nil, errNotRegular
	}
	return data, nil
}

// commandOf is the usable command of a settings document: statusLine.command, a string that is
// not blank and is not this command, untrimmed.
func commandOf(doc any) string {
	object, ok := doc.(map[string]any)
	if !ok {
		return ""
	}
	line, ok := object["statusLine"].(map[string]any)
	if !ok {
		return ""
	}
	command, ok := line["command"].(string)
	if !ok {
		return ""
	}
	trimmed := strings.TrimSpace(command)
	if trimmed == "" || trimmed == self || strings.HasSuffix(trimmed, "/"+self) {
		return ""
	}
	return command
}

func isDir(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

// runInner runs the inner command as /bin/sh -c in its own session, with the payload on stdin,
// this process's environment plus the loop guard, stderr inherited, and stdout captured (S9 to
// S14). At 10 s its group is killed and the output is only the timeout note; a process that
// left the group holding stdout keeps the render at most the runner's grace.
func (d deps) runInner(ctx context.Context, line string, raw []byte, dir string, stderr io.Writer) string {
	run, stop := context.WithCancelCause(ctx)
	defer stop(nil)
	disarm := d.clock.AfterFunc(innerTimeout, func() { stop(errTimedOut) })
	defer disarm()
	out := &capped{limit: maxInner}
	_, err := d.runner.Run(run, execx.Cmd{
		Name:       "/bin/sh",
		Args:       []string{"-c", line},
		Env:        []string{paths.EnvStatusLineInner + "=1"},
		Dir:        dir,
		Stdin:      bytes.NewReader(raw),
		Stdout:     out,
		Stderr:     stderr,
		NewSession: true,
	})
	if ctx.Err() != nil {
		return ""
	}
	if errors.Is(context.Cause(run), errTimedOut) {
		return red + noteTimedOut + reset
	}
	text := strings.TrimRight(lossy(out.buf.Bytes()), "\n")
	if err == nil {
		return text
	}
	note := noteNotRun
	if exitErr, ok := errors.AsType[*execx.ExitError](err); ok {
		status := exitErr.Code
		if exitErr.Signal != 0 {
			status = -int(exitErr.Signal)
		}
		note = "(status line exit " + strconv.Itoa(status) + ")"
	}
	if text == "" {
		return red + note + reset
	}
	return text + " " + red + note + reset
}

// capped keeps the first limit bytes written and drops the rest, so a command that writes
// without end is drained without being held in memory.
type capped struct {
	buf   bytes.Buffer
	limit int
}

func (c *capped) Write(p []byte) (int, error) {
	if room := c.limit - c.buf.Len(); room > 0 {
		c.buf.Write(p[:min(room, len(p))])
	}
	return len(p), nil
}

// lossy is the bytes as UTF-8 with U+FFFD for each invalid byte.
func lossy(b []byte) string {
	var out strings.Builder
	out.Grow(len(b))
	for len(b) > 0 {
		r, size := utf8.DecodeRune(b)
		out.WriteRune(r)
		b = b[size:]
	}
	return out.String()
}
