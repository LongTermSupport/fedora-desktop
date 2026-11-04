package create

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/declare"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/gitx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/playlock"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/teamtest"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/ui"
)

const (
	wzRoot     = "/root"
	wzHostVars = "/home/u/fd/localhost.yml"
	wzHome     = "/home/u"
)

// wzScript answers the calls a flow makes more than once with different answers (each gum
// question in turn), and hands every other call to the Fake. Every call is recorded.
type wzScript struct {
	fake   *execx.Fake
	queues map[string][]execx.Result
	calls  []execx.Cmd
}

func wzQueueKey(c execx.Cmd) string {
	if c.Name == "env" && len(c.Args) > 5 {
		key := "declare " + c.Args[5]
		if c.Args[len(c.Args)-1] == "--dry-run" {
			key += " --dry-run"
		}
		return key
	}
	if len(c.Args) == 0 {
		return c.Name
	}
	return c.Name + " " + c.Args[0]
}

func (s *wzScript) Run(ctx context.Context, c execx.Cmd) (execx.Result, error) {
	s.calls = append(s.calls, c)
	key := wzQueueKey(c)
	if q := s.queues[key]; len(q) > 0 {
		res := q[0]
		s.queues[key] = q[1:]
		if res.ExitCode != 0 {
			return res, &execx.ExitError{Name: c.Name, Code: res.ExitCode, Stderr: string(res.Stderr)}
		}
		return res, nil
	}
	return s.fake.Run(ctx, c)
}

func (s *wzScript) queue(key string, results ...execx.Result) {
	s.queues[key] = append(s.queues[key], results...)
}

// lines is every call, with the screens' headings cut out (headings has them).
func (s *wzScript) lines() []string {
	out := make([]string, 0, len(s.calls))
	for _, c := range s.calls {
		out = append(out, teamtest.Unheaded(c).Line())
	}
	return out
}

// headings is the heading of every gum question, in order.
func (s *wzScript) headings() []string {
	var out []string
	for _, c := range s.calls {
		if c.Name != "gum" || len(c.Args) == 0 || c.Args[0] == "style" || c.Args[0] == "spin" {
			continue
		}
		for _, a := range c.Args {
			if heading := teamtest.HeadingOf(a); heading != "" {
				out = append(out, heading)
				break
			}
		}
	}
	return out
}

// called is whether some call's line starts with prefix.
func (s *wzScript) called(prefix string) bool {
	return slices.ContainsFunc(s.lines(), func(l string) bool { return strings.HasPrefix(l, prefix) })
}

// first is the index of the first call starting with prefix, or -1.
func (s *wzScript) first(prefix string) int {
	return slices.IndexFunc(s.lines(), func(l string) bool { return strings.HasPrefix(l, prefix) })
}

// matching is the lines starting with prefix.
func (s *wzScript) matching(prefix string) []string {
	var out []string
	for _, l := range s.lines() {
		if strings.HasPrefix(l, prefix) {
			out = append(out, l)
		}
	}
	return out
}

func wzOK(out string) execx.Result     { return execx.Result{Stdout: []byte(out)} }
func wzExit(code int) execx.Result     { return execx.Result{ExitCode: code} }
func wzRefuse(msg string) execx.Result { return execx.Result{ExitCode: 64, Stderr: []byte(msg + "\n")} }

// dp is the argv prefix of a declare.py call on the host_vars file.
func wzDP(sub string, rest ...string) []string {
	return append([]string{"env", "PYTHONPATH=" + wzRoot, "python3", "-P", "-m", "helpers.agent_bus.declare", sub, "--file", wzHostVars}, rest...)
}

// dpBare is the prefix of the declare.py calls that take no file.
func wzDPBare(sub string, rest ...string) []string {
	return append([]string{"env", "PYTHONPATH=" + wzRoot, "python3", "-P", "-m", "helpers.agent_bus.declare", sub}, rest...)
}

// wzNoColour is an environment that asks for no colour.
func wzNoColour(k string) string {
	if k == "NO_COLOR" {
		return "1"
	}
	return ""
}

type wzHarness struct {
	run      *wzScript
	fake     *execx.Fake
	c        *Wizard
	stdout   *bytes.Buffer
	stderr   *bytes.Buffer
	declErr  *bytes.Buffer
	tty      bool
	missing  map[string]bool
	applied  [][]string
	applyErr error
	// locks is each hold of the play lock asked for, with whether the team was declared before
	// and after it; lockErr, when set, is a lock not taken (and fn is not run).
	locks   []string
	lockErr error
	pwd     []string
	signIn  []string // "team ask=bool", one per look at the sign-in after a failed apply
	pwdErr  error
	element []string
	elemErr error
	execd   [][]string
	execErr error
}

// newWzHarness is a creator in a checkout of example-org/proj, on a terminal, with an
// address declared, GitHub logged in and able to push to example-org/proj, and
// every declare check accepting. Tests override what they exercise.
func newWzHarness(t *testing.T) *wzHarness {
	t.Helper()
	h := &wzHarness{
		fake: &execx.Fake{}, stdout: &bytes.Buffer{}, stderr: &bytes.Buffer{}, declErr: &bytes.Buffer{},
		tty: true, missing: map[string]bool{},
	}
	h.run = &wzScript{fake: h.fake, queues: map[string][]execx.Result{}}
	f := h.fake
	f.On([]string{"tput", "cols"}, wzOK("100\n"))
	f.On([]string{"gum", "style"}, wzOK(""))
	f.On([]string{"gum", "spin"}, wzOK(""))
	f.On([]string{"id", "-un"}, wzOK("owner\n"))
	f.On([]string{"gh", "auth", "status"}, wzOK(""))
	f.On([]string{"gh", "api", "users/example-org"}, wzOK(""))
	f.On([]string{"gh", "api", "repos/example-org/proj"}, wzOK("true\n"))
	f.On([]string{"gh", "api", "repos/example-org/alpha"}, wzOK("true\n"))
	f.On([]string{"gh", "api", "repos/other/tool"}, wzOK("true\n"))
	f.On([]string{"gh", "api", "repos/example-org/readonly"}, wzOK("false\n"))
	f.On([]string{"gh", "api", "repos/other/missing"}, execx.Result{ExitCode: 1, Stderr: []byte("gh: Not Found (HTTP 404)")})
	f.On([]string{"gh", "api", "users/nope"}, execx.Result{ExitCode: 1})
	f.On([]string{"git", "rev-parse", "--is-inside-work-tree"}, wzOK("true\n"))
	f.On([]string{"git", "rev-parse", "--show-toplevel"}, wzOK("/home/u/proj\n"))
	f.On([]string{"git", "remote", "get-url", "origin"}, wzOK("git@github.com:Example-Org/Proj.git\n"))
	f.On(wzDPBare("github-remote"), wzOK("example-org/proj\n"))
	f.On(wzDP("address"), wzOK("192.0.2.10\n"))
	f.On(wzDP("check"), wzOK(""))
	f.On(wzDPBare("check-address"), wzOK(""))
	f.On(wzDP("suggest-team"), wzOK(""))
	f.On(wzDP("suggest-team", "--name", "proj"), wzOK("proj\n"))
	f.On(wzDP("suggest-team", "--name", "example-org"), wzOK("example-org\n"))
	f.On(wzDP("create"), wzOK("CHANGED\tteam proj, port 6169, in "+wzHostVars+"\n"))
	f.On(wzDP("element"), wzOK("CHANGED\tteam proj Element profile\n"))
	f.On(wzDP("teams-for"), wzOK("TEAM\tproj\n"))
	f.On(wzDP("next-steps"), wzOK("Team proj is up. Later, in ccy-teams:\n"+
		"  Sign in      Enter on your name under the team, then choose Sign in: it sets a password and shows it.\n"+
		"  Element      Enter on the team, then choose Open in Element.\n"+
		"  Agents       in a checkout of any repository of example-org, run ccy --teams proj\n"+
		"               (ccy-teams run there offers it; Enter on the team, then Start a ccy session, does it too).\n"))

	client := declare.NewClient(h.run, wzRoot, wzHostVars)
	client.Err = h.declErr
	h.c = &Wizard{
		Declare: client,
		Gum:     &ui.Gum{Runner: h.run, Stderr: h.stderr, Plain: true},
		Git:     gitx.New(h.run),
		NewTerm: func() *term.Term {
			return &term.Term{Err: h.stderr, IsTerminal: func(any) bool { return h.tty }, Getenv: wzNoColour}
		},
		Runner: h.run, Stdin: strings.NewReader(""), Stdout: h.stdout,
		Cwd: "/home/u/proj/sub", HostVars: wzHostVars, Home: wzHome, Launcher: "/opt/ccy/launcher",
		LookPath: func(name string) (string, error) {
			if h.missing[name] {
				return "", exec.ErrNotFound
			}
			return "/usr/bin/" + name, nil
		},
		ApplyTeams: func(_ context.Context, teams ...string) error {
			h.applied = append(h.applied, teams)
			return h.applyErr
		},
		Locked: h.locked,
		SignInGap: func(_ context.Context, teams []string, ask bool) error {
			h.signIn = append(h.signIn, strings.Join(teams, " ")+" ask="+strconv.FormatBool(ask))
			return nil
		},
		NewPassword: func(_ context.Context, team, human string) error {
			h.pwd = append(h.pwd, team+"/"+human)
			return h.pwdErr
		},
		LaunchElement: func(_ context.Context, team string) error {
			h.element = append(h.element, team)
			return h.elemErr
		},
		ExecLauncher: func(dir string, argv []string) error {
			h.execd = append(h.execd, append([]string{dir}, argv...))
			return h.execErr
		},
	}
	h.c.Gum.Runner = h.run
	return h
}

// locked is the play lock's hold: fn is run, and what was declared and applied before and after
// it is kept; with lockErr set, the lock is not taken and fn is not run.
func (h *wzHarness) locked(ctx context.Context, purpose string, fn func(context.Context) error) error {
	if h.lockErr != nil {
		h.locks = append(h.locks, purpose+" (not taken)")
		return h.lockErr
	}
	before := h.run.called("env PYTHONPATH=/root python3 -P -m helpers.agent_bus.declare create")
	err := fn(ctx)
	after := h.run.called("env PYTHONPATH=/root python3 -P -m helpers.agent_bus.declare create")
	h.locks = append(h.locks, fmt.Sprintf("%s: declared before %v, after %v, applied %d", purpose, before, after, len(h.applied)))
	return err
}

func (h *wzHarness) invoke(args ...string) term.ExitCode {
	return h.c.Run(context.Background(), args)
}

func (h *wzHarness) err() string { return h.stderr.String() }

// boxes is every gum style call: the banner, the explanations and the summary box.
func (h *wzHarness) boxes() string { return strings.Join(h.run.matching("gum style"), "\n") }

func wzContain(t *testing.T, what, got string, wants ...string) {
	t.Helper()
	for _, w := range wants {
		if !strings.Contains(got, w) {
			t.Errorf("%s lacks %q\n---\n%s", what, w, got)
		}
	}
}

func wzLack(t *testing.T, what, got string, unwanted ...string) {
	t.Helper()
	for _, w := range unwanted {
		if strings.Contains(got, w) {
			t.Errorf("%s has %q\n---\n%s", what, w, got)
		}
	}
}

func wzCode(t *testing.T, got, want term.ExitCode, h *wzHarness) {
	t.Helper()
	if got != want {
		t.Fatalf("exit %d, want %d\nstderr:\n%s", got, want, h.err())
	}
}

// happyAnswers: the scope's first choice, every default accepted, the summary yes, no
// sign-in, no Element, yes to a session here.
func (h *wzHarness) happyAnswers() {
	h.run.queue("gum choose", wzOK(scopeRepos+", starting with example-org/proj\n"))
	h.run.queue("gum input", wzOK("example-org/proj\n"), wzOK("proj\n"), wzOK("Builds the example site\n"), wzOK("owner\n"), wzOK("\n"))
	h.run.queue("gum confirm", wzOK(""), wzExit(1), wzExit(1), wzOK(""))
}

// ── the suite's "no teams / wizard" assertions ───────────────────────────────────────────

func TestWizardInACheckoutAcceptsEveryDefault(t *testing.T) {
	h := newWzHarness(t)
	h.happyAnswers()
	wzCode(t, h.invoke(), 0, h)
	lines := h.run.lines()

	// the scope is asked first, listed repositories selected, this one first
	wzContain(t, "choose", strings.Join(h.run.matching("gum choose"), "\n"),
		"gum choose --header Trust --selected Repositories you list (one or more), starting with example-org/proj")
	inputs := h.run.matching("gum input")
	if len(inputs) != 5 {
		t.Fatalf("inputs: %v", inputs)
	}
	wantInputs := []string{
		"gum input --header Repositories (owner/repo, comma-separated) --placeholder e.g. example-org/alpha --value example-org/proj --width 0",
		"gum input --header Team name --placeholder e.g. example-org --value proj --width 0",
		"gum input --header What is this team for? --placeholder e.g. Builds and reviews the example site --value  --width 0",
		"gum input --header People, comma-separated --placeholder e.g. owner, colleague --value owner --width 0",
		"gum input --header Subnets (Enter for none) --placeholder e.g. 192.0.2.0/24 --value  --width 0",
	}
	for i, want := range wantInputs {
		if inputs[i] != want {
			t.Errorf("input %d:\n got %q\nwant %q", i, inputs[i], want)
		}
	}
	// every question says where it is: the step and the answers so far, then each offer after
	wantHeadings := []string{
		"Teams › New team · step 1/6 What the team works on\nnothing is changed until you say yes at the end",
		"Teams › New team · step 2/6 Repositories\nnothing is changed until you say yes at the end",
		"Teams › New team · step 3/6 Team name\nworks on example-org/proj",
		"Teams › New team · step 4/6 What is this team for?\nworks on example-org/proj · team proj",
		"Teams › New team · step 5/6 People\nworks on example-org/proj · team proj",
		"Teams › New team · step 6/6 Other machines\nworks on example-org/proj · team proj · people owner",
		"Teams › New team › Review\nteam proj · works on only example-org/proj, on any branch",
		"Teams › proj › owner › Sign in\nowner in team proj · just created",
		"Teams › proj › Open in Element\nteam proj · just created",
		"Teams › proj › New session\nteam proj · in ~/proj",
	}
	if got := h.run.headings(); !slices.Equal(got, wantHeadings) {
		t.Errorf("headings:\n got %q\nwant %q", got, wantHeadings)
	}
	// checked on GitHub for push access
	if !h.run.called("gh api repos/example-org/proj --jq .permissions.push") {
		t.Error("the repository was not checked for push access")
	}
	// every explanation is printed, wrapped, before its question
	wzContain(t, "gum style", strings.Join(h.run.matching("gum style --width"), "\n"),
		"installer creates the accounts", "--width 96 --padding 0 0 0 5")
	// headers are one short line
	for _, l := range append(h.run.matching("gum input"), h.run.matching("gum choose")...) {
		hdr := l[strings.Index(l, "--header")+9:]
		if i := strings.Index(hdr, " --"); i > 70 {
			t.Errorf("a header of %d characters: %s", i, hdr)
		}
	}
	// the banner, then the one question that covers writing and installing
	wzContain(t, "banner", strings.Join(lines, "\n"),
		"gum style --bold --border rounded --padding 0 1 New agent team",
		"gum confirm --default=true Create team proj? (saved to ~/fd/localhost.yml, then installed: sudo asks for your password)",
		"gum confirm --default=true Sign owner in to Element now? A password is set and shown here once.",
		"gum confirm --default=true Open proj in Element now?",
		"gum confirm --default=true Start a ccy session in ~/proj as a member of proj?")
	e := h.err()
	wzContain(t, "summary", h.boxes(),
		"Team proj", "Works on  only example-org/proj, on any branch", "People    owner",
		"Machines  this one only", "Element   a profile for the team, set up on this desktop")
	wzContain(t, "stderr", e, "ccy --teams proj",
		"1/6  What the team works on", "6/6  Other machines",
		"✓ Repositories you list (one or more), starting with example-org/proj", "✓ example-org/proj",
		"Starting ccy --teams proj in ~/proj.")
	wzLack(t, "stderr", e, "forge_api", "path_prefixes", "meta-deploy", "Traceback")
	if n := strings.Count(h.boxes(), "private room"); n != 1 {
		t.Errorf("private room explained %d times", n)
	}
	// declared with the flags the answers made, installed, session started in the top
	wzContain(t, "create call", strings.Join(h.run.matching("env "), "\n"),
		"create --file "+wzHostVars+" --team proj --human owner --repo example-org/proj --description Builds the example site")
	if len(h.applied) != 1 || !slices.Equal(h.applied[0], []string{"proj"}) {
		t.Errorf("applied %v", h.applied)
	}
	if len(h.pwd) != 0 || len(h.element) != 0 {
		t.Errorf("declined offers ran: %v %v", h.pwd, h.element)
	}
	if len(h.execd) != 1 || !slices.Equal(h.execd[0], []string{"/home/u/proj", "/opt/ccy/launcher", "--teams", "proj"}) {
		t.Errorf("exec %v", h.execd)
	}
	wzContain(t, "stdout", h.stdout.String(), "CHANGED\tteam proj, port 6169")
}

// S-B11: the chooser runs the wizard for this checkout and takes the team it made; the wizard
// neither offers a session of its own nor prints on stdout, which is the chooser's payload.
func TestWizardForTheChooserReturnsTheTeamAndStartsNothing(t *testing.T) {
	h := newWzHarness(t)
	h.run.queue("gum choose", wzOK(scopeRepos+", starting with example-org/proj\n"))
	h.run.queue("gum input", wzOK("example-org/proj\n"), wzOK("proj\n"), wzOK("Builds the example site\n"), wzOK("owner\n"), wzOK("\n"))
	h.run.queue("gum confirm", wzOK(""), wzExit(1), wzExit(1))
	team, code := h.c.RunForChooser(context.Background())
	wzCode(t, code, term.ExitOK, h)
	if team != "proj" {
		t.Fatalf("team %q", team)
	}
	if len(h.applied) != 1 || !slices.Equal(h.applied[0], []string{"proj"}) {
		t.Errorf("applied %v", h.applied)
	}
	if len(h.execd) != 0 || h.run.called("gum confirm --default=true Start a ccy session") {
		t.Errorf("a session was offered or started: %v", h.execd)
	}
	if h.stdout.Len() != 0 {
		t.Errorf("stdout is the chooser's payload, got %q", h.stdout.String())
	}
	wzContain(t, "stderr", h.err(), "CHANGED\tteam proj, port 6169")
	wzLack(t, "stderr", h.err(), "run ccy --teams")
}

func TestWizardForTheChooserCancelledReturnsNoTeam(t *testing.T) {
	h := newWzHarness(t)
	h.run.queue("gum choose", wzExit(130))
	team, code := h.c.RunForChooser(context.Background())
	wzCode(t, code, term.ExitCancelled, h)
	if team != "" || len(h.applied) != 0 {
		t.Fatalf("team %q applied %v", team, h.applied)
	}
}

func TestWizardRunsItsStepsInTheDocumentedOrder(t *testing.T) {
	h := newWzHarness(t)
	h.happyAnswers()
	wzCode(t, h.invoke(), 0, h)
	order := []string{
		"gum style --bold", "gum choose", "gum input --header Repositories", "gh api repos/example-org/proj",
		"env PYTHONPATH=/root python3 -P -m helpers.agent_bus.declare create --file " + wzHostVars + " --team proj --human owner --repo example-org/proj --description Builds the example site --dry-run",
		"gum confirm --default=true Create team",
		"env PYTHONPATH=/root python3 -P -m helpers.agent_bus.declare create --file " + wzHostVars + " --team proj --human owner --repo example-org/proj --description Builds the example site",
		"env PYTHONPATH=/root python3 -P -m helpers.agent_bus.declare element",
		"gum confirm --default=true Sign owner in",
		"gum confirm --default=true Open proj",
		"env PYTHONPATH=/root python3 -P -m helpers.agent_bus.declare next-steps",
		"env PYTHONPATH=/root python3 -P -m helpers.agent_bus.declare teams-for",
		"gum confirm --default=true Start a ccy session",
	}
	last := -1
	for _, prefix := range order {
		idx := -1
		for i, l := range h.run.lines() {
			if i > last && strings.HasPrefix(l, prefix) && (!strings.HasSuffix(prefix, "--repo example-org/proj") || !strings.HasSuffix(l, "--dry-run")) {
				idx = i
				break
			}
		}
		if idx < 0 {
			t.Fatalf("%q does not come after step %d\n%s", prefix, last, strings.Join(h.run.lines(), "\n"))
		}
		last = idx
	}
}

func TestWizardOutsideACheckoutOffersTheOwnerFirstAndNoDefaults(t *testing.T) {
	h := newWzHarness(t)
	h.run.queue("git rev-parse", wzExit(128)) // not a work tree
	h.run.queue("gum choose", wzOK(scopeOwner+"\n"))
	h.run.queue("gum input", wzOK("Example-Org\n"), wzOK("example-org\n"), wzOK("Builds the example site\n"), wzOK("owner\n"), wzOK("\n"))
	h.run.queue("gum confirm", wzOK(""), wzExit(1), wzExit(1))
	wzCode(t, h.invoke(), 0, h)
	wzContain(t, "choose", strings.Join(h.run.matching("gum choose"), "\n"),
		"gum choose --header Trust --selected "+scopeOwner+" "+scopeOwner+" "+scopeRepos)
	wzContain(t, "owner input", h.run.matching("gum input")[0],
		"--header GitHub owner (organisation or user) --placeholder e.g. example-org --value  --width 0")
	if h.run.called("gum confirm --default=true Start a ccy session") {
		t.Error("a session was offered outside a checkout")
	}
	// the owner is looked up, lower-cased, and the scope says so in words
	if !h.run.called("gum spin --title Looking up example-org on GitHub… -- gh api users/example-org --silent") {
		t.Errorf("no lookup: %v", h.run.lines())
	}
	wzContain(t, "summary", h.boxes(), "everything example-org owns: any repository, any branch")
	wzContain(t, "create", strings.Join(h.run.matching("env "), "\n"), "--team example-org --human owner --org example-org")
	wzLack(t, "create", strings.Join(h.run.matching("env "), "\n"), "--repo")
}

func TestWizardInACheckoutOffersTheOwnerOfThatCheckoutSecond(t *testing.T) {
	h := newWzHarness(t)
	h.run.queue("gum choose", wzOK(scopeOwner+": example-org\n"))
	h.run.queue("gum input", wzOK("example-org\n"), wzOK("example-org\n"), wzOK("Builds the example site\n"), wzOK("owner\n"), wzOK("\n"))
	h.run.queue("gum confirm", wzOK(""), wzExit(1), wzExit(1), wzExit(1))
	wzCode(t, h.invoke(), 0, h)
	wzContain(t, "choose", strings.Join(h.run.matching("gum choose"), "\n"),
		"--selected "+scopeRepos+", starting with example-org/proj "+scopeRepos+", starting with example-org/proj "+scopeOwner+": example-org")
	wzContain(t, "owner input", h.run.matching("gum input")[0], "--value example-org --width 0")
}

// ── the questions and their retries ─────────────────────────────────────────────────────

func TestAnOwnerGitHubDoesNotKnowIsRefusedWithAReason(t *testing.T) {
	h := newWzHarness(t)
	h.run.queue("gum spin", wzExit(1))
	h.run.queue("gum choose", wzOK(scopeOwner+": example-org\n"))
	h.run.queue("gum input", wzOK("nope\n"), wzOK("example-org\n"), wzOK("example-org\n"), wzOK("Builds the example site\n"), wzOK("owner\n"), wzOK("\n"))
	h.run.queue("gum confirm", wzOK(""), wzExit(1), wzExit(1), wzExit(1))
	wzCode(t, h.invoke(), 0, h)
	wzContain(t, "stderr", h.err(), "GitHub has no user or organisation called 'nope' that your gh login can see. Try again.")
	// the refused answer is the next try's value
	wzContain(t, "retry", h.run.matching("gum input")[1], "--header GitHub owner (organisation or user)", "--value nope")
}

func TestAnOwnerTheTeamFileRefusesIsNotLookedUp(t *testing.T) {
	h := newWzHarness(t)
	h.fake.On(wzDP("check", "--org", "bad_owner"), wzRefuse("declare: --org: must be a lower-case GitHub owner"))
	h.run.queue("gum choose", wzOK(scopeOwner+"\n"))
	h.run.queue("gum input", wzOK("bad_owner\n"), wzOK("bad_owner\n"), wzOK("bad_owner\n"))
	wzCode(t, h.invoke(), 1, h)
	wzContain(t, "stderr", h.err(), "  --org: must be a lower-case GitHub owner. Try again.", "Giving up after 3 tries; nothing was changed.")
	if h.run.called("gum spin") {
		t.Error("looked a refused owner up")
	}
}

func TestThreeRefusedTriesEndTheRunWithNothingChanged(t *testing.T) {
	h := newWzHarness(t)
	h.fake.On(wzDP("check", "--team", "Bad"), wzRefuse("declare: --team: nope"))
	h.run.queue("gum choose", wzOK(scopeRepos+", starting with example-org/proj\n"))
	h.run.queue("gum input", wzOK("example-org/proj\n"), wzOK("Bad\n"), wzOK("Bad\n"), wzOK("Bad\n"))
	wzCode(t, h.invoke(), 1, h)
	wzContain(t, "stderr", h.err(), "Giving up after 3 tries; nothing was changed.")
	if n := len(h.run.matching("gum input --header Team name")); n != 3 {
		t.Errorf("%d tries", n)
	}
	if h.run.called("env PYTHONPATH=/root python3 -P -m helpers.agent_bus.declare create") {
		t.Error("something was written")
	}
}

func TestAnEmptyTeamNameIsRefused(t *testing.T) {
	h := newWzHarness(t)
	h.run.queue("gum choose", wzOK(scopeRepos+", starting with example-org/proj\n"))
	h.run.queue("gum input", wzOK("example-org/proj\n"), wzOK("  \n"), wzOK("proj\n"), wzOK("Builds the example site\n"), wzOK("owner\n"), wzOK("\n"))
	h.run.queue("gum confirm", wzOK(""), wzExit(1), wzExit(1), wzExit(1))
	wzCode(t, h.invoke(), 0, h)
	wzContain(t, "stderr", h.err(), "  the team needs a name. Try again.")
}

func TestARefusedAnswerComesBackAsTypedNotLowerCased(t *testing.T) {
	h := newWzHarness(t)
	h.fake.On(wzDP("check", "--repo", "bad"), wzRefuse("declare: --repo: no"))
	h.run.queue("gum choose", wzOK(scopeRepos+", starting with example-org/proj\n"))
	typed := "Example-Org/Alpha, example-org/readonly, other/missing, bad"
	h.run.queue("gum input", wzOK(typed+"\n"), wzOK("example-org/alpha,other/tool\n"), wzOK("alpha\n"), wzOK("Builds the example site\n"), wzOK("owner\n"), wzOK("\n"))
	h.run.queue("gum confirm", wzOK(""), wzExit(1), wzExit(1), wzExit(1))
	wzCode(t, h.invoke(), 0, h)
	e := h.err()
	wzContain(t, "stderr", e,
		"  example-org/readonly: read-only for your gh login; a team trusts only repositories you can push to.",
		"  other/missing: not found, or your gh login cannot see it.",
		"  bad: not OWNER/REPO (lower-case; the name not ., .. or ending .git).",
		"  Checking example-org/alpha on GitHub…", "  Try again.",
	)
	wzContain(t, "summary", h.boxes(), "only these repositories, on any branch: example-org/alpha, other/tool")
	wzLack(t, "stderr", e, "example-org/alpha:", "example-org/alpha: not")
	wzContain(t, "second try", h.run.matching("gum input")[1], "--value "+typed+" --width 0")
	wzContain(t, "create", strings.Join(h.run.matching("env "), "\n"), "--repo example-org/alpha --repo other/tool")
}

func TestAnEmptyRepositoryListIsRefused(t *testing.T) {
	h := newWzHarness(t)
	h.run.queue("gum choose", wzOK(scopeRepos+", starting with example-org/proj\n"))
	h.run.queue("gum input", wzOK(" , \n"), wzOK("example-org/proj\n"), wzOK("proj\n"), wzOK("Builds the example site\n"), wzOK("owner\n"), wzOK("\n"))
	h.run.queue("gum confirm", wzOK(""), wzExit(1), wzExit(1), wzExit(1))
	wzCode(t, h.invoke(), 0, h)
	wzContain(t, "stderr", h.err(), "  name at least one repository, as owner/repo. Try again.")
}

func TestTheWholeRepositoryListIsCheckedAgainstTheTeamFile(t *testing.T) {
	h := newWzHarness(t)
	h.fake.On(wzDP("check", "--repo", "example-org/proj", "--repo", "other/tool"), wzRefuse("declare: --repo: at most 1 here"))
	h.run.queue("gum choose", wzOK(scopeRepos+"\n"))
	h.run.queue("gum input", wzOK("example-org/proj, other/tool\n"), wzOK("example-org/proj\n"), wzOK("proj\n"), wzOK("Builds the example site\n"), wzOK("owner\n"), wzOK("\n"))
	h.run.queue("gum confirm", wzOK(""), wzExit(1), wzExit(1), wzExit(1))
	wzCode(t, h.invoke(), 0, h)
	wzContain(t, "stderr", h.err(), "  --repo: at most 1 here. Try again.")
}

func TestAnInnerSpaceIsRefusedNotDeleted(t *testing.T) {
	h := newWzHarness(t)
	h.fake.On(wzDP("check", "--repo", "example-org/pro j"), wzRefuse("declare: --repo: not OWNER/REPO"))
	h.run.queue("gum choose", wzOK(scopeRepos+"\n"))
	h.run.queue("gum input", wzOK("example-org/pro j\n"), wzOK("example-org/proj\n"), wzOK("proj\n"), wzOK("Builds the example site\n"), wzOK("owner\n"), wzOK("\n"))
	h.run.queue("gum confirm", wzOK(""), wzExit(1), wzExit(1), wzExit(1))
	wzCode(t, h.invoke(), 0, h)
	wzContain(t, "stderr", h.err(), "example-org/pro j: not OWNER/REPO")
	if h.run.called("gh api repos/example-org/pro") && h.run.called("gh api repos/example-org/pro j") {
		t.Error("example-org/pro j was looked up as typed")
	}
	if got := h.run.matching("gh api repos/example-org/proj"); len(got) != 1 {
		t.Errorf("example-org/pro j was squeezed into example-org/proj, or the good one was not checked once: %v", got)
	}
	wzContain(t, "second try", h.run.matching("gum input")[1], "--value example-org/pro j")
}

func TestPeopleAreSplitOnCommasAndCheckedTogether(t *testing.T) {
	h := newWzHarness(t)
	h.run.queue("gum choose", wzOK(scopeRepos+"\n"))
	h.run.queue("gum input", wzOK("example-org/proj\n"), wzOK("proj\n"), wzOK("Builds the example site\n"), wzOK(" owner ,colleague,, \n"), wzOK("\n"))
	h.run.queue("gum confirm", wzOK(""), wzExit(1), wzExit(1), wzExit(1), wzExit(1))
	wzCode(t, h.invoke(), 0, h)
	if !h.run.called(strings.Join(wzDP("check", "--human", "owner", "--human", "colleague"), " ")) {
		t.Errorf("not checked together: %v", h.run.lines())
	}
	wzContain(t, "stderr", h.err(), "✓ owner colleague")
	wzContain(t, "summary", h.boxes(), "People    owner colleague")
	wzContain(t, "offers", strings.Join(h.run.lines(), "\n"), "Sign colleague in to Element now?")
}

func TestPeopleNamedByNobodyAreRefused(t *testing.T) {
	h := newWzHarness(t)
	h.run.queue("gum choose", wzOK(scopeRepos+"\n"))
	h.run.queue("gum input", wzOK("example-org/proj\n"), wzOK("proj\n"), wzOK("Builds the example site\n"), wzOK(",\n"), wzOK("owner\n"), wzOK("\n"))
	h.run.queue("gum confirm", wzOK(""), wzExit(1), wzExit(1), wzExit(1))
	wzCode(t, h.invoke(), 0, h)
	wzContain(t, "stderr", h.err(), "  name at least one person; your own login name is the usual answer. Try again.")
}

func TestALoginThatCannotBeAnAccountHasNoDefault(t *testing.T) {
	h := newWzHarness(t)
	h.fake.On(wzDP("check", "--human", "owner"), wzRefuse("declare: --human: owner is reserved"))
	h.run.queue("gum choose", wzOK(scopeRepos+"\n"))
	h.run.queue("gum input", wzOK("example-org/proj\n"), wzOK("proj\n"), wzOK("Builds the example site\n"), wzOK("alice\n"), wzOK("\n"))
	h.run.queue("gum confirm", wzOK(""), wzExit(1), wzExit(1), wzExit(1))
	wzCode(t, h.invoke(), 0, h)
	wzContain(t, "stderr", h.err(),
		"Your login name, owner, cannot be an account name here (--human: owner is reserved), so there is no default.")
	wzContain(t, "people", h.run.matching("gum input --header People")[0],
		"--placeholder e.g. yourname, colleague --value  --width 0")
}

func TestOtherMachinesAreCheckedAndListed(t *testing.T) {
	h := newWzHarness(t)
	h.fake.On(wzDP("check", "--allow-from", "300.0.0.0/8"), wzRefuse("declare: --allow-from: not a CIDR"))
	h.run.queue("gum choose", wzOK(scopeRepos+"\n"))
	h.run.queue("gum input", wzOK("example-org/proj\n"), wzOK("proj\n"), wzOK("Builds the example site\n"), wzOK("owner\n"), wzOK("300.0.0.0/8\n"), wzOK("192.0.2.0/24, 198.51.100.0/24\n"))
	h.run.queue("gum confirm", wzOK(""), wzExit(1), wzExit(1), wzExit(1))
	wzCode(t, h.invoke(), 0, h)
	wzContain(t, "stderr", h.err(), "  --allow-from: not a CIDR. Try again.", "✓ 192.0.2.0/24 198.51.100.0/24")
	wzContain(t, "summary", h.boxes(), "Machines  192.0.2.0/24 198.51.100.0/24")
	wzContain(t, "create", strings.Join(h.run.matching("env "), "\n"), "--allow-from 192.0.2.0/24 --allow-from 198.51.100.0/24")
}

func TestNoOtherMachinesIsAnsweredInWords(t *testing.T) {
	h := newWzHarness(t)
	h.happyAnswers()
	wzCode(t, h.invoke(), 0, h)
	wzContain(t, "stderr", h.err(), "✓ none, this machine only")
}

func TestAMachineWithNoBusAddressAsksForOneAndDeclaresItWithTheTeam(t *testing.T) {
	h := newWzHarness(t)
	h.fake.On(wzDP("address"), wzOK(""))
	h.fake.On(wzDPBare("check-address", "--address", "=not-an-ip"), wzRefuse("declare: --address: not an IP address"))
	h.run.queue("gum choose", wzOK(scopeOwner+"\n"))
	h.run.queue("gum input", wzOK("example-org\n"), wzOK("example-org\n"), wzOK("Builds the example site\n"), wzOK("owner\n"), wzOK("\n"), wzOK("=not-an-ip\n"), wzOK("192.0.2.10\n"))
	h.run.queue("gum confirm", wzOK(""), wzExit(1), wzExit(1))
	wzCode(t, h.invoke(), 0, h)
	e := h.err()
	wzContain(t, "stderr", e, "7/7  Bus address", "not an IP address. Try again.", "✓ 192.0.2.10")
	wzContain(t, "explanation", h.boxes(), "Every team's room lives at one address of this machine")
	wzContain(t, "bus input", h.run.matching("gum input --header This machine's bus address")[1], "--value =not-an-ip")
	wzContain(t, "create", strings.Join(h.run.matching("env "), "\n"), "--org example-org --address 192.0.2.10")
}

// ── cancel, no, and the summary ──────────────────────────────────────────────────────────

func TestEscOnAnyQuestionCancelsWithNothingChanged(t *testing.T) {
	cases := []struct {
		name  string
		setup func(h *wzHarness)
	}{
		{"scope", func(h *wzHarness) { h.run.queue("gum choose", wzExit(130)) }},
		{"empty scope", func(h *wzHarness) { h.run.queue("gum choose", wzOK("\n")) }},
		{"input Ctrl-C", func(h *wzHarness) {
			h.run.queue("gum choose", wzOK(scopeRepos+"\n"))
			h.run.queue("gum input", wzExit(130))
		}},
		{"input Esc", func(h *wzHarness) {
			h.run.queue("gum choose", wzOK(scopeRepos+"\n"))
			h.run.queue("gum input", wzExit(1))
		}},
		{"summary Ctrl-C", func(h *wzHarness) {
			h.run.queue("gum choose", wzOK(scopeRepos+"\n"))
			h.run.queue("gum input", wzOK("example-org/proj\n"), wzOK("proj\n"), wzOK("Builds the example site\n"), wzOK("owner\n"), wzOK("\n"))
			h.run.queue("gum confirm", wzExit(130))
		}},
		{"sign-in offer Ctrl-C", func(h *wzHarness) {
			h.run.queue("gum choose", wzOK(scopeRepos+"\n"))
			h.run.queue("gum input", wzOK("example-org/proj\n"), wzOK("proj\n"), wzOK("Builds the example site\n"), wzOK("owner\n"), wzOK("\n"))
			h.run.queue("gum confirm", wzOK(""), wzExit(130))
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			h := newWzHarness(t)
			c.setup(h)
			wzCode(t, h.invoke(), 130, h)
			wzContain(t, "stderr", h.err(), "Cancelled; nothing was changed.")
			wzLack(t, "stderr", h.err(), "Traceback")
		})
	}
}

func TestCancellingBeforeTheSummaryWritesNothing(t *testing.T) {
	h := newWzHarness(t)
	h.run.queue("gum choose", wzOK(scopeRepos+"\n"))
	h.run.queue("gum input", wzExit(130))
	wzCode(t, h.invoke(), 130, h)
	if h.run.called("env PYTHONPATH=/root python3 -P -m helpers.agent_bus.declare create") || len(h.applied) != 0 {
		t.Error("something was written or installed")
	}
}

func TestNoAtTheSummaryAddsNothing(t *testing.T) {
	h := newWzHarness(t)
	h.run.queue("gum choose", wzOK(scopeRepos+"\n"))
	h.run.queue("gum input", wzOK("example-org/proj\n"), wzOK("proj\n"), wzOK("Builds the example site\n"), wzOK("owner\n"), wzOK("\n"))
	h.run.queue("gum confirm", wzExit(1))
	wzCode(t, h.invoke(), 1, h)
	wzContain(t, "stderr", h.err(), "Nothing was added.")
	if len(h.applied) != 0 {
		t.Error("installed")
	}
	// the dry run ran first, and nothing real did
	if !h.run.called(strings.Join(wzDP("create", "--team", "proj"), " ")) {
		t.Error("no dry run")
	}
	if got := len(h.run.matching("env PYTHONPATH=/root python3 -P -m helpers.agent_bus.declare create")); got != 1 {
		t.Errorf("%d create calls", got)
	}
}

func TestADryRunTheTeamFileRefusesStopsBeforeAnythingIsAsked(t *testing.T) {
	h := newWzHarness(t)
	h.run.queue("declare create --dry-run", execx.Result{ExitCode: 78, Stderr: []byte("declare: team file: too many\n")})
	h.run.queue("gum choose", wzOK(scopeRepos+"\n"))
	h.run.queue("gum input", wzOK("example-org/proj\n"), wzOK("proj\n"), wzOK("Builds the example site\n"), wzOK("owner\n"), wzOK("\n"))
	wzCode(t, h.invoke(), 78, h)
	wzContain(t, "stderr", h.err(), "declare: team file: too many")
	if h.run.called("gum confirm") {
		t.Error("asked the summary question after a refused dry run")
	}
}

// ── --yes ────────────────────────────────────────────────────────────────────────────────

func TestYesWithEveryValueCreatesInstallsAndPrintsTheNextSteps(t *testing.T) {
	h := newWzHarness(t)
	h.tty = false
	wzCode(t, h.invoke("--yes", "--description", "Builds the site", "--team", "beta", "--human", "owner", "--org", "Example-Org"), 0, h)
	wzContain(t, "stdout", h.stdout.String(), "CHANGED\tteam proj, port 6169")
	if len(h.applied) != 1 || h.applied[0][0] != "beta" {
		t.Errorf("applied %v", h.applied)
	}
	e := h.err()
	wzContain(t, "stderr", e, "ccy --teams proj", "Sign in", "Element ")
	wzLack(t, "stderr", e, "meta-deploy")
	for _, l := range h.run.lines() {
		if strings.HasPrefix(l, "gum ") {
			t.Errorf("--yes used gum: %s", l)
		}
	}
	if len(h.pwd) != 0 || len(h.element) != 0 || len(h.execd) != 0 {
		t.Error("--yes offered something")
	}
	if h.run.called("env PYTHONPATH=/root python3 -P -m helpers.agent_bus.declare create --file " + wzHostVars + " --team beta --human owner --org example-org --dry-run") {
		t.Error("--yes dry-ran")
	}
	// the team, then Element, then the install
	create := h.run.first("env PYTHONPATH=/root python3 -P -m helpers.agent_bus.declare create")
	elem := h.run.first("env PYTHONPATH=/root python3 -P -m helpers.agent_bus.declare element")
	steps := h.run.first("env PYTHONPATH=/root python3 -P -m helpers.agent_bus.declare next-steps")
	if create < 0 || elem < create || steps < elem {
		t.Errorf("order create %d element %d next-steps %d", create, elem, steps)
	}
}

func TestYesChecksTheOrgOnGitHubAsTheQuestionWould(t *testing.T) {
	h := newWzHarness(t)
	h.tty = false
	wzCode(t, h.invoke("--yes", "--description", "Builds the site", "--team", "beta", "--human", "owner", "--org", "example-org"), 0, h)
	if !h.run.called("gh auth status") || !h.run.called("gh api users/example-org --silent") {
		t.Errorf("not looked up: %v", h.run.lines())
	}
	if h.run.called("gum spin") {
		t.Error("--yes needs no gum")
	}
	h = newWzHarness(t)
	h.tty = false
	wzCode(t, h.invoke("--yes", "--description", "Builds the site", "--team", "beta", "--human", "owner", "--org", "nope"), 1, h)
	wzContain(t, "stderr", h.err(),
		"GitHub has no user or organisation called 'nope' that your gh login can see.",
		"ERROR: create stopped: fix the organisation above (or drop it); nothing was written.")
	wzLack(t, "stderr", h.err(), "Try again")
	if h.run.called("env PYTHONPATH=/root python3 -P -m helpers.agent_bus.declare create") || len(h.applied) != 0 {
		t.Error("something was written")
	}
}

func TestAnOrgGivenAsAFlagIsCheckedInTheInteractiveRunToo(t *testing.T) {
	h := newWzHarness(t)
	h.run.queue("gum input", wzOK("beta\n"), wzOK("Builds the example site\n"), wzOK("owner\n"), wzOK("\n"))
	h.run.queue("gum confirm", wzOK(""), wzExit(1), wzExit(1), wzExit(1))
	wzCode(t, h.invoke("--org", "example-org"), 0, h)
	if !h.run.called("gh auth status") || !h.run.called("gum spin --title Looking up example-org on GitHub… -- gh api users/example-org --silent") {
		t.Errorf("not looked up: %v", h.run.lines())
	}
	wzContain(t, "steps", h.err(), "1/4  Team name", "4/4  Other machines")
	h = newWzHarness(t)
	h.run.queue("gum spin", wzExit(1))
	wzCode(t, h.invoke("--org", "nope"), 1, h)
	wzContain(t, "stderr", h.err(), "no user or organisation called 'nope'", "create stopped")
}

func TestYesChecksEveryRepositoryAndNamesTheBadOnes(t *testing.T) {
	h := newWzHarness(t)
	h.tty = false
	h.fake.On(wzDP("check", "--repo", "bad"), wzRefuse("declare: --repo: no"))
	wzCode(t, h.invoke("--yes", "--description", "Builds the site", "--team", "beta", "--human", "owner", "--repo", "example-org/proj",
		"--repo", "example-org/readonly", "--repo", "other/missing", "--repo", "bad"), 1, h)
	e := h.err()
	wzContain(t, "stderr", e, "example-org/readonly: read-only", "other/missing: not found", "bad: not OWNER/REPO",
		"ERROR: create stopped: fix the repositories above (or drop them); nothing was written.")
	wzLack(t, "stderr", e, "example-org/proj:", "Try again")
	if h.run.called("env PYTHONPATH=/root python3 -P -m helpers.agent_bus.declare create") || len(h.applied) != 0 {
		t.Error("something was written or installed")
	}
}

func TestYesWithGoodRepositoriesWritesThem(t *testing.T) {
	h := newWzHarness(t)
	h.tty = false
	wzCode(t, h.invoke("--yes", "--description", "Builds the site", "--team", "beta", "--human", "owner", "--repo", "Example-Org/Proj", "--repo", "other/tool"), 0, h)
	for _, r := range []string{"example-org/proj", "other/tool"} {
		if !h.run.called("gh api repos/" + r + " --jq .permissions.push") {
			t.Errorf("%s not checked", r)
		}
	}
	wzContain(t, "create", strings.Join(h.run.matching("env "), "\n"), "--repo example-org/proj --repo other/tool")
	if !h.run.called(strings.Join(wzDP("check", "--repo", "example-org/proj", "--repo", "other/tool"), " ")) {
		t.Error("the whole list was not checked")
	}
}

func TestYesWithNoBusAddressPassesDeclaresRefusalThrough(t *testing.T) {
	h := newWzHarness(t)
	h.tty = false
	h.run.queue("declare create", execx.Result{ExitCode: 78, Stderr: []byte("declare: no bus address: run ccy-teams create on a terminal\n")})
	wzCode(t, h.invoke("--yes", "--description", "Builds the site", "--team", "beta", "--human", "owner", "--org", "example-org"), 78, h)
	wzContain(t, "stderr", h.err(), "declare: no bus address: run ccy-teams create on a terminal")
	wzLack(t, "stderr", h.err(), "localhost.yml")
	if len(h.applied) != 0 {
		t.Error("installed")
	}
}

// The team is declared and installed in one hold of the play lock, so no other play run comes
// between the write and its apply.
func TestTheTeamIsDeclaredAndInstalledInOneHoldOfThePlayLock(t *testing.T) {
	h := newWzHarness(t)
	h.tty = false
	wzCode(t, h.invoke("--yes", "--description", "Builds the site", "--team", "beta", "--human", "owner", "--org", "example-org"), 0, h)
	if want := []string{"create team beta: declared before false, after true, applied 1"}; !slices.Equal(h.locks, want) {
		t.Fatalf("holds %q, want %q", h.locks, want)
	}
}

// The owner's defect: a create while another play run holds the lock, and the wait for it does
// not end in the lock. Nothing is declared, nothing is installed, and the person is told so.
func TestAPlayLockNotTakenAddsNothing(t *testing.T) {
	h := newWzHarness(t)
	h.tty = false
	h.lockErr = fmt.Errorf("gave up: %w", playlock.ErrNotTaken)
	wzCode(t, h.invoke("--yes", "--description", "Builds the site", "--team", "beta", "--human", "owner", "--org", "example-org"), 1, h)
	if h.run.called("env PYTHONPATH=/root python3 -P -m helpers.agent_bus.declare create") || len(h.applied) != 0 {
		t.Fatal("declared or installed without the lock")
	}
	wzContain(t, "stderr", h.err(), "ERROR: nothing was added: team beta was not saved, as the play lock was not free (above). "+
		"Once the other play run has finished, run ccy-teams create again.")
	wzLack(t, "stderr", h.err(), "declared in")
}

func TestAFailedInstallSaysHowToRetry(t *testing.T) {
	h := newWzHarness(t)
	h.tty = false
	h.applyErr = errors.New("play failed")
	wzCode(t, h.invoke("--yes", "--description", "Builds the site", "--team", "beta", "--human", "owner", "--org", "example-org"), 1, h)
	wzContain(t, "stderr", h.err(),
		"ERROR: team beta is declared in ~/fd/localhost.yml but not applied as declared. Once the cause above is fixed, ccy-teams apply beta applies it.")
	if h.run.called("env PYTHONPATH=/root python3 -P -m helpers.agent_bus.declare next-steps") {
		t.Error("next steps after a failed install")
	}
}

// The play made the person's account before the apply failed, so the sign-in is still looked
// at (asked on a terminal, said otherwise) and the failure still stands.
func TestAFailedInstallStillLooksAtTheSignInOfThePeopleMade(t *testing.T) {
	for _, tty := range []bool{false, true} {
		h := newWzHarness(t)
		h.tty = tty
		h.applyErr = errors.New("play failed")
		wzCode(t, h.invoke("--yes", "--description", "Builds the site", "--team", "beta", "--human", "owner", "--org", "example-org"), 1, h)
		if len(h.signIn) != 1 || h.signIn[0] != "beta ask=false" {
			t.Errorf("tty %v: sign-in looked at %v", tty, h.signIn)
		}
		wzContain(t, "stderr", h.err(), "ccy-teams apply beta applies it.")
	}
}

func TestAnInteractiveFailedInstallOffersTheSignInBeforeItsFailure(t *testing.T) {
	h := newWzHarness(t)
	h.applyErr = errors.New("play failed")
	h.run.queue("gum choose", wzOK(scopeRepos+", starting with example-org/proj\n"))
	h.run.queue("gum input", wzOK("example-org/proj\n"), wzOK("proj\n"), wzOK("Builds the example site\n"), wzOK("owner\n"), wzOK("\n"))
	h.run.queue("gum confirm", wzOK(""))
	wzCode(t, h.invoke(), 1, h)
	if len(h.signIn) != 1 || h.signIn[0] != "proj ask=true" {
		t.Errorf("sign-in looked at %v", h.signIn)
	}
	if h.pwd != nil {
		t.Errorf("the success path's offer ran as well: %v", h.pwd)
	}
}

// A sign-in offer that breaks is said, and the failed apply keeps its own message and exit.
func TestAFailedInstallKeepsItsMessageAndExitWhenTheSignInOfferBreaks(t *testing.T) {
	h := newWzHarness(t)
	h.tty = false
	h.applyErr = errors.New("play failed")
	h.c.SignInGap = func(context.Context, []string, bool) error { return errors.New("gum broke") }
	wzCode(t, h.invoke("--yes", "--description", "Builds the site", "--team", "beta", "--human", "owner", "--org", "example-org"), 1, h)
	wzContain(t, "stderr", h.err(), "gum broke", "ccy-teams apply beta applies it.")
	wzLack(t, "stderr", h.err(), "nothing was changed")
}

func TestAPassedInstallLeavesTheSignInToItsOwnOffer(t *testing.T) {
	h := newWzHarness(t)
	h.happyAnswers()
	wzCode(t, h.invoke(), 0, h)
	if len(h.signIn) != 0 {
		t.Errorf("sign-in looked at twice: %v", h.signIn)
	}
}

func TestAnElementEntryThatCannotBeDeclaredFailsBeforeTheInstall(t *testing.T) {
	h := newWzHarness(t)
	h.tty = false
	h.fake.On(wzDP("element"), execx.Result{ExitCode: 78, Stderr: []byte("declare: team beta has no listen address\n")})
	wzCode(t, h.invoke("--yes", "--description", "Builds the site", "--team", "beta", "--human", "owner", "--org", "example-org"), 78, h)
	if len(h.applied) != 0 {
		t.Error("installed after a failed profile")
	}
}

func TestAnUnchangedCreatePrintsUNCHANGED(t *testing.T) {
	h := newWzHarness(t)
	h.tty = false
	h.fake.On(wzDP("create"), wzOK("UNCHANGED\tbeta\n"))
	wzCode(t, h.invoke("--yes", "--description", "Builds the site", "--team", "beta", "--human", "owner", "--org", "example-org"), 0, h)
	if got := h.stdout.String(); got != "UNCHANGED\tbeta\n" {
		t.Errorf("stdout %q", got)
	}
}

// ── flags and preparation ───────────────────────────────────────────────────────────────

func TestFlagsNeedValuesAndUnknownOnesAreRefused(t *testing.T) {
	for _, flag := range []string{"--team", "--human", "--org", "--repo", "--allow-from"} {
		h := newWzHarness(t)
		wzCode(t, h.invoke("--yes", flag), 64, h)
		if want := "ccy-teams: create's " + flag + " needs a value. See ccy-teams --help.\n"; h.err() != want {
			t.Errorf("%s: %q", flag, h.err())
		}
	}
	h := newWzHarness(t)
	wzCode(t, h.invoke("--bogus"), 64, h)
	if want := "ccy-teams: unknown create option '--bogus'. See ccy-teams --help.\n"; h.err() != want {
		t.Errorf("%q", h.err())
	}
}

func TestAFlagValueMayLookLikeAFlag(t *testing.T) {
	h := newWzHarness(t)
	h.tty = false
	wzCode(t, h.invoke("--yes", "--description", "Builds the site", "--team", "--yes", "--human", "owner", "--org", "example-org"), 0, h)
	wzContain(t, "create", strings.Join(h.run.matching("env "), "\n"), "--team --yes")
}

func TestWithoutATerminalAndWithoutYesTheQuestionsCannotBeAsked(t *testing.T) {
	h := newWzHarness(t)
	h.tty = false
	wzCode(t, h.invoke(), 64, h)
	want := "ccy-teams: 'create' asks its questions on a terminal; without one give every value as a flag, and --yes. See ccy-teams --help.\n"
	if h.err() != want {
		t.Errorf("%q", h.err())
	}
	if h.run.called("gum") {
		t.Error("gum ran")
	}
}

func TestWithoutGumTheWizardNamesThePlayThatInstallsIt(t *testing.T) {
	h := newWzHarness(t)
	h.missing["gum"] = true
	wzCode(t, h.invoke(), 1, h)
	want := "ERROR: gum is not installed, and ccy-teams needs it here. playbooks/imports/play-claude-yolo.yml installs it; run that play and try again.\n"
	if h.err() != want {
		t.Errorf("%q", h.err())
	}
}

func TestWithoutGhTheWizardNamesThePlayThatInstallsIt(t *testing.T) {
	h := newWzHarness(t)
	h.missing["gh"] = true
	wzCode(t, h.invoke(), 1, h)
	wzContain(t, "stderr", h.err(), "ERROR: gh is not installed, and ccy-teams needs it here. playbooks/imports/play-git-configure-and-tools.yml installs it; run that play and try again.")
}

func TestGhNeedsNoteWhenEveryScopeIsGivenAsRepositories(t *testing.T) {
	h := newWzHarness(t)
	h.missing["gh"] = true
	h.run.queue("gum input", wzOK("beta\n"), wzOK("Builds the example site\n"), wzOK("owner\n"), wzOK("\n"))
	h.run.queue("gum confirm", wzOK(""), wzExit(1), wzExit(1), wzExit(1))
	// interactive with --repo: no question is asked about GitHub, so gh is not needed
	wzCode(t, h.invoke("--repo", "example-org/proj"), 0, h)
	wzLack(t, "stderr", h.err(), "gh is not installed")
}

func TestAGhThatIsNotLoggedInIsSaidWithItsOwnWords(t *testing.T) {
	h := newWzHarness(t)
	h.fake.On([]string{"gh", "auth", "status"}, execx.Result{ExitCode: 1, Stderr: []byte("You are not logged into any GitHub hosts.\n")})
	wzCode(t, h.invoke(), 1, h)
	wzContain(t, "stderr", h.err(),
		"ERROR: gh is not logged in to GitHub, and the org is looked up there (gh said: You are not logged into any GitHub hosts.). Run gh auth login, then try again.")
	if h.run.called("gum choose") {
		t.Error("went on")
	}
}

func TestTheRemoteIsReducedByDeclareAndAnAddressErrorEndsTheRun(t *testing.T) {
	h := newWzHarness(t)
	h.fake.On(wzDP("address"), execx.Result{ExitCode: 78, Stderr: []byte("declare: cannot read\n")})
	wzCode(t, h.invoke(), 78, h)
	wzContain(t, "stderr", h.err(), "declare: cannot read")
	if !h.run.called("env PYTHONPATH=/root python3 -P -m helpers.agent_bus.declare github-remote --url git@github.com:Example-Org/Proj.git") {
		t.Errorf("remote not reduced: %v", h.run.lines())
	}
}

func TestACheckoutWithNoRemoteHasNoDefaultRepository(t *testing.T) {
	h := newWzHarness(t)
	h.run.queue("git remote", wzExit(2))
	h.run.queue("gum choose", wzOK(scopeOwner+"\n"))
	h.run.queue("gum input", wzOK("example-org\n"), wzOK("example-org\n"), wzOK("Builds the example site\n"), wzOK("owner\n"), wzOK("\n"))
	h.run.queue("gum confirm", wzOK(""), wzExit(1), wzExit(1))
	wzCode(t, h.invoke(), 0, h)
	if h.run.called("env PYTHONPATH=/root python3 -P -m helpers.agent_bus.declare github-remote") {
		t.Error("reduced a remote that is not there")
	}
	wzContain(t, "choose", strings.Join(h.run.matching("gum choose"), "\n"), "--selected "+scopeOwner+" ")
}

func TestGitThatCannotRunEndsTheRun(t *testing.T) {
	h := newWzHarness(t)
	h.fake.On([]string{"git", "rev-parse", "--is-inside-work-tree"}, execx.Result{})
	h.c.Git = gitx.New(wzFailRunner{})
	wzCode(t, h.invoke(), 1, h)
	wzContain(t, "stderr", h.err(), "ERROR:", "git")
}

type wzFailRunner struct{}

func (wzFailRunner) Run(context.Context, execx.Cmd) (execx.Result, error) {
	return execx.Result{}, errors.New("cannot start")
}

func TestAGumThatCannotRunEndsTheRunWithAnError(t *testing.T) {
	h := newWzHarness(t)
	h.run.queue("gum choose", wzOK(scopeRepos+"\n"))
	h.c.Gum = &ui.Gum{Runner: wzFailRunner{}, Stderr: h.stderr}
	wzCode(t, h.invoke(), 1, h)
	wzContain(t, "stderr", h.err(), "ERROR:", "cannot start")
}

// ── after create ─────────────────────────────────────────────────────────────────────────

func TestAcceptingTheSignInAndElementOffersRunThem(t *testing.T) {
	h := newWzHarness(t)
	h.run.queue("gum choose", wzOK(scopeRepos+"\n"))
	h.run.queue("gum input", wzOK("example-org/proj\n"), wzOK("proj\n"), wzOK("Builds the example site\n"), wzOK("owner, colleague\n"), wzOK("\n"))
	h.run.queue("gum confirm", wzOK(""), wzOK(""), wzExit(1), wzOK(""), wzExit(1))
	wzCode(t, h.invoke(), 0, h)
	if !slices.Equal(h.pwd, []string{"proj/owner"}) {
		t.Errorf("passwords %v", h.pwd)
	}
	if !slices.Equal(h.element, []string{"proj"}) {
		t.Errorf("element %v", h.element)
	}
	if len(h.execd) != 0 {
		t.Error("a session was started after a no")
	}
}

func TestOffersThatFailDoNotStopTheRestOrTheNextSteps(t *testing.T) {
	h := newWzHarness(t)
	h.pwdErr = errors.New("sudo said no")
	h.elemErr = errors.New("element failed")
	h.happyAnswers()
	h.run.queues["gum confirm"] = []execx.Result{wzOK(""), wzOK(""), wzOK(""), wzExit(1)}
	wzCode(t, h.invoke(), 0, h)
	if len(h.pwd) != 1 || len(h.element) != 1 {
		t.Errorf("pwd %v element %v", h.pwd, h.element)
	}
	wzContain(t, "stderr", h.err(), "ccy --teams proj", "The team is made either way.")
	if len(h.execd) != 0 {
		t.Error("exec after no")
	}
}

func TestACancelInsideAnOfferCancelsTheRun(t *testing.T) {
	h := newWzHarness(t)
	h.pwdErr = fmt.Errorf("password: %w", ui.ErrCancelled)
	h.run.queue("gum choose", wzOK(scopeRepos+"\n"))
	h.run.queue("gum input", wzOK("example-org/proj\n"), wzOK("proj\n"), wzOK("Builds the example site\n"), wzOK("owner\n"), wzOK("\n"))
	h.run.queue("gum confirm", wzOK(""), wzOK(""))
	wzCode(t, h.invoke(), 130, h)
	wzContain(t, "stderr", h.err(), "Cancelled; nothing was changed.")
}

func TestTheNextStepsDoNotRepeatTheOffersJustMade(t *testing.T) {
	h := newWzHarness(t)
	h.happyAnswers()
	wzCode(t, h.invoke(), 0, h)
	e := h.err()
	wzContain(t, "stderr", e, "Team proj is up.", "run ccy --teams proj", "(ccy-teams run there offers it")
	wzLack(t, "stderr", e, "  Sign in      Enter on your name", "  Element      Enter on the team")
}

func TestTheNextStepsKeepEveryLineWhenNothingWasOffered(t *testing.T) {
	h := newWzHarness(t)
	h.tty = false
	wzCode(t, h.invoke("--yes", "--description", "Builds the site", "--team", "beta", "--human", "owner", "--org", "example-org"), 0, h)
	wzContain(t, "stderr", h.err(), "  Sign in      Enter on your name", "  Element      Enter on the team, then choose Open in Element")
}

func TestNextStepsThatCannotBeReadEndTheRun(t *testing.T) {
	h := newWzHarness(t)
	h.tty = false
	h.fake.On(wzDP("next-steps"), execx.Result{ExitCode: 78, Stderr: []byte("declare: team beta is not declared\n")})
	wzCode(t, h.invoke("--yes", "--description", "Builds the site", "--team", "beta", "--human", "owner", "--org", "example-org"), 78, h)
}

// ── offer_join ───────────────────────────────────────────────────────────────────────────

func TestNoSessionIsOfferedOutsideACheckout(t *testing.T) {
	h := newWzHarness(t)
	h.happyAnswers()
	h.run.queue("git rev-parse", wzOK("true\n"), wzExit(128)) // here: a work tree; offer: not one
	h.run.queues["gum confirm"] = []execx.Result{wzOK(""), wzExit(1), wzExit(1)}
	wzCode(t, h.invoke(), 0, h)
	if h.run.called("gum confirm --default=true Start a ccy session") || len(h.execd) != 0 {
		t.Error("offered")
	}
}

func TestNoSessionIsOfferedForATeamThatDoesNotCoverTheCheckout(t *testing.T) {
	h := newWzHarness(t)
	h.happyAnswers()
	h.fake.On(wzDP("teams-for"), wzOK("TEAM\tother\n"))
	h.run.queues["gum confirm"] = []execx.Result{wzOK(""), wzExit(1), wzExit(1)}
	wzCode(t, h.invoke(), 0, h)
	if h.run.called("gum confirm --default=true Start a ccy session") {
		t.Error("offered")
	}
}

func TestADeclinedSessionStartsNothing(t *testing.T) {
	h := newWzHarness(t)
	h.happyAnswers()
	h.run.queues["gum confirm"] = []execx.Result{wzOK(""), wzExit(1), wzExit(1), wzExit(1)}
	wzCode(t, h.invoke(), 0, h)
	if len(h.execd) != 0 {
		t.Error("exec")
	}
	wzLack(t, "stderr", h.err(), "Starting ccy")
}

func TestTeamsForThatFailsIsSaidAndTheOfferSkipped(t *testing.T) {
	h := newWzHarness(t)
	h.happyAnswers()
	h.fake.On(wzDP("teams-for"), execx.Result{ExitCode: 78, Stderr: []byte("declare: cannot read\n")})
	h.run.queues["gum confirm"] = []execx.Result{wzOK(""), wzExit(1), wzExit(1)}
	wzCode(t, h.invoke(), 0, h)
	wzContain(t, "stderr", h.err(), "declare: cannot read")
}

func TestALauncherThatCannotBeExecutedIsAnError(t *testing.T) {
	h := newWzHarness(t)
	h.happyAnswers()
	h.execErr = errors.New("exec format error")
	wzCode(t, h.invoke(), 1, h)
	wzContain(t, "stderr", h.err(), "ERROR:", "exec format error")
}

func TestTheTopOfTheCheckoutCannotBeFound(t *testing.T) {
	h := newWzHarness(t)
	h.happyAnswers()
	h.run.queue("git rev-parse", wzOK("true\n"), wzOK("true\n"), wzExit(128))
	h.fake.On([]string{"git", "rev-parse", "--show-toplevel"}, wzExit(128))
	wzCode(t, h.invoke(), 1, h)
	wzContain(t, "stderr", h.err(), "ERROR:")
}

func TestLoginLookupFailureEndsTheRun(t *testing.T) {
	h := newWzHarness(t)
	h.fake.On([]string{"id", "-un"}, wzExit(1))
	h.run.queue("gum choose", wzOK(scopeRepos+"\n"))
	h.run.queue("gum input", wzOK("example-org/proj\n"), wzOK("proj\n"))
	wzCode(t, h.invoke(), 1, h)
	wzContain(t, "stderr", h.err(), "ERROR:", "id")
}

func TestSuggestingATeamNameThatFailsEndsTheRun(t *testing.T) {
	h := newWzHarness(t)
	h.fake.On(wzDP("suggest-team"), execx.Result{ExitCode: 78, Stderr: []byte("declare: broken\n")})
	h.run.queue("gum choose", wzOK(scopeRepos+"\n"))
	h.run.queue("gum input", wzOK("example-org/proj\n"))
	wzCode(t, h.invoke(), 78, h)
}

func TestAHardFailureOfACheckerEndsTheRunNotTheRetries(t *testing.T) {
	h := newWzHarness(t)
	h.c.Declare = declare.NewClient(wzFailRunner{}, wzRoot, wzHostVars)
	h.run.queue("gum choose", wzOK(scopeRepos+"\n"))
	wzCode(t, h.invoke(), 1, h)
	wzContain(t, "stderr", h.err(), "ERROR:")
}

// ── the description (Plan 00201 S-A14) ───────────────────────────────────────────────────

func TestTheWizardAsksWhatTheTeamIsForAndShowsItInTheSummary(t *testing.T) {
	h := newWzHarness(t)
	h.happyAnswers()
	wzCode(t, h.invoke(), 0, h)
	wzContain(t, "stderr", h.err(), "What is this team for")
	wzContain(t, "summary", h.boxes(), "For       Builds the example site")
	if !h.run.called(strings.Join(wzDP("check", "--description", "Builds the example site"), " ")) {
		t.Errorf("the answer was not checked: %v", h.run.lines())
	}
	order := []string{"gum input --header Team name", "gum input --header What is this team for?", "gum input --header People"}
	last := -1
	for _, prefix := range order {
		at := -1
		for i, l := range h.run.lines() {
			if i > last && strings.HasPrefix(l, prefix) {
				at = i
				break
			}
		}
		if at < 0 {
			t.Fatalf("%q does not come after step %d", prefix, last)
		}
		last = at
	}
}

func TestARefusedDescriptionIsAskedAgainAsTyped(t *testing.T) {
	h := newWzHarness(t)
	h.run.queue("gum choose", wzOK(scopeRepos+"\n"))
	h.run.queue("gum input", wzOK("example-org/proj\n"), wzOK("proj\n"), wzOK("bad one\n"), wzOK("Builds the example site\n"), wzOK("owner\n"), wzOK("\n"))
	h.fake.On(wzDP("check", "--description", "bad one"), wzRefuse("declare: --description: team file: description: must be 1 to 120 characters"))
	h.run.queue("gum confirm", wzOK(""), wzExit(1), wzExit(1), wzOK(""))
	wzCode(t, h.invoke(), 0, h)
	wzContain(t, "stderr", h.err(), "must be 1 to 120 characters")
	inputs := h.run.matching("gum input --header What is this team for?")
	if len(inputs) != 2 || !strings.Contains(inputs[1], "--value bad one") {
		t.Errorf("the second try starts from what was typed: %v", inputs)
	}
}

func TestADescriptionGivenAsAFlagIsNotAskedAndIsChecked(t *testing.T) {
	h := newWzHarness(t)
	h.run.queue("gum choose", wzOK(scopeRepos+"\n"))
	h.run.queue("gum input", wzOK("example-org/proj\n"), wzOK("proj\n"), wzOK("owner\n"), wzOK("\n"))
	h.run.queue("gum confirm", wzOK(""), wzExit(1), wzExit(1), wzOK(""))
	wzCode(t, h.invoke("--description", "Flag given"), 0, h)
	if h.run.called("gum input --header What is this team for?") {
		t.Error("asked for what the flag gave")
	}
	wzContain(t, "summary", h.boxes(), "For       Flag given")
	wzContain(t, "stderr", h.err(), "1/5  What the team works on")
}

func TestYesNeedsADescription(t *testing.T) {
	h := newWzHarness(t)
	h.tty = false
	wzCode(t, h.invoke("--yes", "--team", "beta", "--human", "owner", "--org", "example-org"), 64, h)
	wzContain(t, "stderr", h.err(), "create's --yes needs --description", "See ccy-teams --help.")
	if h.run.called("env PYTHONPATH=/root python3 -P -m helpers.agent_bus.declare create") || len(h.applied) != 0 {
		t.Error("something was written or installed")
	}
}

func TestYesPassesTheDescriptionToTheEntry(t *testing.T) {
	h := newWzHarness(t)
	h.tty = false
	wzCode(t, h.invoke("--yes", "--description", "Builds the site", "--team", "beta", "--human", "owner", "--org", "example-org"), 0, h)
	wzContain(t, "create", strings.Join(h.run.matching("env "), "\n"),
		"create --file "+wzHostVars+" --team beta --human owner --org example-org --description Builds the site")
}
