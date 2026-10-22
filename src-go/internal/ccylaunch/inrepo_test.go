package ccylaunch

import (
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

// The texts the prelude writes are the bash library's, byte for byte: the bash still writes
// them under CCY_LEGACY and on every launch, so a change to one that is not made to the other
// fails here.
func TestPreludeTextsAreTheLibrarysText(t *testing.T) {
	lib := checkoutFile(t, "files/var/local/claude-yolo/lib/common.bash")
	gitignore := between(t, lib, "local expected_gitignore=\"", "\"\n")
	if gitignoreText != gitignore+"\n" {
		t.Fatalf("ccy-gitignore.txt differs from common.bash:\n%s", firstDifference(gitignore+"\n", gitignoreText))
	}
	version := between(t, lib, "\nCCY_ENV_LOCAL_DIST_VERSION=", "\n")
	if version != strconv.Itoa(distVersion) {
		t.Fatalf("common.bash's dist is version %s, the front's %d", version, distVersion)
	}
	dist := strings.ReplaceAll(between(t, lib, "ccy_env_local_dist_text() {\n    cat <<EOF\n", "\nEOF\n"), "${CCY_ENV_LOCAL_DIST_VERSION}", version)
	if distText != dist+"\n" {
		t.Fatalf("ccy-env-local-dist.txt differs from common.bash:\n%s", firstDifference(dist+"\n", distText))
	}
	tokens := checkoutFile(t, "files/var/local/claude-yolo/lib/token-management.bash")
	body := between(t, tokens, "    cat <<'BODY'\n", "BODY\n}\n")
	if exportBody != body {
		t.Fatalf("export-body.txt differs from token-management.bash:\n%s", firstDifference(body, exportBody))
	}
}

// between is the text between the first open and the next close after it.
func between(t *testing.T, text, open, closing string) string {
	t.Helper()
	start := strings.Index(text, open)
	if start < 0 {
		t.Fatalf("no %q", open)
	}
	rest := text[start+len(open):]
	end := strings.Index(rest, closing)
	if end < 0 {
		t.Fatalf("%q does not end with %q", open, closing)
	}
	return rest[:end]
}

const (
	creatingGitignore = "✓ Creating .claude/ccy/.gitignore for security\n"
	writingDist       = "✓ Writing .claude/ccy/ccy.env.local.dist (version 3); commit it\n"
	preventSaid       = "✓ CCY disabled for this project.\n  Written 'never' to .claude/ccy/allowed-hostnames\n\n" +
		"To re-enable CCY, remove or edit that file:\n  rm .claude/ccy/allowed-hostnames\n"
)

// repoRun is a front run at the root of a fresh checkout (a .git directory, nothing tracked
// under .claude/ccy), with its own HOME and token pool.
func repoRun(t *testing.T, files []string, args ...string) *run {
	t.Helper()
	r := tokenRun(t, install(t), files, args...)
	r.front.Dir = t.TempDir()
	if err := os.Mkdir(r.at(".git"), 0o700); err != nil {
		t.Fatal(err)
	}
	r.tracked()
	return r
}

// at is a path in the checkout.
func (r *run) at(rel string) string { return filepath.Join(r.front.Dir, filepath.FromSlash(rel)) }

// tracked makes git list these files as tracked under .claude/ccy.
func (r *run) tracked(files ...string) {
	var out strings.Builder
	for _, f := range files {
		out.WriteString(f + "\x00")
	}
	r.runner.On([]string{"git", "ls-files", "-z", "--", ".claude/ccy"}, execx.Result{Stdout: []byte(out.String())})
}

func (r *run) write(t *testing.T, rel, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(r.at(rel)), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(r.at(rel), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func (r *run) read(t *testing.T, rel string) string {
	t.Helper()
	raw, err := os.ReadFile(r.at(rel))
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

// current makes the checkout one a launch has already set up: nothing for the prelude to say.
func (r *run) current(t *testing.T) {
	t.Helper()
	r.write(t, gitignoreRel, gitignoreText)
	r.write(t, distRel, distText)
	r.tracked(gitignoreRel, distRel)
}

// F12: --prevent in a fresh checkout: the prelude's .gitignore and dist, the token
// directories, then never.
func TestPreventIsAnswered(t *testing.T) {
	r := repoRun(t, nil, "--prevent")
	r.answered(t, term.ExitOK)
	if want := creatingGitignore + preventSaid; r.stdout.String() != want {
		t.Fatalf("stdout %q, want %q", r.stdout, want)
	}
	if r.stderr.String() != writingDist {
		t.Fatalf("stderr %q, want %q", r.stderr, writingDist)
	}
	for rel, want := range map[string]string{gitignoreRel: gitignoreText, distRel: distText, allowedRel: "never\n"} {
		if got := r.read(t, rel); got != want {
			t.Errorf("%s holds %q, want %q", rel, got, want)
		}
	}
	for _, dir := range []string{"ccy", "ccy/tokens", "ccy/projects"} {
		info, err := os.Stat(filepath.Join(r.home, ".claude-tokens", dir))
		if err != nil || !info.IsDir() || info.Mode().Perm() != 0o700 {
			t.Errorf("%s: %v %v", dir, info, err)
		}
	}
	calls := r.runner.Calls()
	if len(calls) != 1 || calls[0].Dir != r.front.Dir {
		t.Fatalf("git calls %+v, want one ls-files in the checkout", calls)
	}
}

// A checkout a launch has set up, with the files ccy says to commit tracked: the prelude says
// nothing and changes nothing.
func TestPreludeLeavesACurrentCheckoutAlone(t *testing.T) {
	r := repoRun(t, nil, "--prevent")
	r.current(t)
	r.answered(t, term.ExitOK)
	if r.stdout.String() != preventSaid || r.stderr.Len() != 0 {
		t.Fatalf("stdout %q stderr %q", r.stdout, r.stderr)
	}
	if r.read(t, gitignoreRel) != gitignoreText || r.read(t, distRel) != distText {
		t.Fatal("the prelude rewrote a current file")
	}
}

// The .gitignore: rewritten without its ignore-everything line, and each missing exception
// appended, as the bash appends it (onto an unterminated last line too).
func TestPreludeRepairsTheGitignore(t *testing.T) {
	cases := map[string]struct{ before, after, said string }{
		"no ignore-everything line": {
			"!Dockerfile\n", gitignoreText,
			"⚠  Updating .claude/ccy/.gitignore (was missing protection rules)\n",
		},
		"an old one without the exceptions": {
			"*\n!.gitignore\n", "*\n!.gitignore\n!mounts\n!ccy.env.local.dist\n",
			"✓ Adding the mounts exception to .claude/ccy/.gitignore\n✓ Adding the ccy.env.local.dist exception to .claude/ccy/.gitignore\n",
		},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			r := repoRun(t, nil, "--prevent")
			r.current(t)
			r.write(t, gitignoreRel, c.before)
			r.answered(t, term.ExitOK)
			if want := c.said + preventSaid; r.stdout.String() != want {
				t.Fatalf("stdout %q, want %q", r.stdout, want)
			}
			if got := r.read(t, gitignoreRel); got != c.after {
				t.Fatalf(".gitignore %q, want %q", got, c.after)
			}
		})
	}
}

// An unterminated last line, as grep -x and >> see it: "!mounts" is a line, so only the dist
// exception is appended, and it lands on that same line.
func TestPreludeAppendsOntoAnUnterminatedLine(t *testing.T) {
	r := repoRun(t, nil, "--prevent")
	r.current(t)
	r.write(t, gitignoreRel, "*\n!mounts")
	r.answered(t, term.ExitOK)
	if got, want := r.read(t, gitignoreRel), "*\n!mounts!ccy.env.local.dist\n"; got != want {
		t.Fatalf(".gitignore %q, want %q", got, want)
	}
	if want := "✓ Adding the ccy.env.local.dist exception to .claude/ccy/.gitignore\n" + preventSaid; r.stdout.String() != want {
		t.Fatalf("stdout %q, want %q", r.stdout, want)
	}
}

// The dist and ccy.env.local: a newer dist left alone, an edited one rewritten, a local file
// with no or an older based-on line warned of.
func TestPreludeDist(t *testing.T) {
	newer := "# ccy.env.local.dist version 4\nnew\n"
	cases := map[string]struct {
		dist, local, said, after string
	}{
		"a newer ccy's dist": {newer, "", "⚠  .claude/ccy/ccy.env.local.dist (version 4) was written by a newer ccy than this one (version 3); left as it is. Update ccy here.\n", newer},
		"an edited dist":     {distText + "# mine\n", "", "⚠  Updating .claude/ccy/ccy.env.local.dist to version 3; commit it\n", distText},
		"trailing newlines":  {distText + "\n\n", "", "", distText + "\n\n"},
		"a local file with no based-on line": {
			distText, "X=1\n",
			"⚠  .claude/ccy/ccy.env.local has no '# based on ccy.env.local.dist version N' line.\n" +
				"   Compare it with ccy.env.local.dist and add the line where it is placed (its IaC).\n", distText,
		},
		"a local file based on an older dist": {
			distText, "# based on ccy.env.local.dist version 2\n",
			"⚠  .claude/ccy/ccy.env.local is based on ccy.env.local.dist version 2; the dist is now version 3.\n" +
				"   Review: diff .claude/ccy/ccy.env.local.dist .claude/ccy/ccy.env.local\n" +
				"   Update it where it is placed (its IaC), not in the checkout.\n", distText,
		},
		"a local file based on this dist": {distText, "x\n# based on ccy.env.local.dist version 3\n", "", distText},
		"a local file behind a newer dist": {
			newer, "# based on ccy.env.local.dist version 3\n",
			"⚠  .claude/ccy/ccy.env.local.dist (version 4) was written by a newer ccy than this one (version 3); left as it is. Update ccy here.\n" +
				"⚠  .claude/ccy/ccy.env.local is based on ccy.env.local.dist version 3; the dist is now version 4.\n" +
				"   Review: diff .claude/ccy/ccy.env.local.dist .claude/ccy/ccy.env.local\n" +
				"   Update it where it is placed (its IaC), not in the checkout.\n", newer,
		},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			r := repoRun(t, nil, "--prevent")
			r.current(t)
			r.write(t, distRel, c.dist)
			if c.local != "" {
				r.write(t, localRel, c.local)
			}
			r.answered(t, term.ExitOK)
			if r.stderr.String() != c.said || r.stdout.String() != preventSaid {
				t.Fatalf("stderr %q, want %q; stdout %q", r.stderr, c.said, r.stdout)
			}
			if got := r.read(t, distRel); got != c.after {
				t.Fatalf("the dist holds %q, want %q", got, c.after)
			}
		})
	}
}

// D10: the prelude's warnings are yellow only on a terminal, and never with NO_COLOR.
func TestPreludeColourOnlyOnATerminal(t *testing.T) {
	for name, c := range map[string]struct {
		terminal bool
		env      string
		yellow   bool
	}{"a terminal": {true, "", true}, "NO_COLOR": {true, "NO_COLOR=1", false}, "a pipe": {false, "", false}} {
		t.Run(name, func(t *testing.T) {
			r := repoRun(t, nil, "--prevent")
			r.current(t)
			r.write(t, gitignoreRel, "x\n")
			r.write(t, distRel, "x\n")
			r.front.IsTerminal = func(any) bool { return c.terminal }
			if c.env != "" {
				r.env(c.env)
			}
			r.answered(t, term.ExitOK)
			out := "⚠  Updating .claude/ccy/.gitignore (was missing protection rules)"
			errLine := "⚠  Updating .claude/ccy/ccy.env.local.dist to version 3; commit it"
			if c.yellow {
				out, errLine = "\x1b[0;33m"+out+"\x1b[0m", "\x1b[0;33m"+errLine+"\x1b[0m"
			}
			if !strings.HasPrefix(r.stdout.String(), out+"\n") || r.stderr.String() != errLine+"\n" {
				t.Fatalf("stdout %q stderr %q", r.stdout, r.stderr)
			}
		})
	}
}

// F12's F4: wherever the bash's prelude could do or say otherwise (it asks, refuses, warns at
// length, or reads a file another way), the bash answers, and the front writes nothing first.
func TestInRepoHandsOver(t *testing.T) {
	file := func(rel, content string) func(t *testing.T, r *run) {
		return func(t *testing.T, r *run) { t.Helper(); r.write(t, rel, content) }
	}
	dir := func(rel string) func(t *testing.T, r *run) {
		return func(t *testing.T, r *run) {
			t.Helper()
			if err := os.MkdirAll(r.at(rel), 0o700); err != nil {
				t.Fatal(err)
			}
		}
	}
	env := func(kv string) func(t *testing.T, r *run) {
		return func(_ *testing.T, r *run) { r.setenv(kv) }
	}
	cases := map[string]func(t *testing.T, r *run){
		"not at a repository's root": func(t *testing.T, r *run) {
			t.Helper()
			if err := os.Remove(r.at(".git")); err != nil {
				t.Fatal(err)
			}
		},
		".git a file":                      func(t *testing.T, r *run) { t.Helper(); r.front.Dir = t.TempDir(); r.write(t, ".git", "gitdir: x\n") },
		"a restart":                        env("CCY_RESTART_RELAUNCH=0f9c2b3e-1111-4222-8333-444455556666"),
		"CCY_MAX_AGE":                      env("CCY_MAX_AGE=4h"),
		"a carried deadline":               env("CCY_RELAUNCH_DEADLINE_EPOCH=1894712400"),
		".claude a file":                   file(".claude", "x"),
		".claude/ccy a file":               file(".claude/ccy", "x"),
		"a .gitignore that is a directory": dir(gitignoreRel),
		"a .gitignore holding a NUL":       file(gitignoreRel, "*\x00\n"),
		"a dist that is a directory":       dir(distRel),
		"a dist version too long":          file(distRel, "# ccy.env.local.dist version 9999999999999999999\n"),
		"a local file that is a directory": dir(localRel),
		"a based-on version too long":      file(localRel, "# based on ccy.env.local.dist version 9999999999999999999\n"),
		"old session directories":          dir(".claude/ccy/sessions"),
		"a hostname restriction":           file(allowedRel, "*\n"),
		"a custom Dockerfile":              file(".claude/ccy/Dockerfile", "FROM x\n"),
		"git cannot answer": func(_ *testing.T, r *run) {
			r.runner.On([]string{"git", "ls-files"}, execx.Result{Stderr: []byte("fatal: detected dubious ownership"), ExitCode: 128})
		},
		"session data tracked": func(_ *testing.T, r *run) { r.tracked(".claude/ccy/.gitignore", ".claude/ccy/.last-launch.conf") },
		"a relative HOME":      env("HOME=home"),
		"an unclean HOME":      func(_ *testing.T, r *run) { r.setenv("HOME=" + r.home + "/") },
		"a projects directory that is a file": func(t *testing.T, r *run) {
			t.Helper()
			if err := os.WriteFile(filepath.Join(r.home, ".claude-tokens", "ccy", "projects"), nil, 0o600); err != nil {
				t.Fatal(err)
			}
		},
		"git not on PATH": func(_ *testing.T, r *run) { r.missing("git") },
		"another version": func(_ *testing.T, r *run) { r.front.Version = "0.0.1" },
	}
	for name, change := range cases {
		for _, args := range [][]string{{"--prevent"}, {"--export-token", "work"}} {
			t.Run(name+" "+args[0], func(t *testing.T) {
				r := repoRun(t, []string{"work.2030-06-01.token"}, args...)
				change(t, r)
				r.handsOverUntouched(t)
			})
		}
	}
}

// missing takes tool off PATH.
func (r *run) missing(tool string) {
	r.front.LookPath = func(name string) (string, error) {
		if name == tool {
			return "", fs.ErrNotExist
		}
		return "/usr/bin/" + name, nil
	}
}

// handsOverUntouched runs the front and asserts it handed over having printed nothing and
// changed nothing in the checkout or HOME.
func (r *run) handsOverUntouched(t *testing.T) {
	t.Helper()
	before := snapshot(t, r.front.Dir) + snapshot(t, r.home)
	r.start(t)
	if len(r.execer.Execs()) != 1 {
		t.Fatalf("answered: stdout %q stderr %q", r.stdout, r.stderr)
	}
	if r.stdout.Len() != 0 || r.stderr.Len() != 0 {
		t.Fatalf("printed stdout %q stderr %q before handing over", r.stdout, r.stderr)
	}
	if after := snapshot(t, r.front.Dir) + snapshot(t, r.home); after != before {
		t.Fatalf("changed the tree before handing over:\n%s\nwas:\n%s", after, before)
	}
}

// snapshot is every path under root with its mode and size.
func snapshot(t *testing.T, root string) string {
	t.Helper()
	var lines []string
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		lines = append(lines, path+" "+info.Mode().String()+" "+strconv.FormatInt(info.Size(), 10))
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	slices.Sort(lines)
	return strings.Join(lines, "\n") + "\n"
}

// Files ccy says may be tracked (and the docs) do not stop the front.
func TestInRepoAnswersWithTheSafeFilesTracked(t *testing.T) {
	r := repoRun(t, nil, "--prevent")
	r.current(t)
	r.tracked(".claude/ccy/.gitignore", ".claude/ccy/ccy.env", ".claude/ccy/ccy.env.local.dist", ".claude/ccy/mounts",
		".claude/ccy/claude-supervise.py", ".claude/ccy/CLAUDE.md", ".claude/ccy/README.md", ".claude/ccy/allowed-hostnames")
	r.answered(t, term.ExitOK)
}

// D11: a write the prelude cannot make ends the run, naming it. Nobody, root included, makes a
// directory in /proc.
func TestInRepoAFailedWriteFails(t *testing.T) {
	r := repoRun(t, nil, "--prevent")
	r.current(t)
	r.setenv("HOME=/proc/self")
	r.answered(t, term.ExitFailure)
	if want := "ccy: mkdir /proc/self/.claude-tokens"; !strings.HasPrefix(r.stderr.String(), want) || r.stdout.Len() != 0 {
		t.Fatalf("stdout %q stderr %q, want it to start %q", r.stdout, r.stderr, want)
	}
}
