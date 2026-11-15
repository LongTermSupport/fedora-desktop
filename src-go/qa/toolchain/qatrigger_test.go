package toolchain

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"go.yaml.in/yaml/v3"

	"github.com/LongTermSupport/fedora-desktop/src-go/qa/branchhygiene"
	"github.com/LongTermSupport/fedora-desktop/src-go/qa/ciscope"
)

// qaTrigger is the push filter of qa.yml.
type qaTrigger struct {
	On struct {
		Push struct {
			Branches []string `yaml:"branches"`
		} `yaml:"push"`
	} `yaml:"on"`
}

// filterPattern turns a GitHub Actions branch filter into a regexp: `**` matches any run of
// characters, `*` any run without a slash, `?` one character, and `[...]` a class.
func filterPattern(filter string) *regexp.Regexp {
	var b strings.Builder
	b.WriteString("^")
	for i := 0; i < len(filter); i++ {
		switch c := filter[i]; {
		case c == '*' && i+1 < len(filter) && filter[i+1] == '*':
			b.WriteString(".*")
			i++
		case c == '*':
			b.WriteString("[^/]*")
		case c == '?':
			b.WriteString(".")
		case c == '[':
			end := strings.IndexByte(filter[i:], ']')
			b.WriteString(filter[i : i+end+1])
			i += end
		default:
			b.WriteString(regexp.QuoteMeta(string(c)))
		}
	}
	b.WriteString("$")
	return regexp.MustCompile(b.String())
}

func triggers(filters []string, branch string) bool {
	for _, f := range filters {
		if filterPattern(f).MatchString(branch) {
			return true
		}
	}
	return false
}

// qa.yml runs on a push only where its verdict is used: every protected line (the exempt lines
// of the branch policy), every ci/<topic> branch (the gate a protected line moves on) and every
// release branch (the release's tag job waits on it). A work branch push is a backup, and costs
// no runner (issue #306).
func TestQARunsOnlyWhereItsVerdictIsUsed(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	data, err := fs.ReadFile(os.DirFS(root), qaWorkflow)
	if err != nil {
		t.Fatal(err)
	}
	var wf qaTrigger
	if err = yaml.Unmarshal(data, &wf); err != nil {
		t.Fatal(err)
	}
	pol, err := branchhygiene.ReadPolicy(os.ReadFile, root)
	if err != nil {
		t.Fatal(err)
	}
	filters := wf.On.Push.Branches
	must := append([]string{"ci/some-topic", "ci/a/b", ciscope.ReleasePrefix + "44.1.2"}, pol.Exempt...)
	for _, b := range must {
		if !triggers(filters, b) {
			t.Errorf("a push to %s runs no QA under %v", b, filters)
		}
	}
	for _, b := range []string{"plan-00254-phase6", "fix/some-thing", "agent-a1b2c3"} {
		if triggers(filters, b) {
			t.Errorf("a push to work branch %s runs QA under %v", b, filters)
		}
	}
}

func TestTheTriggerFilterReadsLikeGitHub(t *testing.T) {
	cases := []struct {
		filter, branch string
		want           bool
	}{
		{"F[0-9]*", "F44", true},
		{"F[0-9]*", "F44-arm", true},
		{"F[0-9]*", "Fix", false},
		{"ci/**", "ci/a/b", true},
		{"ci/*", "ci/a/b", false},
		{"release-*", "release-44.1.2", true},
		{"**", "anything/at/all", true},
	}
	for _, c := range cases {
		if got := filterPattern(c.filter).MatchString(c.branch); got != c.want {
			t.Errorf("%q against %q: %v, want %v", c.filter, c.branch, got, c.want)
		}
	}
}
