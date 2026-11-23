package teamchatlaunch

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

// The parity oracle for cc-run on agent-bus (Plan 00209 Task 5.1 step 5 (a)): a v1 host launch
// gets the process (program, arguments and added variable) and the settings file the bash
// built, byte for byte.
// testdata/cc-teams-v1.bash.txt is files/var/local/claude-code/cc-teams.bash as it was at the
// commit before the port, less its shebang line (so the bash-limits gate does not measure it as
// a tool). Each scenario runs its cc_teams_settings and cc_teams_command over a
// scratch tree, with ccy-teams replaced by a recorder, and then cc-run over the same tree.

const ccTeamsFile = "testdata/cc-teams-v1.bash.txt"

const ccTeamsDriver = `set -euo pipefail
print_error() { printf '%s\n' "$*" >&2; }
ccy_tmux_current_session() { printf '%s' "$SESSION"; }
source "$FROZEN"
CC_TEAMS_KIT=$KIT CC_TEAMS_DENY_SETTINGS=$DENY CC_TEAMS_CCY_TEAMS=$STUB
CC_TEAMS_BUS=$BUS CC_TEAMS_SEATS=$SEATS
cc_teams_settings "$TMP/settings.json"
cc_teams_command "$TMP" "$CHECKOUT" "$@"
printf '%s\0' "$$" "${CC_TEAMS_CMD[@]}"
`

// The membership-init recorder: its arguments after `internal membership-init`, and the
// directory the Go fake prints for them.
const ccTeamsStub = `#!/bin/sh
shift 2
printf '%s\0' "$@" >"$RECORD"
printf '/run/m/%s\n' "$1"
`

func TestCCRunOnAgentBusIsTheBashLaunchByteForByte(t *testing.T) {
	for name, c := range map[string]struct {
		session, seats, bus string
		args                []string
	}{
		"a team in tmux":      {"cc-s1", "1@v1team", "false", []string{"--model", "opus", "--", "x y"}},
		"two teams":           {"cc-s1", "1@v1team,2@v1other", "false", nil},
		"--bus outside tmux":  {"", "", "true", []string{"-c"}},
		"a team outside tmux": {"", "1@v1team", "false", []string{"--resume", "abc"}},
	} {
		t.Run(name, func(t *testing.T) {
			w := newCC(t)
			w.systems["v1other"] = "agent-bus"
			writeTestFile(t, w.cc.V1Kit+"/settings.json", `{"statusLine":{"type":"command","command":"pingbus-status-line"},"permissions":{"allow":["Bash(pingbus:*)"]},"env":{"A":"1"}}`)
			writeTestFile(t, w.cc.DenySettings, "{\n  \"permissions\": {\n    \"deny\": [\"Read(~/.config/Element/**)\", \"Bash(*keyring*)\"]\n  }\n}\n")
			stub := filepath.Join(t.TempDir(), "ccy-teams")
			writeTestFile(t, stub, ccTeamsStub)
			if err := os.Chmod(stub, 0o700); err != nil {
				t.Fatal(err)
			}
			record := filepath.Join(t.TempDir(), "record")
			frozen, err := filepath.Abs(ccTeamsFile)
			if err != nil {
				t.Fatal(err)
			}
			driver := filepath.Join(t.TempDir(), "driver.bash")
			writeTestFile(t, driver, ccTeamsDriver)
			res, err := execx.OS{}.Run(t.Context(), execx.Cmd{
				Name: "bash", Args: append([]string{driver}, c.args...),
				Env: []string{"FROZEN=" + frozen, "KIT=" + w.cc.V1Kit, "DENY=" + w.cc.DenySettings, "STUB=" + stub,
					"RECORD=" + record, "SESSION=" + c.session, "BUS=" + c.bus, "SEATS=" + c.seats, "TMP=" + w.tmp, "CHECKOUT=" + w.checkout},
			})
			if err != nil {
				t.Fatalf("the bash: %v: %s", err, res.Stderr)
			}
			bashLine := strings.Split(strings.TrimSuffix(string(res.Stdout), "\x00"), "\x00")
			pid, bashLine := bashLine[0], bashLine[1:]
			bashSettings := readTestFile(t, w.tmp+"/settings.json")
			bashInit := strings.Split(strings.TrimSuffix(readTestFile(t, record), "\x00"), "\x00")
			if err := os.Remove(w.tmp + "/settings.json"); err != nil {
				t.Fatal(err)
			}

			args := append([]string{"cc-run", w.tmp, w.checkout, c.session, pid, c.seats, c.bus, "--"}, c.args...)
			if code := w.run(args...); code != term.ExitOK {
				t.Fatalf("exit %d: %s", code, w.err)
			}
			execs := w.exec.Execs()
			if len(execs) != 1 {
				t.Fatalf("execs %+v", execs)
			}
			// The bash ran env with the variable before the program; Go adds it to the program's
			// environment and execs the program itself.
			goLine := append(append([]string{"env"}, execs[0].Env...), append([]string{execs[0].Name}, execs[0].Args...)...)
			if !slices.Equal(goLine, bashLine) {
				t.Errorf("command\ngo   %q\nbash %q", goLine, bashLine)
			}
			if got := readTestFile(t, w.tmp+"/settings.json"); got != bashSettings {
				t.Errorf("settings\ngo   %s\nbash %s", got, bashSettings)
			}
			if info, err := os.Stat(w.tmp + "/settings.json"); err != nil || info.Mode().Perm() != 0o600 {
				t.Errorf("settings mode %v, %v", info, err)
			}
			if !slices.Equal(w.initArgs, bashInit) {
				t.Errorf("membership-init\ngo   %q\nbash %q", w.initArgs, bashInit)
			}
			if w.taker.took != nil || len(w.fg.cmds) != 0 {
				t.Errorf("took %+v, started %v", w.taker.took, w.fg.cmds)
			}
		})
	}
}

func readTestFile(t *testing.T, name string) string {
	t.Helper()
	data, err := os.ReadFile(name)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}
