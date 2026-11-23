package teamchatlaunch

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

// repoDenySettings is the deny file play-claude-yolo.yml installs at CC.DenySettings, read from
// this checkout.
const repoDenySettings = "../../../../files/var/local/claude-code/cc-teams-settings.json"

// hostLaneDeny is the host seat's lane (Plan 00263 decision D3, the static half): a host cc
// session runs deployments and triage, never tests, builds or the heavy QA, which belong in a
// container. The heavy QA is named script by script (each runs tests or builds); a light,
// host-only gate such as qa-deployed-drift.bash stays allowed. These are Claude Code permission
// rules; a deny rule matches the command text only, so the hooks daemon's duty-aware handler is
// the check that sees past them.
var hostLaneDeny = []string{
	"Bash(go test)", "Bash(go test *)",
	"Bash(go build)", "Bash(go build *)",
	"Bash(go vet)", "Bash(go vet *)",
	"Bash(golangci-lint)", "Bash(golangci-lint *)",
	"Bash(*scripts/qa-all.bash*)",
	"Bash(*scripts/qa-changed.bash*)",
	"Bash(*scripts/qa-go-qa.bash*)",
	"Bash(*scripts/qa-go-deps.bash*)",
	"Bash(*scripts/qa-go-vuln.bash*)",
	"Bash(*scripts/qa-helper-tests.bash*)",
	"Bash(*scripts/qa-plan-tests.bash*)",
	"Bash(*scripts/qa-hook-handlers.bash*)",
	"Bash(npm test)", "Bash(npm test *)",
	"Bash(npm run test)", "Bash(npm run test *)",
	"Bash(pytest)", "Bash(pytest *)",
	"Bash(python -m pytest*)", "Bash(python3 -m pytest*)",
	"Bash(make)", "Bash(make *)",
	"Bash(podman build)", "Bash(podman build *)",
	"Bash(ansible-playbook)", "Bash(ansible-playbook *)",
	"Bash(semgrep)", "Bash(semgrep *)",
}

func denyRulesOf(t *testing.T, data []byte) []string {
	t.Helper()
	var settings struct {
		Permissions struct {
			Deny []string `json:"deny"`
		} `json:"permissions"`
	}
	if err := json.Unmarshal(data, &settings); err != nil {
		t.Fatalf("settings %q: %v", data, err)
	}
	return settings.Permissions.Deny
}

func missingHostLane(rules []string) []string {
	var missing []string
	for _, r := range hostLaneDeny {
		if !slices.Contains(rules, r) {
			missing = append(missing, r)
		}
	}
	return missing
}

// The deny file the host kit ships holds every host-lane rule.
func TestTheDenyFileHoldsTheHostLane(t *testing.T) {
	data, err := os.ReadFile(filepath.FromSlash(repoDenySettings))
	if err != nil {
		t.Fatal(err)
	}
	if missing := missingHostLane(denyRulesOf(t, data)); len(missing) > 0 {
		t.Errorf("%s does not deny %q", repoDenySettings, missing)
	}
}

// denies is whether a Bash deny rule of rules matches command, as Claude Code matches one: the
// whole command against the rule's pattern, where * is any text.
func denies(rules []string, command string) bool {
	for _, r := range rules {
		pattern, ok := strings.CutPrefix(r, "Bash(")
		if !ok {
			continue
		}
		parts := strings.Split(strings.TrimSuffix(pattern, ")"), "*")
		for i, p := range parts {
			parts[i] = regexp.QuoteMeta(p)
		}
		if regexp.MustCompile("^" + strings.Join(parts, ".*") + "$").MatchString(command) {
			return true
		}
	}
	return false
}

// The shipped rules deny the heavy QA however it is spelled, and leave the light host-only gate,
// the deployment route and the plain commands a host seat needs. No rule names a verb that does
// not exist (run.bash has no qa verb) or every qa script at once.
func TestTheDenyFileDeniesOnlyTheHeavyQA(t *testing.T) {
	data, err := os.ReadFile(filepath.FromSlash(repoDenySettings))
	if err != nil {
		t.Fatal(err)
	}
	rules := denyRulesOf(t, data)
	for _, command := range []string{
		"./scripts/qa-all.bash", "scripts/qa-changed.bash", "./scripts/qa-changed.bash --all",
		"./scripts/qa-go-qa.bash", "bash scripts/qa-go-deps.bash", "./scripts/qa-go-vuln.bash",
		"./scripts/qa-helper-tests.bash", "./scripts/qa-plan-tests.bash", "./scripts/qa-hook-handlers.bash",
		"go test ./...", "make",
	} {
		if !denies(rules, command) {
			t.Errorf("%q is not denied", command)
		}
	}
	for _, command := range []string{
		"./scripts/qa-deployed-drift.bash", "scripts/qa-deployed-drift.bash",
		"./CLAUDE/Plan/meta-deploy.bash", "./run.bash", "git status", "teamchat whoami --json",
	} {
		if denies(rules, command) {
			t.Errorf("%q is denied, but a host seat runs it", command)
		}
	}
	for _, gone := range []string{"Bash(*scripts/qa-*.bash*)", "Bash(*run.bash qa*)"} {
		if slices.Contains(rules, gone) {
			t.Errorf("the deny file still holds %s", gone)
		}
	}
}

// Both cc-run paths assemble the shipped deny file into the session's settings, so each
// host-lane rule reaches claude: teamchat's extra settings and agent-bus's merged settings.
func TestCCRunAssemblesTheHostLane(t *testing.T) {
	data, err := os.ReadFile(filepath.FromSlash(repoDenySettings))
	if err != nil {
		t.Fatal(err)
	}
	w := newCC(t)
	writeTestFile(t, w.cc.DenySettings, string(data))
	w.pool("repo.1+site.host")
	if code := w.ccRun("cc-s1", "1@alpha", "false"); code != term.ExitOK {
		t.Fatalf("exit %d: %s", code, w.err)
	}
	if len(w.fg.extra) != 1 {
		t.Fatalf("extra settings %q", w.fg.extra)
	}
	extra := w.fg.extra[0][:len(w.fg.extra[0])-1]
	if missing := missingHostLane(denyRulesOf(t, []byte(extra))); len(missing) > 0 {
		t.Errorf("teamchat extra settings miss %q", missing)
	}
	merged, err := v1Settings(w.cc.V1Kit+"/settings.json", w.cc.DenySettings, false)
	if err != nil {
		t.Fatal(err)
	}
	if missing := missingHostLane(denyRulesOf(t, merged)); len(missing) > 0 {
		t.Errorf("agent-bus settings miss %q", missing)
	}
}
