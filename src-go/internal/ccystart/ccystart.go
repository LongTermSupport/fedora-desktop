// Package ccystart is the end of a ccy container's start (Plan 00209 Task 4.2): after
// entrypoint.sh has prepared the container and sourced the project's ccy.env, it execs
// ccy-start-claude with the session's command, and this package builds the command line the
// session runs and execs it. It was the bash at the end of entrypoint.sh.
//
// Each launch is on one agent team bus system at most (DESIGN-v2-D-migration.md OD-D2):
//
//   - S1. v1, the launcher's PINGBUS_MEMBERSHIP: `off`, or the membership mount, which puts
//     `pingbus seat exec --` in front of the command and the v1 plugin and settings after
//     claude. Every v1 launch gets the command line, refusals and messages the bash gave, byte
//     for byte: parity_test.go runs the bash as it was against this package.
//   - S2. v2, the launcher's TEAMCHAT_MEMBERSHIP, the read-only mount of the session's
//     desired.json, beside PINGBUS_MEMBERSHIP=off: /usr/local/bin/teamchat is linked to the
//     image's kit and the session runs
//     `teamchat run --claude --kind ccy --desired /run/teamchat/membership -- <wrapper> claude ...`
//     (DESIGN-v2-B-member.md C3), the wrapper being the supervisor line v1 would run.
//     TEAMCHAT_MEMBERSHIP does not reach the session: run takes every launch fact as an argument.
//   - S3. A v2 launch beside a v1 membership, a TEAMCHAT_MEMBERSHIP that is not the mount, a
//     command other than claude, or an image or mount missing a file the launch needs, is
//     refused with exit 1 and nothing linked.
//   - S4. The supervisor wrap (default on when the project ships one, `--no-supervise` off),
//     the lifecycle plugin for --max-age/--run-for/--until, and the plugin for a bus session's
//     first-join relaunch, as entrypoint.sh had them; a v2 session counts as on a bus.
package ccystart

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

// Paths are the fixed places in the image and the container the start reads and links.
type Paths struct {
	AgentBus          string // the v1 kit
	PingbusLink       string
	Membership        string // the v1 membership mount, PINGBUS_MEMBERSHIP's one seated value
	SessionHome       string // where `pingbus seat exec` builds the session home
	DefaultSupervisor string
	SupervisorPlugins string
	Teamchat          string // the v2 kit
	TeamchatLink      string
	TeamchatDesired   string // the v2 membership mount, TEAMCHAT_MEMBERSHIP's one value
}

// ImagePaths are the ccy image's.
func ImagePaths() Paths {
	return Paths{
		AgentBus:          "/opt/claude-yolo/optional/agent-bus",
		PingbusLink:       "/usr/local/bin/pingbus",
		Membership:        "/run/ccy/membership",
		SessionHome:       "/tmp/pingbus-home",
		DefaultSupervisor: "/workspace/.claude/ccy/claude-supervise.py",
		SupervisorPlugins: "/opt/claude-yolo/supervisor-plugins",
		Teamchat:          "/opt/claude-yolo/optional/teamchat",
		TeamchatLink:      "/usr/local/bin/teamchat",
		TeamchatDesired:   "/run/teamchat/membership",
	}
}

// Session is one start: the environment entrypoint.sh exported, the session's command, and the
// adapters.
type Session struct {
	Env    func(string) (string, bool)
	Args   []string
	Paths  Paths
	Stderr io.Writer
	Runner execx.Runner // the supervisor's syntax probe
	Execer execx.Execer
}

const (
	envMembership = "PINGBUS_MEMBERSHIP"
	envTeamchat   = "TEAMCHAT_MEMBERSHIP"
	envWrapperVar = "CCY_CLAUDE_WRAPPER"
	membershipOff = "off"
	claudeCmd     = "claude"
)

// errRefused is a refusal already written to stderr.
var errRefused = errors.New("refused")

type start struct {
	Session

	failed error // the first stderr write that failed
	prefix []string
	cmd    execx.Cmd
	onBus  bool
}

func (s *start) sayf(format string, args ...any) {
	if s.failed != nil {
		return
	}
	if _, err := fmt.Fprintf(s.Stderr, format, args...); err != nil {
		s.failed = err
	}
}

func (s *start) get(name string) string {
	v, ok := s.Env(name)
	if !ok {
		return ""
	}
	return v
}

func (s *start) arg0() string {
	if len(s.Args) == 0 {
		return ""
	}
	return s.Args[0]
}

// Start builds the session's command and execs it; it returns only on a refusal or a failed
// exec.
func Start(ctx context.Context, sess Session) term.ExitCode {
	s := &start{Session: sess}
	s.cmd.Args = slices.Clone(s.Args)
	err := s.bus()
	if err == nil {
		err = s.wrap(ctx)
	}
	if err == nil {
		argv := slices.Concat(s.prefix, s.cmd.Args)
		if len(argv) == 0 {
			return term.ExitOK // bash's exec with no command did nothing, and the entrypoint ended
		}
		s.cmd.Name, s.cmd.Args = argv[0], argv[1:]
		if err = s.Execer.Exec(s.cmd); err == nil {
			return term.ExitOK // only a recording Execer returns from a successful exec
		}
		s.sayf("✗ CCY: could not start the session: %v\n", err)
	}
	return term.ExitFailure
}

// bus is the agent team bus step: one system's kit, or neither.
func (s *start) bus() error {
	old := ""
	for _, name := range []string{"PINGBUS_SEATS", "PINGBUS_TEAMS", "PINGBUS_HOME"} {
		if _, set := s.Env(name); set {
			old = name + " came from the launcher, which passes only PINGBUS_MEMBERSHIP"
		}
	}
	if _, v2 := s.Env(envTeamchat); v2 && old == "" {
		return s.teamchat()
	}
	membership, set := s.Env(envMembership)
	switch {
	case old == "" && set && membership == membershipOff:
		return nil
	case old == "" && set && membership == s.Paths.Membership:
		return s.pingbus()
	}
	why := old
	if why == "" {
		shown := "unset"
		if set {
			shown = membership
		}
		why = "PINGBUS_MEMBERSHIP is " + shown + ", not a value ccy passes"
	}
	s.sayf("✗ CCY: %s: ccy on the host is older than this image.\n  Update ccy (play-claude-yolo.yml), then launch again.\n", why)
	return errRefused
}

func (s *start) pingbus() error {
	kit := s.Paths.AgentBus
	for _, need := range []string{
		kit + "/pingbus", kit + "/pingbus-status-line", kit + "/plugin/pingbus/.claude-plugin/plugin.json",
		kit + "/plugin/pingbus/hooks/hooks.json", s.Paths.Membership + "/membership.json",
	} {
		if !isFile(need) {
			s.sayf("✗ CCY: this session was launched with the agent team bus kit, but %s is missing.\n  An image older than the kit (rebuild it: ccy --rebuild), or a launch that did not mount the membership (update ccy).\n", need)
			return errRefused
		}
	}
	if s.arg0() != claudeCmd {
		s.sayf("✗ CCY: this session was launched with the agent team bus kit, but the command is '%s', not claude, so the bus plugin cannot be added.\n", s.arg0())
		return errRefused
	}
	if err := link(kit+"/pingbus", s.Paths.PingbusLink); err != nil {
		s.sayf("ln: %v\n", err)
		return errRefused
	}
	s.cmd.Args = slices.Concat([]string{claudeCmd, "--plugin-dir", kit + "/plugin/pingbus", "--settings", s.Paths.SessionHome + "/settings.json"}, s.Args[1:])
	s.prefix = []string{kit + "/pingbus", "seat", "exec", "--"}
	s.onBus = true
	return nil
}

// teamchat is the v2 launch (S2, S3).
func (s *start) teamchat() error {
	desired := s.get(envTeamchat)
	if m := s.get(envMembership); m != membershipOff {
		s.sayf("✗ CCY: this session was launched on agent team bus teams of both systems (PINGBUS_MEMBERSHIP is %s beside TEAMCHAT_MEMBERSHIP).\n  A session is on one system only: run two sessions, or migrate the other team. Update ccy if it passed both.\n", m)
		return errRefused
	}
	if desired != s.Paths.TeamchatDesired {
		s.sayf("✗ CCY: TEAMCHAT_MEMBERSHIP is %s, not a value ccy passes.\n  Update ccy (play-claude-yolo.yml), then launch again.\n", desired)
		return errRefused
	}
	bin := s.Paths.Teamchat + "/bin/teamchat"
	for _, need := range []string{bin, desired + "/desired.json"} {
		if !isFile(need) {
			s.sayf("✗ CCY: this session was launched as a teamchat member, but %s is missing.\n  An image older than the teamchat kit (rebuild it: ccy --rebuild), or a launch that did not mount the membership (update ccy).\n", need)
			return errRefused
		}
	}
	if s.arg0() != claudeCmd {
		s.sayf("✗ CCY: this session was launched as a teamchat member, but the command is '%s', not claude.\n", s.arg0())
		return errRefused
	}
	if err := link(bin, s.Paths.TeamchatLink); err != nil {
		s.sayf("✗ CCY: could not link teamchat onto PATH: %v\n", err)
		return errRefused
	}
	s.prefix = []string{s.Paths.TeamchatLink, "run", "--claude", "--kind", "ccy", "--desired", desired, "--"}
	s.cmd.Unset = append(s.cmd.Unset, envTeamchat)
	s.onBus = true
	return nil
}

// link is `ln -sf target name`.
func link(target, name string) error {
	if err := os.Remove(name); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("replace %s: %w", name, err)
	}
	if err := os.Symlink(target, name); err != nil {
		return fmt.Errorf("link %s: %w", name, err)
	}
	return nil
}

func isFile(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular()
}

// wrap is the supervisor wrap and its plugins (S4).
func (s *start) wrap(ctx context.Context) error {
	wrapper, exported := s.Env(envWrapperVar)
	supervisor := s.get("CCY_SUPERVISOR_PATH")
	if supervisor == "" {
		supervisor = s.Paths.DefaultSupervisor
	}
	switch {
	case s.get("CCY_NO_SUPERVISOR") == "1":
		if wrapper != "" {
			s.sayf("Supervisor: off (--no-supervise); the wrapper the project set is not used: %s\n", wrapper)
		}
		wrapper = ""
		s.cmd.Unset = append(s.cmd.Unset, envWrapperVar)
	case wrapper == "" && isFile(supervisor):
		out, err := execx.Combined(ctx, s.Runner, execx.Cmd{
			Name: "python3",
			Args: []string{"-c", "import ast,sys; ast.parse(open(sys.argv[1]).read(), sys.argv[1])", supervisor},
		})
		if err != nil {
			s.sayf("✗ CCY: the project supervisor at %s does not parse.\n%s\n", supervisor, strings.TrimRight(out, "\n"))
			s.sayf("  Restore it from git, or re-deploy it with the hooks daemon.\n  To launch without it (NO ctrl+z guard): ccy --no-supervise\n")
			return fmt.Errorf("%w: the supervisor probe: %w", errRefused, err)
		}
		wrapper = "python3 " + supervisor + " --"
		if exported {
			s.cmd.Env = append(s.cmd.Env, envWrapperVar+"="+wrapper)
		}
		s.sayf("Supervisor: on by default (ctrl+z guard active, auto-compaction unarmed)\n")
	case wrapper == "":
		s.sayf("Supervisor: not present at %s — ctrl+z is UNGUARDED in this session.\n  Install the hooks daemon in this project to get the guard back.\n", supervisor)
	}
	return s.plugins(wrapper)
}

// plugins puts the wrapper in front of the command, with the lifecycle plugin when the launch
// asked for a limit or the session is on a bus.
func (s *start) plugins(wrapper string) error {
	lifecycle := s.get("CCY_LIFECYCLE_MAX_AGE_SECONDS") != "" || s.get("CCY_LIFECYCLE_DEADLINE_EPOCH") != ""
	if wrapper == "" {
		if lifecycle {
			s.sayf("✗ CCY: --max-age/--run-for/--until are carried out by the supervisor, and this session runs without one.\n  Install the hooks daemon in this project (it deploys the supervisor), or drop the option.\n")
			return errRefused
		}
		return nil
	}
	// read -ra read the first line only.
	firstLine, _, _ := strings.Cut(wrapper, "\n")
	words := strings.FieldsFunc(firstLine, func(r rune) bool { return r == ' ' || r == '\t' })
	if pythonOnShell(words) {
		s.sayf("✗ CCY: CCY_CLAUDE_WRAPPER runs a shell script through Python: %s\n  Point CCY_CLAUDE_WRAPPER at claude-supervise (no python3) or claude-supervise.py, in .claude/ccy/ccy.env (or ccy.env.local).\n", wrapper)
		return errRefused
	}
	switch {
	case lifecycle:
		extended, why := s.lifecyclePlugin(words, wrapper)
		if why != "" {
			s.sayf("%s", why)
			return errRefused
		}
		words = extended
	case s.onBus:
		extended, why := s.lifecyclePlugin(words, wrapper)
		if why != "" {
			s.sayf("⚠ CCY: a first team join cannot relaunch this session by itself, so it will need a relaunch by hand: %s\n", strings.ReplaceAll(why, "\n", " "))
		} else {
			words = extended
		}
	}
	s.prefix = slices.Concat(s.prefix, words)
	return nil
}

var shellShebang = regexp.MustCompile(`^#!.*[/ ](ba|da)?sh( |$)`)

// pythonOnShell is a python interpreter handed a sh or bash script.
func pythonOnShell(words []string) bool {
	if len(words) < 2 || !strings.HasPrefix(filepath.Base(words[0]), "python") || !isFile(words[1]) {
		return false
	}
	f, err := os.Open(words[1])
	if err != nil {
		return false
	}
	line, err := bufio.NewReader(f).ReadString('\n')
	if cerr := f.Close(); cerr != nil || (err != nil && !errors.Is(err, io.EOF)) {
		return false
	}
	return shellShebang.MatchString(strings.TrimSuffix(line, "\n"))
}

var (
	pluginAPI     = regexp.MustCompile(`^PLUGIN_API = [0-9]+$`)
	supervisorAPI = regexp.MustCompile(`^_PLUGIN_API_MAJOR = [0-9]+$`)
)

// lifecyclePlugin is the wrapper with the lifecycle plugin named before its final --, or the
// refusal saying why it cannot be (the text ends in a newline).
func (s *start) lifecyclePlugin(words []string, wrapper string) ([]string, string) {
	plugin := s.Paths.SupervisorPlugins + "/ccy_lifecycle.py"
	supervisor := ""
	for _, word := range words {
		if strings.HasSuffix(word, "claude-supervise.py") {
			supervisor = word
			break
		}
		if strings.HasSuffix(word, "/claude-supervise") {
			supervisor = word + ".py"
			break
		}
	}
	last := len(words) - 1
	if supervisor == "" || last < 1 || words[last] != "--" {
		return nil, "✗ CCY: --max-age/--run-for/--until need the hooks-daemon supervisor as the claude wrapper, but the wrapper is:\n    " +
			wrapper + "\n  It must run the supervisor (claude-supervise.py, or the daemon's claude-supervise launcher) and end in --.\n  Fix CCY_CLAUDE_WRAPPER, or drop the option.\n"
	}
	if !isFile(plugin) {
		return nil, "✗ CCY: the lifecycle plugin is missing from this image (" + plugin + ").\n  Rebuild the image: ccy --rebuild\n"
	}
	if !isFile(supervisor) {
		return nil, "✗ CCY: the supervisor the wrapper runs is not there: " + supervisor + "\n    (the wrapper is: " + wrapper + ")\n"
	}
	want, err := apiOf(plugin, pluginAPI)
	if err != nil || want == "" {
		return nil, "✗ CCY: the lifecycle plugin in this image declares no PLUGIN_API (" + plugin + "). Rebuild: ccy --rebuild\n"
	}
	have, err := apiOf(supervisor, supervisorAPI)
	if err != nil {
		return nil, "✗ CCY: could not read the supervisor at " + supervisor + ".\n"
	}
	if have == "" {
		return nil, "✗ CCY: this project's supervisor predates the plugin API that --max-age/--run-for/--until need.\n  upgrade the hooks daemon in this project to a release with the supervisor plugin API, or drop the option.\n"
	}
	if have != want {
		return nil, "✗ CCY: this project's supervisor speaks plugin API " + have + ", and the lifecycle plugin speaks plugin API " + want +
			".\n  Update ccy, or upgrade the hooks daemon in this project, so the two agree; or drop the option.\n"
	}
	return slices.Concat(words[:last], []string{"--plugin", "ccy-lifecycle=" + plugin, "--"}), ""
}

// apiOf is the third field of the first line of path matching line, as awk printed it.
func apiOf(path string, line *regexp.Regexp) (string, error) {
	data, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		return "", fmt.Errorf("read %s: %w", path, err)
	}
	for l := range bytes.SplitSeq(data, []byte("\n")) {
		if line.Match(l) {
			return strings.Fields(string(l))[2], nil
		}
	}
	return "", nil
}
