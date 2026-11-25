package teamchatlaunch

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"syscall"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/fsx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/seatchoice"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/seatmodel"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/system"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

// CC is what a host cc launch (`internal cc-run`, Plan 00209 Task 5.1) reads and names: the
// v1 kit, cc's deny rules on the Matrix clients' sign-in, cc-claude and the host's teamchat kit.
type CC struct {
	V1Kit        string
	DenySettings string
	CCClaude     string
	TeamchatKit  string
}

// teamchatProgram is the member program play-teamchat.yml installs on PATH, which cc-run starts
// on teamchat; on agent-bus it becomes the v1 kit's pingbus.
const teamchatProgram = "teamchat"

// HostCC is the installed host's CC (play-agent-bus.yml, play-claude-yolo.yml, play-teamchat.yml).
var HostCC = CC{
	V1Kit:        "/usr/local/share/agent-bus/kit",
	DenySettings: "/var/local/claude-code/cc-teams-settings.json",
	CCClaude:     "/var/local/claude-code/cc-claude",
	TeamchatKit:  paths.HostKitCurrent().String(),
}

// The prompt line each system's session is given: where its teams are said, and the one way it
// posts. Nothing else. V1Prompt is cc-teams.bash's CC_TEAMS_PROMPT, byte for byte.
const (
	V1Prompt       = "Your agent-bus teams and seats are given in the session context and may change while you run; the latest agent-bus membership notice is current. Post to your teams only with pingbus, as yourself; the pingbus skill says how."
	TeamchatPrompt = "Your teamchat teams and seats are given in the session context and may change while you run; the latest teamchat membership notice is current. Post to your teams only with teamchat, as yourself; the teamchat skill says how."
)

// The refusals that stop a host launch before anything is taken: a kit file missing, and a
// teamchat launch with no session name (decision H2). Both exit 78.
var (
	ErrHostKit   = errors.New("a file the launch needs is missing or unusable")
	ErrNoSession = errors.New("a teamchat launch needs a session name")
)

const ccRunUsage = "internal cc-run <tmp> <checkout> <session or empty> <cc pid> <seats> <true|false> -- <claude args...>"

// ccLaunch is cc-run's arguments.
type ccLaunch struct {
	tmp, checkout, session, pid, seats string
	bus                                bool
	claude                             []string
}

// ccRun is `internal cc-run <tmp> <checkout> <session or empty> <cc pid> <seats> <bus> -- <claude
// args...>`, host cc's session (DESIGN-host-cc-v2.md section 6 step 5). On agent-bus it writes
// cc-teams.bash's settings and becomes the process its `env VAR=... pingbus ...` line started:
// the same program, arguments and added variable. On teamchat it takes the
// seats with no mounts, runs `teamchat run --kind host` as a child with the seat store's session
// roots (H1), cc's deny rules as extra settings (H4) and cc-claude's wrapper form (H5), then ends
// the session by its status, or keeps the seats when SIGHUP or SIGTERM ended it.
func (v *Verbs) ccRun(ctx context.Context, args []string) (term.ExitCode, error) {
	if len(args) < 7 || args[6] != "--" || (args[5] != "true" && args[5] != "false") ||
		!filepath.IsAbs(args[0]) || !filepath.IsAbs(args[1]) {
		return 0, fmt.Errorf("%w: takes %s", errUsage, ccRunUsage)
	}
	if pid, err := strconv.Atoi(args[3]); err != nil || pid <= 0 {
		return 0, fmt.Errorf("%w: the cc pid %q is not a process id", errUsage, args[3])
	}
	sys := system.AgentBus
	if teams, ok := teamsOf(args[4]); ok {
		lines, err := v.Resolve(ctx, args[1], teams)
		if err != nil {
			return 0, err
		}
		if sys, err = system.One(lines); err != nil {
			return 0, err
		}
	}
	l := ccLaunch{tmp: args[0], checkout: args[1], session: args[2], pid: args[3], seats: args[4], bus: args[5] == "true", claude: args[7:]}
	if sys == system.AgentBus {
		return v.ccRunV1(ctx, l)
	}
	return v.ccRunTeamchat(ctx, l)
}

// needFiles refuses the launch unless each file is a regular file, naming the plays.
func needFiles(plays string, files ...string) error {
	for _, f := range files {
		if info, err := os.Stat(f); err != nil || !info.Mode().IsRegular() {
			return fmt.Errorf("%w: --teams and --bus need %s, and it is missing: run %s", ErrHostKit, f, plays)
		}
	}
	return nil
}

// ccRunV1 is cc-teams.bash's cc_teams_settings and cc_teams_command.
func (v *Verbs) ccRunV1(ctx context.Context, l ccLaunch) (term.ExitCode, error) {
	kit := v.CC.V1Kit
	if err := needFiles("play-agent-bus.yml and play-claude-yolo.yml", kit+"/pingbus", kit+"/pingbus-status-line",
		kit+"/settings.json", kit+"/plugin/pingbus/.claude-plugin/plugin.json", v.CC.DenySettings); err != nil {
		return 0, err
	}
	settings, err := v1Settings(kit+"/settings.json", v.CC.DenySettings, l.bus)
	if err != nil {
		return 0, err
	}
	if err := fsx.WriteAtomic(l.tmp+"/settings.json", "the session's settings", settings, 0o600); err != nil {
		return 0, err
	}
	session := l.session
	if session == "" {
		session = "cc." + l.pid
	}
	dir, code := v.Membership(ctx, []string{"--kind=cc", session, l.checkout, l.seats})
	if code != term.ExitOK {
		return code, nil
	}
	return term.ExitOK, v.Exec.Exec(execx.Cmd{Name: kit + "/pingbus", Env: []string{"PINGBUS_MEMBERSHIP=" + dir}, Args: append([]string{
		"seat", "exec",
		"--root", l.checkout + v1SeatsRoot, "--home", l.tmp + "/pingbus-home", "--",
		v.CC.CCClaude, "--plugin-dir", kit + "/plugin/pingbus", "--settings", l.tmp + "/settings.json",
		"--append-system-prompt", V1Prompt,
	}, l.claude...)})
}

// v1Settings is jq's `.[0] * .[1]` of the kit's settings and cc's deny rules, with
// crossSessionInbound refuse for --bus, as jq prints it.
func v1Settings(kitFile, denyFile string, bus bool) ([]byte, error) {
	kitData, err := os.ReadFile(kitFile)
	if err != nil {
		return nil, fmt.Errorf("%w: read %s: %w", ErrHostKit, kitFile, err)
	}
	denyData, err := os.ReadFile(denyFile)
	if err != nil {
		return nil, fmt.Errorf("%w: read %s: %w", ErrHostKit, denyFile, err)
	}
	merged, err := deepMerge(kitData, denyData)
	if err == nil && bus {
		merged, err = deepMerge(merged, []byte(`{"crossSessionInbound":"refuse"}`))
	}
	if err != nil {
		return nil, fmt.Errorf("%w: merge %s and %s: %w", ErrHostKit, kitFile, denyFile, err)
	}
	var out bytes.Buffer
	if err := json.Indent(&out, merged, "", "  "); err != nil {
		return nil, err
	}
	return append(out.Bytes(), '\n'), nil
}

// member is one key of a JSON object, in the order the text gives it.
type member struct {
	key string
	val json.RawMessage
}

// isObject is whether a JSON value's text is an object.
func isObject(raw []byte) bool {
	t := bytes.TrimLeft(raw, " \t\r\n")
	return len(t) > 0 && t[0] == '{'
}

// objectOf is the members of a JSON object's text, in order.
func objectOf(data []byte) ([]member, error) {
	if !isObject(data) {
		return nil, errors.New("only objects merge")
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	if _, err := dec.Token(); err != nil {
		return nil, err
	}
	var members []member
	for dec.More() {
		tok, err := dec.Token()
		if err != nil {
			return nil, err
		}
		key, ok := tok.(string)
		if !ok {
			return nil, errors.New("a key that is not a string")
		}
		var val json.RawMessage
		if err := dec.Decode(&val); err != nil {
			return nil, err
		}
		members = append(members, member{key, val})
	}
	if _, err := dec.Token(); err != nil {
		return nil, err
	}
	if dec.More() {
		return nil, errors.New("text after the object")
	}
	return members, nil
}

// deepMerge is jq's `a * b` of two objects: b's keys over a's, an object under both merged
// again, a's keys kept in their place and b's new ones after them. The result is compact.
func deepMerge(a, b []byte) (json.RawMessage, error) {
	am, err := objectOf(a)
	if err != nil {
		return nil, err
	}
	bm, err := objectOf(b)
	if err != nil {
		return nil, err
	}
	for _, m := range bm {
		i := slices.IndexFunc(am, func(x member) bool { return x.key == m.key })
		switch {
		case i < 0:
			am = append(am, m)
		case isObject(am[i].val) && isObject(m.val):
			if am[i].val, err = deepMerge(am[i].val, m.val); err != nil {
				return nil, err
			}
		default:
			am[i].val = m.val
		}
	}
	var out bytes.Buffer
	out.WriteByte('{')
	for i, m := range am {
		if i > 0 {
			out.WriteByte(',')
		}
		key, err := json.Marshal(m.key)
		if err != nil {
			return nil, err
		}
		out.Write(key)
		out.WriteByte(':')
		if err := json.Compact(&out, m.val); err != nil {
			return nil, err
		}
	}
	out.WriteByte('}')
	return out.Bytes(), nil
}

// ccRunTeamchat is H1's line, run as a child.
func (v *Verbs) ccRunTeamchat(ctx context.Context, l ccLaunch) (term.ExitCode, error) {
	if l.session == "" {
		return 0, fmt.Errorf("%w: cc outside ccy's tmux has none, and the seat sweep could judge its seats over while it runs; start cc at a terminal outside ccy's tmux, which moves it into a ccy tmux session and writes its session record", ErrNoSession)
	}
	name, err := paths.ParseSessionName(l.session)
	if err != nil {
		return 0, fmt.Errorf("%w: the session %q: %w", errUsage, l.session, err)
	}
	kit := v.CC.TeamchatKit
	if err := needFiles("play-teamchat.yml (its member tag) and play-claude-yolo.yml", kit+"/settings.json",
		kit+"/plugin/teamchat/.claude-plugin/plugin.json", v.CC.DenySettings); err != nil {
		return 0, err
	}
	deny, err := os.ReadFile(v.CC.DenySettings)
	if err != nil {
		return 0, fmt.Errorf("%w: read %s: %w", ErrHostKit, v.CC.DenySettings, err)
	}
	store, key, state, err := system.Here(v.Getenv, func() (string, error) { return l.checkout, nil })
	if err != nil {
		return 0, err
	}
	launch := &Launch{Store: store, Checkout: key, Kind: seatchoice.KindHost, Taker: v.Taker(v.config(state, key)), Status: v.Status, NowMS: v.NowMS}
	taken, err := launch.Take(ctx, name, l.seats, nil, false)
	if err != nil {
		return 0, err
	}
	env, err := v.modelEnv(taken.Model)
	if err != nil {
		return 0, errors.Join(err, v.ccEnd(ctx, state, key, name, "1"))
	}
	extra := l.tmp + "/cc-deny.json"
	if err := fsx.WriteAtomic(extra, "cc's deny rules", deny, 0o600); err != nil {
		return 0, errors.Join(err, v.ccEnd(ctx, state, key, name, "1"))
	}
	run := append([]string{
		"run", "--claude", "--kind", "host", "--state", store.Session(name), "--desired", store.Control(name),
		"--extra-settings", extra, "--", v.CC.CCClaude, "--", "claude", "--append-system-prompt", TeamchatPrompt,
	}, l.claude...)
	exit, err := v.Foreground.Foreground(execx.Cmd{Name: teamchatProgram, Args: run, Env: env})
	if err != nil {
		return 0, errors.Join(err, v.ccEnd(ctx, state, key, name, "1"))
	}
	status := strconv.Itoa(exit.Code)
	if slices.Contains(exit.Received, os.Signal(syscall.SIGHUP)) || slices.Contains(exit.Received, os.Signal(syscall.SIGTERM)) {
		status = ""
	}
	if err := v.ccEnd(ctx, state, key, name, status); err != nil {
		return 0, err
	}
	if exit.Code == 0 {
		return term.ExitOK, nil
	}
	return term.FromChild(exit.Code), nil
}

// modelEnv is the host session's environment: ANTHROPIC_MODEL for the seats' model (register
// R39), unless the person's environment already sets it, which is kept and said.
func (v *Verbs) modelEnv(pick seatmodel.Pick) ([]string, error) {
	env := []string{"PINGBUS_MEMBERSHIP=off"}
	if pick.Model == "" {
		return env, nil
	}
	if own := v.Getenv(seatmodel.EnvVar); own != "" {
		return env, v.note("Model: " + seatmodel.EnvVar + "=" + own + " from your environment, not " + pick.Model + " the seats ask for.")
	}
	return append(env, seatmodel.EnvVar+"="+pick.Model), v.note(pick.Note())
}

// ccEnd is the session's end by its status, as bus-disconnected's for a ccy session.
func (v *Verbs) ccEnd(ctx context.Context, state string, key paths.CheckoutKey, name paths.SessionName, status string) error {
	holder, err := v.Holder(v.config(state, key))
	if err != nil {
		return err
	}
	return errors.Join(v.end(ctx, holder, name, status), holder.Close())
}
