package godeps

import (
	"slices"
	"strings"
	"testing"
)

const keepoutRow = "keepout\tex.com/a\t./cmd/admin\tthe admin binary reads JSON only\n"

// GD1: a keepout row is four fields, and keeps its module out of its package.
func TestAllowlistReadsAKeepoutRow(t *testing.T) {
	got, err := parseAllowlist(goodAllowlist + keepoutRow)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.rows) != 2 || got.rows[1].kind != kindKeepout || got.rows[1].pkg != "./cmd/admin" ||
		got.rows[1].reason != "the admin binary reads JSON only" {
		t.Fatalf("rows = %+v", got.rows)
	}
}

// GD2: a keepout row needs the module's own row, a ./cmd/<name> package, and is listed once.
func TestAllowlistRefusesABadKeepoutRow(t *testing.T) {
	for name, text := range map[string]string{
		"no module row":   keepoutRow,
		"three fields":    goodAllowlist + "keepout\tex.com/a\t./cmd/admin\n",
		"listed twice":    goodAllowlist + keepoutRow + keepoutRow,
		"not a command":   goodAllowlist + "keepout\tex.com/a\t./internal/admin\twhy\n",
		"a path above":    goodAllowlist + "keepout\tex.com/a\t./cmd/../x\twhy\n",
		"empty package":   goodAllowlist + "keepout\tex.com/a\t\twhy\n",
		"module, 4 field": goodAllowlist + "module\tex.com/b\t./cmd/admin\twhy\n",
	} {
		if _, err := parseAllowlist("# header\n" + text); err == nil || !strings.Contains(err.Error(), "line ") {
			t.Errorf("%s: err = %v, want a refusal naming the line", name, err)
		}
	}
	if _, err := parseAllowlist(goodAllowlist + keepoutRow + "keepout\tex.com/a\t./cmd/other\twhy\n"); err != nil {
		t.Fatalf("two packages kept off one module: %v", err)
	}
}

func TestAKeepoutRowIsNotStale(t *testing.T) {
	allow, err := parseAllowlist(goodAllowlist + keepoutRow)
	if err != nil {
		t.Fatal(err)
	}
	v, err := judge([]byte(goodGoMod), allow)
	if err != nil || len(v.findings) != 0 || len(v.keepouts) != 1 {
		t.Fatalf("verdict %+v, %v", v, err)
	}
}

func keptOut(t *testing.T) *repo {
	t.Helper()
	r := inStep(t)
	writeTree(t, r.root+"/src-go", map[string]string{"go-deps.allowlist": goodAllowlist + keepoutRow})
	return r
}

const listKey = "list ./cmd/admin"

// GD1: go list -deps runs over the package in src-go under the fixed environment, and a
// package clear of the module passes.
func TestAPackageClearOfItsKeptOutModulePasses(t *testing.T) {
	r := keptOut(t)
	r.goFake.outputs[listKey] = "ex.com/me\n\nex.com/b\n"
	if code := r.deps(t); code != ExitOK {
		t.Fatalf("code %d: %s", code, r.err.String())
	}
	var list []string
	for _, c := range r.goFake.calls {
		if subcommand(c.Argv) == listKey {
			list = c.Argv
			if c.Dir != r.root+"/src-go" || !slices.Equal(c.Env, pinnedEnv) {
				t.Fatalf("go list ran in %s with %v", c.Dir, c.Env)
			}
		}
	}
	want := []string{"go", "list", "-deps", "-f", "{{with .Module}}{{.Path}}{{end}}", "./cmd/admin"}
	if !slices.Equal(list, want) {
		t.Fatalf("go list argv %q, want %q", list, want)
	}
	if line := r.out.String(); !strings.Contains(line, "), 1 keepout(s) held; go.sum verified") {
		t.Fatalf("the pass line does not count the keepout row: %q", line)
	}
}

func TestAPackageThatReachesItsKeptOutModuleFails(t *testing.T) {
	r := keptOut(t)
	r.goFake.outputs[listKey] = "ex.com/me\nex.com/a\nex.com/a\n"
	if code := r.deps(t); code != ExitFailed {
		t.Fatalf("code %d, want 1", code)
	}
	if got := r.err.String(); strings.Count(got, "./cmd/admin depends on ex.com/a") != 1 ||
		!strings.Contains(got, "the admin binary reads JSON only") {
		t.Fatalf("stderr = %s", got)
	}
}

func TestAGoListThatFailsIsAFindingOrCouldNotJudge(t *testing.T) {
	r := keptOut(t)
	r.goFake.codes[listKey] = 1
	r.goFake.outputs[listKey] = "cmd/admin/main.go:3:2: no required module provides package ex.com/c"
	if code := r.deps(t); code != ExitFailed || !strings.Contains(r.err.String(), "go list failed over ./cmd/admin") {
		t.Fatalf("a broken build: code %d: %s", code, r.err.String())
	}
	r = keptOut(t)
	r.goFake.codes[listKey] = 1
	r.goFake.outputs[listKey] = "dial tcp: lookup proxy.golang.org: no such host"
	if code := r.deps(t); code != ExitCannotRun {
		t.Fatalf("no proxy: code %d: %s", code, r.err.String())
	}
	r = keptOut(t)
	r.goFake.failRun = listKey
	if code := r.deps(t); code != ExitCannotRun {
		t.Fatalf("go cannot start: code %d", code)
	}
}
