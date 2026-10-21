package golden

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/pingbus/hooks"
)

const (
	worldMark   = "{world}"
	hsMark      = "{hs}"
	binDir      = "bin"
	spawnLog    = "spawn.log"
	fakePingbus = "fake-pingbus"
	stepTimeout = 60 * time.Second
	// spawnWait bounds the wait for a watcher start a step expects; settleFor is how long
	// the end of a scenario waits for a start none expected.
	spawnWait = 10 * time.Second
	settleFor = 300 * time.Millisecond
	pollEvery = 20 * time.Millisecond
)

// renderer runs the implementation once for event in the world at root, with exactly env,
// stdin and the clock nowMs.
type renderer func(ctx context.Context, root, event string, env map[string]string, stdin []byte, nowMs int64) ([]byte, []byte, int, error)

// world is one scenario's world while it runs.
type world struct {
	root string
	hs   *homeserver
	env  map[string]string
}

func (w *world) expand(text string) string {
	return strings.NewReplacer(worldMark, w.root, hsMark, w.hs.url).Replace(text)
}

func (w *world) normalise(b []byte) []byte {
	b = bytes.ReplaceAll(b, []byte(w.hs.url), []byte(hsMark))
	return bytes.ReplaceAll(b, []byte(w.root), []byte(worldMark))
}

func parseMode(s string) (os.FileMode, error) {
	v, err := strconv.ParseUint(s, 8, 32)
	if err != nil {
		return 0, fmt.Errorf("mode %q: %w", s, err)
	}
	return os.FileMode(v), nil
}

// place writes one world entry, its parents made and anything in its way replaced.
func (w *world) place(e fileEntry) error {
	path := filepath.Join(w.root, e.Path)
	if err := os.MkdirAll(filepath.Dir(path), dirMode); err != nil {
		return err
	}
	if info, err := os.Lstat(path); err == nil && (info.Mode()&fs.ModeSymlink != 0 || !info.IsDir() || e.Kind != "dir") {
		if err := os.RemoveAll(path); err != nil {
			return err
		}
	}
	switch e.Kind {
	case "symlink":
		return os.Symlink(w.expand(e.Target), path)
	case "dir":
		return placeDir(path, e)
	case "file":
		return w.placeFile(path, e)
	}
	return fmt.Errorf("%s: unknown kind %q", e.Path, e.Kind)
}

func placeDir(path string, e fileEntry) error {
	mode, err := parseMode(e.Mode)
	if err != nil {
		return fmt.Errorf("%s: %w", e.Path, err)
	}
	if mkErr := os.Mkdir(path, dirMode); mkErr != nil && !errors.Is(mkErr, fs.ErrExist) {
		return mkErr
	}
	return os.Chmod(path, mode)
}

func (w *world) placeFile(path string, e fileEntry) error {
	mode, err := parseMode(e.Mode)
	if err != nil {
		return fmt.Errorf("%s: %w", e.Path, err)
	}
	data, err := w.content(e)
	if err != nil {
		return fmt.Errorf("%s: %w", e.Path, err)
	}
	if err := os.WriteFile(path, data, mode); err != nil {
		return err
	}
	return os.Chmod(path, mode)
}

func (w *world) content(e fileEntry) ([]byte, error) {
	switch {
	case e.ContentB64 != nil:
		return base64.StdEncoding.DecodeString(*e.ContentB64)
	case e.Content != nil:
		return []byte(w.expand(*e.Content)), nil
	}
	return nil, nil
}

func (w *world) stdinOf(h stdinHolder) ([]byte, error) {
	switch {
	case h.StdinB64 != nil:
		return base64.StdEncoding.DecodeString(*h.StdinB64)
	case h.StdinRepeat != nil:
		return []byte(h.StdinRepeat.Prefix + strings.Repeat(h.StdinRepeat.Text, h.StdinRepeat.Count)), nil
	case h.Stdin != nil:
		return []byte(w.expand(*h.Stdin)), nil
	}
	return nil, nil
}

// buildWorld places the fake pingbus, the fragments the scenario uses and its own files, and
// sets its environment.
func buildWorld(t *testing.T, file scenarioFile, sc scenario) *world {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	w := &world{root: root, hs: startHomeserver(t, file.Rooms, sc.Homeserver), env: map[string]string{}}
	script, err := fs.ReadFile(os.DirFS("testdata"), fakePingbus)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, binDir), dirMode); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, binDir, "pingbus"), script, 0o700); err != nil {
		t.Fatal(err)
	}
	var files []fileEntry
	for _, name := range sc.Use {
		files = append(files, file.Fragments[name].Files...)
		maps.Copy(w.env, file.Fragments[name].Env)
	}
	files = append(files, sc.Files...)
	maps.Copy(w.env, sc.Env)
	for _, e := range files {
		if err := w.place(e); err != nil {
			t.Fatalf("world: %v", err)
		}
	}
	for k, v := range w.env {
		w.env[k] = w.expand(v)
	}
	return w
}

// applyStep changes the world as the step names, and returns its stdin.
func (w *world) applyStep(t *testing.T, st step) []byte {
	t.Helper()
	for _, rel := range st.Remove {
		if err := os.RemoveAll(filepath.Join(w.root, rel)); err != nil {
			t.Fatalf("remove %s: %v", rel, err)
		}
	}
	for _, e := range st.Files {
		if err := w.place(e); err != nil {
			t.Fatalf("step world: %v", err)
		}
	}
	for k, v := range st.Env {
		w.env[k] = w.expand(v)
	}
	for _, k := range st.Unset {
		delete(w.env, k)
	}
	if st.Homeserver != nil {
		w.hs.setFail(*st.Homeserver)
	}
	stdin, err := w.stdinOf(st.stdinHolder)
	if err != nil {
		t.Fatalf("step stdin: %v", err)
	}
	return stdin
}

// holdLocks takes each lock the step names with an exclusive flock, its file holding the
// content, until the returned release runs.
func (w *world) holdLocks(t *testing.T, locks []heldLock) func() {
	t.Helper()
	held := make([]*os.File, 0, len(locks))
	release := func() {
		for _, f := range held {
			if err := syscall.Flock(int(f.Fd()), syscall.LOCK_UN); err != nil {
				t.Errorf("unlock %s: %v", f.Name(), err)
			}
			if err := f.Close(); err != nil {
				t.Errorf("close %s: %v", f.Name(), err)
			}
		}
	}
	for _, l := range locks {
		f, err := os.OpenFile(filepath.Join(w.root, l.Path), os.O_RDWR|os.O_CREATE|os.O_TRUNC, fileMode)
		if err != nil {
			release()
			t.Fatalf("hold %s: %v", l.Path, err)
		}
		held = append(held, f)
		if _, err := f.WriteString(l.Content); err != nil {
			release()
			t.Fatalf("hold %s: %v", l.Path, err)
		}
		if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
			release()
			t.Fatalf("hold %s: %v", l.Path, err)
		}
	}
	return release
}

// spawns is how many watcher starts the fake pingbus has recorded.
func (w *world) spawns() int {
	raw, err := os.ReadFile(filepath.Join(w.root, spawnLog))
	if err != nil {
		return 0
	}
	return bytes.Count(raw, []byte("\n"))
}

// waitSpawns waits, at most within, for the record to reach want starts.
func (w *world) waitSpawns(want int, within time.Duration) {
	deadline := time.NewTimer(within)
	defer deadline.Stop()
	for w.spawns() < want {
		select {
		case <-deadline.C:
			return
		case <-time.After(pollEvery):
		}
	}
}

// snapEntry is one entry of a step's files.json.
type snapEntry struct {
	Path       string  `json:"path"`
	Kind       string  `json:"kind"`
	Mode       string  `json:"mode"`
	Content    *string `json:"content,omitempty"`
	ContentB64 *string `json:"content_b64,omitempty"`
	Target     *string `json:"target,omitempty"`
}

// snapshot is every entry under the world but bin/, sorted by path, normalised.
func (w *world) snapshot() ([]byte, error) {
	entries := []snapEntry{}
	err := filepath.WalkDir(w.root, func(path string, _ fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, err := filepath.Rel(w.root, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		if rel == binDir {
			return filepath.SkipDir
		}
		e, err := w.snapOne(path, rel)
		if err != nil {
			return err
		}
		entries = append(entries, e)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return encodeJSON(entries)
}

// snapOne is one entry of the snapshot: its kind, mode, and content or link target.
func (w *world) snapOne(path, rel string) (snapEntry, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return snapEntry{}, err
	}
	e := snapEntry{Path: filepath.ToSlash(rel), Mode: fmt.Sprintf("%04o", info.Mode().Perm())}
	switch {
	case info.Mode()&fs.ModeSymlink != 0:
		target, err := os.Readlink(path)
		if err != nil {
			return snapEntry{}, err
		}
		text := string(w.normalise([]byte(target)))
		e.Kind, e.Mode, e.Target = "symlink", "", &text
	case info.IsDir():
		e.Kind = "dir"
	case info.Mode().IsRegular():
		raw, err := os.ReadFile(path)
		if err != nil {
			return snapEntry{}, err
		}
		e.Kind = "file"
		raw = w.normalise(raw)
		text := string(raw)
		if !utf8.Valid(raw) {
			text = base64.StdEncoding.EncodeToString(raw)
			e.ContentB64 = &text
			break
		}
		e.Content = &text
	default:
		e.Kind = "other"
	}
	return e, nil
}

// runScenario builds the scenario's world and runs each step in it.
func runScenario(t *testing.T, render renderer, file scenarioFile, sc scenario) []stepResult {
	t.Helper()
	w := buildWorld(t, file, sc)
	results := make([]stepResult, 0, len(sc.Steps))
	expected := 0
	for i, st := range sc.Steps {
		stdin := w.applyStep(t, st)
		release := w.holdLocks(t, st.Hold)
		ctx, cancel := context.WithTimeout(context.Background(), stepTimeout)
		out, errOut, code, err := render(ctx, w.root, st.Event, maps.Clone(w.env), stdin, st.NowMs)
		cancel()
		if err != nil {
			release()
			t.Fatalf("step %d: run: %v", i+1, err)
		}
		var problems []string
		if st.Spawn {
			expected++
			w.waitSpawns(expected, spawnWait)
			if got := w.spawns(); got != expected {
				problems = append(problems, fmt.Sprintf("the watcher was started %d times by the end of the step, want %d", got, expected))
			}
		}
		files, err := w.snapshot()
		release()
		if err != nil {
			t.Fatalf("step %d: snapshot: %v", i+1, err)
		}
		requests, err := encodeJSON(w.hs.drain(w.normalise))
		if err != nil {
			t.Fatal(err)
		}
		results = append(results, stepResult{
			stdout: w.normalise(out), stderr: w.normalise(errOut), code: code,
			files: files, requests: requests, problems: problems, unordered: st.Event == "session-end",
		})
	}
	w.waitSpawns(expected+1, settleFor)
	if got := w.spawns(); got != expected && len(results) > 0 {
		last := &results[len(results)-1]
		last.problems = append(last.problems, fmt.Sprintf("the watcher was started %d times in all, the steps expect %d", got, expected))
	}
	return results
}

// goRender runs hooks.Run in-process with the step's clock; stdin is cut where a hook stops
// reading it.
func goRender(ctx context.Context, _, event string, env map[string]string, stdin []byte, nowMs int64) ([]byte, []byte, int, error) {
	var stdout, stderr bytes.Buffer
	at := time.UnixMilli(nowMs)
	in := hooks.Input{
		Event: event, Stdin: stdin[:min(len(stdin), hooks.InputMax+1)], Env: env,
		Now: func() time.Time { return at },
	}
	code := hooks.Run(ctx, in, &stdout, &stderr)
	return stdout.Bytes(), stderr.Bytes(), int(code), nil
}
