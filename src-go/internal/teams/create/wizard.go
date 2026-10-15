// Package create is "ccy-teams create": the questions for what the flags leave out, a summary
// and a yes, then the entry written by declare.py and the team installed. It owns the wizard
// only; installing the team, signing a person in and opening Element are flows it is handed.
package create

import (
	"context"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/ccyenv"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/declare"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/gitx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/host"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/login"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/ops"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/ui"
)

// The two ways a team's trust is scoped, in the words the person chooses between.
const (
	scopeRepos = "Repositories you list (one or more)"
	scopeOwner = "Everything one GitHub owner owns"
)

const (
	// maxTries is how often a refused answer is asked again before the run ends.
	maxTries = 3
	// reposMax is the most repositories a team may list (declare.py check enforces it).
	reposMax = 32

	playGum = "playbooks/imports/play-claude-yolo.yml"
	playGh  = "playbooks/imports/play-git-configure-and-tools.yml"
)

// stop ends the run with an exit code; whatever the person needs to read has been printed.
type stop struct{ code term.ExitCode }

func (s *stop) Error() string { return fmt.Sprintf("stopped with exit %d", s.code) }

// Wizard is "ccy-teams create": the questions for what the flags leave out, a summary and a
// yes, then the entry written by declare.py and the team installed. Every dependency is a
// field so a test can stand in for each.
type Wizard struct {
	Declare *declare.Client
	Gum     *ui.Gum
	Git     *gitx.Git
	// NewTerm is a Term of its own for each run: the step counters live in it.
	NewTerm func() *term.Term
	Runner  execx.Runner // gh, id

	Stdin  io.Reader // must be a terminal when questions are asked
	Stdout io.Writer // the CHANGED line declare.py prints

	Cwd      string
	HostVars string
	Home     string // for the ~ abbreviation
	Launcher string // the ccy launcher offered after the team is made

	LookPath    func(name string) (string, error)
	ApplyTeams  func(ctx context.Context, teams ...string) error
	NewPassword func(ctx context.Context, team, human string) error
	// SignInGap is, after an apply that failed, the sign-in of the team's people who have no
	// Element session: the play made their accounts, so it is offered (ask) or said.
	SignInGap     func(ctx context.Context, teams []string, ask bool) error
	LaunchElement func(ctx context.Context, team string) error
	// ExecLauncher replaces this process with argv in dir. It returns only on failure
	// (a test's stand-in returns nil).
	ExecLauncher func(dir string, argv []string) error

	term *term.Term
}

// New is the Wizard over a host and the flows it hands the work after the entry is written to:
// apply installs the team, login signs a person in and opens Element.
func New(h *host.Host, o *ops.Ops, l *login.Login) *Wizard {
	return &Wizard{
		Declare: h.Declare, Gum: h.Gum, Git: h.Git, NewTerm: h.FreshTerm, Runner: h.Runner,
		Stdin: h.Stdin, Stdout: h.Stdout,
		Cwd: h.Here(), HostVars: h.HostVars, Home: h.Home, Launcher: h.Launcher,
		LookPath: h.LookPath,
		ApplyTeams: func(ctx context.Context, teams ...string) error {
			return o.ApplyTeams(ctx, teams)
		},
		NewPassword:   l.NewPassword,
		SignInGap:     l.SignInGap,
		LaunchElement: l.Open,
		ExecLauncher: func(dir string, argv []string) error {
			return h.Exec(h.Launcher, argv, dir)
		},
	}
}

// createOpts is what the flags said; the questions fill in the rest.
type createOpts struct {
	yes       bool
	team      string
	humans    []string
	org       string
	repos     []string
	allowFrom []string
	address   string
}

func (o *createOpts) entry() declare.Entry {
	return declare.Entry{
		Team: o.team, Humans: nonEmpty(o.humans), Org: o.org, Repos: nonEmpty(o.repos),
		AllowFrom: nonEmpty(o.allowFrom), Address: o.address,
	}
}

func nonEmpty(in []string) []string {
	var out []string
	for _, s := range in {
		if s != "" {
			out = append(out, s)
		}
	}
	return out
}

// Run is ccy-teams create with the arguments after "create". It returns the exit code:
// 0 done, 1 refused or failed, 64 usage, 130 cancelled, or declare.py's own refusal code.
func (c *Wizard) Run(ctx context.Context, args []string) term.ExitCode {
	c.term = c.NewTerm()
	err := c.create(ctx, args)
	if err == nil {
		return term.ExitOK
	}
	if s, ok := errors.AsType[*stop](err); ok {
		return s.code
	}
	if de, ok := errors.AsType[*declare.Error](err); ok {
		c.term.Printf("%s\n", de.Error())
		return term.ExitCode(de.Code)
	}
	c.term.PrintError(err.Error())
	return term.ExitFailure
}

func (c *Wizard) usage(msg string) error {
	c.term.Printf("ccy-teams: %s See ccy-teams --help.\n", msg)
	return &stop{term.ExitUsage}
}

func (c *Wizard) cancelled() error {
	c.term.Printf("Cancelled; nothing was changed.\n")
	return &stop{term.ExitCancelled}
}

// failed prints a refusal as an ERROR and ends the run with exit 1.
func (c *Wizard) failed(msg string) error {
	c.term.PrintError(msg)
	return &stop{term.ExitFailure}
}

// gumErr is a gum question's error: leaving it is the run's cancellation.
func (c *Wizard) gumErr(err error) error {
	if errors.Is(err, ui.ErrCancelled) {
		return c.cancelled()
	}
	return err
}

func (c *Wizard) parseFlags(args []string) (createOpts, error) {
	var o createOpts
	for i := 0; i < len(args); i++ {
		flag := args[i]
		switch flag {
		case "--yes":
			o.yes = true
		case "--team", "--human", "--org", "--repo", "--allow-from":
			if i+1 >= len(args) {
				return o, c.usage("create's " + flag + " needs a value.")
			}
			i++
			value := args[i]
			switch flag {
			case "--team":
				o.team = value
			case "--human":
				o.humans = append(o.humans, value)
			case "--org":
				o.org = strings.ToLower(value)
			case "--repo":
				o.repos = append(o.repos, strings.ToLower(value))
			default:
				o.allowFrom = append(o.allowFrom, value)
			}
		default:
			return o, c.usage("unknown create option '" + flag + "'.")
		}
	}
	return o, nil
}

// needTool is a hard dependency: missing is a failure naming the play that installs it.
func (c *Wizard) needTool(name, play string) error {
	if _, err := c.LookPath(name); err != nil {
		return c.failed(name + " is not installed, and ccy-teams needs it here. " + play + " installs it; run that play and try again.")
	}
	return nil
}

// needGh is gh installed and logged in: the org and repositories are looked up there.
func (c *Wizard) needGh(ctx context.Context) error {
	if err := c.needTool("gh", playGh); err != nil {
		return err
	}
	res, err := c.Runner.Run(ctx, execx.Cmd{Name: "gh", Args: []string{"auth", "status"}})
	if err == nil {
		return nil
	}
	if !isExit(err) {
		return fmt.Errorf("gh auth status: %w", err)
	}
	said := strings.TrimSpace(string(res.Stdout) + string(res.Stderr))
	return c.failed("gh is not logged in to GitHub, and the org is looked up there (gh said: " + said + "). Run gh auth login, then try again.")
}

// remoteRepo is this checkout's lower-case GitHub OWNER/REPO, or "" outside one.
func (c *Wizard) remoteRepo(ctx context.Context) (string, error) {
	inTree, err := c.Git.ProjectHere(ctx, c.Cwd)
	if err != nil || !inTree {
		return "", err
	}
	url, err := c.Git.RemoteURL(ctx, c.Cwd)
	if err != nil || url == "" {
		return "", err
	}
	return c.Declare.GithubRemote(ctx, url)
}

func (c *Wizard) create(ctx context.Context, args []string) error {
	o, err := c.parseFlags(args)
	if err != nil {
		return err
	}
	here, err := c.remoteRepo(ctx)
	if err != nil {
		return err
	}
	if o.yes {
		if err = c.checkFlags(ctx, o, o.org != "", false); err != nil {
			return err
		}
	} else if err = c.ask(ctx, &o, here); err != nil {
		return err
	}
	return c.write(ctx, o, here)
}

// ask is the questions, for what the flags leave out, then the summary and its yes.
func (c *Wizard) ask(ctx context.Context, o *createOpts, here string) error {
	orgFlag := o.org != ""
	scopeGiven := orgFlag || len(o.repos) > 0
	prep, err := c.prepare(ctx, *o, scopeGiven)
	if err != nil {
		return err
	}
	if !scopeGiven {
		if err = c.askScope(ctx, o, here); err != nil {
			return err
		}
	}
	if err = c.checkFlags(ctx, *o, orgFlag, prep.ghReady); err != nil {
		return err
	}
	if err = c.askRest(ctx, o, prep.knownAddress); err != nil {
		return err
	}
	return c.confirmSummary(ctx, *o)
}

// prepared is what prepare found out: the bus address already declared, and whether gh
// has been checked.
type prepared struct {
	knownAddress string
	ghReady      bool
}

// prepare is what questions need before the first is asked: a terminal, gum, gh when
// GitHub is to be asked, the banner, and how many steps there are.
func (c *Wizard) prepare(ctx context.Context, o createOpts, scopeGiven bool) (prepared, error) {
	var prep prepared
	if !c.term.IsTerminal(c.Stdin) || !c.term.IsTerminal(c.term.Err) {
		return prep, c.usage("'create' asks its questions on a terminal; without one give every value as a flag, and --yes.")
	}
	if err := c.needTool("gum", playGum); err != nil {
		return prep, err
	}
	if !scopeGiven || o.org != "" {
		if err := c.needGh(ctx); err != nil {
			return prep, err
		}
		prep.ghReady = true
	}
	if err := c.intro(ctx); err != nil {
		return prep, err
	}
	address, err := c.Declare.Address(ctx)
	if err != nil {
		return prep, err
	}
	prep.knownAddress = address
	c.term.Steps = countSteps(o, scopeGiven, address)
	return prep, nil
}

func (c *Wizard) intro(ctx context.Context) error {
	if err := c.Gum.Box(ctx, true, "New agent team"); err != nil {
		return err
	}
	return c.Gum.Explain(ctx, "Agents and people working together in one private room on this machine, which you follow in Element. Ctrl-C leaves at any point; nothing changes until you say yes at the end.", 0)
}

// countSteps is how many questions this run asks: what the flags leave out.
func countSteps(o createOpts, scopeGiven bool, knownAddress string) int {
	steps := 0
	if !scopeGiven {
		steps += 2
	}
	if o.team == "" {
		steps++
	}
	if len(nonEmpty(o.humans)) == 0 {
		steps++
	}
	if len(nonEmpty(o.allowFrom)) == 0 {
		steps++
	}
	if knownAddress == "" {
		steps++
	}
	return steps
}

func (c *Wizard) step(ctx context.Context, title, hint string) error {
	c.term.Step(title)
	return c.Gum.Explain(ctx, hint, 5)
}

// ownerOf is the owner part of OWNER/REPO (all of a name with no slash).
func ownerOf(repo string) string {
	return strings.SplitN(repo, "/", 2)[0]
}

// repoPart is the repository part of OWNER/REPO (all of a name with no slash).
func repoPart(repo string) string {
	parts := strings.SplitN(repo, "/", 2)
	return parts[len(parts)-1]
}

// isExit is err being a command that ran and exited non-zero.
func isExit(err error) bool {
	var exit *execx.ExitError
	return errors.As(err, &exit)
}

// isRefusal is err being declare.py refusing a value, as opposed to failing to run.
func isRefusal(err error) bool {
	var refusal *declare.Error
	return errors.As(err, &refusal)
}

// askScope is what the team works on, first: its name defaults to that.
func (c *Wizard) askScope(ctx context.Context, o *createOpts, here string) error {
	// Inside a checkout the repository is the default; elsewhere the whole owner.
	choices := []string{scopeOwner, scopeRepos}
	if here != "" {
		choices = []string{scopeRepos + ", starting with " + here, scopeOwner + ": " + ownerOf(here)}
	}
	if err := c.step(ctx, "What the team works on", "Its agents may point each other only at these repositories, on any branch. Anything else reaches them as outside content, marked so."); err != nil {
		return err
	}
	scope, err := c.Gum.Choose(ctx, "Trust", choices[0], choices)
	if err != nil {
		return c.gumErr(err)
	}
	c.term.Answered(scope)
	if strings.HasPrefix(scope, scopeOwner) {
		return c.askOwner(ctx, o, here)
	}
	hint := fmt.Sprintf("owner/repo, comma-separated: one, or up to %d of any owners, each one your gh login can push to.", reposMax)
	if err = c.step(ctx, "Repositories", hint); err != nil {
		return err
	}
	text, err := c.askValid(ctx, func(ctx context.Context, answer string) (bool, error) {
		return c.checkRepos(ctx, splitCommas(answer), true)
	}, "Repositories (owner/repo, comma-separated)", "e.g. example-org/alpha", here)
	if err != nil {
		return err
	}
	o.repos = splitCommas(strings.ToLower(text))
	c.term.Answered(strings.Join(o.repos, " "))
	return nil
}

func (c *Wizard) askOwner(ctx context.Context, o *createOpts, here string) error {
	if err := c.step(ctx, "GitHub owner", "The organisation or user the team works for: all of its repositories, never forks."); err != nil {
		return err
	}
	owner, err := c.askValid(ctx, func(ctx context.Context, answer string) (bool, error) {
		return c.checkOwner(ctx, answer, true, true)
	}, "GitHub owner (organisation or user)", "e.g. example-org", ownerOf(here))
	if err != nil {
		return err
	}
	o.org = strings.ToLower(owner)
	c.term.Answered(o.org)
	return nil
}

// checkFlags looks up the owner and repositories the flags gave, on GitHub, as the questions
// would have: a team never trusts what its owner cannot see, whichever way it was named.
func (c *Wizard) checkFlags(ctx context.Context, o createOpts, orgFlag, ghReady bool) error {
	if !orgFlag && (!o.yes || len(o.repos) == 0) {
		return nil
	}
	if !ghReady {
		if err := c.needGh(ctx); err != nil {
			return err
		}
	}
	if orgFlag {
		good, err := c.checkOwner(ctx, o.org, !o.yes, false)
		if err != nil {
			return err
		}
		if !good {
			return c.failed("create stopped: fix the organisation above (or drop it); nothing was written.")
		}
	}
	if o.yes && len(o.repos) > 0 {
		good, err := c.checkRepos(ctx, o.repos, false)
		if err != nil {
			return err
		}
		if !good {
			return c.failed("create stopped: fix the repositories above (or drop them); nothing was written.")
		}
	}
	return nil
}

func (c *Wizard) askRest(ctx context.Context, o *createOpts, knownAddress string) error {
	if o.team == "" {
		if err := c.askTeam(ctx, o); err != nil {
			return err
		}
	}
	if len(nonEmpty(o.humans)) == 0 {
		if err := c.askHumans(ctx, o); err != nil {
			return err
		}
	}
	if len(nonEmpty(o.allowFrom)) == 0 {
		if err := c.askAllowFrom(ctx, o); err != nil {
			return err
		}
	}
	if knownAddress == "" {
		return c.askAddress(ctx, o)
	}
	return nil
}

func (c *Wizard) askTeam(ctx context.Context, o *createOpts) error {
	name := ""
	switch {
	case o.org != "":
		name = o.org
	case len(o.repos) > 0:
		name = repoPart(o.repos[0])
	}
	suggestion := ""
	if name != "" {
		var err error
		if suggestion, err = c.Declare.SuggestTeam(ctx, name); err != nil {
			return err
		}
	}
	if err := c.step(ctx, "Team name", "Used in the room's name and its agents' names: a-z, 0-9 and -, up to 24."); err != nil {
		return err
	}
	team, err := c.askValid(ctx, func(ctx context.Context, answer string) (bool, error) {
		if answer == "" {
			c.problem("the team needs a name", true)
			return false, nil
		}
		return c.checked(c.Declare.Check(ctx, declare.Entry{Team: answer}), true)
	}, "Team name", "e.g. example-org", suggestion)
	if err != nil {
		return err
	}
	o.team = team
	c.term.Answered(team)
	return nil
}

func (c *Wizard) askHumans(ctx context.Context, o *createOpts) error {
	res, err := c.Runner.Run(ctx, execx.Cmd{Name: "id", Args: []string{"-un"}})
	if err != nil {
		return fmt.Errorf("id -un: %w", err)
	}
	login := strings.TrimSpace(string(res.Stdout))
	if checkErr := c.Declare.Check(ctx, declare.Entry{Humans: []string{login}}); checkErr != nil {
		de, ok := errors.AsType[*declare.Error](checkErr)
		if !ok {
			return checkErr
		}
		c.term.Printf("Your login name, %s, cannot be an account name here (%s), so there is no default.\n", login, de.Message())
		login = ""
	}
	if err = c.step(ctx, "People", "Each gets an Element login to the room; the installer creates the accounts. Add others after a comma."); err != nil {
		return err
	}
	example := login
	if example == "" {
		example = "yourname"
	}
	text, err := c.askValid(ctx, func(ctx context.Context, answer string) (bool, error) {
		names := splitCommas(answer)
		if len(names) == 0 {
			c.problem("name at least one person; your own login name is the usual answer", true)
			return false, nil
		}
		return c.checked(c.Declare.Check(ctx, declare.Entry{Humans: names}), true)
	}, "People, comma-separated", "e.g. "+example+", colleague", login)
	if err != nil {
		return err
	}
	o.humans = splitCommas(text)
	c.term.Answered(strings.Join(o.humans, " "))
	return nil
}

func (c *Wizard) askAllowFrom(ctx context.Context, o *createOpts) error {
	if err := c.step(ctx, "Other machines", "Usually none: ccy sessions and LXC containers on this machine are admitted automatically. Only for agents on other machines (WireGuard) or on a non-default bridge, their subnet."); err != nil {
		return err
	}
	text, err := c.askValid(ctx, func(ctx context.Context, answer string) (bool, error) {
		subnets := splitCommas(answer)
		if len(subnets) == 0 {
			return true, nil
		}
		return c.checked(c.Declare.Check(ctx, declare.Entry{AllowFrom: subnets}), true)
	}, "Subnets (Enter for none)", "e.g. 192.0.2.0/24", "")
	if err != nil {
		return err
	}
	o.allowFrom = splitCommas(text)
	if len(o.allowFrom) == 0 {
		c.term.Answered("none, this machine only")
		return nil
	}
	c.term.Answered(strings.Join(o.allowFrom, " "))
	return nil
}

func (c *Wizard) askAddress(ctx context.Context, o *createOpts) error {
	if err := c.step(ctx, "Bus address", "Every team's room lives at one address of this machine, used for nothing else: an IP its agents and your Element can reach, such as its WireGuard address, or one added for the bus."); err != nil {
		return err
	}
	address, err := c.askValid(ctx, func(ctx context.Context, answer string) (bool, error) {
		return c.checked(c.Declare.CheckAddress(ctx, answer), true)
	}, "This machine's bus address", "e.g. 192.0.2.10", "")
	if err != nil {
		return err
	}
	o.address = address
	c.term.Answered(address)
	return nil
}

// checker accepts an answer, or says what is wrong with it and refuses it. An error is a
// failure to check at all, which ends the run.
type checker func(ctx context.Context, answer string) (bool, error)

// askValid asks until check accepts the answer, maxTries at most. A refused answer comes
// back as the next try's value, as typed.
func (c *Wizard) askValid(ctx context.Context, check checker, header, placeholder, value string) (string, error) {
	for range maxTries {
		answer, err := c.Gum.Input(ctx, header, placeholder, value)
		if err != nil {
			return "", c.gumErr(err)
		}
		good, err := check(ctx, answer)
		if err != nil {
			return "", err
		}
		if good {
			return answer, nil
		}
		value = answer
	}
	c.term.Printf("Giving up after %d tries; nothing was changed.\n", maxTries)
	return "", &stop{term.ExitFailure}
}

// problem says why an answer was refused, as a person would.
func (c *Wizard) problem(msg string, tryAgain bool) {
	msg = strings.TrimPrefix(msg, "declare: ")
	if tryAgain {
		c.term.Printf("  %s. Try again.\n", msg)
		return
	}
	c.term.Printf("  %s.\n", msg)
}

// checked is the outcome of a declare.py check: accepted, refused (said, and false), or a
// failure to run it (an error).
func (c *Wizard) checked(err error, tryAgain bool) (bool, error) {
	if err == nil {
		return true, nil
	}
	de, ok := errors.AsType[*declare.Error](err)
	if !ok {
		return false, err
	}
	c.problem(de.Error(), tryAgain)
	return false, nil
}

// splitCommas is one name per comma-separated part, the spaces around each dropped. A
// space inside a name is kept, so the check refuses it rather than a different name being
// made of it.
func splitCommas(text string) []string {
	var out []string
	for part := range strings.SplitSeq(text, ",") {
		if name := strings.TrimSpace(part); name != "" {
			out = append(out, name)
		}
	}
	return out
}

// checkOwner is a GitHub user or organisation that exists (GitHub's names are not
// case-sensitive, so it is checked lower-cased, as the team file holds it). On a terminal
// the lookup has a spinner; only an answer in a question invites another try.
func (c *Wizard) checkOwner(ctx context.Context, answer string, spin, tryAgain bool) (bool, error) {
	owner := strings.ToLower(answer)
	if good, err := c.checked(c.Declare.Check(ctx, declare.Entry{Org: owner}), tryAgain); !good || err != nil {
		return false, err
	}
	var err error
	if spin {
		err = c.Gum.Spin(ctx, "Looking up "+owner+" on GitHub…", "gh", "api", "users/"+owner, "--silent")
	} else {
		_, err = c.Runner.Run(ctx, execx.Cmd{Name: "gh", Args: []string{"api", "users/" + owner, "--silent"}})
	}
	if err == nil {
		return true, nil
	}
	if !isExit(err) {
		return false, err
	}
	c.problem("GitHub has no user or organisation called '"+owner+"' that your gh login can see", tryAgain)
	return false, nil
}

// checkRepos is each repository well formed, on GitHub, and one your gh login can push to
// (GitHub's own answer for that login: the repository's permissions.push). Every entry
// that fails is named, with why; then the list as a whole is checked against the team
// file's limits.
func (c *Wizard) checkRepos(ctx context.Context, repos []string, tryAgain bool) (bool, error) {
	var listed []string
	bad := false
	for _, entry := range repos {
		repo := strings.ToLower(entry)
		if repo == "" {
			continue
		}
		listed = append(listed, repo)
		good, err := c.checkRepo(ctx, repo)
		if err != nil {
			return false, err
		}
		bad = bad || !good
	}
	if len(listed) == 0 {
		c.problem("name at least one repository, as owner/repo", tryAgain)
		return false, nil
	}
	if bad {
		if tryAgain {
			c.term.Printf("  Try again.\n")
		}
		return false, nil
	}
	return c.checked(c.Declare.Check(ctx, declare.Entry{Repos: listed}), tryAgain)
}

func (c *Wizard) checkRepo(ctx context.Context, repo string) (bool, error) {
	if err := c.Declare.Check(ctx, declare.Entry{Repos: []string{repo}}); err != nil {
		if !isRefusal(err) {
			return false, err
		}
		c.term.Printf("  %s: not OWNER/REPO (lower-case; the name not ., .. or ending .git).\n", repo)
		return false, nil
	}
	c.term.Printf("  Checking %s on GitHub…\n", repo)
	res, err := c.Runner.Run(ctx, execx.Cmd{Name: "gh", Args: []string{"api", "repos/" + repo, "--jq", ".permissions.push"}})
	if err != nil {
		if !isExit(err) {
			return false, err
		}
		c.term.Printf("  %s: not found, or your gh login cannot see it.\n", repo)
		return false, nil
	}
	if strings.TrimSpace(string(res.Stdout)) != "true" {
		c.term.Printf("  %s: read-only for your gh login; a team trusts only repositories you can push to.\n", repo)
		return false, nil
	}
	return true, nil
}

// trusts says what the team trusts, in words.
func (o *createOpts) trusts() string {
	repos := nonEmpty(o.repos)
	switch {
	case o.org != "":
		return "everything " + o.org + " owns: any repository, any branch"
	case len(repos) == 1:
		return "only " + repos[0] + ", on any branch"
	default:
		return "only these repositories, on any branch: " + strings.Join(repos, ", ")
	}
}

// confirmSummary dry-runs the whole entry (declare.py checks it before anything is
// asked), shows what will be written, and asks the one yes that covers writing it and
// installing the team. No changes nothing, exit 1; Ctrl-C changes nothing, exit 130.
func (c *Wizard) confirmSummary(ctx context.Context, o createOpts) error {
	if _, err := c.Declare.CreateDryRun(ctx, o.entry()); err != nil {
		return err
	}
	machines := "this one only"
	if len(nonEmpty(o.allowFrom)) > 0 {
		machines = strings.Join(nonEmpty(o.allowFrom), " ")
	}
	body := strings.Join([]string{
		summaryLine("Works on", o.trusts()),
		summaryLine("People", strings.Join(nonEmpty(o.humans), " ")),
		summaryLine("Machines", machines),
		summaryLine("Element", "a profile for the team, set up on this desktop"),
	}, "\n")
	c.term.Printf("\n")
	if err := c.Gum.Box(ctx, false, "Team "+o.team, "", body); err != nil {
		return err
	}
	question := "Create team " + o.team + "? (saved to " + term.AbbrevHome(c.HostVars, c.Home) + ", then installed: sudo asks for your password)"
	yes, err := c.Gum.Confirm(ctx, question, true)
	if err != nil {
		return c.gumErr(err)
	}
	if !yes {
		c.term.Printf("Nothing was added.\n")
		return &stop{term.ExitFailure}
	}
	return nil
}

func summaryLine(label, value string) string {
	return fmt.Sprintf("%-9s %s", label, value)
}

// write declares the team and its Element profile, installs it, and, on a terminal,
// offers what comes next.
func (c *Wizard) write(ctx context.Context, o createOpts, here string) error {
	change, err := c.Declare.Create(ctx, o.entry())
	if err != nil {
		return err
	}
	marker := "UNCHANGED"
	if change.Changed {
		marker = "CHANGED"
	}
	if _, err = fmt.Fprintf(c.Stdout, "%s\t%s\n", marker, change.Text); err != nil {
		return fmt.Errorf("write the result: %w", err)
	}
	// Element is set up with the team: its profile is declared beside it, and apply runs its play.
	if _, err = c.Declare.Element(ctx, o.team); err != nil {
		return err
	}
	if err = c.ApplyTeams(ctx, o.team); err != nil {
		// The play ran before it failed, so the person's account exists with a password
		// nobody was shown: the sign-in is offered (or said) here, the failure still stands.
		if signErr := c.SignInGap(ctx, []string{o.team}, !o.yes); errors.Is(signErr, ui.ErrCancelled) {
			return c.cancelled()
		}
		return c.failed("team " + o.team + " is declared in " + term.AbbrevHome(c.HostVars, c.Home) +
			" but not applied as declared. Once the cause above is fixed, ccy-teams apply " + o.team + " applies it.")
	}
	if !o.yes {
		if err = c.afterCreate(ctx, o); err != nil {
			return err
		}
	}
	steps, err := c.Declare.NextSteps(ctx, o.team)
	if err != nil {
		return err
	}
	if !o.yes {
		steps = withoutOffered(steps)
	}
	c.term.Printf("%s", steps)
	if o.yes {
		return nil
	}
	return c.offerJoin(ctx, o.team, here)
}

// withoutOffered drops the next-steps lines for what afterCreate has just offered: the
// sign-in and Element, which were asked about a moment ago.
func withoutOffered(steps string) string {
	var kept strings.Builder
	for line := range strings.Lines(steps) {
		if strings.HasPrefix(line, "  Sign in ") || strings.HasPrefix(line, "  Element ") {
			continue
		}
		kept.WriteString(line)
	}
	return kept.String()
}

// afterCreate is, straight after create, each person's sign-in and then Element opened.
func (c *Wizard) afterCreate(ctx context.Context, o createOpts) error {
	for _, human := range nonEmpty(o.humans) {
		yes, err := c.askYes(ctx, "Sign "+human+" in to Element now? A password is set and shown here once.", true)
		if err != nil {
			return err
		}
		if yes {
			if err = c.offered(c.NewPassword(ctx, o.team, human)); err != nil {
				return err
			}
		}
	}
	yes, err := c.askYes(ctx, "Open "+o.team+" in Element now?", true)
	if err != nil || !yes {
		return err
	}
	return c.offered(c.LaunchElement(ctx, o.team))
}

// offered is the outcome of something offered after the team was made: it has said why
// it failed, and the team is made either way, so only leaving it cancels the run.
func (c *Wizard) offered(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, ui.ErrCancelled) {
		return c.cancelled()
	}
	c.term.Printf("  That did not finish (see above). The team is made either way.\n")
	return nil
}

// askYes is a gum confirm: yes, no, or (Ctrl-C) the run cancelled.
func (c *Wizard) askYes(ctx context.Context, question string, defaultYes bool) (bool, error) {
	yes, err := c.Gum.Confirm(ctx, question, defaultYes)
	if err != nil {
		return false, c.gumErr(err)
	}
	return yes, nil
}

// offerJoin, in a checkout of the team's repositories, offers to start a ccy session there
// as a member (ccy --teams <team>, no seat, so ccy chooses one and says it).
func (c *Wizard) offerJoin(ctx context.Context, team, here string) error {
	inTree, err := c.Git.ProjectHere(ctx, c.Cwd)
	if err != nil || !inTree || here == "" {
		return err
	}
	covering, err := c.Declare.TeamsFor(ctx, here)
	if err != nil {
		// The session is only an offer and the team is made: say why it is not offered.
		c.term.Printf("%s\n", err.Error())
		return nil
	}
	if !slices.Contains(covering, team) {
		return nil
	}
	top, err := c.Git.TopLevel(ctx, c.Cwd)
	if err != nil {
		return err
	}
	shown := term.AbbrevHome(top, c.Home)
	yes, err := c.askYes(ctx, "Start a ccy session in "+shown+" as a member of "+team+"?", true)
	if err != nil || !yes {
		return err
	}
	c.term.Printf("Starting ccy --teams %s in %s.\n", team, shown)
	if err = c.ExecLauncher(top, []string{c.Launcher, ccyenv.FlagTeams, team}); err != nil {
		return fmt.Errorf("start ccy --teams %s: %w", team, err)
	}
	return nil
}
