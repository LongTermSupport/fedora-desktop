package lxcadd

import (
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

// The directories the container shows, as the host-side reader lists them. home is the rig's
// user's home, where the directory list opens.
const (
	home = "/home/" + "agent"

	homeListing = `{"path": "` + home + `", "exists": true, "git": false, "truncated": false, "dirs": [` +
		`{"name": ".cache", "git": false}, {"name": "project", "git": true}, {"name": "src", "git": false}, ` +
		`{"name": "srv", "git": false}]}`
	projectListing = `{"path": "` + home + `/project", "exists": true, "git": true, "truncated": false, "dirs": [` +
		`{"name": "docs", "git": false}]}`
	emptyListing = `"exists": true, "git": false, "truncated": false, "dirs": []}`
)

// browsing is a rig whose user has no checkout found in the home, so the Checkout step opens
// the directory list at once, and whose answers after it are the role and the yes.
func browsing(t *testing.T, fzf ...string) *rig {
	t.Helper()
	r := newRig(t)
	r.OnDeclare("lxc-checkouts", "")
	r.lxc.dirs = map[string]string{home: homeListing, home + "/project": projectListing}
	r.answers(choose("container-a"), choose("agent"), choose("worker"), "YES")
	r.Script.Fzf = fzf
	return r
}

// checkoutWritten runs the flow and is the checkout it declared.
func (r *rig) checkoutWritten(t *testing.T) string {
	t.Helper()
	if code := r.run("alpha"); code != term.ExitOK {
		t.Fatalf("exit %d: %s", code, r.ErrOut.String())
	}
	_, checkout, found := strings.Cut(r.addLine(), "--checkout ")
	if !found {
		t.Fatalf("nothing was declared: %s", r.ErrOut.String())
	}
	return checkout
}

func (r *rig) fzfArg(t *testing.T, call int, prefix string) string {
	t.Helper()
	for _, a := range r.Script.FzfArgs[call] {
		if v, ok := strings.CutPrefix(a, prefix); ok {
			return v
		}
	}
	t.Fatalf("fzf call %d has no %s: %q", call, prefix, r.Script.FzfArgs[call])
	return ""
}

func TestEveryQuestionShowsTheTeamTheStepTheContainerAndTheUser(t *testing.T) {
	r := newRig(t)
	r.happy()
	if code := r.run("alpha"); code != term.ExitOK {
		t.Fatalf("exit %d: %s", code, r.ErrOut.String())
	}
	wants := []string{
		"--header Add an LXC member to alpha · step 1/4 Container\n",
		"--header Add an LXC member to alpha · step 2/4 User · container-a\n",
		"--header Add an LXC member to alpha · step 3/4 Checkout · container-a, user agent\n",
		"--header Add an LXC member to alpha · step 4/4 Role · container-a, user agent\n",
	}
	for i, want := range wants {
		if !strings.Contains(r.Script.GumLog[i], want) {
			t.Fatalf("question %d lacks its context %q: %q", i+1, want, r.Script.GumLog[i])
		}
	}
	said := r.ErrOut.String()
	for _, want := range []string{
		"1/4  Container · Add an LXC member to alpha\n",
		"3/4  Checkout · Add an LXC member to alpha · container-a, user agent\n",
	} {
		if !strings.Contains(said, want) {
			t.Fatalf("the step heading lacks %q:\n%s", want, said)
		}
	}
}

func TestTypeAPathOpensTheContainersDirectoriesToWalkWithArrowsAndEnter(t *testing.T) {
	r := browsing(t, "|project/", "|Use "+home+"/project")
	if got := r.checkoutWritten(t); got != home+"/project" {
		t.Fatalf("declared %q", got)
	}
	rows := r.Script.Rows[0]
	// Each line carries its place in the list; Tab is the shortcut of a row of its own.
	want := []string{
		"project/   (a git checkout)\t0", "src/\t1", "srv/\t2", "Use " + home + " as the checkout\t3",
		"../   (up to /home)\t4", "Complete what is typed as a path   (Tab)\t5", "Cancel",
	}
	if !slices.Equal(rows, want) {
		t.Fatalf("rows %q\nwant %q", rows, want)
	}
	if got := r.fzfArg(t, 0, "--prompt="); got != home+"/" {
		t.Fatalf("prompt %q", got)
	}
	if got := r.fzfArg(t, 0, "--border-label="); !strings.Contains(got, "Add an LXC member to alpha · step 3/4 Checkout · container-a, user agent") {
		t.Fatalf("title %q", got)
	}
	if got := r.HeaderOf(0); !strings.Contains(got, "Tab complete") || !strings.Contains(got, "Enter open or choose") {
		t.Fatalf("header %q", got)
	}
	if !slices.Contains(r.Script.FzfArgs[0], "--expect=tab") || !slices.Contains(r.Script.FzfArgs[0], "--print-query") {
		t.Fatalf("args %q", r.Script.FzfArgs[0])
	}
	// Inside a checkout, using it is the first row.
	if first := r.Script.Rows[1][0]; first != "Use "+home+"/project as the checkout   (a git checkout)\t0" {
		t.Fatalf("rows in the checkout %q", r.Script.Rows[1])
	}
	if !slices.Contains(r.lxc.reads, "dirs container-a agent "+home) {
		t.Fatalf("reads %v", r.lxc.reads)
	}
}

func TestTabOnARowOpensItAsEnterDoes(t *testing.T) {
	r := browsing(t, "tab|project/", "|Use "+home+"/project")
	if got := r.checkoutWritten(t); got != home+"/project" {
		t.Fatalf("declared %q", got)
	}
}

func TestTabCompletesTypedTextThatMatchesOneDirectory(t *testing.T) {
	r := browsing(t, "tab|TYPED:pro", "|Use "+home+"/project")
	if got := r.checkoutWritten(t); got != home+"/project" {
		t.Fatalf("declared %q", got)
	}
	if got := r.fzfArg(t, 1, "--prompt="); got != home+"/project/" {
		t.Fatalf("prompt after the completion %q", got)
	}
}

func TestTabCompletesTypedTextToWhatSeveralDirectoriesShare(t *testing.T) {
	r := browsing(t, "tab|TYPED:s", "|srv/", "|Use "+home+"/srv")
	r.lxc.dirs[home+"/srv"] = `{"path": "` + home + `/srv", ` + emptyListing
	if got := r.checkoutWritten(t); got != home+"/srv" {
		t.Fatalf("declared %q", got)
	}
	if got := r.fzfArg(t, 1, "--query="); got != "sr" {
		t.Fatalf("the query after the completion is %q, want the shared sr", got)
	}
}

func TestTabWithTypedTextCompletesItEvenWhenARowIsHighlighted(t *testing.T) {
	// fzf highlights the first row the text matches; Tab still completes the text, so typing
	// "sr" beside src/ and srv/ never opens src/.
	r := browsing(t, "tab|TYPED:s", "tab|src/", "|srv/", "|Use "+home+"/srv")
	r.lxc.dirs[home+"/srv"] = `{"path": "` + home + `/srv", ` + emptyListing
	if got := r.checkoutWritten(t); got != home+"/srv" {
		t.Fatalf("declared %q", got)
	}
	if got := r.fzfArg(t, 2, "--query="); got != "sr" {
		t.Fatalf("Tab on a highlighted row did not complete the text: query %q", got)
	}
	if got := r.fzfArg(t, 2, "--prompt="); got != home+"/" {
		t.Fatalf("Tab on a highlighted row opened it: prompt %q", got)
	}
}

func TestTheSharedPrefixOfNamesNeverEndsInsideACharacter(t *testing.T) {
	if got := sharedPrefix([]string{"café-a", "caféb", "cafè"}); got != "caf" {
		t.Fatalf("got %q", got)
	}
	if got := sharedPrefix([]string{"ñandú", "ñandu"}); got != "ñand" {
		t.Fatalf("got %q", got)
	}
}

func TestTabCompletesAnAbsolutePathThroughItsParent(t *testing.T) {
	r := browsing(t, "tab|TYPED:/srv/ag", "|Use /srv/agent")
	r.lxc.dirs["/srv"] = `{"path": "/srv", "exists": true, "git": false, "truncated": false, "dirs": [{"name": "agent", "git": false}]}`
	r.lxc.dirs["/srv/agent"] = `{"path": "/srv/agent", ` + emptyListing
	if got := r.checkoutWritten(t); got != "/srv/agent" {
		t.Fatalf("declared %q", got)
	}
}

func TestTabCompletesAHiddenDirectoryThatIsNotListed(t *testing.T) {
	r := browsing(t, "tab|TYPED:.ca", "|Use "+home+"/.cache")
	r.lxc.dirs[home+"/.cache"] = `{"path": "` + home + `/.cache", ` + emptyListing
	if got := r.checkoutWritten(t); got != home+"/.cache" {
		t.Fatalf("declared %q", got)
	}
}

func TestTabWithNothingToCompleteSaysSo(t *testing.T) {
	r := browsing(t, "tab|TYPED:zz", "|TYPED:/srv/agent/one")
	if got := r.checkoutWritten(t); got != "/srv/agent/one" {
		t.Fatalf("declared %q", got)
	}
	if !strings.Contains(r.ErrOut.String(), "Nothing in "+home+"/ starts with zz.") {
		t.Fatalf("stderr %s", r.ErrOut.String())
	}
}

func TestEnterOnTypedTextTakesItAsThePathOrOpensItWithASlash(t *testing.T) {
	r := browsing(t, "|TYPED:project/", "|TYPED:../src/../project")
	if got := r.checkoutWritten(t); got != home+"/project" {
		t.Fatalf("declared %q", got)
	}
	if got := r.fzfArg(t, 1, "--prompt="); got != home+"/project/" {
		t.Fatalf("a typed path ending in / was not opened: %q", got)
	}
}

func TestTheUpRowGoesToTheParent(t *testing.T) {
	r := browsing(t, "|../", "|Use /home as")
	r.lxc.dirs["/home"] = `{"path": "/home", "exists": true, "git": false, "truncated": false, "dirs": [{"name": "agent", "git": false}]}`
	if got := r.checkoutWritten(t); got != "/home" {
		t.Fatalf("declared %q", got)
	}
}

func TestLeavingTheDirectoryListGoesBackToTheCheckoutsFound(t *testing.T) {
	for _, leave := range []string{"|ABORT", "|EXIT"} {
		r := newRig(t)
		r.lxc.dirs = map[string]string{home: homeListing}
		r.answers(choose("container-a"), choose("agent"), choose(typePath), choose(oneRow), choose("worker"), "YES")
		r.Script.Fzf = []string{leave}
		if got := r.checkoutWritten(t); got != "/srv/agent/one" {
			t.Fatalf("%s: declared %q", leave, got)
		}
		if last := r.Script.Rows[0][len(r.Script.Rows[0])-1]; last != "Back to the checkouts found" {
			t.Fatalf("%s: the way back is not a row: %q", leave, last)
		}
	}
}

func TestLeavingTheDirectoryListWithNothingFoundToGoBackToCancels(t *testing.T) {
	r := browsing(t, "|ABORT")
	if code := r.run("alpha"); code != term.ExitCancelled {
		t.Fatalf("exit %d: %s", code, r.ErrOut.String())
	}
	if r.addLine() != "" {
		t.Fatalf("declared %q", r.addLine())
	}
}

func TestADirectoryThatCannotBeListedIsSaidAndTheListStaysWhereItWas(t *testing.T) {
	r := browsing(t, "|src/", "|project/", "|Use "+home+"/project")
	r.lxc.dirsErr = map[string]error{home + "/src": errors.New(home + "/src is a symbolic link")}
	if got := r.checkoutWritten(t); got != home+"/project" {
		t.Fatalf("declared %q", got)
	}
	if !strings.Contains(r.ErrOut.String(), home+"/src is a symbolic link") {
		t.Fatalf("stderr %s", r.ErrOut.String())
	}
	if got := r.fzfArg(t, 1, "--prompt="); got != home+"/" {
		t.Fatalf("the list moved after a failed listing: %q", got)
	}
}

func TestAContainerWhoseDirectoriesCannotBeReadEndsTheRunSayingWhy(t *testing.T) {
	r := browsing(t)
	r.lxc.dirsErr = map[string]error{home: errors.New("container container-a has no /etc/passwd")}
	if code := r.run("alpha"); code != term.ExitFailure {
		t.Fatalf("exit %d", code)
	}
	if !strings.Contains(r.ErrOut.String(), "has no /etc/passwd") || r.addLine() != "" {
		t.Fatalf("stderr %s", r.ErrOut.String())
	}
}

func TestAHomeThatDoesNotExistIsSaidAndTheListOpensAtItsParent(t *testing.T) {
	r := browsing(t, "|TYPED:/srv/agent/one")
	r.lxc.dirs = map[string]string{"/home": `{"path": "/home", ` + emptyListing}
	if got := r.checkoutWritten(t); got != "/srv/agent/one" {
		t.Fatalf("declared %q", got)
	}
	if !strings.Contains(r.ErrOut.String(), home+"/ does not exist in container-a") {
		t.Fatalf("stderr %s", r.ErrOut.String())
	}
	if got := r.fzfArg(t, 0, "--prompt="); got != "/home/" {
		t.Fatalf("prompt %q", got)
	}
}

func TestAListingCutAtItsLimitSaysSo(t *testing.T) {
	r := browsing(t, "|TYPED:/srv/agent/one")
	r.lxc.dirs[home] = strings.Replace(homeListing, `"truncated": false`, `"truncated": true`, 1)
	r.checkoutWritten(t)
	if got := r.HeaderOf(0); !strings.Contains(got, "Only the first") {
		t.Fatalf("header %q", got)
	}
}

func TestAListingThatIsNotWhatTheReaderPrintsEndsTheRun(t *testing.T) {
	for _, bad := range []string{
		`{`, `{"path": "` + home + `", "exists": true}`,
		`{"path": "` + home + `", "exists": true, "git": false, "truncated": false, "dirs": [{"name": "a/b", "git": false}]}`,
		`{"path": "` + home + `", "exists": true, "git": false, "truncated": false, "dirs": [{"name": "..", "git": false}]}`,
	} {
		r := browsing(t)
		r.lxc.dirs[home] = bad
		if code := r.run("alpha"); code != term.ExitFailure {
			t.Fatalf("%s: exit %d", bad, code)
		}
	}
}

func TestANameThatCouldSteerTheTerminalIsNotShown(t *testing.T) {
	r := browsing(t, "|TYPED:/srv/agent/one")
	r.lxc.dirs[home] = `{"path": "` + home + `", "exists": true, "git": false, "truncated": false, "dirs": [` +
		`{"name": "red\u001b[31m", "git": false}, {"name": "plain", "git": false}]}`
	r.checkoutWritten(t)
	for _, row := range r.Script.Rows[0] {
		if strings.Contains(row, "red") {
			t.Fatalf("rows %q", r.Script.Rows[0])
		}
	}
	if got := r.HeaderOf(0); !strings.Contains(got, "1 name that cannot be shown") {
		t.Fatalf("header %q", got)
	}
}

func TestAHomeSearchPastItsTimeIsSaidAndThePathIsTyped(t *testing.T) {
	r := browsing(t, "|TYPED:/srv/agent/one")
	r.lxc.hang = true
	r.a.ReadTimeout = 1
	if got := r.checkoutWritten(t); got != "/srv/agent/one" {
		t.Fatalf("declared %q", got)
	}
	if !strings.Contains(r.ErrOut.String(), "took too long and was stopped") {
		t.Fatalf("stderr %s", r.ErrOut.String())
	}
}

func TestASearchStoppedAtItsLimitSaysItMayHaveMissedSome(t *testing.T) {
	r := newRig(t)
	r.OnDeclare("lxc-checkouts", "CHECKOUT\t/srv/agent/one\texample-org/one\t-\nTIMEDOUT\n")
	r.happy()
	if got := r.checkoutWritten(t); got != "/srv/agent/one" {
		t.Fatalf("declared %q", got)
	}
	if !strings.Contains(r.ErrOut.String(), "may have missed some") {
		t.Fatalf("stderr %s", r.ErrOut.String())
	}
}
