package ansible

import (
	"bytes"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
)

// outcome is what one Main run printed and wrote.
type outcome struct {
	out, errOut, report string
	code                ExitCode
}

// runIn runs Main over the real file system with the report in a fresh temporary directory.
func runIn(t *testing.T, cwd string, args ...string) outcome {
	t.Helper()
	jsonPath := filepath.Join(t.TempDir(), "out.json")
	var stdout, stderr bytes.Buffer
	env := Env{
		Getenv: func(k string) string {
			if k == "QA_JSON_OUT" {
				return jsonPath
			}
			return ""
		},
		Stdout:    &stdout,
		Stderr:    &stderr,
		Cwd:       cwd,
		Open:      os.DirFS,
		Ignored:   noneIgnored,
		WriteFile: func(path string, data []byte) error { return os.WriteFile(path, data, 0o600) },
	}
	res := outcome{code: Main(args, env), out: stdout.String(), errOut: stderr.String()}
	body, err := os.ReadFile(jsonPath)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		t.Fatal(err)
	}
	res.report = string(body)
	return res
}

// noneIgnored is a work tree with no gitignored file.
func noneIgnored(string) ([]string, error) { return nil, nil }

// runWith runs Main over a real tree with the given gitignored list (or error) and stdout.
func runWith(t *testing.T, root string, ignored func(string) ([]string, error), stdout io.Writer) (ExitCode, string) {
	t.Helper()
	var stderr bytes.Buffer
	env := Env{
		Getenv:    func(string) string { return filepath.Join(t.TempDir(), "out.json") },
		Stdout:    stdout,
		Stderr:    &stderr,
		Cwd:       root,
		Open:      os.DirFS,
		Ignored:   ignored,
		WriteFile: func(path string, data []byte) error { return os.WriteFile(path, data, 0o600) },
	}
	return Main([]string{"--root", root}, env), stderr.String()
}

func TestMainLeavesGitignoredFilesUnread(t *testing.T) {
	files := cleanTree()
	files["environment/host_vars/leak.yml"] = "  failed_when: false\n"
	root := writeTree(t, files)
	chmodX(t, root, "playbooks/a.yml")
	symlink(t, root, "environment/host_vars/localhost.yml", "../../nope/localhost.yml")
	ignored := []string{"environment/host_vars/localhost.yml", "environment/host_vars/leak.yml"}
	code, stderr := runWith(t, root, func(string) ([]string, error) { return ignored, nil }, io.Discard)
	if code != ExitOK || stderr != "" {
		t.Fatalf("an ignored dangling link and an ignored violation counted: exit %d, %q", code, stderr)
	}
	code, stderr = runWith(t, root, noneIgnored, io.Discard)
	if code != ExitCannotRun || !strings.HasPrefix(stderr, "ERROR: ") {
		t.Fatalf("nothing ignored: exit %d, %q", code, stderr)
	}
}

func TestMainSymlinkedDirectoryInScopeCannotRun(t *testing.T) {
	files := cleanTree()
	files["elsewhere/bad.yml"] = "  failed_when: false\n"
	root := writeTree(t, files)
	chmodX(t, root, "playbooks/a.yml")
	symlink(t, root, "tasks/shared", "../elsewhere")
	code, stderr := runWith(t, root, noneIgnored, io.Discard)
	if code != ExitCannotRun || !strings.HasPrefix(stderr, "ERROR: ") || !strings.Contains(stderr, "tasks/shared") {
		t.Fatalf("exit %d, %q", code, stderr)
	}
}

func TestMainGitIgnoredListFailureCannotRun(t *testing.T) {
	root := writeTree(t, cleanTree())
	failing := func(string) ([]string, error) { return nil, errors.New("not a git work tree") }
	code, stderr := runWith(t, root, failing, io.Discard)
	if code != ExitCannotRun || !strings.Contains(stderr, "ERROR: cannot list the gitignored files") || !strings.Contains(stderr, "not a git work tree") {
		t.Fatalf("exit %d, %q", code, stderr)
	}
}

func TestMainStdoutWriteFailureIsAnErrorLine(t *testing.T) {
	root := writeTree(t, cleanTree())
	chmodX(t, root, "playbooks/a.yml")
	full, err := os.OpenFile("/dev/full", os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := full.Close(); err != nil {
			t.Error(err)
		}
	})
	code, stderr := runWith(t, root, noneIgnored, full)
	if code != ExitCannotRun || !strings.HasPrefix(stderr, "ERROR: cannot write to stdout: ") {
		t.Fatalf("exit %d, %q", code, stderr)
	}
}

func cleanTree() map[string]string {
	return map[string]string{
		"playbooks/a.yml": playbookShebang + "\n---\n- hosts: all\n  vars:\n    scope: general\n",
		"tasks/t.yml":     "k: v\n",
	}
}

func chmodX(t *testing.T, root, rel string) {
	t.Helper()
	if err := os.Chmod(filepath.Join(root, rel), 0o700); err != nil {
		t.Fatal(err)
	}
}

func TestMainCleanTreePasses(t *testing.T) {
	root := writeTree(t, cleanTree())
	chmodX(t, root, "playbooks/a.yml")
	res := runIn(t, root)
	if res.code != ExitOK || res.errOut != "" || !strings.HasPrefix(res.out, "✓ ansible: ") || !strings.Contains(res.out, "1 playbook(s)") {
		t.Fatalf("%+v", res)
	}
	if !strings.Contains(res.report, `"status": "pass"`) {
		t.Fatalf("report %s", res.report)
	}
}

func TestMainReportsEveryCheckInOrder(t *testing.T) {
	files := cleanTree()
	files["tasks/ff.yml"] = "  failed_when: false\n"
	files["vars/sr.yml"] = "foo: \"{{ foo | default(1) }}\"\n"
	files["environment/e.yml"] = "x: ansible_env\n"
	files["roles/r/rd.yml"] = "p: /run/user/1000\n"
	files["tasks/rb.yml"] = "v: >-\n  {{ s | regex_search('a(b)', '\\\\1') }}\n"
	files["playbooks/b.yml"] = playbookShebang + "\n- hosts: all\n"
	root := writeTree(t, files)
	chmodX(t, root, "playbooks/a.yml")
	res := runIn(t, root)
	if res.code != ExitFailed {
		t.Fatalf("%+v", res)
	}
	var order []string
	for line := range strings.SplitSeq(res.out, "\n") {
		if i := strings.Index(line, "):"); strings.HasPrefix(line, "  ERROR (") && i > 0 {
			order = append(order, line[len("  ERROR ("):i])
		}
	}
	want := "fail-fast hygiene self-ref var scope deprecated fact var guessed uid in a runtime path regex-backref-escape"
	if got := strings.Join(order, " "); got != want {
		t.Fatalf("order %q want %q\n%s", got, want, res.out)
	}
	if !strings.Contains(res.out, "✗ ansible: 7 violation(s) — 1 fail-fast, 1 hygiene, 1 self-ref var, 1 scope/guard, 2 deprecated fact var, 1 regex backref\n") {
		t.Fatalf("summary:\n%s", res.out)
	}
}

func TestMainWithoutSearchDirectoriesCannotRun(t *testing.T) {
	root := t.TempDir()
	res := runIn(t, root)
	want := "ERROR: none of the expected Ansible dirs (playbooks/ tasks/ vars/ environment/ roles/) exist under " + root + "\n"
	if res.code != ExitCannotRun || res.errOut != want || res.report != "" {
		t.Fatalf("%+v", res)
	}
}

func TestMainWithoutPlaybooksDirectoryCannotRun(t *testing.T) {
	root := writeTree(t, map[string]string{"tasks/t.yml": "k: v\n"})
	res := runIn(t, root)
	if res.code != ExitCannotRun || !strings.Contains(res.errOut, "playbooks") || res.report != "" {
		t.Fatalf("%+v", res)
	}
}

func TestMainUsage(t *testing.T) {
	root := writeTree(t, cleanTree())
	help := runIn(t, root, "--help")
	if help.code != ExitOK || !strings.HasPrefix(help.out, "usage: qa-ansible") || help.errOut != "" {
		t.Fatalf("help: %+v", help)
	}
	typo := runIn(t, root, "--typo")
	if typo.code != ExitCannotRun || typo.out != "" || !strings.Contains(typo.errOut, "unknown argument '--typo'") || typo.report != "" {
		t.Fatalf("typo: %+v", typo)
	}
	bare := runIn(t, root, "--root")
	if bare.code != ExitCannotRun || !strings.Contains(bare.errOut, "--root needs a directory") {
		t.Fatalf("--root: %+v", bare)
	}
}

func TestMainRootFromScriptsDirectory(t *testing.T) {
	root := writeTree(t, cleanTree())
	chmodX(t, root, "playbooks/a.yml")
	res := runIn(t, t.TempDir(), "--scripts", filepath.Join(root, "scripts"))
	if res.code != ExitOK || !strings.HasPrefix(res.out, "✓ ansible: ") {
		t.Fatalf("%+v", res)
	}
}

func TestReportPath(t *testing.T) {
	if got := reportPath(func(string) string { return "" }); got != "/tmp/qa-ansible-results.json" {
		t.Fatalf("default %q", got)
	}
	if got := reportPath(func(string) string { return "/x/y.json" }); got != "/x/y.json" {
		t.Fatalf("override %q", got)
	}
}

func TestMainUnwritableReportCannotRun(t *testing.T) {
	root := writeTree(t, cleanTree())
	chmodX(t, root, "playbooks/a.yml")
	var stdout, stderr bytes.Buffer
	env := Env{
		Getenv:    func(string) string { return "/x/out.json" },
		Stdout:    &stdout,
		Stderr:    &stderr,
		Cwd:       root,
		Open:      os.DirFS,
		Ignored:   noneIgnored,
		WriteFile: func(string, []byte) error { return errors.New("read-only") },
	}
	if code := Main([]string{"--root", root}, env); code != ExitCannotRun {
		t.Fatalf("code %d", code)
	}
	if !strings.Contains(stderr.String(), "ERROR: cannot write the report /x/out.json: read-only") {
		t.Fatalf("stderr %q", stderr.String())
	}
}

// failingFS reads every file but the named one.
type failingFS struct {
	fs.FS

	bad string
}

func (f failingFS) Open(name string) (fs.File, error) {
	if name == f.bad {
		return nil, &fs.PathError{Op: "open", Path: name, Err: fs.ErrPermission}
	}
	return f.FS.Open(name)
}

func TestScanUnreadableFileCannotRunInEveryCheck(t *testing.T) {
	mem := fstest.MapFS{
		"playbooks/a.yml": &fstest.MapFile{Data: []byte("x"), Mode: 0o755},
		"tasks/t.yml":     &fstest.MapFile{Data: []byte("x")},
	}
	for _, bad := range []string{"tasks/t.yml", "playbooks/a.yml"} {
		_, err := Scan(failingFS{FS: mem, bad: bad}, "/r")
		if err == nil || !strings.Contains(err.Error(), bad) {
			t.Errorf("%s: err %v", bad, err)
		}
	}
}

func TestScanBinaryFileIsText(t *testing.T) {
	mem := fstest.MapFS{
		"playbooks/a.yml": &fstest.MapFile{Data: []byte("- hosts: all\n"), Mode: 0o755},
		"tasks/b.yml":     &fstest.MapFile{Data: []byte("  failed_when: false\x00\n")},
	}
	f, err := Scan(mem, "/r")
	if err != nil {
		t.Fatal(err)
	}
	if len(f.FailFast) != 1 || !strings.HasPrefix(f.FailFast[0], "tasks/b.yml:1:") {
		t.Fatalf("got %q", f.FailFast)
	}
}

func TestScanOrdersFilesByName(t *testing.T) {
	mem := fstest.MapFS{
		"playbooks/a.yml": &fstest.MapFile{Data: []byte("- hosts: all\n"), Mode: 0o755},
		"tasks/z.yml":     &fstest.MapFile{Data: []byte("  failed_when: no\n")},
		"tasks/a.yml":     &fstest.MapFile{Data: []byte("  failed_when: no\n")},
		"tasks/m/b.yml":   &fstest.MapFile{Data: []byte("  failed_when: no\n")},
	}
	f, err := Scan(mem, "/r")
	if err != nil {
		t.Fatal(err)
	}
	got := make([]string, 0, len(f.FailFast))
	for _, v := range f.FailFast {
		name, _, hasColon := strings.Cut(v, ":")
		if !hasColon {
			t.Fatalf("no colon in %q", v)
		}
		got = append(got, name)
	}
	if strings.Join(got, " ") != "tasks/a.yml tasks/m/b.yml tasks/z.yml" {
		t.Fatalf("got %q", got)
	}
}

func TestScanWithoutAnyPlaybookCannotRun(t *testing.T) {
	mem := fstest.MapFS{
		"playbooks/notes.yml": &fstest.MapFile{Data: []byte("k: v\n")},
		"tasks/t.yml":         &fstest.MapFile{Data: []byte("k: v\n")},
	}
	_, err := Scan(mem, "/r")
	if err == nil || !strings.Contains(err.Error(), "no playbook") {
		t.Fatalf("err %v", err)
	}
}
