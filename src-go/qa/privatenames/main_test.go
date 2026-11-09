package privatenames

import (
	"bytes"
	"context"
	"errors"
	"io/fs"
	"maps"
	"strconv"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/qa/internal/proc"
)

// Every name below is invented: this file is committed to a public repository.
const status = "TEAM\tacme-ops\tshop-front.1+local.podman\ttrust=trusted\n" +
	"ABOUT\t1\tacme-ops\t\"free text that names nobody\"\n" +
	"MEMBER\tacme-ops\tjoseph\thuman\t-\n" +
	"MEMBER\tacme-ops\tzebra-billing.2+local.host\tworker\tstale\n" +
	"MEMBER\tacme-ops\tfedora-desktop.1+local.podman\tcoordinator\tlistening\n" +
	"MEMBER\tacme-ops\tpump.1+local.podman\tworker\tstale\n" +
	"ROOM\tacme-ops\treports\tjoin\t-\n" +
	"TEAM\tdesktop\tfedora-desktop.1+local.podman\ttrust=trusted\n"

func termTexts(terms []Term) string {
	parts := make([]string, 0, len(terms))
	for _, t := range terms {
		parts = append(parts, t.Text)
	}
	return "|" + strings.Join(parts, "|") + "|"
}

func TestDeriveKeepsSpecificNamesAndDropsGenericOnes(t *testing.T) {
	got := termTexts(Derive(Inputs{Status: status, Hostnames: []string{"abc123def456", "x"}, Public: []string{"joseph", "fedora-desktop"}}))
	for _, want := range []string{"acme-ops", "shop-front.1+local.podman", "shop-front.1", "shop-front", "zebra-billing", "abc123def456", "pump.1"} {
		if !strings.Contains(got, "|"+want+"|") {
			t.Errorf("%q is missing from %s", want, got)
		}
	}
	for _, unwanted := range []string{"desktop", "joseph", "x", "acme", "shop", "fedora", "reports", "human", "local", "pump"} {
		if strings.Contains(got, "|"+unwanted+"|") {
			t.Errorf("%q must not be a term: %s", unwanted, got)
		}
	}
}

func TestSpecificRejectsShortAndGenericTerms(t *testing.T) {
	for text, want := range map[string]bool{
		"dev": false, "infra": false, "desktop": false, "dev-infra": false, "test-server": false,
		"workspace": false, "ab.1": false, "zebra-billing": true, "billing": true, "abc123def456": true,
		"dev-zebra": true,
	} {
		if got := Specific(text); got != want {
			t.Errorf("Specific(%q) = %t, want %t", text, got, want)
		}
	}
}

func TestPublicTokensAreNeverTerms(t *testing.T) {
	got := termTexts(Derive(Inputs{Status: status, Public: []string{"ZEBRA-BILLING", "zebra-billing.2+local.host"}}))
	if strings.Contains(got, "|zebra-billing|") || strings.Contains(got, "|zebra-billing.2+local.host|") {
		t.Fatalf("a public token became a term: %s", got)
	}
}

// A seat handle built on a public name (the repository's own) names nothing of an install when
// the rest of it is seat numbers and generic words; a handle whose site is a real name stays.
func TestAHandleOfAPublicNameIsPublicUnlessItsSiteIsSpecific(t *testing.T) {
	st := "MEMBER\tacme-ops\tfedora-desktop.1+local.podman\tcoordinator\tlistening\n" +
		"MEMBER\tacme-ops\tfedora-desktop.2+local.host\tworker\tstale\n" +
		"MEMBER\tacme-ops\tfedora-desktop.3+zebrahost.podman\tworker\tstale\n"
	got := termTexts(Derive(Inputs{Status: st, Public: []string{"fedora-desktop"}}))
	for _, unwanted := range []string{"fedora-desktop.1", "fedora-desktop.1+local.podman", "fedora-desktop.2", "fedora-desktop.2+local.host", "fedora-desktop.3"} {
		if strings.Contains(got, "|"+unwanted+"|") {
			t.Errorf("%q must not be a term: %s", unwanted, got)
		}
	}
	if !strings.Contains(got, "|fedora-desktop.3+zebrahost.podman|") {
		t.Errorf("a handle with a specific site was dropped: %s", got)
	}
	stored := []Term{{SourceMember, "fedora-desktop.1"}, {SourceMember, "fedora-desktop.2+local.host"}, {SourceMember, "zebra-billing.2"}}
	if kept := termTexts(withoutPublic(stored, []string{"fedora-desktop"})); kept != "|zebra-billing.2|" {
		t.Errorf("a stored handle of a public name was kept: %s", kept)
	}
}

func TestListRoundTripsAndRefusesADamagedLine(t *testing.T) {
	in := []Term{{SourceTeam, "acme-ops"}, {SourceMember, "zebra-billing.2"}}
	out, err := Parse(Format(in))
	if err != nil || len(out) != 2 || out[1].Text != "zebra-billing.2" {
		t.Fatalf("%v %v", out, err)
	}
	_, err = Parse("agent-bus team\tacme-ops\nno tab here\n")
	var perr *ParseError
	if !errors.As(err, &perr) || perr.Line != 2 || strings.Contains(err.Error(), "no tab") {
		t.Fatalf("%v", err)
	}
}

func mustMatcher(t *testing.T, texts ...string) *Matcher {
	t.Helper()
	terms := make([]Term, 0, len(texts))
	for _, x := range texts {
		terms = append(terms, Term{Source: SourceMember, Text: x})
	}
	m, err := NewMatcher(terms)
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func TestMatcherIsCaseInsensitiveAndWordBounded(t *testing.T) {
	m := mustMatcher(t, "zebra-billing", "shop-front.1")
	for line, want := range map[string]bool{
		"the ZEBRA-Billing team": true, "(zebra-billing)": true, "zebra-billing": true,
		"path/shop-front.1/x": true, "shop-front.12": false, "myzebra-billing": false,
		"zebra-billings": false, "zebra billing": false, "": false,
	} {
		if _, got := m.Find(line); got != want {
			t.Errorf("Find(%q) = %t, want %t", line, got, want)
		}
	}
	if _, err := NewMatcher(nil); err == nil {
		t.Fatal("an empty term list must not make a matcher")
	}
}

const diff = `diff --git a/docs/a.md b/docs/a.md
--- a/docs/a.md
+++ b/docs/a.md
@@ -3,0 +4,2 @@
+clean line
+the Zebra-Billing seat
@@ -9 +11 @@
-removed zebra-billing line
+++ not a header zebra-billing
diff --git a/zebra-billing/new.txt b/zebra-billing/new.txt
--- /dev/null
+++ b/zebra-billing/new.txt
@@ -0,0 +1 @@
+nothing
`

func TestScanDiffReportsAddedLinesAndPathsOnly(t *testing.T) {
	hits := ScanDiff(mustMatcher(t, "zebra-billing"), "", diff)
	got := make([]string, 0, len(hits))
	for _, h := range hits {
		got = append(got, h.Where+":"+strconv.Itoa(h.Line))
	}
	want := "docs/a.md:5 docs/a.md:11 zebra-billing/new.txt:0"
	if strings.Join(got, " ") != want {
		t.Fatalf("got %v want %s", got, want)
	}
}

// fake answers by argv; an argv it was not given is an error, so an unplanned call fails loudly.
type fake struct {
	answers map[string]proc.Result
	stdins  map[string]string
}

func (f *fake) Run(_ context.Context, c proc.Cmd) (proc.Result, error) {
	key := strings.Join(c.Argv, " ")
	if c.Stdin != nil {
		buf := new(bytes.Buffer)
		if _, err := buf.ReadFrom(c.Stdin); err != nil {
			return proc.Result{}, err
		}
		f.stdins[key] = buf.String()
	}
	res, ok := f.answers[key]
	if !ok {
		return proc.Result{}, errors.New("unplanned call: " + key)
	}
	return res, nil
}

type files map[string]string

func (f files) read(path string) ([]byte, error) {
	v, ok := f[path]
	if !ok {
		return nil, fs.ErrNotExist
	}
	if v == "UNREADABLE" {
		return nil, errors.New("permission denied")
	}
	return []byte(v), nil
}

func (f files) write(path string, data []byte) error {
	f[path] = string(data)
	return nil
}

const listFile = "/repo/.git/private-terms"

// rig is one run's world: the fake machine and what the command wrote.
type rig struct {
	env    Env
	out    *bytes.Buffer
	errOut *bytes.Buffer
	fake   *fake
	files  files
}

func newRig(container bool, answers map[string]proc.Result, fl files, stdin string) rig {
	base := map[string]proc.Result{
		"git rev-parse --show-toplevel":                         {Stdout: "/repo\n"},
		"git rev-parse --path-format=absolute --git-common-dir": {Stdout: "/repo/.git\n"},
	}
	maps.Copy(base, answers)
	f := &fake{answers: base, stdins: map[string]string{}}
	r := rig{out: new(bytes.Buffer), errOut: new(bytes.Buffer), fake: f, files: fl}
	r.env = Env{
		Stdout: r.out, Stderr: r.errOut, Cwd: "/repo", Run: f, Container: container,
		ReadFile: fl.read, WriteFile: fl.write, Hostnames: []string{"abc123def456"},
	}
	if stdin != "" {
		r.env.Stdin = strings.NewReader(stdin)
	}
	return r
}

func (r rig) run(args ...string) ExitCode { return Main(args, r.env) }

func (r rig) text() string { return r.out.String() + r.errOut.String() }

const stagedKey = "git diff --cached -U0 --no-color --no-ext-diff"

func TestStagedFindsANameAndNeverPrintsIt(t *testing.T) {
	r := newRig(true, map[string]proc.Result{stagedKey: {Stdout: diff}, "pingbus status": {Stdout: status}}, files{}, "")
	if code := r.run("staged"); code != ExitFindings {
		t.Fatalf("exit %d: %s", code, r.text())
	}
	if !strings.Contains(r.text(), "PRIVATE-NAME docs/a.md:5 (agent-bus member #") || !strings.Contains(r.text(), "PRIVATE-NAME <private>/new.txt (") {
		t.Fatalf("the finding is missing: %s", r.text())
	}
	if lower := strings.ToLower(r.text()); strings.Contains(lower, "zebra") || strings.Contains(lower, "acme") {
		t.Fatalf("a term was printed: %s", r.text())
	}
	if !strings.Contains(r.files[listFile], "zebra-billing") {
		t.Fatal("the list was not written")
	}
}

func TestStagedCleanPasses(t *testing.T) {
	r := newRig(true, map[string]proc.Result{stagedKey: {Stdout: "+++ b/x\n"}, "pingbus status": {Stdout: status}}, files{}, "")
	if code := r.run("staged"); code != ExitOK || !strings.Contains(r.text(), "0 finding(s)") {
		t.Fatalf("exit %d: %s", code, r.text())
	}
}

func TestContainerWithNoBusIsAHardFailure(t *testing.T) {
	r := newRig(true, nil, files{}, "") // pingbus is not planned: it cannot start
	r.env.Hostnames = nil
	if code := r.run("staged"); code != ExitCannotRun || !strings.Contains(r.text(), "has not run") {
		t.Fatalf("exit %d: %s", code, r.text())
	}
}

func TestContainerWithAFailingBusIsAHardFailure(t *testing.T) {
	r := newRig(true, map[string]proc.Result{"pingbus status": {Code: 1}}, files{}, "")
	if code := r.run("staged"); code != ExitCannotRun || !strings.Contains(r.text(), "pingbus status exited 1") {
		t.Fatalf("exit %d: %s", code, r.text())
	}
}

// noTeam is pingbus's answer for a session that is on no team: a documented empty state, not a
// bus that crashed. This repository's own agents launch that way and must still be able to commit.
const noTeam = 78

func TestContainerOnNoTeamPassesOnItsHostNames(t *testing.T) {
	r := newRig(true, map[string]proc.Result{stagedKey: {Stdout: "+++ b/x\n"}, "pingbus status": {Code: noTeam}}, files{}, "")
	if code := r.run("staged"); code != ExitOK || !strings.Contains(r.text(), "0 finding(s)") {
		t.Fatalf("exit %d: %s", code, r.text())
	}
	if !strings.Contains(r.files[listFile], "abc123def456") {
		t.Fatalf("the host name was not learned: %q", r.files[listFile])
	}
}

func TestContainerOnNoTeamStillGuardsItsHostNames(t *testing.T) {
	added := "--- a/x\n+++ b/x\n@@ -0,0 +1 @@\n+runs on abc123def456\n"
	r := newRig(true, map[string]proc.Result{stagedKey: {Stdout: added}, "pingbus status": {Code: noTeam}}, files{}, "")
	if code := r.run("staged"); code != ExitFindings || strings.Contains(r.text(), "abc123def456") {
		t.Fatalf("exit %d: %s", code, r.text())
	}
}

func TestContainerOnNoTeamWithNoHostNamesIsAHardFailure(t *testing.T) {
	r := newRig(true, map[string]proc.Result{"pingbus status": {Code: noTeam}}, files{}, "")
	r.env.Hostnames = nil
	if code := r.run("staged"); code != ExitCannotRun || !strings.Contains(r.text(), "has not run") {
		t.Fatalf("exit %d: %s", code, r.text())
	}
}

// Only "no team" is an empty answer. Any other status is a bus that failed, and a list built
// without it could read as a pass.
func TestContainerWithAnyOtherFailingStatusIsAHardFailure(t *testing.T) {
	for _, code := range []int{1, 2, 64, 77, 79, 126, 127} {
		r := newRig(true, map[string]proc.Result{"pingbus status": {Code: code}}, files{}, "")
		want := "pingbus status exited " + strconv.Itoa(code)
		if got := r.run("staged"); got != ExitCannotRun || !strings.Contains(r.text(), want) {
			t.Errorf("status %d: exit %d: %s", code, got, r.text())
		}
	}
}

func TestContainerWithAnUnreadableOrDamagedListIsAHardFailure(t *testing.T) {
	for _, body := range []string{"UNREADABLE", "no tab here\n"} {
		r := newRig(true, map[string]proc.Result{"pingbus status": {Stdout: status}}, files{listFile: body}, "")
		if code := r.run("staged"); code != ExitCannotRun || !strings.Contains(r.text(), "private-term list") {
			t.Fatalf("%q: exit %d: %s", body, code, r.text())
		}
	}
}

func TestOutsideAContainerWithNothingSaysSkippedAndPasses(t *testing.T) {
	r := newRig(false, nil, files{}, "")
	r.env.Hostnames = nil
	if code := r.run("staged"); code != ExitOK || !strings.Contains(r.text(), "SKIPPED") {
		t.Fatalf("exit %d: %s", code, r.text())
	}
}

func TestAStoredListStillGuardsAfterTheTeamLeavesTheBus(t *testing.T) {
	r := newRig(false, map[string]proc.Result{stagedKey: {Stdout: diff}}, files{listFile: "agent-bus member\tzebra-billing\n"}, "")
	r.env.Hostnames = nil
	if code := r.run("staged"); code != ExitFindings {
		t.Fatalf("exit %d: %s", code, r.text())
	}
}

func TestThePublicAllowlistRemovesAStoredTerm(t *testing.T) {
	fl := files{listFile: "agent-bus member\tzebra-billing\n", "/repo/" + AllowlistPath: "public_tokens:\n  - Zebra-Billing\n"}
	r := newRig(false, map[string]proc.Result{stagedKey: {Stdout: diff}}, fl, "")
	r.env.Hostnames = nil
	if code := r.run("staged"); code != ExitOK || !strings.Contains(r.text(), "SKIPPED") {
		t.Fatalf("exit %d: %s", code, r.text())
	}
}

const logOut = "\x01aaaaaaaaaaaaaaaa\x1fsubject\n\nbody mentions zebra-billing\n\x02\n" + diff

func TestPushScansEachNewCommitRange(t *testing.T) {
	logKey := "git log -p -U0 --no-color --no-ext-diff --format=" + LogFormat + " "
	answers := map[string]proc.Result{
		"pingbus status": {Stdout: status},
		// An update scans only what no remote ref has yet: a merge of the default branch brings
		// thousands of commits the remote already publishes, which are not this push's to judge.
		logKey + "cccc --not bbbb --remotes": {Stdout: logOut},
		logKey + "dddd --not --remotes":      {Stdout: ""},
	}
	stdin := "refs/heads/a cccc refs/heads/a bbbb\nrefs/heads/n dddd refs/heads/n " + zeroID + "\nrefs/heads/gone " + zeroID + " refs/heads/gone eeee\n"
	r := newRig(true, answers, files{}, stdin)
	if code := r.run("push", "origin", "url"); code != ExitFindings {
		t.Fatalf("exit %d: %s", code, r.text())
	}
	for _, want := range []string{"aaaaaaaaaaaa:(commit message):3", "aaaaaaaaaaaa:docs/a.md:5"} {
		if !strings.Contains(r.text(), want) {
			t.Errorf("%q missing from %s", want, r.text())
		}
	}
}

func TestPushRefusesAMalformedRefLine(t *testing.T) {
	r := newRig(true, map[string]proc.Result{"pingbus status": {Stdout: status}}, files{}, "only three fields\n")
	if code := r.run("push"); code != ExitCannotRun || !strings.Contains(r.text(), "four fields") {
		t.Fatalf("exit %d: %s", code, r.text())
	}
}

func TestTreeScansEveryBlobAndSkipsSubmodulesAndBinaries(t *testing.T) {
	list := "100644 a.txt\x00100644 zebra-billing/b.txt\x00160000 sub\x00100644 bin.dat\x00"
	batch := "o1 blob 5\nhello\no2 blob 23\nplain\nthe zebra-billing\no3 blob 4\n\x00zeb\n"
	answers := map[string]proc.Result{
		"pingbus status": {Stdout: status},
		"git ls-tree -r -z --format=%(objectmode) %(path) HEAD": {Stdout: list},
		"git cat-file --batch": {Stdout: batch},
	}
	r := newRig(true, answers, files{}, "")
	if code := r.run("tree", "HEAD"); code != ExitFindings {
		t.Fatalf("exit %d: %s", code, r.text())
	}
	if got := r.fake.stdins["git cat-file --batch"]; got != "HEAD:a.txt\nHEAD:zebra-billing/b.txt\nHEAD:bin.dat\n" {
		t.Fatalf("asked for %q", got)
	}
	for _, want := range []string{"<private>/b.txt (", "<private>/b.txt:2 ("} {
		if !strings.Contains(r.text(), want) {
			t.Errorf("%q missing from %s", want, r.text())
		}
	}
	if strings.Contains(r.text(), "bin.dat:") {
		t.Fatal("a binary was scanned")
	}
}

func TestUsageAndUnknownCommand(t *testing.T) {
	bus := map[string]proc.Result{"pingbus status": {Stdout: status}}
	r := newRig(true, bus, files{}, "")
	if code := r.run("--help"); code != ExitOK || !strings.Contains(r.text(), "usage:") {
		t.Fatalf("exit %d", code)
	}
	if code := r.run(); code != ExitCannotRun {
		t.Fatalf("exit %d", code)
	}
	r2 := newRig(true, bus, files{}, "")
	if code := r2.run("bogus"); code != ExitCannotRun || !strings.Contains(r2.text(), "unknown command") {
		t.Fatalf("exit %d: %s", code, r2.text())
	}
}

func TestRefreshPrintsOnlyACount(t *testing.T) {
	r := newRig(true, map[string]proc.Result{"pingbus status": {Stdout: status}}, files{}, "")
	if code := r.run("refresh"); code != ExitOK || !strings.Contains(r.text(), " terms learned") || strings.Contains(r.text(), "zebra") {
		t.Fatalf("exit %d: %s", code, r.text())
	}
}

func TestMaskHidesEveryTermInAPlace(t *testing.T) {
	m := mustMatcher(t, "zebra-billing", "shop-front")
	if got := m.Mask("a/Zebra-Billing/shop-front/x.md"); got != "a/<private>/<private>/x.md" {
		t.Fatalf("%s", got)
	}
}
