// Package declare calls helpers/agent_bus/declare.py, which owns the host_vars editing, the
// team-file validation and the applied-state digests, and parses the marker lines it
// prints on stdout. Its exit codes (64 usage, 78 refused, 130 Ctrl-C) become typed errors
// that carry its "declare: <message>" stderr line.
package declare

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"slices"
	"strconv"
	"strings"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/marker"
)

// Exit kinds of declare.py. An *Error matches the kind of its exit code under errors.Is.
var (
	ErrFailed      = errors.New("declare failed") // any code not listed
	ErrUsage       = errors.New("declare: usage mistake")
	ErrRefused     = errors.New("declare: refused")
	ErrInterrupted = errors.New("declare: cancelled")
)

// Error is a declare.py run that exited non-zero.
type Error struct {
	Code   int
	Stderr string // as printed, trimmed: "declare: <message>"
	cause  error
}

// Error implements error: declare's own stderr line, which already names the helper.
func (e *Error) Error() string {
	if e.Stderr != "" {
		return e.Stderr
	}
	return fmt.Sprintf("declare exited %d", e.Code)
}

// Message is the stderr line without its "declare: " prefix.
func (e *Error) Message() string { return strings.TrimPrefix(e.Stderr, "declare: ") }

// Is matches the kind of the exit code.
func (e *Error) Is(target error) bool {
	switch e.Code {
	case 64:
		return target == ErrUsage
	case 78:
		return target == ErrRefused
	case 130:
		return target == ErrInterrupted
	default:
		return target == ErrFailed
	}
}

// Unwrap returns the runner's error.
func (e *Error) Unwrap() error { return e.cause }

// Client runs declare.py from the checkout at Root against the host_vars file File. It runs
// in the caller's working directory: the module is found through PYTHONPATH. Err receives
// the stderr of a successful run (the note that names the backup of the file); it is never
// dropped, and defaults to the process's stderr.
type Client struct {
	Runner execx.Runner
	Root   string
	File   string
	Err    io.Writer
}

// NewClient is a Client whose stderr notes go to the process's stderr.
func NewClient(r execx.Runner, root, file string) *Client {
	return &Client{Runner: r, Root: root, File: file, Err: os.Stderr}
}

// Team is a TEAM line of "declare declared".
type Team struct {
	Name       string
	Port       int // 0 when the entry sets none
	Present    bool
	ServerName string
	URL        string // empty when the entry has no address to reach it on
	// Description is what the team is for; empty when the entry sets none (or a vaulted one).
	Description string
	// DescriptionState is set when the entry has a description that cannot be shown: it is
	// vaulted, or fails the team file's grammar. Description is then empty.
	DescriptionState DescriptionState
}

// DescriptionState says why a team's description is not shown.
type DescriptionState string

// The two states declare.py marks with !vaulted and !invalid.
const (
	DescriptionVaulted DescriptionState = "vaulted"
	DescriptionInvalid DescriptionState = "invalid"
)

// Status is a STATUS line: whether a team is applied as declared.
type Status struct {
	Team   string
	State  ApplyState
	Detail string // empty when declare printed "-"
}

// ApplyState is a team's applied state, the word declare.py status prints for it. A word this
// build does not know is kept as it came, so it can be reported as unknown, not lost.
type ApplyState string

// The applied states declare.py status reports.
const (
	// ApplyAbsent is a team declare did not report on (the zero Status).
	ApplyAbsent     ApplyState = ""
	ApplyCurrent    ApplyState = "current"
	ApplyOutdated   ApplyState = "outdated"
	ApplyUnbuilt    ApplyState = "unbuilt"
	ApplyRemoved    ApplyState = "removed"
	ApplyDecide     ApplyState = "decide"
	ApplyUnreadable ApplyState = "unreadable"
)

// Limit is a LIMITED line: a team still limited to some folders.
type Limit struct {
	Team     string
	Prefixes string // "docs/, CLAUDE/Plan/"
	Trusts   string // what the team trusts, in words
}

// Undescribed is an UNDESCRIBED line: a present team that has no description yet.
type Undescribed struct {
	Team   string
	Trusts string // what the team trusts, in words
}

// Profile is an ELEMENT line: a team's Element profile and the homeserver it uses.
type Profile struct {
	Team    string
	BaseURL string
}

// Change is the CHANGED or UNCHANGED line of a command that edits the host_vars.
type Change struct {
	Changed bool
	Text    string // what follows the marker
}

// Entry is the values the check and create subcommands take; empty fields are not passed.
type Entry struct {
	Team      string
	Humans    []string
	Org       string
	Repos     []string
	AllowFrom []string
	Address   string
	// Description is what the team is for (create needs it).
	Description string
}

func (e Entry) args() []string {
	var args []string
	if e.Team != "" {
		args = append(args, "--team", e.Team)
	}
	for _, h := range e.Humans {
		args = append(args, "--human", h)
	}
	if e.Org != "" {
		args = append(args, "--org", e.Org)
	}
	for _, r := range e.Repos {
		args = append(args, "--repo", r)
	}
	for _, a := range e.AllowFrom {
		args = append(args, "--allow-from", a)
	}
	if e.Address != "" {
		args = append(args, "--address", e.Address)
	}
	if e.Description != "" {
		args = append(args, "--description", e.Description)
	}
	return args
}

func plain(out string) string { return strings.TrimRight(out, "\r\n") }

// Declared is the teams of the host_vars, in file order.
func (c *Client) Declared(ctx context.Context) ([]Team, error) {
	out, err := c.runFile(ctx, "declared")
	if err != nil {
		return nil, err
	}
	return parseTeams(out)
}

func parseTeams(out string) ([]Team, error) {
	rows, err := marker.Fields(out, "TEAM", 7)
	if err != nil {
		return nil, fmt.Errorf("declare declared: %w", err)
	}
	teams := make([]Team, 0, len(rows))
	for _, f := range rows {
		team := Team{Name: f[1], ServerName: f[4], URL: marker.DashEmpty(f[5])}
		if f[2] != "-" {
			if team.Port, err = strconv.Atoi(f[2]); err != nil {
				return nil, fmt.Errorf("declare declared: team %s: port %q is not a number", f[1], f[2])
			}
		}
		if team.Description, team.DescriptionState, err = parseDescription(f[6]); err != nil {
			return nil, fmt.Errorf("declare declared: team %s: %w", f[1], err)
		}
		switch f[3] {
		case "present":
			team.Present = true
		case "absent":
		default:
			return nil, fmt.Errorf("declare declared: team %s: state %q is neither present nor absent", f[1], f[3])
		}
		teams = append(teams, team)
	}
	return teams, nil
}

// parseDescription reads declare.py's description field: a JSON string, "-" for none, or
// !vaulted / !invalid for one that cannot be shown.
func parseDescription(field string) (string, DescriptionState, error) {
	switch field {
	case "-":
		return "", "", nil
	case "!vaulted":
		return "", DescriptionVaulted, nil
	case "!invalid":
		return "", DescriptionInvalid, nil
	}
	var text string
	if err := json.Unmarshal([]byte(field), &text); err != nil {
		return "", "", fmt.Errorf("description %q is not a JSON string", field)
	}
	return text, "", nil
}

// Status compares each team's applied stamp in appliedDir with the declaration and the
// software in source. installed names the teams that have a homeserver unit; the flags are
// passed sorted so the call is the same whatever order a map gave them in.
func (c *Client) Status(ctx context.Context, source, appliedDir string, installed []string) ([]Status, error) {
	out, err := c.runFile(ctx, "status", statusArgs(source, appliedDir, installed)...)
	if err != nil {
		return nil, err
	}
	return parseStatuses(out)
}

// Overview is Declared and Status from one run of declare.py: the list redraws on every key,
// and starting Python twice for it doubled the cost of a redraw.
func (c *Client) Overview(ctx context.Context, source, appliedDir string, installed []string) ([]Team, []Status, error) {
	out, err := c.runFile(ctx, "overview", statusArgs(source, appliedDir, installed)...)
	if err != nil {
		return nil, nil, err
	}
	teams, err := parseTeams(out)
	if err != nil {
		return nil, nil, err
	}
	states, err := parseStatuses(out)
	if err != nil {
		return nil, nil, err
	}
	return teams, states, nil
}

func statusArgs(source, appliedDir string, installed []string) []string {
	args := make([]string, 0, 4+2*len(installed))
	args = append(args, "--source", source, "--applied", appliedDir)
	for _, team := range slices.Sorted(slices.Values(installed)) {
		args = append(args, "--installed", team)
	}
	return args
}

func parseStatuses(out string) ([]Status, error) {
	rows, err := marker.Fields(out, "STATUS", 4)
	if err != nil {
		return nil, fmt.Errorf("declare status: %w", err)
	}
	states := make([]Status, 0, len(rows))
	for _, f := range rows {
		states = append(states, Status{Team: f[1], State: ApplyState(f[2]), Detail: marker.DashEmpty(f[3])})
	}
	return states, nil
}

// Limited is the present teams that still limit the paths their agents may name.
func (c *Client) Limited(ctx context.Context) ([]Limit, error) {
	out, err := c.runFile(ctx, "limited")
	if err != nil {
		return nil, err
	}
	rows, err := marker.Fields(out, "LIMITED", 4)
	if err != nil {
		return nil, fmt.Errorf("declare limited: %w", err)
	}
	limits := make([]Limit, 0, len(rows))
	for _, f := range rows {
		limits = append(limits, Limit{Team: f[1], Prefixes: f[2], Trusts: f[3]})
	}
	return limits, nil
}

// Undescribed is the present teams that have no description.
func (c *Client) Undescribed(ctx context.Context) ([]Undescribed, error) {
	out, err := c.runFile(ctx, "undescribed")
	if err != nil {
		return nil, err
	}
	rows, err := marker.Fields(out, "UNDESCRIBED", 3)
	if err != nil {
		return nil, fmt.Errorf("declare undescribed: %w", err)
	}
	teams := make([]Undescribed, 0, len(rows))
	for _, f := range rows {
		teams = append(teams, Undescribed{Team: f[1], Trusts: f[2]})
	}
	return teams, nil
}

// ElementTeams is the teams that have an Element profile entry.
func (c *Client) ElementTeams(ctx context.Context) ([]Profile, error) {
	out, err := c.runFile(ctx, "element-teams")
	if err != nil {
		return nil, err
	}
	rows, err := marker.Fields(out, "ELEMENT", 3)
	if err != nil {
		return nil, fmt.Errorf("declare element-teams: %w", err)
	}
	profiles := make([]Profile, 0, len(rows))
	for _, f := range rows {
		profiles = append(profiles, Profile{Team: f[1], BaseURL: f[2]})
	}
	return profiles, nil
}

// TeamsFor is the present teams whose trust covers the repository (OWNER/REPO).
func (c *Client) TeamsFor(ctx context.Context, repo string) ([]string, error) {
	out, err := c.runFile(ctx, "teams-for", "--repo", repo)
	if err != nil {
		return nil, err
	}
	rows, err := marker.Fields(out, "TEAM", 2)
	if err != nil {
		return nil, fmt.Errorf("declare teams-for: %w", err)
	}
	names := make([]string, 0, len(rows))
	for _, f := range rows {
		names = append(names, f[1])
	}
	return names, nil
}

// Standing is how a present team stands to a repository, the word declare.py choices prints.
type Standing string

// The standings of declare.py choices.
const (
	// StandingCovers: the team trusts the repository now.
	StandingCovers Standing = "covers"
	// StandingWiden: it does not, and adding the repository to its scope would make it.
	StandingWiden Standing = "widen"
	// StandingExcluded: the team's org owns it and excludes it on purpose.
	StandingExcluded Standing = "excluded"
	// StandingUnreadable: the team's scope cannot be read (vaulted), so nothing can be said.
	StandingUnreadable Standing = "unreadable"
)

// Choice is a CHOICE line: a present team, how it stands to the repository, what it is for.
type Choice struct {
	Team             string
	Standing         Standing
	Description      string
	DescriptionState DescriptionState
}

// Choices is every present team with its standing to the repository (OWNER/REPO), in file
// order. Unlike TeamsFor it also names the teams that would need their scope widened.
func (c *Client) Choices(ctx context.Context, repo string) ([]Choice, error) {
	out, err := c.runFile(ctx, "choices", "--repo", repo)
	if err != nil {
		return nil, err
	}
	rows, err := marker.Fields(out, "CHOICE", 4)
	if err != nil {
		return nil, fmt.Errorf("declare choices: %w", err)
	}
	choices := make([]Choice, 0, len(rows))
	for _, f := range rows {
		standing := Standing(f[2])
		switch standing {
		case StandingCovers, StandingWiden, StandingExcluded, StandingUnreadable:
		default:
			return nil, fmt.Errorf("declare choices: team %s: standing %q is not one this build knows", f[1], f[2])
		}
		choice := Choice{Team: f[1], Standing: standing}
		if choice.Description, choice.DescriptionState, err = parseDescription(f[3]); err != nil {
			return nil, fmt.Errorf("declare choices: team %s: %w", f[1], err)
		}
		choices = append(choices, choice)
	}
	return choices, nil
}

// GithubRemote is the lower-case OWNER/REPO a GitHub remote URL names, or "" when it names none.
func (c *Client) GithubRemote(ctx context.Context, url string) (string, error) {
	out, err := c.run(ctx, "github-remote", "--url", url)
	return plain(out), err
}

// Address is the machine's bus address, or "" when none is declared yet.
func (c *Client) Address(ctx context.Context) (string, error) {
	out, err := c.runFile(ctx, "address")
	return plain(out), err
}

// CheckAddress refuses (ErrUsage) an address that cannot be the bus address.
func (c *Client) CheckAddress(ctx context.Context, address string) error {
	_, err := c.run(ctx, "check-address", "--address", address)
	return err
}

// Check refuses (ErrUsage) the first value of e that the team file would not accept.
func (c *Client) Check(ctx context.Context, e Entry) error {
	_, err := c.runFile(ctx, "check", e.args()...)
	return err
}

// SuggestTeam is a free team name made from a GitHub owner's or repository's name, or "".
func (c *Client) SuggestTeam(ctx context.Context, name string) (string, error) {
	out, err := c.runFile(ctx, "suggest-team", "--name", name)
	return plain(out), err
}

// NextSteps is the text saying what is left to do once the team is installed.
func (c *Client) NextSteps(ctx context.Context, team string) (string, error) {
	out, err := c.runFile(ctx, "next-steps", "--team", team)
	return out, err
}

// change reads the CHANGED or UNCHANGED line of an editing subcommand's output.
func change(sub string, out string) (Change, error) {
	for line := range strings.Lines(out) {
		text := strings.TrimRight(line, "\r\n")
		if rest, ok := strings.CutPrefix(text, "CHANGED\t"); ok {
			return Change{Changed: true, Text: rest}, nil
		}
		if rest, ok := strings.CutPrefix(text, "UNCHANGED\t"); ok {
			return Change{Text: rest}, nil
		}
	}
	return Change{}, fmt.Errorf("declare %s printed neither CHANGED nor UNCHANGED: %q", sub, strings.TrimSpace(out))
}

// Create adds the team's entry to the host_vars (a backup of the file is kept beside it).
func (c *Client) Create(ctx context.Context, e Entry) (Change, error) {
	return c.edit(ctx, "create", e.args()...)
}

// CreateDryRun is the entry Create would add, as the lines it would write; nothing is written.
func (c *Client) CreateDryRun(ctx context.Context, e Entry) (string, error) {
	out, err := c.runFile(ctx, "create", append(e.args(), "--dry-run")...)
	return out, err
}

// Describe sets or replaces the present team's description; Changed is false when it was
// already that.
func (c *Client) Describe(ctx context.Context, team, description string) (Change, error) {
	return c.edit(ctx, "describe", "--team", team, "--description", description)
}

// AddRepo adds the repository (OWNER/REPO, any branch) to the present team's scope.
func (c *Client) AddRepo(ctx context.Context, team, repo string) (Change, error) {
	return c.edit(ctx, "add-repo", "--team", team, "--repo", repo)
}

// Element adds or updates the team's Element profile entry; Changed is false when it was right.
func (c *Client) Element(ctx context.Context, team string) (Change, error) {
	return c.edit(ctx, "element", "--team", team)
}

// DropLimit removes the team's path limit.
func (c *Client) DropLimit(ctx context.Context, team string) (Change, error) {
	return c.edit(ctx, "drop-limit", "--team", team)
}

// Remove declares the team absent; purge also marks its data for deletion.
func (c *Client) Remove(ctx context.Context, team string, purge bool) (Change, error) {
	args := []string{flagTeam, team}
	if purge {
		args = append(args, "--purge")
	}
	return c.edit(ctx, "remove", args...)
}

func (c *Client) run(ctx context.Context, sub string, args ...string) (string, error) {
	res, err := c.Runner.Run(ctx, execx.Cmd{Name: "env", Args: slices.Concat(
		[]string{"PYTHONPATH=" + c.Root, "python3", "-P", "-m", "helpers.agent_bus.declare", sub}, args)})
	if err != nil {
		if exit, ok := errors.AsType[*execx.ExitError](err); ok {
			return "", &Error{Code: exit.Code, Stderr: strings.TrimSpace(exit.Stderr), cause: err}
		}
		return "", fmt.Errorf("declare %s: %w", sub, err)
	}
	if len(res.Stderr) > 0 {
		if _, werr := c.stderr().Write(res.Stderr); werr != nil {
			return "", fmt.Errorf("declare %s stderr: %w", sub, werr)
		}
	}
	return string(res.Stdout), nil
}

// stderr is where a successful run's stderr goes: Err, or the process's own stderr when Err
// is unset, so the note naming the host_vars backup is never dropped.
func (c *Client) stderr() io.Writer {
	if c.Err == nil {
		return os.Stderr
	}
	return c.Err
}

func (c *Client) runFile(ctx context.Context, sub string, args ...string) (string, error) {
	return c.run(ctx, sub, slices.Concat([]string{"--file", c.File}, args)...)
}

func (c *Client) edit(ctx context.Context, sub string, args ...string) (Change, error) {
	out, err := c.runFile(ctx, sub, args...)
	if err != nil {
		return Change{}, err
	}
	return change(sub, out)
}
