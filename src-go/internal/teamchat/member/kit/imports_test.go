package kit_test

import (
	"go/build"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

const module = "github.com/LongTermSupport/fedora-desktop/src-go"

// deps is every package of this module that pkg imports, itself included, and every standard
// or vendored package they import, read from the source with go/build under the build tags.
func deps(t *testing.T, root, pkg string, tags []string) map[string]bool {
	t.Helper()
	ctx := build.Default
	ctx.BuildTags = tags
	seen := map[string]bool{}
	todo := []string{pkg}
	for len(todo) > 0 {
		p := todo[len(todo)-1]
		todo = todo[:len(todo)-1]
		if seen[p] {
			continue
		}
		seen[p] = true
		rel, ours := strings.CutPrefix(p, module+"/")
		if !ours {
			continue
		}
		bp, err := ctx.ImportDir(filepath.Join(root, filepath.FromSlash(rel)), 0)
		if err != nil {
			t.Fatalf("%s: %v", p, err)
		}
		todo = append(todo, bp.Imports...)
	}
	return seen
}

// n3e: the production binary imports no fake and no test support, under any of its tags.
func TestTheBinaryImportsNoFake(t *testing.T) {
	root, err := filepath.Abs("../../../..")
	if err != nil {
		t.Fatal(err)
	}
	for _, tags := range [][]string{nil, {"busfailpoints"}, {"teamchatrelease"}, {"tuwunel"}} {
		all := deps(t, root, module+"/cmd/teamchat", tags)
		if !all[module+"/internal/teamchat/member/cli"] {
			t.Fatalf("%v: the walk did not reach cli: %d packages", tags, len(all))
		}
		if bad := testOnly(all); len(bad) > 0 {
			t.Errorf("%v: cmd/teamchat imports %v", tags, bad)
		}
	}
	// The check can fail: a fake's own walk holds the fake and what it imports for tests.
	fake := deps(t, root, module+"/internal/teamchat/member/fakeinbox", nil)
	if bad := testOnly(fake); !slices.Contains(bad, module+"/internal/teamchat/member/fakeinbox") {
		t.Errorf("the fake's own walk: %v", bad)
	}
}

// testOnly is the packages of all that only tests may import: a fake, testing, reexec.
func testOnly(all map[string]bool) []string {
	var bad []string
	for p := range all {
		base := p[strings.LastIndex(p, "/")+1:]
		if strings.HasPrefix(base, "fake") || p == "testing" || p == module+"/internal/teamchat/reexec" {
			bad = append(bad, p)
		}
	}
	slices.Sort(bad)
	return bad
}
