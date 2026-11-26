package chatclientinfo

// The hole this command punches through cc --teams' deny rules must stay a hole and never become
// a narrowing of them: the rules still refuse every route into a chat client's sandbox, and only
// this command's own invocation passes them (R82, Plan 00265).

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
)

const (
	repoRoot     = "../../.."
	denySettings = "files/var/local/claude-code/cc-teams-settings.json"
	deployPlay   = "playbooks/imports/play-claude-yolo.yml"
	// below is a deny rule's "everything under" glob, spelt so no line here reads as one.
	below = "/" + "**"
)

func denies(t *testing.T) []string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(repoRoot, denySettings))
	if err != nil {
		t.Fatal(err)
	}
	var settings struct {
		Permissions struct {
			Deny []string `json:"deny"`
		} `json:"permissions"`
	}
	if err := json.Unmarshal(data, &settings); err != nil {
		t.Fatal(err)
	}
	return settings.Permissions.Deny
}

// bashDenied reports which Bash deny rule refuses command: Claude Code's Bash rule is a glob
// over the whole command line, where * is any run of characters.
func bashDenied(rules []string, command string) (string, bool) {
	for _, rule := range rules {
		glob, ok := strings.CutPrefix(rule, "Bash(")
		if !ok {
			continue
		}
		glob = strings.TrimSuffix(glob, ")")
		pattern := "^" + strings.ReplaceAll(regexp.QuoteMeta(glob), `\*`, ".*") + "$"
		if regexp.MustCompile(pattern).MatchString(command) {
			return rule, true
		}
	}
	return "", false
}

func TestTheDenyRulesStillCoverEveryClient(t *testing.T) {
	rules := denies(t)
	for _, c := range clients(t) {
		for _, want := range []string{
			"Bash(*" + c.AppID + "*)",
			"Read(~/" + c.FlatpakDir() + below + ")",
			"Edit(~/" + c.FlatpakDir() + below + ")",
		} {
			if !slices.Contains(rules, want) {
				t.Errorf("%s no longer denies %s: the chat-client-info hole must never narrow the rule", denySettings, want)
			}
		}
	}
}

func TestEveryRouteIntoTheSandboxIsStillRefused(t *testing.T) {
	rules := denies(t)
	for _, c := range clients(t) {
		for _, command := range []string{
			"flatpak run --command=sh " + c.AppID,
			"flatpak enter " + c.AppID + " sh",
			"flatpak info " + c.AppID,
			"cat ~/" + c.FlatpakDir() + "/config/x",
		} {
			if _, denied := bashDenied(rules, command); !denied {
				t.Errorf("%s no longer refuses %q", denySettings, command)
			}
		}
	}
}

func TestTheCommandPassesTheDenyRules(t *testing.T) {
	rules := denies(t)
	for _, command := range []string{InstallPath, filepath.Base(InstallPath)} {
		if rule, denied := bashDenied(rules, command); denied {
			t.Errorf("%s refuses %q by %s: the hole is closed", denySettings, command, rule)
		}
	}
}

func TestThePlayInstallsTheCommandWhereTheDocsSay(t *testing.T) {
	data, err := os.ReadFile(filepath.Join(repoRoot, deployPlay))
	if err != nil {
		t.Fatal(err)
	}
	block := "go_command_name: " + filepath.Base(InstallPath) + "\n        go_command_dest: " + InstallPath + "\n"
	if !strings.Contains(string(data), block) {
		t.Errorf("%s does not build %s into %s", deployPlay, filepath.Base(InstallPath), InstallPath)
	}
}
