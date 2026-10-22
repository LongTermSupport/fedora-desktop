package declare

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"unicode"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/bus/pycompat"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
)

// The parity test runs the real declare.py over every fixture under testdata/parity and the
// reader over the same file, and requires the same answer: on success the reader's values,
// written out as v1's marker lines, are v1's stdout byte for byte; a refusal is the same exit
// class and the same stderr line; a v1 traceback is the reader's crash class (B-S3).

const parityDir = "testdata/parity"

// The repositories and teams every fixture is asked about.
var (
	parityRepos = []string{"example-org/proj", "example-org/legacy", "other-org/tool", "Example-Org/Proj"}
	parityTeams = []string{"alpha", "beta", "gamma", "old", "ghost"}
	parityURLs  = []string{
		"git@github.com:Example-Org/Proj.git", "git@github.com-work:example-org/proj", "ssh://git@github.com/example-org/proj.git/",
		"ssh://git@github.com:22/example-org/proj", "https://github.com/example-org/proj.git.git", "  https://github.com/example-org/proj\n",
		"https://github.com/example-org/proj.git", "https://example.com/example-org/proj", "https://github.com/example-org/a/b",
		"https://github.com/example_org/proj", "http://github.com/example-org/proj", "git@github.com:example-org/.git",
		"git@github.com:example-org/..", "https://github.com/example-org/proj/", "https://github.com/example-org/proj//",
		"https://github.com/EXAMPLE-ORG/PROJ.GIT", "git@github.com\n:example-org/proj", "git@github.com:\xc4\xb0x/proj",
		"https://github.com/" + strings.Repeat("a", 39) + "/b", "https://github.com/" + strings.Repeat("a", 40) + "/b",
		"\xe2\x80\x83https://github.com/example-org/proj\xc2\x85", "", "https://github.com/a-/b", "git@github.com:a/b.git/",
	}
)

// repoRoot walks up from the test's directory to the checkout holding the real declare.py.
func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "helpers", "agent_bus", "declare.py")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("no helpers/agent_bus/declare.py above the test directory")
		}
		dir = parent
	}
}

type pyAnswer struct {
	code           int
	stdout, stderr string
}

func runDeclarePy(t *testing.T, root string, args []string) pyAnswer {
	t.Helper()
	res, err := execx.OS{}.Run(context.Background(), execx.Cmd{Name: "env", Args: slices.Concat(
		[]string{"PYTHONPATH=" + root, "PYTHONDONTWRITEBYTECODE=1", "python3", "-P", "-m", "helpers.agent_bus.declare"}, args)})
	if err != nil {
		exit, ok := errors.AsType[*execx.ExitError](err)
		if !ok {
			t.Fatalf("declare.py %q: %v", args, err)
		}
		return pyAnswer{code: exit.Code, stdout: string(res.Stdout), stderr: exit.Stderr}
	}
	return pyAnswer{stdout: string(res.Stdout), stderr: string(res.Stderr)}
}

func flag(args []string, name string) string {
	i := slices.Index(args, name)
	if i < 0 || i+1 >= len(args) {
		return ""
	}
	return args[i+1]
}

// goAnswer is the reader's answer to one declare.py command line, written as v1 prints it.
func goAnswer(args []string) (string, error) {
	if args[0] == "github-remote" {
		if name := GithubRemote(flag(args, "--url")); name != "" {
			return name + "\n", nil
		}
		return "", nil
	}
	text, err := Read(flag(args, "--file"), os.ReadFile)
	if err != nil {
		return "", err
	}
	return answer(HostVars(text), args[0], flag(args, "--repo"), flag(args, "--team"))
}

func answer(hv HostVars, sub, repo, team string) (string, error) {
	switch sub {
	case "declared":
		teams, err := hv.Declared()
		return render(teams, err, renderTeam)
	case "limited":
		limits, err := hv.Limited()
		return render(limits, err, func(l Limit) string { return line("LIMITED", l.Team, l.Prefixes, l.Trusts) })
	case "undescribed":
		teams, err := hv.Undescribed()
		return render(teams, err, func(u Undescribed) string { return line("UNDESCRIBED", u.Team, u.Trusts) })
	case "teams-for":
		names, err := hv.TeamsFor(repo)
		return render(names, err, func(n string) string { return line("TEAM", n) })
	case "choices":
		choices, err := hv.Choices(repo)
		return render(choices, err, func(c Choice) string {
			return line("CHOICE", c.Team, string(c.Standing), descriptionText(c.Description, c.DescriptionState))
		})
	case "element-teams":
		profiles, err := hv.ElementTeams()
		return render(profiles, err, func(p Profile) string { return line("ELEMENT", p.Team, p.BaseURL) })
	case "address":
		addr, err := hv.Address()
		if addr == "" {
			return "", err
		}
		return addr + "\n", err
	case "settings":
		settings, err := hv.Settings(team)
		return render(settings, err, renderSetting)
	case "next-steps":
		return hv.NextSteps(team)
	case "lxc-members":
		found, err := hv.LxcMembers()
		return render(found, err, func(m LxcMember) string {
			return line("LXCMEMBER", m.Team, m.Container, m.User, dash(m.Host), m.Role, m.State, dash(m.Checkout), dash(m.Seat))
		})
	}
	return "", fmt.Errorf("no such subcommand %q", sub)
}

func render[T any](rows []T, err error, one func(T) string) (string, error) {
	if err != nil {
		return "", err
	}
	var b strings.Builder
	for _, r := range rows {
		b.WriteString(one(r))
	}
	return b.String(), nil
}

func line(fields ...string) string { return strings.Join(fields, "\t") + "\n" }

func dash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

// renderTeam writes a TEAM line. Port 0 and no port are one value in Team, as in
// internal/declare's; the URL tells them apart when there is one.
func renderTeam(team Team) string {
	port := "-"
	if team.Port != 0 || strings.HasSuffix(team.URL, ":0") {
		port = strconv.Itoa(team.Port)
	}
	state := "absent"
	if team.Present {
		state = "present"
	}
	return line("TEAM", team.Name, port, state, team.ServerName, dash(team.URL), descriptionText(team.Description, team.DescriptionState))
}

func descriptionText(text string, state DescriptionState) string {
	switch state {
	case DescriptionVaulted:
		return "!vaulted"
	case DescriptionInvalid:
		return "!invalid"
	}
	if text == "" {
		return "-"
	}
	out, err := pycompat.Dumps(pycompat.Str(text), false)
	if err != nil {
		return "unprintable: " + err.Error()
	}
	return out
}

func renderSetting(s Setting) string {
	value := "-"
	switch {
	case s.State == SettingVaulted:
		value = "!vaulted"
	case s.State == SettingInvalid:
		value = "!invalid"
	case s.Value != nil:
		value = string(s.Value)
	}
	return line("SETTING", s.Key, value)
}

// parityCase is one declare.py command line.
type parityCase struct {
	name string
	args []string
}

// fixtureGlobs are the fixture sets and what each is asked: every query of a whole file; of a
// file refused whole, one of each (the refusal comes before any flag is judged); of an LXC
// member set, its members and its teams.
var fixtureGlobs = []struct {
	glob                string
	plain, repos, teams []string
}{
	{"*.yml", []string{"declared", "limited", "undescribed", "element-teams", "address", "lxc-members"}, parityRepos, parityTeams},
	{"refused/*.yml", []string{"declared", "limited", "undescribed", "element-teams", "address", "lxc-members"}, []string{"Bad", "example-org/proj"}, []string{"alpha"}},
	{"lxc/*.yml", []string{"lxc-members", "declared"}, nil, nil},
}

func parityCases(t *testing.T) []parityCase {
	t.Helper()
	var cases []parityCase
	add := func(name string, args ...string) { cases = append(cases, parityCase{name, args}) }
	files := 0
	for _, set := range fixtureGlobs {
		fixtures, err := fs.Glob(os.DirFS(parityDir), set.glob)
		if err != nil || len(fixtures) < 30 {
			t.Fatalf("%s: %d fixtures, %v", set.glob, len(fixtures), err)
		}
		files += len(fixtures)
		for _, fixture := range fixtures {
			file := parityDir + "/" + fixture
			for _, sub := range set.plain {
				add(fixture+"/"+sub, sub, "--file", file)
			}
			for _, repo := range set.repos {
				add(fixture+"/teams-for/"+repo, "teams-for", "--file", file, "--repo", repo)
				add(fixture+"/choices/"+repo, "choices", "--file", file, "--repo", repo)
			}
			for _, team := range set.teams {
				add(fixture+"/settings/"+team, "settings", "--file", file, "--team", team)
				add(fixture+"/next-steps/"+team, "next-steps", "--file", file, "--team", team)
			}
		}
	}
	t.Logf("%d fixtures", files)
	for _, file := range []string{parityDir + "/missing.yml", "./testdata//parity/./missing.yml", parityDir, parityDir + "/basic.yml/x"} {
		add("special/declared/"+file, "declared", "--file", file)
		add("special/teams-for/"+file, "teams-for", "--file", file, "--repo", "Bad")
	}
	for i, url := range parityURLs {
		add(fmt.Sprintf("github-remote/%d", i), "github-remote", "--url", url)
	}
	return cases
}

func TestParityWithDeclarePy(t *testing.T) {
	root := repoRoot(t)
	cases := parityCases(t)
	var ok, refused, crashed atomic.Int64
	t.Run("cases", func(t *testing.T) {
		for _, c := range cases {
			t.Run(c.name, func(t *testing.T) {
				t.Parallel()
				switch compareWithPython(t, runDeclarePy(t, root, c.args), c.args) {
				case 0:
					ok.Add(1)
				case 1:
					crashed.Add(1)
				default:
					refused.Add(1)
				}
			})
		}
	})
	t.Logf("%d cases over %s: %d answered alike, %d refused alike, %d v1 tracebacks matched by the crash class",
		len(cases), parityDir, ok.Load(), refused.Load(), crashed.Load())
}

// compareWithPython fails t unless the reader answers as v1 did; it returns v1's exit code.
func compareWithPython(t *testing.T, py pyAnswer, args []string) int {
	t.Helper()
	got, err := goAnswer(args)
	e, isErr := asError(err)
	switch {
	case err != nil && !isErr:
		t.Fatalf("%q: not a declare error: %v", args, err)
	case py.code == 0 && err != nil:
		t.Errorf("%q: v1 printed %q, the reader refused: kind %d %q", args, py.stdout, e.Kind, e.Message)
	case py.code == 0 && got != py.stdout:
		t.Errorf("%q:\n v1     %q\n reader %q", args, py.stdout, got)
	case py.code != 0:
		compareRefusal(t, py, args, got, e)
	}
	return py.code
}

// compareRefusal fails t unless the reader refused as v1 did: a traceback as the crash
// class, a refusal with the same exit code and stderr line.
func compareRefusal(t *testing.T, py pyAnswer, args []string, got string, e *Error) {
	t.Helper()
	switch {
	case py.code == 1 && !strings.Contains(py.stderr, "Traceback (most recent call last)"):
		t.Errorf("%q: v1 exited 1 without a traceback: %q", args, py.stderr)
	case py.code == 1 && (e == nil || e.Kind != KindCrash):
		t.Errorf("%q: v1 crashed (%s), the reader answered %q, %+v", args, lastLine(py.stderr), got, e)
	case py.code == 1:
	case py.code != 64 && py.code != 78:
		t.Errorf("%q: v1 exited %d: %q", args, py.code, py.stderr)
	case e == nil:
		t.Errorf("%q: v1 refused (%d %q), the reader answered %q", args, py.code, py.stderr, got)
	case e.Code() != py.code || "declare: "+e.Message+"\n" != py.stderr:
		t.Errorf("%q:\n v1     %d %q\n reader %d %q", args, py.code, py.stderr, e.Code(), e.Message)
	}
}

func lastLine(s string) string {
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	return lines[len(lines)-1]
}

// TestOtherDigitsAreIsdigitLessIsdecimal compares the reader's table of the digits int()
// refuses with python's str.isdigit and str.isdecimal over every code point both editions of
// Unicode assign.
func TestOtherDigitsAreIsdigitLessIsdecimal(t *testing.T) {
	const script = `import json, sys, unicodedata
out = []
for cp in range(0x110000):
    c = chr(cp)
    if unicodedata.category(c) != "Cn":
        out.append([cp, c.isdigit() and not c.isdecimal()])
json.dump(out, sys.stdout)
`
	res, err := execx.OS{}.Run(context.Background(), execx.Cmd{Name: "python3", Args: []string{"-I", "-c", script}})
	if err != nil {
		t.Fatal(err)
	}
	var rows [][2]any
	if err := json.Unmarshal(res.Stdout, &rows); err != nil {
		t.Fatal(err)
	}
	compared, digits := 0, 0
	for _, row := range rows {
		cp, okCP := row[0].(float64)
		want, okWant := row[1].(bool)
		if !okCP || !okWant {
			t.Fatalf("row %v", row)
		}
		r := rune(cp)
		if !assigned(r) {
			continue
		}
		compared++
		if want {
			digits++
		}
		if got := isDigitChar(r) && !isDecimal(r); got != want {
			t.Errorf("%U: reader %v, python %v", r, got, want)
		}
	}
	if compared < 100000 || digits < 100 {
		t.Fatalf("compared %d code points, %d other digits: the oracle did not run", compared, digits)
	}
	t.Logf("%d assigned code points compared, %d digits int() refuses", compared, digits)
}

func assigned(r rune) bool {
	return unicode.In(r, unicode.L, unicode.M, unicode.N, unicode.P, unicode.S, unicode.Z,
		unicode.Cc, unicode.Cf, unicode.Co, unicode.Cs)
}
