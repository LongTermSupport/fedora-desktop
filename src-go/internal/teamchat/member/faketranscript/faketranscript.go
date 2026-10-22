// Package faketranscript is the session transcript a test drives in place of Claude Code's
// (B 2.15, F3): for each frame the inbox accepted it appends the JSONL a real session would
// write, the idle `user` record or the busy `queued_command` attachment (host check U20, pinned
// per Claude Code version by host check E3), after a delay on the fake clock or never, and it
// rotates, truncates or clears (/clear: a new file and path) the transcript on demand.
//
// It imports no inbox package: Attach takes the inbox's accept hook adapted to the frame's
// content. A write that fails is kept for Err, so a test reads it after the fake clock fired.
package faketranscript

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/clock"
)

// ErrSessionID is a session id that is not one plain file name.
var ErrSessionID = errors.New("faketranscript: a session id must be one file name")

// Mode is the session's state when a frame arrives, which decides the record's shape.
type Mode int

const (
	// Idle is a session at its prompt: the frame becomes a `user` record.
	Idle Mode = iota
	// Busy is a session in a turn: the frame is queued, a `queued_command` attachment.
	Busy
)

// Never is a delay after which nothing is written: a frame Claude Code never recorded.
const Never time.Duration = -1

const (
	fileMode = 0o600
	suffix   = ".jsonl"
	noFollow = syscall.O_NOFOLLOW
	// user is the type of a user turn record and the role of its message.
	user = "user"
)

// Transcript is one session's fake transcript file.
type Transcript struct {
	clk clock.Clock
	dir string

	mu    sync.Mutex
	path  string
	noise int
	err   error
}

// New makes the empty transcript <dir>/<session_id>.jsonl, mode 0600; one already there is
// refused.
func New(dir, sessionID string, clk clock.Clock) (*Transcript, error) {
	path, err := pathFor(dir, sessionID)
	if err != nil {
		return nil, err
	}
	if err := create(path); err != nil {
		return nil, err
	}
	return &Transcript{clk: clk, dir: dir, path: path}, nil
}

// Path is the transcript's current path.
func (t *Transcript) Path() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.path
}

// Err is the first write that failed, nil while none has.
func (t *Transcript) Err() error {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.err
}

// Record appends envelope in m's shape after `after` on the fake clock, to whatever file is at
// the transcript's path then; 0 writes at once, Never writes nothing.
func (t *Transcript) Record(envelope string, m Mode, after time.Duration) {
	switch {
	case after == Never:
	case after <= 0:
		t.record(envelope, m)
	default:
		t.clk.AfterFunc(after, func() { t.record(envelope, m) })
	}
}

// Noise appends n tool-output lines, which hold no needle.
func (t *Transcript) Noise(n int) {
	for range n {
		t.mu.Lock()
		t.noise++
		i := t.noise
		t.mu.Unlock()
		t.append(noiseLine{Type: user, Message: noiseMessage{Role: user, Content: []toolResult{{
			Type: "tool_result", Content: fmt.Sprintf("output line %d", i),
		}}}})
	}
}

// Rotate puts a new, empty inode at the transcript's path, as a rotation does.
func (t *Transcript) Rotate() error {
	t.mu.Lock()
	defer t.mu.Unlock()
	fresh := t.path + ".rotated"
	if err := create(fresh); err != nil {
		return err
	}
	if err := os.Rename(fresh, t.path); err != nil {
		return errors.Join(fmt.Errorf("faketranscript: rotate %s: %w", t.path, err), os.Remove(fresh))
	}
	return nil
}

// Truncate empties the transcript, keeping its inode.
func (t *Transcript) Truncate() error {
	t.mu.Lock()
	defer t.mu.Unlock()
	f, err := os.OpenFile(t.path, os.O_WRONLY|noFollow, 0)
	if err != nil {
		return fmt.Errorf("faketranscript: truncate: %w", err)
	}
	return errors.Join(f.Truncate(0), f.Close())
}

// Clear is /clear: a new, empty transcript for sessionID in the same directory, which every
// later record goes to; the old file stays as it was. It returns the new path.
func (t *Transcript) Clear(sessionID string) (string, error) {
	path, err := pathFor(t.dir, sessionID)
	if err != nil {
		return "", err
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if err := create(path); err != nil {
		return "", err
	}
	t.path = path
	return path, nil
}

// Attach records each frame the inbox accepts, in the mode of the moment, after `after`. on is
// the inbox's accept hook adapted to the frame's content, so this package imports no inbox:
//
//	tr.Attach(func(f func(string)) { inbox.OnAccepted(func(fr fakeinbox.Frame) { f(fr.Content) }) }, mode, after)
func (t *Transcript) Attach(on func(func(content string)), mode func() Mode, after time.Duration) {
	on(func(content string) { t.Record(content, mode(), after) })
}

type origin struct {
	Kind string `json:"kind"`
}

// idleRecord is a frame an idle session took as the user's turn.
type idleRecord struct {
	Type    string      `json:"type"`
	Message userMessage `json:"message"`
	Origin  origin      `json:"origin"`
}

type userMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// busyRecord is a frame a busy session queued.
type busyRecord struct {
	Type       string        `json:"type"`
	Attachment queuedCommand `json:"attachment"`
}

type queuedCommand struct {
	Type   string `json:"type"`
	Prompt string `json:"prompt"`
	Origin origin `json:"origin"`
}

type noiseLine struct {
	Type    string       `json:"type"`
	Message noiseMessage `json:"message"`
}

type noiseMessage struct {
	Role    string       `json:"role"`
	Content []toolResult `json:"content"`
}

type toolResult struct {
	Type    string `json:"type"`
	Content string `json:"content"`
}

func (t *Transcript) record(envelope string, m Mode) {
	peer := origin{Kind: "peer"}
	switch m {
	case Idle:
		t.append(idleRecord{Type: user, Message: userMessage{Role: user, Content: envelope}, Origin: peer})
	case Busy:
		t.append(busyRecord{Type: "attachment", Attachment: queuedCommand{Type: "queued_command", Prompt: envelope, Origin: peer}})
	default:
		t.fail(fmt.Errorf("faketranscript: mode %d is neither Idle nor Busy", m))
	}
}

// append writes v as one JSON line in one write. Like Claude Code's JSON.stringify, it does
// not escape <, > and &.
func (t *Transcript) append(v any) {
	var line bytes.Buffer
	enc := json.NewEncoder(&line)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		t.fail(err)
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	f, err := os.OpenFile(t.path, os.O_WRONLY|os.O_APPEND|noFollow, 0)
	if err == nil {
		_, err = f.Write(line.Bytes())
		err = errors.Join(err, f.Close())
	}
	if err != nil && t.err == nil {
		t.err = fmt.Errorf("faketranscript: append: %w", err)
	}
}

func (t *Transcript) fail(err error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.err == nil {
		t.err = err
	}
}

// pathFor is <dir>/<sessionID>.jsonl, sessionID being one plain name.
func pathFor(dir, sessionID string) (string, error) {
	if sessionID == "" || sessionID == "." || sessionID == ".." || strings.ContainsAny(sessionID, "/\x00") {
		return "", fmt.Errorf("%w: %q", ErrSessionID, sessionID)
	}
	return filepath.Join(dir, sessionID+suffix), nil
}

// create makes path empty, mode 0600 whatever the umask, never over anything there.
func create(path string) error {
	f, err := os.OpenFile(filepath.Clean(path), os.O_WRONLY|os.O_CREATE|os.O_EXCL|noFollow, fileMode)
	if err != nil {
		return fmt.Errorf("faketranscript: create: %w", err)
	}
	return errors.Join(f.Chmod(fileMode), f.Close())
}
