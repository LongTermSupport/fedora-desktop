package cli

import (
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/exits"
)

// afterRun is a launch line's arguments after `teamchat run`, wherever the binary sits in it.
func afterRun(t *testing.T, line []string) []string {
	t.Helper()
	for i, a := range line {
		if strings.HasSuffix(a, "/teamchat") || a == "teamchat" {
			if i+1 < len(line) && line[i+1] == "run" {
				return line[i+2:]
			}
		}
	}
	t.Fatalf("no teamchat run in %q", line)
	return nil
}

func isUsage(err error) bool {
	e, ok := errors.AsType[*exits.Error](err)
	return ok && e.Exit == exits.ExitUsage
}

// CL5: every launch line the design spells parses with run's own parser.
func TestTheDesignsLaunchLinesParse(t *testing.T) {
	cases := []struct {
		name string
		line []string
		want Launch
	}{
		{
			// C 3.9.4's ExecStart, after tmux's own arguments.
			"lxc unit",
			strings.Fields("/usr/bin/tmux -L teamchat new-session -d -s teamchat /usr/local/bin/teamchat run --claude --kind lxc --desired /home/<user>/.config/teamchat/membership --resume-recorded -- claude"),
			Launch{Mode: LaunchClaude, Kind: KindLXC, Desired: "/home/<user>/.config/teamchat/membership", ResumeRecorded: true, Cmd: []string{"claude"}, Claude: 0},
		},
		{
			// ccy's entrypoint (B 6 C3, D 12), the supervisor as CMD.
			"ccy",
			strings.Fields("teamchat run --claude --kind ccy --desired /run/teamchat/membership -- /opt/ccy/supervisor --arm -- claude"),
			Launch{Mode: LaunchClaude, Kind: KindCCY, Desired: "/run/teamchat/membership", Cmd: []string{"/opt/ccy/supervisor", "--arm", "--", "claude"}, Claude: 3},
		},
		{
			// host cc (B 2.11's kinds table).
			"host cc",
			strings.Fields("teamchat run --claude --kind host --desired /home/<user>/.config/teamchat/membership -- claude"),
			Launch{Mode: LaunchClaude, Kind: KindHost, Desired: "/home/<user>/.config/teamchat/membership", Cmd: []string{"claude"}, Claude: 0},
		},
		{
			// C 3.3's acceptance probe: CMD under --plain is opaque, the shell's -c included.
			"probe",
			[]string{"teamchat", "run", "--plain", "--kind", "host", "--state", "/var/lib/probe/alpha", "--seat", "alpha/probe", "--", "/bin/sh", "-c", `teamchat ping check --to "$1" --team "$2" && teamchat status --team "$2"`, "probe", "repo.1+site.podman", "alpha"},
			Launch{Mode: LaunchPlain, Kind: KindHost, State: "/var/lib/probe/alpha", Seats: []SeatRef{{Team: mustTeam(t, "alpha"), Seat: mustSeat(t, "probe")}}, Cmd: []string{"/bin/sh", "-c", `teamchat ping check --to "$1" --team "$2" && teamchat status --team "$2"`, "probe", "repo.1+site.podman", "alpha"}, Claude: -1},
		},
		{
			// B 2.4.1 step 2's re-exec form.
			"re-exec into the kit",
			strings.Fields("/usr/local/lib/teamchat/kit/0123456789abcdef/bin/teamchat run --kit /usr/local/lib/teamchat/kit/0123456789abcdef --claude --kind lxc --desired /home/<user>/.config/teamchat/membership --resume-recorded -- claude --model opus"),
			Launch{Mode: LaunchClaude, Kind: KindLXC, Desired: "/home/<user>/.config/teamchat/membership", Kit: "/usr/local/lib/teamchat/kit/0123456789abcdef", ResumeRecorded: true, Cmd: []string{"claude", "--model", "opus"}, Claude: 0},
		},
		{
			"other, a shell around a wrapper",
			strings.Fields("teamchat run --claude --kind other -- /usr/local/bin/my-wrapper -x /opt/bin/claude --resume abc"),
			Launch{Mode: LaunchClaude, Kind: KindOther, Cmd: []string{"/usr/local/bin/my-wrapper", "-x", "/opt/bin/claude", "--resume", "abc"}, Claude: 2},
		},
		{
			"plain shell with no team",
			strings.Fields("teamchat run --plain --kind other -- bash"),
			Launch{Mode: LaunchPlain, Kind: KindOther, Cmd: []string{"bash"}, Claude: -1},
		},
		{
			"two probe seats",
			strings.Fields("teamchat run --plain --kind host --state /s --seat alpha/probe --seat beta/2 -- sleep 1"),
			Launch{Mode: LaunchPlain, Kind: KindHost, State: "/s", Seats: []SeatRef{{mustTeam(t, "alpha"), mustSeat(t, "probe")}, {mustTeam(t, "beta"), mustSeat(t, "2")}}, Cmd: []string{"sleep", "1"}, Claude: -1},
		},
	}
	for _, c := range cases {
		got, err := ParseLaunch(afterRun(t, c.line))
		if err != nil {
			t.Errorf("%s: %v", c.name, err)
			continue
		}
		if got.Mode != c.want.Mode || got.Kind != c.want.Kind || got.Desired != c.want.Desired || got.State != c.want.State ||
			got.Kit != c.want.Kit || got.ResumeRecorded != c.want.ResumeRecorded || !slices.Equal(got.Cmd, c.want.Cmd) ||
			got.Claude != c.want.Claude || !slices.Equal(got.Seats, c.want.Seats) {
			t.Errorf("%s:\ngot  %+v\nwant %+v", c.name, got, c.want)
		}
	}
}

func TestLaunchLinesRefused(t *testing.T) {
	for name, line := range map[string]string{
		"neither claude nor plain":       "--kind host -- claude",
		"both claude and plain":          "--claude --plain --kind host -- claude",
		"claude twice":                   "--claude --claude --kind host -- claude",
		"no kind":                        "--claude -- claude",
		"an unknown kind":                "--claude --kind vm -- claude",
		"kind twice":                     "--claude --kind host --kind ccy -- claude",
		"kind with no value":             "--claude --kind",
		"no separator":                   "--claude --kind host claude",
		"nothing after the separator":    "--claude --kind host --",
		"no arguments":                   "",
		"an unknown flag":                "--claude --kind host --colour -- claude",
		"a flag=value":                   "--claude --kind=host -- claude",
		"a stray argument":               "--claude --kind host extra -- claude",
		"a relative desired":             "--claude --kind host --desired rel/dir -- claude",
		"an unclean desired":             "--claude --kind host --desired /a/../b -- claude",
		"the root as state":              "--plain --kind host --state / -- sh",
		"desired twice":                  "--claude --kind host --desired /a --desired /b -- claude",
		"a relative kit":                 "--claude --kind host --kit kit -- claude",
		"no claude under --claude":       "--claude --kind host -- bash",
		"claude only in a name's middle": "--claude --kind host -- /opt/claude-code/bin/node",
		"settings anywhere":              "--claude --kind host -- claude --settings /x",
		"settings= anywhere":             "--claude --kind host -- claude --settings=/x",
		"plugin-dir anywhere":            "--claude --kind host -- wrap --plugin-dir /x -- claude",
		"a second separator":             "--claude --kind ccy -- sup -- --arm -- claude",
		"a separator not before claude":  "--claude --kind ccy -- sup -- --arm claude",
		"a separator after claude":       "--claude --kind ccy -- claude -- x",
		"resume beside resume-recorded":  "--claude --kind lxc --resume-recorded -- claude --resume abc",
		"resume= beside it":              "--claude --kind lxc --resume-recorded -- claude --resume=abc",
		"continue beside it":             "--claude --kind lxc --resume-recorded -- claude --continue",
		"-c beside it":                   "--claude --kind lxc --resume-recorded -- claude -c",
		"-r beside it":                   "--claude --kind lxc --resume-recorded -- claude -r abc",
		"resume-recorded under plain":    "--plain --kind lxc --resume-recorded -- sh",
		"seat without state":             "--plain --kind host --seat alpha/probe -- sh",
		"seat with desired":              "--plain --kind host --state /s --desired /d --seat alpha/probe -- sh",
		"a seat with no slash":           "--plain --kind host --state /s --seat alpha -- sh",
		"a seat with a bad team":         "--plain --kind host --state /s --seat Alpha/probe -- sh",
		"a seat with a bad name":         "--plain --kind host --state /s --seat alpha/Probe -- sh",
		"the same seat team twice":       "--plain --kind host --state /s --seat alpha/1 --seat alpha/2 -- sh",
	} {
		_, err := ParseLaunch(strings.Fields(line))
		if !isUsage(err) {
			t.Errorf("%s: %v", name, err)
		}
	}
}

// Under --plain, CMD is opaque: what --claude refuses in CMD is the command's own business.
func TestPlainCmdIsOpaque(t *testing.T) {
	got, err := ParseLaunch(strings.Fields("--plain --kind host -- sh -- -- --settings x --resume claude"))
	if err != nil || got.Claude != -1 || len(got.Cmd) != 7 {
		t.Errorf("%+v %v", got, err)
	}
}

func TestKindsAreBs(t *testing.T) {
	if !slices.Equal(Kinds, []Kind{KindCCY, KindLXC, KindHost, KindOther}) {
		t.Errorf("%v", Kinds)
	}
	if KindCCY != "ccy" || KindLXC != "lxc" || KindHost != "host" || KindOther != "other" {
		t.Error("kind names")
	}
}
