// Package golden is the parity oracle for `pingbus status-line` (Plan 00213, SPEC-status-line.md
// section 10). It is test only.
//
// testdata/scenarios.json is the source of truth: one world per line (files, environment, stdin
// and steps). testdata/golden/<scenario>/<n>.{stdout,stderr,exit} is what the Python zipapp
// printed for step n, with the world's root written as {world}. Each step renders once; the
// steps of a scenario share one world, so a cache meets fresh and invalidated states.
//
// The default run drives the Go implementation in-process (statusline.Run). `-impl=python`
// builds the Python zipapp from this checkout and runs that; `-impl=<command>` runs any other
// binary, split on spaces and never through a shell. `-update` re-records golden/ from the
// Python zipapp; review the diff before committing it.
//
// stdout and the exit status must match byte for byte. stderr must match byte for byte, except
// for the one role diagnostic line, which is compared by shape (SPEC-status-line.md S24, S37).
package golden

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/pingbus/statusline"
)

const (
	worldMark   = "{world}"
	startedName = "inner-started"
	stepTimeout = 40 * time.Second
	pollEvery   = 20 * time.Millisecond
	goneChecks  = 250 // goneChecks * pollEvery is the 5 s a killed process gets to die
	innerNote   = "[oracle: the inner never started, no signal sent]\n"
	dirMode     = 0o750
	fileMode    = 0o600
	// settleBy is how far past the world's newest change the in-process run's clock is set: more
	// than the cache's 2 s racy window, so a render the cache may keep is kept. It is far inside
	// the 300 s a down mark counts for, and no scenario's mark sits within it of that edge.
	settleBy     = 3 * time.Second
	cachePrefix  = "cache-"
	cacheStored  = "stored"
	cacheHit     = "hit"
	cacheFileRel = "statusline-cache.json"
)

var (
	implFlag = flag.String("impl", "", `"python" runs the Python zipapp; another value is a command, split on spaces; empty runs the Go run in-process`)
	update   = flag.Bool("update", false, "re-record golden/ from the Python zipapp built from this checkout")
)

// roleLinePrefixes are the two stderr shapes a role failure may take (S24, S20).
var roleLinePrefixes = []string{
	"pingbus status-line: the role cannot be read (",
	"pingbus status-line: no team's role can be read: ",
}

type fileEntry struct {
	Path       string  `json:"path"`
	Kind       string  `json:"kind"`
	Content    *string `json:"content"`
	ContentB64 *string `json:"content_b64"`
	Mode       string  `json:"mode"`
	AgeS       float64 `json:"age_s"`
	Target     string  `json:"target"`
}

type stdinRepeat struct {
	Text  string `json:"text"`
	Count int    `json:"count"`
}

// stdinHolder is a stdin given as one of three keys.
type stdinHolder struct {
	Stdin       *string      `json:"stdin"`
	StdinB64    *string      `json:"stdin_b64"`
	StdinRepeat *stdinRepeat `json:"stdin_repeat"`
}

type step struct {
	stdinHolder

	Files  []fileEntry       `json:"files"`
	Remove []string          `json:"remove"`
	Env    map[string]string `json:"env"`
	Signal string            `json:"signal"`
	Gone   []string          `json:"gone"`
	// Cache is what the in-process Go run must do to the cache file in this step: cacheStored
	// (a miss, and a new file written) or cacheHit (served: the file was there and is unchanged).
	// Every step of a cache- scenario names one; no other step may.
	Cache string `json:"cache"`
}

type scenario struct {
	stdinHolder

	Name  string            `json:"name"`
	About string            `json:"about"`
	Files []fileEntry       `json:"files"`
	Env   map[string]string `json:"env"`
	Steps []step            `json:"steps"`
}

type scenarioFile struct {
	V         int        `json:"v"`
	Scenarios []scenario `json:"scenarios"`
}

// stepResult is what one step printed, with the world's root already written as {world}.
type stepResult struct {
	stdout, stderr []byte
	code           int
	problems       []string
}

// renderer runs the implementation once. The context is cancelled with the step's signal once
// the inner command has started; the renderer must then end as the signal ends it.
type renderer func(ctx context.Context, root string, env map[string]string, stdin []byte, sig syscall.Signal) ([]byte, []byte, int, error)

func loadScenarios(t *testing.T) []scenario {
	t.Helper()
	raw, err := fs.ReadFile(os.DirFS("testdata"), "scenarios.json")
	if err != nil {
		t.Fatal(err)
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	var file scenarioFile
	if err := dec.Decode(&file); err != nil {
		t.Fatalf("scenarios.json: %v", err)
	}
	if file.V != 1 {
		t.Fatalf("scenarios.json: version %d, want 1", file.V)
	}
	return file.Scenarios
}

// clockAt is the time the filesystem gives a file written now: a test reads no wall clock.
func clockAt(t *testing.T) time.Time {
	t.Helper()
	path := filepath.Join(t.TempDir(), "stamp")
	if err := os.WriteFile(path, nil, fileMode); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	return info.ModTime()
}

func expand(text, root string) string {
	return strings.ReplaceAll(text, worldMark, root)
}

func parseMode(s string) (os.FileMode, error) {
	v, err := strconv.ParseUint(s, 8, 32)
	if err != nil {
		return 0, fmt.Errorf("mode %q: %w", s, err)
	}
	return os.FileMode(v), nil
}

func decodeB64(s string) ([]byte, error) {
	return base64.StdEncoding.DecodeString(s)
}

// place writes one world entry. As the Python does, the parents are made and a symlink or a
// non-directory in the way is replaced; a file's mtime is now minus its age.
func place(root string, e fileEntry, now time.Time) error {
	path := filepath.Join(root, e.Path)
	if err := os.MkdirAll(filepath.Dir(path), dirMode); err != nil {
		return err
	}
	if info, err := os.Lstat(path); err == nil && (info.Mode()&fs.ModeSymlink != 0 || !info.IsDir()) {
		if err := os.Remove(path); err != nil {
			return err
		}
	}
	switch e.Kind {
	case "dir":
		return placeDir(path, e)
	case "symlink":
		return os.Symlink(expand(e.Target, root), path)
	case "file":
		return placeFile(root, path, e, now)
	default:
		return fmt.Errorf("%s: unknown kind %q", e.Path, e.Kind)
	}
}

func placeDir(path string, e fileEntry) error {
	mode, err := parseMode(e.Mode)
	if err != nil {
		return fmt.Errorf("%s: %w", e.Path, err)
	}
	if err := os.Mkdir(path, dirMode); err != nil && !errors.Is(err, fs.ErrExist) {
		return err
	}
	return os.Chmod(path, mode)
}

func placeFile(root, path string, e fileEntry, now time.Time) error {
	mode, err := parseMode(e.Mode)
	if err != nil {
		return fmt.Errorf("%s: %w", e.Path, err)
	}
	data, err := fileContent(root, e)
	if err != nil {
		return fmt.Errorf("%s: %w", e.Path, err)
	}
	if err := os.WriteFile(path, data, mode); err != nil {
		return err
	}
	if err := os.Chmod(path, mode); err != nil {
		return err
	}
	when := now.Add(-time.Duration(e.AgeS * float64(time.Second)))
	return os.Chtimes(path, when, when)
}

func fileContent(root string, e fileEntry) ([]byte, error) {
	switch {
	case e.ContentB64 != nil:
		return decodeB64(*e.ContentB64)
	case e.Content != nil:
		return []byte(expand(*e.Content, root)), nil
	}
	return nil, nil
}

func removeEntry(root, rel string) error {
	path := filepath.Join(root, rel)
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if info.IsDir() {
		return os.RemoveAll(path)
	}
	return os.Remove(path)
}

func stdinOf(h stdinHolder, root string) ([]byte, error) {
	switch {
	case h.StdinB64 != nil:
		return decodeB64(*h.StdinB64)
	case h.StdinRepeat != nil:
		return []byte(strings.Repeat(h.StdinRepeat.Text, h.StdinRepeat.Count)), nil
	case h.Stdin != nil:
		return []byte(expand(*h.Stdin, root)), nil
	}
	return nil, nil
}

// alive reports whether pid runs and is not a zombie, read the way the oracle reads /proc.
func alive(pid int) bool {
	raw, err := fs.ReadFile(os.DirFS("/proc"), filepath.Join(strconv.Itoa(pid), "stat"))
	if err != nil {
		return false
	}
	// The command name is in parentheses and may hold spaces; the state follows the last ')'.
	fields := strings.Fields(string(raw[bytes.LastIndexByte(raw, ')')+1:]))
	return len(fields) > 0 && fields[0] != "Z"
}

// gone checks the named pid files: each process must be dead or a zombie within 5 s.
func gone(t *testing.T, root string, pidFiles []string) []string {
	t.Helper()
	var problems []string
	for _, rel := range pidFiles {
		problem := goneOne(t, root, rel)
		if problem != "" {
			problems = append(problems, problem)
		}
	}
	return problems
}

func goneOne(t *testing.T, root, rel string) string {
	t.Helper()
	raw, err := fs.ReadFile(os.DirFS(root), rel)
	if err != nil {
		return rel + ": no pid was written"
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(raw)))
	if err != nil {
		return rel + ": no pid was written"
	}
	for i := 0; i < goneChecks && alive(pid); i++ {
		time.Sleep(pollEvery)
	}
	if !alive(pid) {
		return ""
	}
	if err := syscall.Kill(pid, syscall.SIGKILL); err != nil {
		t.Errorf("kill %d: %v", pid, err)
	}
	return fmt.Sprintf("%s: process %d outlived the render", rel, pid)
}

func signalNamed(name string) (syscall.Signal, error) {
	switch name {
	case "":
		return 0, nil
	case "SIGTERM":
		return syscall.SIGTERM, nil
	case "SIGINT":
		return syscall.SIGINT, nil
	case "SIGHUP":
		return syscall.SIGHUP, nil
	}
	return 0, fmt.Errorf("unknown signal %q", name)
}

// startedOrDone polls for the inner-started marker. It reports true when the marker appears
// first, and false when done closes or the step times out.
func startedOrDone(root string, done <-chan struct{}) bool {
	deadline := time.NewTimer(stepTimeout)
	defer deadline.Stop()
	for {
		if _, err := os.Stat(filepath.Join(root, startedName)); err == nil {
			return true
		}
		select {
		case <-done:
			return false
		case <-deadline.C:
			return false
		case <-time.After(pollEvery):
		}
	}
}

// goRender runs statusline.Run in-process. The signal reaches it as the context's cancel cause,
// as main delivers it, so the renderer needs no copy of it. Its clock is settleBy past the
// world's newest change, so the cache stores what it may and every scenario meets the real
// cache: a world built moments before would be racy, and never cached.
func goRender(ctx context.Context, root string, env map[string]string, stdin []byte, _ syscall.Signal) ([]byte, []byte, int, error) {
	newest, err := newestChange(root)
	if err != nil {
		return nil, nil, 0, err
	}
	at := newest.Add(settleBy)
	var stderr bytes.Buffer
	in := statusline.Input{Env: env, Cwd: root, Stdin: stdin, Now: func() time.Time { return at }}
	out, code := statusline.Run(ctx, in, &stderr)
	return out, stderr.Bytes(), code, nil
}

// newestChange is the latest ctime of anything under root, not following links.
func newestChange(root string) (time.Time, error) {
	var newest int64
	err := filepath.WalkDir(root, func(path string, _ fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		var st syscall.Stat_t
		if err := syscall.Lstat(path, &st); err != nil {
			return err
		}
		newest = max(newest, st.Ctim.Nano())
		return nil
	})
	return time.Unix(0, newest), err
}

// execRender runs an external binary through execx, the one process seam, in the world's
// directory with exactly the step's environment. A cancel sends the step's signal.
func execRender(argv []string) renderer {
	return func(ctx context.Context, root string, env map[string]string, stdin []byte, sig syscall.Signal) ([]byte, []byte, int, error) {
		add := make([]string, 0, len(env))
		for k, v := range env {
			add = append(add, k+"="+v)
		}
		cmd := execx.Cmd{
			Name: argv[0], Args: argv[1:], Env: add, Unset: unsetFor(env), Dir: root,
			Stdin: bytes.NewReader(stdin), Interrupt: sig,
		}
		res, err := execx.OS{}.Run(ctx, cmd)
		return exitOf(res, err)
	}
}

// exitOf is a finished command's output and status, as Python's returncode: negative for a
// signal.
func exitOf(res execx.Result, err error) ([]byte, []byte, int, error) {
	var exitErr *execx.ExitError
	switch {
	case err == nil:
		return res.Stdout, res.Stderr, 0, nil
	case errors.As(err, &exitErr) && exitErr.Signal != 0:
		return res.Stdout, res.Stderr, -int(exitErr.Signal), nil
	case errors.As(err, &exitErr):
		return res.Stdout, res.Stderr, exitErr.Code, nil
	}
	return nil, nil, 0, err
}

// unsetFor names the variables of this process that the child must not inherit.
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

// renderStep runs one step. For a step with a signal, the signal is sent once the inner
// command has created its marker; a step that ends before that gets the oracle's note.
func renderStep(render renderer, root string, env map[string]string, stdin []byte, st step) ([]byte, []byte, int, error) {
	sig, err := signalNamed(st.Signal)
	if err != nil {
		return nil, nil, 0, err
	}
	ctx, cancel := context.WithCancelCause(context.Background())
	defer cancel(nil)
	done := make(chan struct{})
	started := make(chan bool, 1)
	if sig != 0 {
		go func() {
			ok := startedOrDone(root, done)
			if ok {
				cancel(statusline.Cancel(sig))
			}
			started <- ok
		}()
	}
	out, errOut, code, err := render(ctx, root, env, stdin, sig)
	close(done)
	if err != nil {
		return nil, nil, 0, err
	}
	if sig != 0 && !<-started {
		errOut = append(errOut, innerNote...)
	}
	return out, errOut, code, nil
}

// buildWorld places a scenario's files in root and returns its environment and stdin.
func buildWorld(t *testing.T, sc scenario, root string) (map[string]string, []byte) {
	t.Helper()
	now := clockAt(t)
	for _, e := range sc.Files {
		if err := place(root, e, now); err != nil {
			t.Fatalf("world: %v", err)
		}
	}
	env := make(map[string]string, len(sc.Env))
	for k, v := range sc.Env {
		env[k] = expand(v, root)
	}
	stdin, err := stdinOf(sc.stdinHolder, root)
	if err != nil {
		t.Fatalf("stdin: %v", err)
	}
	return env, stdin
}

// applyStep changes the world and the environment for one step, as the step names.
func applyStep(t *testing.T, st step, root string, env map[string]string, stdin []byte) []byte {
	t.Helper()
	for _, rel := range st.Remove {
		if err := removeEntry(root, rel); err != nil {
			t.Fatalf("remove %s: %v", rel, err)
		}
	}
	stepNow := clockAt(t)
	for _, e := range st.Files {
		if err := place(root, e, stepNow); err != nil {
			t.Fatalf("step world: %v", err)
		}
	}
	for k, v := range st.Env {
		env[k] = expand(v, root)
	}
	if st.Stdin != nil || st.StdinB64 != nil {
		next, err := stdinOf(st.stdinHolder, root)
		if err != nil {
			t.Fatalf("step stdin: %v", err)
		}
		return next
	}
	return stdin
}

// runScenario builds the scenario's world and renders each step in it. With checkCache, each
// step's cache expectation is judged against the cache file before and after the render.
func runScenario(t *testing.T, render renderer, sc scenario, checkCache bool) []stepResult {
	t.Helper()
	root := t.TempDir()
	env, stdin := buildWorld(t, sc, root)
	results := make([]stepResult, 0, len(sc.Steps))
	for _, st := range sc.Steps {
		stdin = applyStep(t, st, root, env, stdin)
		if err := os.Remove(filepath.Join(root, startedName)); err != nil && !errors.Is(err, fs.ErrNotExist) {
			t.Fatal(err)
		}
		before := readCache(t, env)
		out, errOut, code, err := renderStep(render, root, env, stdin, st)
		if err != nil {
			t.Fatalf("render: %v", err)
		}
		problems := gone(t, root, st.Gone)
		if checkCache {
			problems = append(problems, cacheProblems(st.Cache, before, readCache(t, env))...)
		}
		marker := []byte(root)
		results = append(results, stepResult{
			stdout:   bytes.ReplaceAll(out, marker, []byte(worldMark)),
			stderr:   bytes.ReplaceAll(errOut, marker, []byte(worldMark)),
			code:     code,
			problems: problems,
		})
	}
	return results
}

// readCache is the cache file under the step's PINGBUS_HOME, or nil when there is none.
func readCache(t *testing.T, env map[string]string) []byte {
	t.Helper()
	home, ok := env["PINGBUS_HOME"]
	if !ok {
		return nil
	}
	raw, err := os.ReadFile(filepath.Join(home, cacheFileRel))
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

// cacheProblems judges one step's expectation: stored is a cache file that is new or rewritten,
// hit is one that was there before the render and is unchanged by it.
func cacheProblems(want string, before, after []byte) []string {
	switch want {
	case "":
		return nil
	case cacheStored:
		if after == nil || bytes.Equal(before, after) {
			return []string{fmt.Sprintf("cache: want a new entry stored; before %d bytes, after %d bytes", len(before), len(after))}
		}
	case cacheHit:
		if before == nil || !bytes.Equal(before, after) {
			return []string{fmt.Sprintf("cache: want a hit, the file unchanged; before %d bytes, after %d bytes", len(before), len(after))}
		}
	default:
		return []string{fmt.Sprintf("cache: unknown expectation %q", want)}
	}
	return nil
}

// stderrMatches is byte equality, except for the role line of S24, which is compared by shape:
// exactly one such line on each side, the same prefix, and for S24 the same failed teams in the
// same order. Every other line must be identical and in the same order.
func stderrMatches(want, got []byte) bool {
	if bytes.Equal(want, got) {
		return true
	}
	wantRole, wantOther := splitRole(splitLines(want))
	gotRole, gotOther := splitRole(splitLines(got))
	if wantRole == "" || gotRole == "" || !slices.Equal(wantOther, gotOther) {
		return false
	}
	wantPrefix, wantTeams := parseRoleLine(wantRole)
	gotPrefix, gotTeams := parseRoleLine(gotRole)
	if wantPrefix == "" || wantPrefix != gotPrefix {
		return false
	}
	return slices.Equal(wantTeams, gotTeams)
}

func splitLines(b []byte) []string {
	if len(b) == 0 {
		return nil
	}
	return strings.SplitAfter(string(b), "\n")
}

// splitRole returns the one role line ("" when there is not exactly one) and the other lines.
func splitRole(lines []string) (string, []string) {
	var role string
	var rest []string
	count := 0
	for _, line := range lines {
		if roleLinePrefix(line) != "" {
			role = line
			count++
			continue
		}
		rest = append(rest, line)
	}
	if count != 1 {
		return "", rest
	}
	return role, rest
}

func roleLinePrefix(line string) string {
	for _, p := range roleLinePrefixes {
		if strings.HasPrefix(line, p) {
			return p
		}
	}
	return ""
}

// parseRoleLine returns the prefix a role line takes and, for the S24 line, the failed team
// names in order. A team is the text before the first ": " of each "; "-separated item.
func parseRoleLine(line string) (string, []string) {
	prefix := roleLinePrefix(line)
	if prefix != roleLinePrefixes[0] {
		return prefix, nil
	}
	inner, ok := strings.CutSuffix(strings.TrimSuffix(strings.TrimPrefix(line, prefix), "\n"), ")")
	if !ok {
		return "", nil
	}
	var teams []string
	for item := range strings.SplitSeq(inner, "; ") {
		teams = append(teams, strings.SplitN(item, ": ", 2)[0])
	}
	return prefix, teams
}

// readRecorded is the recorded stdout, stderr and exit status of step n of a scenario.
func readRecorded(t *testing.T, name string, n int) ([]byte, []byte, int) {
	t.Helper()
	dir := filepath.Join("testdata", "golden", name)
	read := func(ext string) []byte {
		data, err := fs.ReadFile(os.DirFS(dir), fmt.Sprintf("%d.%s", n, ext))
		if err != nil {
			t.Fatalf("no record: %v (run with -update)", err)
		}
		return data
	}
	stdout, stderr := read("stdout"), read("stderr")
	code, err := strconv.Atoi(strings.TrimSpace(string(read("exit"))))
	if err != nil {
		t.Fatalf("%s step %d: exit record: %v", name, n, err)
	}
	return stdout, stderr, code
}

func rendererFor(t *testing.T) renderer {
	t.Helper()
	switch *implFlag {
	case "":
		return goRender
	case "python":
		return execRender(pythonZipapp(t))
	default:
		return execRender(strings.Fields(*implFlag))
	}
}

func TestStatusLineGolden(t *testing.T) {
	if *update {
		recordGolden(t)
		return
	}
	render := rendererFor(t)
	inProcess := *implFlag == ""
	for _, sc := range loadScenarios(t) {
		t.Run(sc.Name, func(t *testing.T) {
			t.Parallel()
			results := runScenario(t, render, sc, inProcess)
			if len(results) != len(sc.Steps) {
				t.Fatalf("ran %d steps, want %d", len(results), len(sc.Steps))
			}
			for i, got := range results {
				checkStep(t, sc.Name, i+1, got)
			}
		})
	}
}

// A cache- scenario must judge the cache at every step, so none can pass on the uncached path;
// a scenario of another kind leaves it unjudged.
func TestEveryCacheScenarioJudgesTheCache(t *testing.T) {
	for _, sc := range loadScenarios(t) {
		isCache := strings.HasPrefix(sc.Name, cachePrefix)
		for i, st := range sc.Steps {
			if isCache && st.Cache == "" {
				t.Errorf("%s step %d: a cache- scenario's step names no cache expectation", sc.Name, i+1)
			}
			if !isCache && st.Cache != "" {
				t.Errorf("%s step %d: a cache expectation outside a cache- scenario", sc.Name, i+1)
			}
		}
	}
}

// checkStep compares one step with its record.
func checkStep(t *testing.T, name string, n int, got stepResult) {
	t.Helper()
	want, wantErr, wantCode := readRecorded(t, name, n)
	for _, p := range got.problems {
		t.Errorf("step %d: %s", n, p)
	}
	if !bytes.Equal(got.stdout, want) {
		t.Errorf("step %d: stdout %q, recorded %q", n, got.stdout, want)
	}
	if !stderrMatches(wantErr, got.stderr) {
		t.Errorf("step %d: stderr %q, recorded %q", n, got.stderr, wantErr)
	}
	if got.code != wantCode {
		t.Errorf("step %d: exit %d, recorded %d", n, got.code, wantCode)
	}
}

// recordGolden re-records golden/ from the Python zipapp, and fails where the Python does not
// meet the scenario's own expectations.
func recordGolden(t *testing.T) {
	t.Helper()
	render := execRender(pythonZipapp(t))
	if err := os.RemoveAll(filepath.Join("testdata", "golden")); err != nil {
		t.Fatal(err)
	}
	for _, sc := range loadScenarios(t) {
		results := runScenario(t, render, sc, false)
		for i, r := range results {
			recordStep(t, sc, i, r)
		}
	}
}

// recordStep writes one step's record, after checking the Python met the scenario's own check.
func recordStep(t *testing.T, sc scenario, i int, r stepResult) {
	t.Helper()
	n := i + 1
	if len(r.problems) > 0 {
		t.Fatalf("%s step %d: the Python failed the scenario's own check: %v", sc.Name, n, r.problems)
	}
	if want := expectedExit(sc.Steps[i]); r.code != want {
		t.Fatalf("%s step %d: the Python exited %d, want %d", sc.Name, n, r.code, want)
	}
	dir := filepath.Join("testdata", "golden", sc.Name)
	if err := os.MkdirAll(dir, dirMode); err != nil {
		t.Fatal(err)
	}
	files := map[string][]byte{
		"stdout": r.stdout,
		"stderr": r.stderr,
		"exit":   []byte(strconv.Itoa(r.code) + "\n"),
	}
	for ext, data := range files {
		if err := os.WriteFile(filepath.Join(dir, fmt.Sprintf("%d.%s", n, ext)), data, fileMode); err != nil {
			t.Fatal(err)
		}
	}
}

func expectedExit(st step) int {
	sig, err := signalNamed(st.Signal)
	if err != nil || sig == 0 {
		return 0
	}
	return 128 + int(sig)
}

// pythonZipapp builds the Python zipapp from this checkout with the bundle tool, as the play
// does, and returns the command that runs its status line.
func pythonZipapp(t *testing.T) []string {
	t.Helper()
	repo := repoRoot(t)
	out := filepath.Join(t.TempDir(), "pingbus.pyz")
	build := execx.Cmd{
		Name: "python3",
		Args: []string{"-m", "helpers.pingbus.bundle", "--source", repo, "--out", out, "pingbus"},
		Dir:  repo,
	}
	if _, err := (execx.OS{}).Run(context.Background(), build); err != nil {
		t.Fatalf("build the Python zipapp: %v", err)
	}
	return []string{out, "status-line"}
}

// repoRoot walks up from the package directory to the checkout that holds the bundle tool.
func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "helpers", "pingbus", "bundle.py")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("no checkout with helpers/pingbus/bundle.py above the package")
		}
		dir = parent
	}
}
