package difftest

import (
	"bytes"
	"context"
	"crypto/md5"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/shellword"
)

const (
	sandboxName  = "difftest"
	buildContext = "opt/claude-yolo"
	gpuDir       = "dev/dri" // absent unless a scenario places a file in it
	imageHashLen = 16
	sandboxMark  = "{sandbox}"
	pidMark      = "{pid}"
	versionMark  = "{ccy_version}"
	hashMark     = "{ccy_hash}"
	dirMode      = 0o755
	libMode      = 0o644
	defaultMode  = "0644"
	runTimeout   = 90 * time.Second
	cwdProject   = "project"
	cwdOutside   = "outside"
	matrixFormat = 1
	hoursPerDay  = 24
)

// sandboxDirs are made in every sandbox. home is HOME; project is a git repository root with
// one commit, the directory a launch starts in; outside is a directory in no repository; tmp is
// TMPDIR and run is XDG_RUNTIME_DIR.
var sandboxDirs = []string{"bin", "home", cwdProject, cwdOutside, "tmp", "run"}

// snapshotDirs are the sandbox directories whose files the golden records after a run.
var snapshotDirs = []string{"home", cwdProject, cwdOutside, "tmp", "run"}

// fileSpec is one file placed in the sandbox before the run, its path relative to the sandbox.
// Its modification time is the matrix's clock minus AgeDays.
type fileSpec struct {
	Path    string `json:"path"`
	Content string `json:"content"`
	Mode    string `json:"mode"`
	AgeDays int    `json:"ageDays"`
}

// world is a reusable part of a scenario: files, environment and answers.
type world struct {
	About  string            `json:"about"`
	Files  []fileSpec        `json:"files"`
	Env    map[string]string `json:"env"`
	Rules  []rule            `json:"rules"`
	Remote string            `json:"remote"`
}

// scenario is one run of the launcher: the worlds it is built from (in order; a scenario's own
// rules are tried first, then each world's), its arguments, and where it starts.
type scenario struct {
	Name   string            `json:"name"`
	About  string            `json:"about"`
	Area   string            `json:"area"`
	Worlds []string          `json:"worlds"`
	Args   []string          `json:"args"`
	Cwd    string            `json:"cwd"`
	Env    map[string]string `json:"env"`
	Files  []fileSpec        `json:"files"`
	Rules  []rule            `json:"rules"`
	Stdin  string            `json:"stdin"`
}

// matrix is testdata/scenarios.json.
type matrix struct {
	V         int              `json:"v"`
	Now       string           `json:"now"`
	Secrets   []string         `json:"secrets"`
	Worlds    map[string]world `json:"worlds"`
	Scenarios []scenario       `json:"scenarios"`

	tokens []string // the names of every {token:NAME} the matrix uses
}

// tokenMark is an OAuth token in a file or an answer: the token itself is made by the harness
// (tokenValue), so no token-shaped text is ever committed, and is written back as its mark.
var tokenMark = regexp.MustCompile(`\{token:([a-z0-9-]+)\}`)

// tokenLen is the length of a made token: inside the 90 to 120 bytes the launcher accepts.
const tokenLen = 100

func tokenValue(name string) string {
	v := "sk-ant-" + "oat01-" + name + "-"
	return v + strings.Repeat("x", tokenLen-len(v))
}

// expandTokens writes each {token:NAME} as its token.
func expandTokens(s string) string {
	return tokenMark.ReplaceAllStringFunc(s, func(mark string) string {
		return tokenValue(tokenMark.FindStringSubmatch(mark)[1])
	})
}

// privateValues are the values that must never reach an argv: the matrix's own and every token.
func (m matrix) privateValues() []string {
	out := slices.Clone(m.Secrets)
	for _, name := range m.tokens {
		out = append(out, tokenValue(name))
	}
	return out
}

// outcome is what one run left behind, before it is written out as the golden text.
type outcome struct {
	exit           int
	stdout, stderr string
	calls          []call
	files          []string
}

func loadMatrix(t *testing.T) matrix {
	t.Helper()
	raw, err := fs.ReadFile(os.DirFS("testdata"), "scenarios.json")
	if err != nil {
		t.Fatal(err)
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	var m matrix
	if err := dec.Decode(&m); err != nil {
		t.Fatalf("scenarios.json: %v", err)
	}
	if m.V != matrixFormat {
		t.Fatalf("scenarios.json: format %d, want %d", m.V, matrixFormat)
	}
	for _, match := range tokenMark.FindAllStringSubmatch(string(raw), -1) {
		if !slices.Contains(m.tokens, match[1]) {
			m.tokens = append(m.tokens, match[1])
		}
	}
	return m
}

func (m matrix) epoch(t *testing.T) int64 {
	t.Helper()
	now, err := time.Parse(time.RFC3339, m.Now)
	if err != nil {
		t.Fatalf("scenarios.json now: %v", err)
	}
	return now.Unix()
}

// launcher is the program under test and the names the golden text abstracts over.
type launcher struct {
	path    string
	version string
	hash    string
}

// repoRoot is the checkout this package is in.
func repoRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", "..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	return root
}

// bashLauncherDir is the bash launcher's source in the checkout.
func bashLauncherDir(t *testing.T) string {
	t.Helper()
	return filepath.Join(repoRoot(t), "files", "var", "local", "claude-yolo")
}

// installBash lays the checkout's bash launcher out as the play installs it (the launcher,
// executable, with lib/ beside it) in a directory of its own, and returns the launcher's path.
// edit, when not nil, changes the launcher's text first: the oracle's own red test.
func installBash(t *testing.T, edit func(string) string) string {
	t.Helper()
	src := bashLauncherDir(t)
	dst := filepath.Join(t.TempDir(), "claude-yolo")
	if err := os.MkdirAll(filepath.Join(dst, "lib"), dirMode); err != nil {
		t.Fatal(err)
	}
	text := readFile(t, filepath.Join(src, "claude-yolo"))
	if edit != nil {
		edited := edit(text)
		if edited == text {
			t.Fatal("the edit changed nothing in the launcher")
		}
		text = edited
	}
	writeFile(t, filepath.Join(dst, "claude-yolo"), text, dirMode)
	libs, err := filepath.Glob(filepath.Join(src, "lib", "*.bash"))
	if err != nil {
		t.Fatal(err)
	}
	for _, lib := range libs {
		writeFile(t, filepath.Join(dst, "lib", filepath.Base(lib)), readFile(t, lib), libMode)
	}
	return filepath.Join(dst, "claude-yolo")
}

// launcherUnderTest is LAUNCHER from the environment when set (any program, run as is), or else
// the checkout's bash launcher installed into a scratch directory.
func launcherUnderTest(t *testing.T) string {
	t.Helper()
	if given := os.Getenv("LAUNCHER"); given != "" {
		path, err := filepath.Abs(given)
		if err != nil {
			t.Fatal(err)
		}
		return path
	}
	return installBash(t, nil)
}

var versionLine = regexp.MustCompile(`\Accy version (\S+)\nHash: ([0-9a-f]+)\n\z`)

// probe asks the launcher its version and hash, which every golden names as {ccy_version} and
// {ccy_hash}: they change with every release, the behaviour the golden pins does not.
func probe(t *testing.T, m matrix, path string) launcher {
	t.Helper()
	got := runScenario(t, m, launcher{path: path}, scenario{Name: "probe", Args: []string{"--version"}, Cwd: cwdOutside})
	match := versionLine.FindStringSubmatch(got.stdout)
	if got.exit != 0 || match == nil {
		t.Fatalf("%s --version: exit %d, stdout %q, stderr %q", path, got.exit, got.stdout, got.stderr)
	}
	return launcher{path: path, version: match[1], hash: match[2]}
}

// sandbox is one scenario's scratch world. epoch is the pinned clock; machineNow is the
// machine's, read once from the file system (a test reads no wall clock).
type sandbox struct {
	root       string
	epoch      int64
	machineNow time.Time
}

// machineClock is the time the file system gives a file written now.
func machineClock(t *testing.T, root string) time.Time {
	t.Helper()
	path := filepath.Join(root, "clock")
	writeFile(t, path, "", logFileMode)
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	return info.ModTime()
}

func newSandbox(t *testing.T, epoch int64) sandbox {
	t.Helper()
	// The sandbox's own name is fixed, not the temporary directory's: the launcher names a
	// project after its parent directory too.
	tmp, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(tmp, sandboxName)
	if err = os.Mkdir(root, dirMode); err != nil {
		t.Fatal(err)
	}
	for _, d := range sandboxDirs {
		if mkErr := os.Mkdir(filepath.Join(root, d), dirMode); mkErr != nil {
			t.Fatal(mkErr)
		}
	}
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	link := func(name, target string) {
		if err := os.Symlink(target, filepath.Join(root, "bin", name)); err != nil {
			t.Fatal(err)
		}
	}
	for _, name := range realTools {
		path, err := exec.LookPath(name)
		if err != nil {
			t.Fatalf("%s is needed inside the sandbox and is not installed: %v", name, err)
		}
		link(name, path)
	}
	for _, name := range slices.Concat(observedTools, quietTools, clockTools, []string{starterName}) {
		link(name, self)
	}
	return sandbox{root: root, epoch: epoch, machineNow: machineClock(t, root)}
}

// gitEnv keeps the machine's git configuration out of the sandbox's repository.
func (s sandbox) gitEnv() []string {
	return []string{"HOME=" + filepath.Join(s.root, "home"), "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null"}
}

func (s sandbox) git(t *testing.T, args ...string) {
	t.Helper()
	base := []string{"-c", "user.name=difftest", "-c", "user.email=difftest@example.com", "-c", "init.defaultBranch=main"}
	c := execx.Cmd{Name: "git", Args: slices.Concat(base, args), Dir: filepath.Join(s.root, cwdProject), Env: s.gitEnv()}
	if _, err := (execx.OS{}).Run(context.Background(), c); err != nil {
		t.Fatalf("git %v: %v", args, err)
	}
}

func (s sandbox) place(t *testing.T, f fileSpec) {
	t.Helper()
	mode := f.Mode
	if mode == "" {
		mode = defaultMode
	}
	perm, err := strconv.ParseUint(mode, 8, 32)
	if err != nil {
		t.Fatalf("%s: mode %q: %v", f.Path, mode, err)
	}
	path := filepath.Join(s.root, filepath.FromSlash(f.Path))
	if !strings.HasPrefix(path, s.root+string(filepath.Separator)) {
		t.Fatalf("%s is outside the sandbox", f.Path)
	}
	if err := os.MkdirAll(filepath.Dir(path), dirMode); err != nil {
		t.Fatal(err)
	}
	writeFile(t, path, expandTokens(strings.ReplaceAll(f.Content, sandboxMark, s.root)), fs.FileMode(perm))
	// Stamped on the machine's clock, as a file the launcher writes is: stat shifts both.
	stamp := s.machineNow.Add(-time.Duration(f.AgeDays) * hoursPerDay * time.Hour)
	if err := os.Chtimes(path, stamp, stamp); err != nil {
		t.Fatal(err)
	}
}

// baseEnv is the environment every run starts with; these names are never recorded.
func (s sandbox) baseEnv() map[string]string {
	return map[string]string{
		"HOME":                filepath.Join(s.root, "home"),
		"PATH":                filepath.Join(s.root, "bin"),
		"TERM":                "xterm-256color",
		"USER":                "tester",
		"LOGNAME":             "tester",
		"LANG":                "C.UTF-8",
		"TZ":                  "UTC",
		"TMPDIR":              filepath.Join(s.root, "tmp"),
		"XDG_RUNTIME_DIR":     filepath.Join(s.root, "run"),
		"GIT_CONFIG_NOSYSTEM": "1",
		"CCY_DOCKERFILE_DIR":  filepath.Join(s.root, buildContext),
		"CCY_GPU_DEVICE_DIR":  filepath.Join(s.root, gpuDir),
		sandboxEnv:            s.root,
	}
}

// startupText stands in for ccy-startup-info.txt, which the launcher hands Claude on a launch
// with no arguments: its own text is not the launcher's behaviour, reading it and naming the
// engine in it are.
const startupText = "difftest startup info: the engine is __CONTAINER_ENGINE__.\n"

// stageBuildContext copies the checkout's Dockerfile into the sandbox's build context, as the
// play stages it, with startupText beside it, and returns the marks a rule's output may use for
// the image the Dockerfile would build: {image_version}, its claude-yolo-version label, and
// {image_hash}, the hash the launcher compares (the first 16 hex digits of its md5).
func (s sandbox) stageBuildContext(t *testing.T) imageMarks {
	t.Helper()
	dir := filepath.Join(s.root, buildContext)
	if err := os.MkdirAll(dir, dirMode); err != nil {
		t.Fatal(err)
	}
	dockerfile := readFile(t, filepath.Join(bashLauncherDir(t), "Dockerfile"))
	writeFile(t, filepath.Join(dir, "Dockerfile"), dockerfile, libMode)
	writeFile(t, filepath.Join(dir, "ccy-startup-info.txt"), startupText, libMode)
	label := imageLabel.FindStringSubmatch(dockerfile)
	if label == nil {
		t.Fatal("the Dockerfile has no claude-yolo-version label")
	}
	sum := md5.Sum([]byte(dockerfile))
	return imageMarks{version: label[1], hash: hex.EncodeToString(sum[:])[:imageHashLen]}
}

// imageMarks are the image the staged Dockerfile builds: rules name them as marks, and the
// golden writes them back as marks, so a Dockerfile change does not move a golden.
type imageMarks struct {
	version, hash string
}

func (im imageMarks) expand(s, root string) string {
	return strings.NewReplacer("{image_version}", im.version, "{image_hash}", im.hash, sandboxMark, root).Replace(s)
}

var imageLabel = regexp.MustCompile(`(?m)^LABEL claude-yolo-version="([^"]+)"`)

// composed is a scenario joined with its worlds.
type composed struct {
	files  []fileSpec
	env    map[string]string
	rules  []rule
	remote string
}

// compose joins a scenario with its worlds: files and environment in world order with the
// scenario's last, rules with the scenario's first.
func compose(t *testing.T, m matrix, sc scenario) composed {
	t.Helper()
	c := composed{env: map[string]string{}, rules: slices.Clone(sc.Rules)}
	for _, name := range sc.Worlds {
		w, ok := m.Worlds[name]
		if !ok {
			t.Fatalf("scenario %s: no world %q", sc.Name, name)
		}
		c.files = append(c.files, w.Files...)
		maps.Copy(c.env, w.Env)
		c.rules = append(c.rules, w.Rules...)
		if w.Remote != "" {
			c.remote = w.Remote
		}
	}
	c.files = append(c.files, sc.Files...)
	maps.Copy(c.env, sc.Env)
	return c
}

// writeFakeConfig writes what the fakes read: the rules, the real clock programs, the pinned
// clock and the environment names the runner sets.
func (s sandbox) writeFakeConfig(t *testing.T, rules []rule, base []string) {
	t.Helper()
	programs := map[string]string{}
	for _, name := range clockTools {
		path, err := exec.LookPath(name)
		if err != nil {
			t.Fatalf("%s is needed inside the sandbox: %v", name, err)
		}
		programs[name] = path
	}
	cfg := fakeConfig{
		Rules: rules, Real: programs, Epoch: s.epoch, Shift: s.machineNow.Unix() - s.epoch, Base: base,
	}
	raw, err := json.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(s.root, fakesFile), string(raw), logFileMode)
}

// runScenario builds the sandbox, runs the launcher in it once and collects what it did.
func runScenario(t *testing.T, m matrix, l launcher, sc scenario) outcome {
	t.Helper()
	s := newSandbox(t, m.epoch(t))
	world := compose(t, m, sc)
	s.git(t, "init", "-q", ".")
	s.git(t, "commit", "-q", "--allow-empty", "-m", "difftest")
	if world.remote != "" {
		s.git(t, "remote", "add", "origin", world.remote)
	}
	for _, f := range world.files {
		s.place(t, f)
	}
	marks := s.stageBuildContext(t)
	for i := range world.rules {
		world.rules[i].Stdout = expandTokens(marks.expand(world.rules[i].Stdout, s.root))
	}
	env := s.baseEnv()
	s.writeFakeConfig(t, world.rules, slices.Sorted(maps.Keys(env)))
	maps.Copy(env, world.env)
	args := make([]string, 0, 1+len(env)+2+len(sc.Args))
	args = append(args, "-i")
	for _, k := range slices.Sorted(maps.Keys(env)) {
		args = append(args, k+"="+env[k])
	}
	args = append(args, filepath.Join(s.root, "bin", starterName), l.path)
	args = append(args, sc.Args...)
	cwd := sc.Cwd
	if cwd == "" {
		cwd = cwdProject
	}
	c := execx.Cmd{Name: "env", Args: args, Dir: filepath.Join(s.root, cwd)}
	if sc.Stdin != "" {
		c.Stdin = strings.NewReader(sc.Stdin)
	}
	ctx, cancel := context.WithTimeout(context.Background(), runTimeout)
	defer cancel()
	res, err := (execx.OS{}).Run(ctx, c)
	if exitErr, ok := errors.AsType[*execx.ExitError](err); ok && exitErr.Signal == 0 {
		err = nil
	}
	if err != nil {
		t.Fatalf("scenario %s: the launcher did not run: %v\nstderr:\n%s", sc.Name, err, res.Stderr)
	}
	out := outcome{exit: res.ExitCode, stdout: string(res.Stdout), stderr: string(res.Stderr)}
	out.calls = readCalls(t, s.root)
	out.files = snapshot(t, s.root)
	back := make([]string, 0, 4+2*len(m.tokens))
	back = append(back, marks.hash, "{image_hash}", marks.version, "{image_version}")
	for _, name := range m.tokens {
		back = append(back, tokenValue(name), "{token:"+name+"}")
	}
	return l.normalise(out, s.root, launcherPid(t, s.root), strings.NewReplacer(back...))
}

// launcherPid matches the launcher's pid as a word.
func launcherPid(t *testing.T, root string) *regexp.Regexp {
	t.Helper()
	pid := readFile(t, filepath.Join(root, pidFile))
	return regexp.MustCompile(`\b` + regexp.QuoteMeta(pid) + `\b`)
}

func readCalls(t *testing.T, root string) []call {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(root, callsFile))
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		t.Fatal(err)
	}
	var calls []call
	for line := range strings.SplitSeq(strings.TrimSuffix(string(raw), "\n"), "\n") {
		var c call
		if err := json.Unmarshal([]byte(line), &c); err != nil {
			t.Fatalf("calls.jsonl: %v: %q", err, line)
		}
		calls = append(calls, c)
	}
	return calls
}

// snapshot lists every directory, file and link the run left in the sandbox's own directories
// (the repository's .git aside), with modes and file contents.
func snapshot(t *testing.T, root string) []string {
	t.Helper()
	var lines []string
	for _, top := range snapshotDirs {
		err := filepath.WalkDir(filepath.Join(root, top), func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			rel, err := filepath.Rel(root, path)
			if err != nil {
				return err
			}
			if rel == filepath.Join(cwdProject, ".git") {
				return filepath.SkipDir
			}
			if rel == top {
				return nil
			}
			return describe(&lines, path, filepath.ToSlash(rel), d)
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	return lines
}

func describe(lines *[]string, path, rel string, d fs.DirEntry) error {
	info, err := d.Info()
	if err != nil {
		return err
	}
	switch {
	case d.Type()&fs.ModeSymlink != 0:
		target, err := os.Readlink(path)
		if err != nil {
			return err
		}
		*lines = append(*lines, fmt.Sprintf("link %s -> %s", rel, target))
	case d.IsDir():
		*lines = append(*lines, fmt.Sprintf("dir  %s %04o", rel, info.Mode().Perm()))
	default:
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		*lines = append(*lines, fmt.Sprintf("file %s %04o", rel, info.Mode().Perm()))
		*lines = append(*lines, contentLines(raw)...)
	}
	return nil
}

func contentLines(raw []byte) []string {
	if !utf8.Valid(raw) || bytes.IndexByte(raw, 0) >= 0 {
		sum := sha256.Sum256(raw)
		return []string{"     | (binary, sha256 " + hex.EncodeToString(sum[:]) + ")"}
	}
	text := string(raw)
	if text != "" && !strings.HasSuffix(text, "\n") {
		text += "\n(no newline at end)"
	}
	var out []string
	for line := range strings.SplitSeq(strings.TrimSuffix(text, "\n"), "\n") {
		out = append(out, "     | "+line)
	}
	return out
}

// normalise writes the sandbox, the tokens, the image, the launcher's pid, its version and its
// hash as their marks, and the volatile parts as fixed text.
func (l launcher) normalise(o outcome, root string, pid *regexp.Regexp, marks *strings.Replacer) outcome {
	fix := func(s string) string {
		s = marks.Replace(strings.ReplaceAll(s, root, sandboxMark))
		s = pid.ReplaceAllString(s, pidMark)
		if l.hash != "" {
			s = strings.ReplaceAll(s, l.hash, hashMark)
		}
		if l.version != "" {
			s = strings.ReplaceAll(s, l.version, versionMark)
		}
		for _, v := range volatile {
			s = v.re.ReplaceAllString(s, v.with)
		}
		return s
	}
	o.stdout, o.stderr = fix(o.stdout), fix(o.stderr)
	for i := range o.calls {
		for j := range o.calls[i].Args {
			o.calls[i].Args[j] = fix(o.calls[i].Args[j])
		}
	}
	for i := range o.files {
		o.files[i] = fix(o.files[i])
	}
	return o
}

// volatile are the parts of a run that differ every time by design, each written as a fixed
// mark: a name mktemp chose, and $RANDOM.
var volatile = []struct {
	re   *regexp.Regexp
	with string
}{
	{regexp.MustCompile(`(/tmp/claude-yolo-)[A-Za-z0-9]{6}\b`), "${1}XXXXXX"},
	{regexp.MustCompile(`(/tmp/ccy-[a-z-]+-)[A-Za-z0-9]{6}(\.log|\.txt)`), "${1}XXXXXX${2}"},
	{regexp.MustCompile(`(launch-[0-9]+-\{pid\}-)[0-9]+`), "${1}{random}"},
	{regexp.MustCompile(`(\{sandbox\}/tmp/tmp\.)[A-Za-z0-9]{10}\b`), "${1}XXXXXXXXXX"},
}

// render is the golden text of one outcome.
func (o outcome) render() string {
	var b strings.Builder
	b.WriteString("=== exit " + strconv.Itoa(o.exit) + "\n")
	section := func(name, text string) {
		b.WriteString("=== " + name + "\n" + text)
		if text != "" && !strings.HasSuffix(text, "\n") {
			b.WriteString("\n(no newline at end)\n")
		}
	}
	section("stdout", o.stdout)
	section("stderr", o.stderr)
	b.WriteString("=== calls\n")
	var before []string
	for _, c := range o.calls {
		b.WriteString(shellword.Join(append([]string{c.Tool}, c.Args...)) + "\n")
		switch {
		case len(c.Env) == 0:
		case slices.Equal(c.Env, before):
			b.WriteString("    env: (as above)\n")
		default:
			b.WriteString("    env: " + strings.Join(c.Env, " ") + "\n")
		}
		before = c.Env
	}
	b.WriteString("=== files\n")
	for _, f := range o.files {
		b.WriteString(f + "\n")
	}
	return b.String()
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

func writeFile(t *testing.T, path, text string, mode fs.FileMode) {
	t.Helper()
	if err := os.WriteFile(path, []byte(text), mode); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, mode); err != nil {
		t.Fatal(err)
	}
}
