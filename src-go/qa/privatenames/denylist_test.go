package privatenames

import (
	"io/fs"
	"regexp"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/qa/internal/proc"
)

// denyOut is where the denylist tests write: outside both work trees the fake git lists.
const denyOut = "/secrets/denylist"

// worktrees is `git worktree list --porcelain` for a checkout with a second work tree.
const worktrees = "worktree /repo\nHEAD aaaa\nbranch refs/heads/main\n\nworktree /work/other\nHEAD bbbb\ndetached\n\n"

// create is the exclusive create the denylist mode is given: an existing path is fs.ErrExist.
func (f files) create(path string, data []byte) error {
	if _, ok := f[path]; ok {
		return fs.ErrExist
	}
	f[path] = string(data)
	return nil
}

// denyRig is a run of the denylist mode over the everyday names, the given list and the given files.
func denyRig(fl files) rig {
	r := newRig(true, map[string]proc.Result{
		"pingbus status":                {Stdout: everydayStatus},
		"git worktree list --porcelain": {Stdout: worktrees},
	}, fl, "")
	r.env.CreateFile = fl.create
	r.env.EvalSymlinks = func(p string) (string, error) { return p, nil }
	return r
}

// patterns are the ERE lines of a denylist file, without its comments and blank lines.
func patterns(t *testing.T, text string) []string {
	t.Helper()
	var out []string
	for line := range strings.SplitSeq(text, "\n") {
		if line != "" && !strings.HasPrefix(line, "#") {
			out = append(out, line)
		}
	}
	return out
}

// ereMatch is grep -E -i: the ERE compiled with POSIX semantics, both sides folded to lower case
// (the ERE's own letters are the term's, and its classes are spelled in lower case).
func ereMatch(t *testing.T, ere, line string) bool {
	t.Helper()
	re, err := regexp.CompilePOSIX(strings.ToLower(ere))
	if err != nil {
		t.Fatalf("an ERE line does not compile: %v", err)
	}
	return re.MatchString(strings.ToLower(line))
}

func TestDenylistWritesExactlyTheBlockingTerms(t *testing.T) {
	fl := files{"/repo/" + AllowlistPath: "public_tokens:\n  - harbour-tools.2+local.host\n", listFile: "agent-bus member\tquokka-yard\n"}
	r := denyRig(fl)
	if code := r.run("denylist", denyOut); code != ExitOK {
		t.Fatalf("exit %d: %s", code, r.text())
	}
	got := patterns(t, fl[denyOut])
	want := []string{"abc123def456", "harbour-tools", "harbour-tools.1", "harbour-tools.1+local.podman", "harbour-tools.2", "lantern-crew", "quokka-yard"}
	if len(got) != len(want) {
		t.Fatalf("%d lines, want %d: %q", len(got), len(want), got)
	}
	for i, term := range want {
		if got[i] != DenylistPattern(term) {
			t.Errorf("line %d is %q, want the pattern of %q", i+1, got[i], term)
		}
	}
	for _, unwanted := range []string{"harbour", "lantern", "local.host"} {
		for _, p := range got {
			if ereMatch(t, p, "x "+unwanted+" y") {
				t.Errorf("a non-blocking word %q is matched by %q", unwanted, p)
			}
		}
	}
}

func TestDenylistHeaderNamesNoTerm(t *testing.T) {
	fl := files{}
	r := denyRig(fl)
	if code := r.run("denylist", denyOut); code != ExitOK {
		t.Fatalf("exit %d: %s", code, r.text())
	}
	for line := range strings.SplitSeq(fl[denyOut], "\n") {
		if strings.HasPrefix(line, "#") && (strings.Contains(line, "harbour") || strings.Contains(line, "lantern") || !strings.Contains(line, "denylist")) {
			t.Errorf("header line %q names a term or not the mode", line)
		}
	}
	if !strings.HasPrefix(fl[denyOut], "# ") {
		t.Errorf("no header comment: %q", fl[denyOut])
	}
}

func TestDenylistPrintsOnlyTheCountToStderr(t *testing.T) {
	r := denyRig(files{})
	if code := r.run("denylist", denyOut); code != ExitOK {
		t.Fatalf("exit %d: %s", code, r.text())
	}
	if r.out.String() != "" || r.errOut.String() != "private-names: 7 terms written\n" {
		t.Fatalf("stdout %q stderr %q", r.out.String(), r.errOut.String())
	}
}

func TestDenylistRefusesAnExistingFile(t *testing.T) {
	fl := files{denyOut: "kept\n"}
	r := denyRig(fl)
	if code := r.run("denylist", denyOut); code != ExitCannotRun || !strings.Contains(r.text(), "exists") {
		t.Fatalf("exit %d: %s", code, r.text())
	}
	if fl[denyOut] != "kept\n" {
		t.Fatalf("the existing file was changed: %q", fl[denyOut])
	}
}

func TestDenylistRefusesAPathInsideAnyWorkTree(t *testing.T) {
	for _, path := range []string{"/repo/denylist", "/repo/untracked/scratch/d", "/work/other/d", "/work/other", "rel/d"} {
		fl := files{}
		r := denyRig(fl)
		if code := r.run("denylist", path); code != ExitCannotRun || !strings.Contains(r.text(), "work tree") {
			t.Errorf("%s: exit %d: %s", path, code, r.text())
		}
		if len(fl) != 0 {
			t.Errorf("%s: something was written: %v", path, fl)
		}
	}
}

func TestDenylistResolvesTheDirectoryBeforeJudgingIt(t *testing.T) {
	fl := files{}
	r := denyRig(fl)
	r.env.EvalSymlinks = func(p string) (string, error) {
		if p == "/link" {
			return "/repo/inside", nil
		}
		return p, nil
	}
	if code := r.run("denylist", "/link/d"); code != ExitCannotRun || !strings.Contains(r.text(), "work tree") {
		t.Fatalf("exit %d: %s", code, r.text())
	}
	r.env.EvalSymlinks = func(string) (string, error) { return "", fs.ErrNotExist }
	if code := r.run("denylist", denyOut); code != ExitCannotRun {
		t.Fatalf("an unresolvable directory: exit %d: %s", code, r.text())
	}
}

func TestDenylistNeedsExactlyOneFile(t *testing.T) {
	for _, args := range [][]string{{"denylist"}, {"denylist", "/a", "/b"}, {"denylist", ""}} {
		r := denyRig(files{})
		if code := r.run(args...); code != ExitCannotRun {
			t.Errorf("%q: exit %d: %s", args, code, r.text())
		}
	}
}

func TestDenylistWithNoTermsIsRefusedNotEmpty(t *testing.T) {
	fl := files{}
	r := denyRig(fl)
	r.env.Container = false
	r.env.Hostnames = nil
	delete(r.fake.answers, "pingbus status")
	if code := r.run("denylist", denyOut); code != ExitCannotRun || !strings.Contains(r.text(), "no terms") {
		t.Fatalf("exit %d: %s", code, r.text())
	}
	if _, ok := fl[denyOut]; ok {
		t.Fatal("an empty denylist was written")
	}
}

func TestDenylistPatternEscapesEveryMetacharacter(t *testing.T) {
	term := `a.b+c*d?e(f)g[h]i{j}k|l^m$n\o`
	p := DenylistPattern(term)
	if !ereMatch(t, p, "x "+term+" y") {
		t.Fatalf("%q does not match its own term", p)
	}
	for _, near := range []string{"aXb+c*d?e(f)g[h]i{j}k|l^m$n\\o", "a.bbbc*d?e(f)g[h]i{j}k|l^m$n\\o"} {
		if ereMatch(t, p, near) {
			t.Errorf("%q matched %q: a metacharacter was left live", p, near)
		}
	}
}

// Each written ERE matches what the gate's own matcher matches: case-insensitive, a hyphen, dot
// or slash a boundary, a letter, digit or underscore not.
func TestDenylistPatternMatchesAsTheGateDoes(t *testing.T) {
	for _, term := range []string{"zebra-billing", "shop-front.1", "shop-front.1+local.podman"} {
		p := DenylistPattern(term)
		m := mustMatcher(t, term)
		upper := strings.ToUpper(term)
		for line, want := range map[string]bool{
			term: true, "see " + upper + " here": true, "path/" + term + "/x": true,
			"a-" + term + "-b": true, "a." + term + ".b": true, "(" + term + ")": true,
			"my" + term: false, term + "s": false, term + "9": false, "9" + term: false,
			"_" + term: false, term + "_": false, "": false,
		} {
			if got := ereMatch(t, p, line); got != want {
				t.Errorf("ERE %q on %q = %t, want %t", p, line, got, want)
			}
			if _, gate := m.Find(line); gate != want {
				t.Errorf("the gate on %q = %t, want %t", line, gate, want)
			}
		}
	}
}
