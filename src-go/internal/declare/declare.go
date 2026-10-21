// Package declare is ccy-teams' one door to the host_vars file. The reads (Declared, Limited,
// Undescribed, ElementTeams, TeamsFor, Choices, GithubRemote, Address, Settings, NextSteps and
// LxcMembers) answer in-process through internal/bus/declare, the read side of
// helpers/agent_bus/declare.py (Plan 00209 SPEC-view.md V1-V23). Everything else calls
// declare.py, which owns the host_vars editing, the team-file validation, the LXC checks and
// the applied-state digests, and the marker lines it prints on stdout are parsed here: the
// edits, check, check-address, suggest-team, the lxc-* subcommands other than lxc-members, and
// status. Overview is Declared in-process beside one status run.
//
// Either way a refusal is an *Error with declare.py's exit code (64 usage, 78 refused, 130
// Ctrl-C, 1 for the reader's crash class) and its "declare: <message>" line, so a caller cannot
// tell which side answered.
package declare

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"slices"
	"strings"

	busdeclare "github.com/LongTermSupport/fedora-desktop/src-go/internal/bus/declare"
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

// The answers of the reads are the reader's own types, whose fields and meanings are
// declare.py's marker lines'; internal/bus/declare documents each.
type (
	// Team is one agent_bus_teams entry (declare declared).
	Team = busdeclare.Team
	// DescriptionState says why a team's description is not shown.
	DescriptionState = busdeclare.DescriptionState
	// Limit is a team still limited to some folders (declare limited).
	Limit = busdeclare.Limit
	// Undescribed is a present team that has no description yet (declare undescribed).
	Undescribed = busdeclare.Undescribed
	// Profile is a team's Element profile and the homeserver it uses (declare element-teams).
	Profile = busdeclare.Profile
	// Standing is how a present team stands to a repository (declare choices).
	Standing = busdeclare.Standing
	// Choice is a present team, its standing to a repository and what it is for.
	Choice = busdeclare.Choice
	// Setting is one key of a present team's entry (declare settings).
	Setting = busdeclare.Setting
	// SettingState says why a setting's value is not shown.
	SettingState = busdeclare.SettingState
	// LxcMember is an agent_bus_lxc_members entry as the play reads it (declare lxc-members).
	LxcMember = busdeclare.LxcMember
)

// The words declare.py prints for a description or setting it cannot show, and the standings
// of declare.py choices.
const (
	DescriptionVaulted = busdeclare.DescriptionVaulted
	DescriptionInvalid = busdeclare.DescriptionInvalid
	SettingVaulted     = busdeclare.SettingVaulted
	SettingInvalid     = busdeclare.SettingInvalid
	StandingCovers     = busdeclare.StandingCovers
	StandingWiden      = busdeclare.StandingWiden
	StandingExcluded   = busdeclare.StandingExcluded
	StandingUnreadable = busdeclare.StandingUnreadable
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
func (c *Client) Declared(context.Context) ([]Team, error) {
	return read(c, busdeclare.HostVars.Declared)
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

// Overview is Declared, read in-process, and Status, the one run of declare.py the list
// needs: it redraws on every key. A file the reader refuses starts no process.
func (c *Client) Overview(ctx context.Context, source, appliedDir string, installed []string) ([]Team, []Status, error) {
	teams, err := c.Declared(ctx)
	if err != nil {
		return nil, nil, err
	}
	states, err := c.Status(ctx, source, appliedDir, installed)
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
func (c *Client) Limited(context.Context) ([]Limit, error) {
	return read(c, busdeclare.HostVars.Limited)
}

// Undescribed is the present teams that have no description.
func (c *Client) Undescribed(context.Context) ([]Undescribed, error) {
	return read(c, busdeclare.HostVars.Undescribed)
}

// ElementTeams is the teams that have an Element profile entry.
func (c *Client) ElementTeams(context.Context) ([]Profile, error) {
	return read(c, busdeclare.HostVars.ElementTeams)
}

// TeamsFor is the present teams whose trust covers the repository (OWNER/REPO); a repository
// that is not a lower-case OWNER/REPO is refused (ErrUsage).
func (c *Client) TeamsFor(_ context.Context, repo string) ([]string, error) {
	return read(c, func(hv busdeclare.HostVars) ([]string, error) { return hv.TeamsFor(repo) })
}

// Choices is every present team with its standing to the repository (OWNER/REPO), in file
// order. Unlike TeamsFor it also names the teams that would need their scope widened.
func (c *Client) Choices(_ context.Context, repo string) ([]Choice, error) {
	return read(c, func(hv busdeclare.HostVars) ([]Choice, error) { return hv.Choices(repo) })
}

// GithubRemote is the lower-case OWNER/REPO a GitHub remote URL names, or "" when it names
// none. It reads no file and cannot fail; the error is kept so callers do not change.
func (c *Client) GithubRemote(_ context.Context, url string) (string, error) {
	return busdeclare.GithubRemote(url), nil
}

// Address is the machine's bus address, or "" when none is declared yet.
func (c *Client) Address(context.Context) (string, error) {
	return read(c, busdeclare.HostVars.Address)
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
func (c *Client) NextSteps(_ context.Context, team string) (string, error) {
	return read(c, func(hv busdeclare.HostVars) (string, error) { return hv.NextSteps(team) })
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

// Settings is every key of the present team's entry, in the order declare.py lists them. A
// value json.dumps wrote that is not JSON (NaN, Infinity) is refused: a Value is JSON.
func (c *Client) Settings(_ context.Context, team string) ([]Setting, error) {
	settings, err := read(c, func(hv busdeclare.HostVars) ([]Setting, error) { return hv.Settings(team) })
	if err != nil {
		return nil, err
	}
	for _, s := range settings {
		if s.Value != nil && !json.Valid(s.Value) {
			return nil, fmt.Errorf("SETTING %s: the value is not JSON: %q", s.Key, string(s.Value))
		}
	}
	return settings, nil
}

// Value is one key's new value for Set: JSON, as declare.py set reads it.
type Value struct {
	Key, JSON string
}

func setArgs(team string, values []Value) []string {
	args := make([]string, 0, 2+3*len(values))
	args = append(args, flagTeam, team)
	for _, v := range values {
		args = append(args, "--value", v.Key, v.JSON)
	}
	return args
}

// Set changes the present team's keys in one write, judged whole by the team file's rules
// (ErrUsage naming the rule); Changed is false when every value was already that.
func (c *Client) Set(ctx context.Context, team string, values []Value) (Change, error) {
	return c.edit(ctx, "set", setArgs(team, values)...)
}

// CheckSet judges what Set would write, and writes nothing.
func (c *Client) CheckSet(ctx context.Context, team string, values []Value) error {
	_, err := c.runFile(ctx, "set", append(setArgs(team, values), "--check")...)
	return err
}

// Remove declares the team absent; purge also marks its data for deletion.
func (c *Client) Remove(ctx context.Context, team string, purge bool) (Change, error) {
	args := []string{flagTeam, team}
	if purge {
		args = append(args, "--purge")
	}
	return c.edit(ctx, "remove", args...)
}

// systemPython runs the helper: the interpreter agent-bus itself runs on, named by path because a
// bare python3 resolves through a pyenv shim on a host that has one, a bash script that costs a
// tenth of a second before Python starts.
const systemPython = "/usr/bin/python3"

// hostVars reads the file as declare.py read it (SPEC-view.md V1).
func (c *Client) hostVars() (busdeclare.HostVars, error) {
	text, err := busdeclare.Read(c.File, os.ReadFile)
	return busdeclare.HostVars(text), fromReader(err)
}

// fromReader is the reader's refusal as the *Error the run of declare.py gave: its exit code
// and its "declare: <message>" line; the reader's error stays the cause.
func fromReader(err error) error {
	if e, ok := errors.AsType[*busdeclare.Error](err); ok {
		return &Error{Code: e.Code(), Stderr: "declare: " + e.Message, cause: err}
	}
	return err
}

// read is one query of the reader over the file.
func read[T any](c *Client, query func(busdeclare.HostVars) (T, error)) (T, error) {
	hv, err := c.hostVars()
	if err != nil {
		var none T
		return none, err
	}
	found, err := query(hv)
	return found, fromReader(err)
}

func (c *Client) run(ctx context.Context, sub string, args ...string) (string, error) {
	res, err := c.Runner.Run(ctx, execx.Cmd{Name: "env", Args: slices.Concat(
		[]string{"PYTHONPATH=" + c.Root, systemPython, "-P", "-m", "helpers.agent_bus.declare", sub}, args)})
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
