package ghaccount

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

func hasEnv(c execx.Cmd, kv string) bool { return slices.Contains(c.Env, kv) }

func hasSecret(c execx.Cmd, name, value string) bool {
	return slices.ContainsFunc(c.Secrets, func(s execx.Secret) bool { return s.Name() == name && s.Is(value) })
}

// E1: the usage, with nothing run.
func TestHelpAndNoArgumentPrintTheUsage(t *testing.T) {
	for _, args := range [][]string{nil, {"--help"}, {"--check", "-h"}} {
		w := newWorld(t)
		if code := w.run(args...); code != term.ExitOK {
			t.Fatalf("%q: exit %d", args, code)
		}
		w.mustContain("stdout", "--add=ALIAS:USERNAME")
		w.mustContain("stdout", "vars/github-required-scopes.yml")
		if len(w.lines()) != 0 {
			t.Errorf("%q ran %v", args, w.lines())
		}
	}
}

// E2.
func TestUsageErrorsExit64(t *testing.T) {
	w := newWorld(t)
	if code := w.run("--check", "--setup-all"); code != term.ExitUsage {
		t.Fatalf("exit %d", code)
	}
	w.mustContain("stderr", "gh-account-setup: ")
	w.mustContain("stderr", "— see 'gh-account-setup --help'.")
	if len(w.lines()) != 0 {
		t.Errorf("ran %v", w.lines())
	}
}

// E4.
func TestTheCheckoutMustBeGiven(t *testing.T) {
	for _, root := range []string{"", "relative/dir", "/does/not/exist"} {
		w := newWorld(t)
		w.env["GH_ACCOUNT_SETUP_ROOT"] = root
		if code := w.run("--check"); code != term.ExitFailure {
			t.Fatalf("%q: exit %d", root, code)
		}
		w.mustContain("stderr", "scripts/gh-account-setup.bash")
		if len(w.lines()) != 0 {
			t.Errorf("%q ran %v", root, w.lines())
		}
	}
	w := newWorld(t)
	w.env["HOME"] = ""
	if code := w.run("--check"); code != term.ExitFailure {
		t.Fatalf("no HOME: exit %d", code)
	}
}

// E5.
func TestGhMustBeInstalledAndNewEnough(t *testing.T) {
	w := newWorld(t)
	w.env["TEST_NO_GH"] = "1"
	if code := w.run("--check"); code != term.ExitFailure {
		t.Fatalf("exit %d", code)
	}
	w.mustContain("stderr", "GitHub CLI (gh) is not installed")

	w = newWorld(t)
	w.ghVersion = "gh version 2.80.0 (2025-09-10)\n"
	if code := w.run("--check"); code != term.ExitFailure {
		t.Fatalf("exit %d", code)
	}
	w.mustContain("stderr", "2.80.0")
	w.mustContain("stderr", "v2.81.0+")
	if w.ran("gh auth") {
		t.Error("went on past an old gh")
	}
}

// E6.
func TestAStatusGhCannotGiveStopsTheRun(t *testing.T) {
	w := newWorld(t)
	w.config("work:alice")
	w.statusFails = true
	if code := w.run("--check"); code != term.ExitFailure {
		t.Fatalf("exit %d", code)
	}
	w.mustContain("stderr", "fatal")
}

// E7, E8.
func TestConfigurationProblems(t *testing.T) {
	w := newWorld(t)
	if code := w.run("--check"); code != term.ExitFailure {
		t.Fatalf("missing file: exit %d", code)
	}
	w.mustContain("stderr", "Config not found: "+w.yml)

	w = newWorld(t)
	w.write(w.yml, "github_accounts:\n  work:\n")
	if code := w.run("--setup-all"); code != term.ExitFailure {
		t.Fatalf("null username: exit %d", code)
	}
	w.mustContain("stderr", "work")

	w = newWorld(t)
	w.write(w.yml, "a: 1\n")
	for _, mode := range []string{"--check", "--setup-all"} {
		if code := w.run(mode); code != term.ExitOK {
			t.Fatalf("%s no accounts: exit %d", mode, code)
		}
		w.mustContain("stderr", "No github_accounts entries found in "+w.yml)
	}
}

// E13-E20, E26: a full setup of an account that needs nothing and one that needs everything.
func TestSetupAllSetsUpEveryAccount(t *testing.T) {
	w := newWorld(t)
	w.config("work:alice", "oss:bob")
	w.ready("work", "alice")
	w.active = "alice"
	w.loginAs = "bob"
	if code := w.run("--setup-all"); code != term.ExitOK {
		t.Fatalf("exit %d\n%s", code, w.stderr.String())
	}
	for _, s := range []string{
		"Setting up 2 GitHub account(s)", "━━━ work (alice) ━━━", "✓ Authenticated: alice",
		"✓ SSH verified: work → alice", "✓ Key uploaded: bob", "✓ SSH verified: oss → bob", "All accounts configured!",
	} {
		w.mustContain("stderr", s)
	}
	if w.stdout.Len() != 0 {
		t.Errorf("stdout: %q", w.stdout.String())
	}

	login := w.mustForeground("gh auth login")
	if want := "gh auth login --hostname github.com --git-protocol ssh --web --skip-ssh-key --scopes " + fakeRequiredScopes; login.Line() != want {
		t.Errorf("login %q", login.Line())
	}
	if !hasEnv(login, `GH_BROWSER=bash "`+filepath.Join(w.root, "files/usr/local/bin/gh-print-auth-url")+`"`) {
		t.Errorf("login env %v", login.Env)
	}

	keygen := w.mustCall("ssh-keygen")
	key := filepath.Join(w.home, ".ssh", "github_oss")
	if want := "ssh-keygen -q -t ed25519 -C bob@github -f " + key; keygen.Line() != want {
		t.Errorf("keygen %q", keygen.Line())
	}
	for _, kv := range []string{"SSH_ASKPASS=/cache/gh-account-setup", "SSH_ASKPASS_REQUIRE=force", "GH_ACCOUNT_SETUP_ASKPASS=keygen"} {
		if !hasEnv(keygen, kv) {
			t.Errorf("keygen lacks %s: %v", kv, keygen.Env)
		}
	}
	if !hasSecret(keygen, askpassSecretVar, "vault pw") {
		t.Error("keygen has no passphrase secret")
	}
	fi, err := os.Stat(key)
	if err != nil || fi.Mode().Perm() != 0o600 {
		t.Errorf("key mode: %v %v", fi, err)
	}

	add := w.mustCall("gh ssh-key add")
	if add.Line() != "gh ssh-key add "+key+".pub --title desk-oss --type authentication" {
		t.Errorf("upload %q", add.Line())
	}

	// The login made bob active: alice is put back.
	if w.active != "alice" {
		t.Errorf("active %q", w.active)
	}
	w.mustContain("stderr", "gh is back on alice")
}

// E20, E21: the SSH test is isolated and gets the passphrase through askpass only.
func TestTheSSHTestUsesTheKeyItself(t *testing.T) {
	w := newWorld(t)
	w.config("work:alice")
	w.ready("work", "alice")
	if code := w.run("--setup-all"); code != term.ExitOK {
		t.Fatalf("exit %d\n%s", code, w.stderr.String())
	}
	ssh := w.mustCall("ssh ")
	key := filepath.Join(w.home, ".ssh", "github_work")
	want := "ssh -F /dev/null -o IdentityAgent=none -o IdentitiesOnly=yes -o ConnectTimeout=10 " +
		"-o StrictHostKeyChecking=accept-new -o PreferredAuthentications=publickey " +
		"-o PasswordAuthentication=no -o KbdInteractiveAuthentication=no -i " + key + " -T git@github.com"
	if ssh.Line() != want {
		t.Errorf("ssh\n got %q\nwant %q", ssh.Line(), want)
	}
	if !hasEnv(ssh, "GH_ACCOUNT_SETUP_ASKPASS=ssh") || !hasSecret(ssh, askpassSecretVar, "vault pw") {
		t.Errorf("ssh env %v secrets %v", ssh.Env, ssh.Secrets)
	}
	if ssh.Stdin != nil {
		t.Error("ssh stdin is not /dev/null")
	}
	for _, c := range w.fake.Calls() {
		if strings.Contains(c.Line(), "vault pw") {
			t.Errorf("passphrase in argv: %s", c.Line())
		}
	}
	if entries, err := os.ReadDir(filepath.Join(w.home, ".ssh")); err != nil || len(entries) != 2 {
		t.Errorf("a key copy was left: %v %v", entries, err)
	}
}

// E11: the passphrase from run.bash is used, and ansible is not.
func TestThePassphraseFromTheEnvironment(t *testing.T) {
	w := newWorld(t)
	w.config("work:alice")
	w.ready("work", "alice")
	w.env["GITHUB_SSH_PASSPHRASE"] = "from env"
	if code := w.run("--setup-all"); code != term.ExitOK {
		t.Fatalf("exit %d\n%s", code, w.stderr.String())
	}
	if w.ran("ansible") {
		t.Error("decrypted the vault although the passphrase was given")
	}
	if _, set := w.env["GITHUB_SSH_PASSPHRASE"]; set {
		t.Error("the passphrase was left in the environment")
	}
	if !hasSecret(w.mustCall("ssh "), askpassSecretVar, "from env") {
		t.Error("ssh did not get the given passphrase")
	}
}

// E11: the passphrase leaves the environment before any program starts, so gh's version and
// status calls and the scope helper never inherit it, in every mode.
func TestThePassphraseIsTakenBeforeAnyProgramRuns(t *testing.T) {
	for _, mode := range []string{"--check", "--setup-all", "--add=oss:bob"} {
		w := newWorld(t)
		w.config("work:alice")
		w.ready("work", "alice")
		w.env["GITHUB_SSH_PASSPHRASE"] = "from env"
		w.env["RUN_BASH_HEADLESS"] = "1"
		w.run(mode)
		after, taken := w.takenAfter["GITHUB_SSH_PASSPHRASE"]
		if !taken {
			t.Errorf("%s: the passphrase was never taken", mode)
		} else if after != 0 {
			t.Errorf("%s: %d command(s) ran with the passphrase in the environment", mode, after)
		}
	}
}

// E11: the vault call.
func TestThePassphraseFromTheVault(t *testing.T) {
	w := newWorld(t)
	w.config("work:alice")
	w.ready("work", "alice")
	if code := w.run("--setup-all"); code != term.ExitOK {
		t.Fatalf("exit %d\n%s", code, w.stderr.String())
	}
	c := w.mustCall("ansible")
	if want := "ansible localhost -c local -e @" + w.yml + " -m debug -a msg={{ github_ssh_passphrase }}"; c.Line() != want {
		t.Errorf("ansible %q", c.Line())
	}
	if c.Dir != w.root {
		t.Errorf("dir %q", c.Dir)
	}
	for _, kv := range []string{"ANSIBLE_STDOUT_CALLBACK=ansible.builtin.minimal", "ANSIBLE_NOCOLOR=1", "ANSIBLE_CONFIG=" + filepath.Join(w.root, "ansible.cfg")} {
		if !hasEnv(c, kv) {
			t.Errorf("ansible lacks %s: %v", kv, c.Env)
		}
	}
	if strings.Contains(w.stderr.String()+w.stdout.String(), "vault pw") {
		t.Error("the passphrase was printed")
	}
}

// E4, E11: relative LOCALHOST_YML, SCOPES_FILE and VAULT_PASS_FILE mean the current
// directory, for this command and for ansible, which runs in the checkout.
func TestRelativePathsAreTheCurrentDirectorys(t *testing.T) {
	w := newWorld(t)
	w.config("work:alice")
	w.ready("work", "alice")
	cwd := filepath.Dir(filepath.Dir(w.yml))
	t.Chdir(cwd)
	w.write(filepath.Join(cwd, "pw-file"), "pw\n")
	w.env["LOCALHOST_YML"] = filepath.Join("host_vars", filepath.Base(w.yml))
	w.env["VAULT_PASS_FILE"] = "pw-file"
	w.env["SCOPES_FILE"] = "scopes.yml"
	if code := w.run("--setup-all"); code != term.ExitOK {
		t.Fatalf("exit %d\n%s", code, w.stderr.String())
	}
	if h := w.mustCall("python3"); argAfter(h, "--scopes-file") != filepath.Join(cwd, "scopes.yml") {
		t.Errorf("the scope helper was not given the scope file's absolute path: %q", h.Line())
	}
	c := w.mustCall("ansible")
	if !slices.Contains(c.Args, "@"+w.yml) {
		t.Errorf("ansible was not given the configuration's absolute path: %q", c.Line())
	}
	if !hasEnv(c, "ANSIBLE_VAULT_PASSWORD_FILE="+filepath.Join(cwd, "pw-file")) {
		t.Errorf("ansible was not given the password file's absolute path: %v", c.Env)
	}
}

// E11: a password file named by VAULT_PASS_FILE must exist and is the one ansible uses.
func TestAGivenVaultPasswordFile(t *testing.T) {
	w := newWorld(t)
	w.config("work:alice")
	w.ready("work", "alice")
	given := filepath.Join(w.root, "elsewhere", "vault-password")
	w.env["VAULT_PASS_FILE"] = given
	if code := w.run("--setup-all"); code != term.ExitFailure {
		t.Fatalf("missing file: exit %d", code)
	}
	w.mustContain("stderr", "Vault password file not found: "+given)
	if w.ran("ansible") {
		t.Error("ran ansible without the file")
	}
	// --check goes on without a passphrase it could not decrypt (E12), not past a wrong path.
	if code := w.run("--check"); code != term.ExitFailure {
		t.Fatalf("--check, missing file: exit %d", code)
	}
	w.write(given, "pw\n")
	if code := w.run("--setup-all"); code != term.ExitOK {
		t.Fatalf("exit %d\n%s", code, w.stderr.String())
	}
	if c := w.mustCall("ansible"); !hasEnv(c, "ANSIBLE_VAULT_PASSWORD_FILE="+given) {
		t.Errorf("ansible env %v", c.Env)
	}
}

// E11, E12.
func TestADecryptThatFailsStopsSetup(t *testing.T) {
	for name, set := range map[string]func(*world){
		"ansible fails": func(w *world) { w.vaultFails = true },
		"empty value":   func(w *world) { w.vaultOut = "localhost | SUCCESS => {\"msg\": \"\"}" },
		"other shape":   func(w *world) { w.vaultOut = "localhost | FAILED! nothing" },
	} {
		w := newWorld(t)
		w.config("work:alice")
		w.ready("work", "alice")
		set(w)
		if code := w.run("--setup-all"); code != term.ExitFailure {
			t.Fatalf("%s: exit %d", name, code)
		}
		w.mustContain("stderr", "Failed to decrypt github_ssh_passphrase from the vault")
		if w.ran("ssh") {
			t.Errorf("%s: went on to the SSH test", name)
		}
	}
}

// E24, headless: every account audited first, one failure naming every problem.
func TestHeadlessSetupAllAuditsEveryAccountFirst(t *testing.T) {
	w := newWorld(t)
	w.config("a:alice", "b:bob", "c:carol")
	w.logIn("alice")
	w.logIn("bob").missing = "beta,gamma"
	w.env["RUN_BASH_HEADLESS"] = "true"
	if code := w.run("--setup-all"); code != term.ExitFailure {
		t.Fatalf("exit %d", code)
	}
	w.mustContain("stderr", "MISSING bob beta,gamma")
	w.mustContain("stderr", "NOT-AUTHENTICATED carol")
	w.mustContain("stderr", "vars/github-required-scopes.yml")
	if strings.Contains(w.stderr.String(), "alice") {
		t.Errorf("a healthy account is named:\n%s", w.stderr.String())
	}
	c := w.mustCall("python3 -m helpers.github_scopes.cli audit")
	if c.Line() != "python3 -m helpers.github_scopes.cli audit --user alice --user bob --user carol --scopes-file "+filepath.Join(w.root, "vars/github-required-scopes.yml") || c.Dir != w.root {
		t.Errorf("audit %q in %q", c.Line(), c.Dir)
	}
	for _, p := range []string{"ansible", "FG ", "ssh", "gh auth switch"} {
		if w.ran(p) {
			t.Errorf("ran %s before failing", p)
		}
	}
}

// E15 headless: never a browser flow; a healthy headless run goes on.
func TestHeadlessNeverStartsABrowserFlow(t *testing.T) {
	w := newWorld(t)
	w.config("work:alice")
	w.env["RUN_BASH_HEADLESS"] = "true"
	w.env["GITHUB_SSH_PASSPHRASE"] = "pw"
	if code := w.run("--add=work:alice"); code != term.ExitFailure {
		t.Fatalf("exit %d", code)
	}
	w.mustContain("stderr", "RUN_BASH_GITHUB_TOKEN_FILE")
	if w.ran("FG ") {
		t.Error("started a browser flow headless")
	}

	w = newWorld(t)
	w.config("work:alice")
	w.ready("work", "alice")
	w.accounts["alice"].missing = "beta"
	w.env["RUN_BASH_HEADLESS"] = "true"
	w.env["GITHUB_SSH_PASSPHRASE"] = "pw"
	if code := w.run("--add=work:alice"); code != term.ExitFailure {
		t.Fatalf("headless missing scopes: exit %d", code)
	}
	if w.ran("FG ") {
		t.Error("refreshed headless")
	}

	w = newWorld(t)
	w.config("work:alice")
	w.ready("work", "alice")
	w.env["RUN_BASH_HEADLESS"] = "true"
	if code := w.run("--setup-all"); code != term.ExitOK {
		t.Fatalf("healthy headless: exit %d\n%s", code, w.stderr.String())
	}
}

// E15: the login authorised another account, or failed.
func TestALoginAsTheWrongAccountStops(t *testing.T) {
	w := newWorld(t)
	w.config("oss:bob")
	w.loginAs = "mallory"
	if code := w.run("--setup-all"); code != term.ExitFailure {
		t.Fatalf("exit %d", code)
	}
	w.mustContain("stderr", "Authenticated as 'mallory' but expected 'bob'")
	w.mustContain("stderr", "gh auth logout --hostname github.com --user mallory")
	if w.ran("ssh-keygen") {
		t.Error("went on")
	}

	w = newWorld(t)
	w.config("oss:bob")
	w.loginFails = true
	if code := w.run("--setup-all"); code != term.ExitFailure {
		t.Fatalf("failed login: exit %d", code)
	}
	w.mustContain("stderr", "Authentication failed for bob")
}

// E6, B8: logins compare without case.
func TestLoginsCompareWithoutCase(t *testing.T) {
	w := newWorld(t)
	w.config("work:Alice")
	w.ready("work", "alice")
	if code := w.run("--setup-all"); code != term.ExitOK {
		t.Fatalf("exit %d\n%s", code, w.stderr.String())
	}
	if w.ran("FG ") {
		t.Error("logged in again an account gh holds in other case")
	}
}

// E16, E19: one refresh for every missing scope, with the account made active first.
func TestMissingScopesAreAskedForOnce(t *testing.T) {
	w := newWorld(t)
	w.config("oss:bob")
	w.ready("oss", "bob")
	w.logIn("alice")
	w.active = "alice"
	missing := "beta,gamma"
	w.accounts["bob"].missing = missing
	if code := w.run("--setup-all"); code != term.ExitOK {
		t.Fatalf("exit %d\n%s", code, w.stderr.String())
	}
	if refresh := w.mustForeground("gh auth refresh"); refresh.Line() != "gh auth refresh --hostname github.com --scopes "+missing {
		t.Fatalf("refresh %q", refresh.Line())
	}
	if len(w.fake.Foregrounds()) != 1 {
		t.Errorf("foregrounds %v", w.lines())
	}
	w.mustContain("stderr", "⚠ Missing scopes: beta,gamma")
	w.mustContain("stderr", "✓ Scopes updated: bob")
	if !w.ran("gh auth switch --hostname github.com --user bob") {
		t.Error("refreshed without making bob active")
	}
	if w.active != "alice" {
		t.Errorf("active %q", w.active)
	}
}

func TestARefreshThatLeavesScopesMissingStops(t *testing.T) {
	w := newWorld(t)
	w.config("oss:bob")
	w.ready("oss", "bob")
	w.active = "bob"
	w.accounts["bob"].missing = "beta,gamma"
	w.accounts["bob"].afterRefresh = "gamma"
	if code := w.run("--setup-all"); code != term.ExitFailure {
		t.Fatalf("exit %d", code)
	}
	w.mustContain("stderr", "still left these scopes missing: gamma")
	if w.ran("gh auth switch") {
		t.Error("switched although bob was active")
	}

	w = newWorld(t)
	w.config("oss:bob")
	w.ready("oss", "bob")
	w.accounts["bob"].missing = "beta"
	w.refreshFail = true
	if code := w.run("--setup-all"); code != term.ExitFailure {
		t.Fatalf("failed refresh: exit %d", code)
	}
	w.mustContain("stderr", "Failed to update scopes for bob")
}

// E16: an account the helper cannot read stops the run.
func TestUnreadableScopesStop(t *testing.T) {
	w := newWorld(t)
	w.config("oss:bob")
	w.ready("oss", "bob")
	w.accounts["bob"].unreadable = true
	if code := w.run("--setup-all"); code != term.ExitFailure {
		t.Fatalf("exit %d", code)
	}
	w.mustContain("stderr", "HTTP 401")
}

// E19, B4: a switch that fails is an error.
func TestASwitchThatFailsStops(t *testing.T) {
	w := newWorld(t)
	w.config("oss:bob")
	w.ready("oss", "bob")
	w.logIn("alice")
	w.active = "alice"
	w.accounts["bob"].missing = "beta"
	w.switchFails = true
	if code := w.run("--setup-all"); code != term.ExitFailure {
		t.Fatalf("exit %d", code)
	}
	w.mustContain("stderr", "switch refused")
	if w.ran("FG gh auth refresh") {
		t.Error("refreshed the wrong account")
	}
}

// E17: a key without its .pub, and a missing ~/.ssh.
func TestKeyFiles(t *testing.T) {
	w := newWorld(t)
	w.config("work:alice")
	w.logIn("alice")
	key := w.key("work", "alice")
	if err := os.Remove(key + ".pub"); err != nil {
		t.Fatal(err)
	}
	if code := w.run("--setup-all"); code != term.ExitFailure {
		t.Fatalf("exit %d", code)
	}
	w.mustContain("stderr", "ssh-keygen -y -f "+key)

	w = newWorld(t)
	w.config("work:alice")
	w.logIn("alice")
	if code := w.run("--setup-all"); code != term.ExitOK {
		t.Fatalf("exit %d\n%s", code, w.stderr.String())
	}
	fi, err := os.Stat(filepath.Join(w.home, ".ssh"))
	if err != nil || fi.Mode().Perm() != 0o700 {
		t.Errorf("~/.ssh: %v %v", fi, err)
	}
}

// E18, B5: the key listing is read as the account, and a failed listing is not "unregistered".
func TestKeyRegistration(t *testing.T) {
	w := newWorld(t)
	w.config("work:alice")
	w.ready("work", "alice")
	w.keysFail = true
	if code := w.run("--setup-all"); code != term.ExitFailure {
		t.Fatalf("exit %d", code)
	}
	if w.ran("gh ssh-key add") {
		t.Error("uploaded after a failed listing")
	}

	w = newWorld(t)
	w.config("work:alice")
	w.logIn("alice")
	w.key("work", "alice")
	w.addFails = true
	if code := w.run("--setup-all"); code != term.ExitFailure {
		t.Fatalf("failed upload: exit %d", code)
	}
	w.mustContain("stderr", "Key upload failed for alice")

	w = newWorld(t)
	w.config("work:alice")
	w.ready("work", "alice")
	if code := w.run("--setup-all"); code != term.ExitOK {
		t.Fatalf("registered: exit %d", code)
	}
	w.mustContain("stderr", "✓ Key registered on GitHub: alice")
	if w.ran("gh ssh-key add") {
		t.Error("uploaded a registered key")
	}
	if c := w.mustCall("gh api user/keys"); c.Line() != "gh api user/keys --paginate --jq .[].key" {
		t.Errorf("listing %q", c.Line())
	}
}

// E20: a key GitHub greets as another account stops setup; one it does not greet does not.
func TestSSHVerdicts(t *testing.T) {
	w := newWorld(t)
	w.config("work:alice", "oss:bob")
	w.ready("work", "alice")
	w.sshAs[filepath.Join(w.home, ".ssh", "github_work")] = "bob"
	w.ready("oss", "bob")
	if code := w.run("--setup-all"); code != term.ExitFailure {
		t.Fatalf("exit %d", code)
	}
	w.mustContain("stderr", "SSH key github_work authenticates as 'bob', expected 'alice'")
	if strings.Contains(w.stderr.String(), "━━━ oss") {
		t.Error("went on to the next account")
	}

	w = newWorld(t)
	w.config("work:alice")
	w.ready("work", "alice")
	w.sshAs[filepath.Join(w.home, ".ssh", "github_work")] = ""
	if code := w.run("--setup-all"); code != term.ExitOK {
		t.Fatalf("inconclusive: exit %d", code)
	}
	w.mustContain("stderr", "⚠ SSH test inconclusive for work")
	w.mustContain("stderr", "ssh -F /dev/null -o IdentityAgent=none -i ~/.ssh/github_work -T git@github.com")
}

// E14.
func TestTheBrowserHelper(t *testing.T) {
	w := newWorld(t)
	w.config("oss:bob")
	w.loginAs = "bob"
	w.write(w.helper, "#!/bin/bash\n")
	if code := w.run("--setup-all"); code != term.ExitOK {
		t.Fatalf("exit %d\n%s", code, w.stderr.String())
	}
	if login := w.mustForeground("gh auth login"); !hasEnv(login, `GH_BROWSER=bash "`+w.helper+`"`) {
		t.Errorf("env %v", login.Env)
	}

	w = newWorld(t)
	w.config("oss:bob")
	if err := os.Remove(filepath.Join(w.root, "files/usr/local/bin/gh-print-auth-url")); err != nil {
		t.Fatal(err)
	}
	if code := w.run("--setup-all"); code != term.ExitFailure {
		t.Fatalf("no helper: exit %d", code)
	}
	w.mustContain("stderr", "gh-print-auth-url")

	w = newWorld(t)
	w.config("oss:bob")
	w.helper = filepath.Join(t.TempDir(), `a"b`)
	w.write(w.helper, "#!/bin/bash\n")
	if code := w.run("--setup-all"); code != term.ExitFailure {
		t.Fatalf("quoted helper: exit %d", code)
	}
}

// E22, E23.
func TestAddANewAccount(t *testing.T) {
	w := newWorld(t)
	w.config("work:alice")
	w.ready("work", "alice")
	w.loginAs = "jane"
	if code := w.run("--add=oss:jane"); code != term.ExitOK {
		t.Fatalf("exit %d\n%s", code, w.stderr.String())
	}
	data, err := os.ReadFile(w.yml)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "  work: \"alice\"\n  oss: \"jane\"\n") {
		t.Errorf("config %q", data)
	}
	w.mustContain("stderr", "━━━ oss (jane) ━━━")
	w.mustContain("stderr", "Done! Account oss is ready.")
	w.mustContain("stderr", filepath.Join(w.root, "playbooks/imports/play-github-cli-multi.yml"))
	if strings.Contains(w.stderr.String(), "━━━ work") {
		t.Error("--add set up another account")
	}
}

func TestAddAnAccountAlreadyConfigured(t *testing.T) {
	w := newWorld(t)
	w.config("work:alice")
	w.ready("work", "alice")
	if code := w.run("--add=work:ALICE"); code != term.ExitOK {
		t.Fatalf("exit %d\n%s", code, w.stderr.String())
	}
	w.mustContain("stderr", "Account 'work' is already in "+w.yml)

	w = newWorld(t)
	w.config("work:alice")
	before, err := os.ReadFile(w.yml)
	if err != nil {
		t.Fatal(err)
	}
	if code := w.run("--add=work:bob"); code != term.ExitFailure {
		t.Fatalf("other username: exit %d", code)
	}
	w.mustContain("stderr", "alice")
	if w.ran("ansible") || w.ran("FG ") {
		t.Error("went on")
	}
	after, err := os.ReadFile(w.yml)
	if err != nil || string(after) != string(before) {
		t.Errorf("config changed: %q %v", after, err)
	}
}

// B10: the file changes only once the passphrase is in hand.
func TestAddLeavesTheConfigWhenThePassphraseFails(t *testing.T) {
	w := newWorld(t)
	w.config("work:alice")
	w.vaultFails = true
	before, err := os.ReadFile(w.yml)
	if err != nil {
		t.Fatal(err)
	}
	if code := w.run("--add=oss:jane"); code != term.ExitFailure {
		t.Fatalf("exit %d", code)
	}
	after, err := os.ReadFile(w.yml)
	if err != nil || string(after) != string(before) {
		t.Errorf("config changed: %q %v", after, err)
	}
}

// E25: read only, report on stdout.
func TestCheckReportsAndChangesNothing(t *testing.T) {
	w := newWorld(t)
	w.config("work:alice", "oss:bob")
	w.ready("work", "alice")
	w.ready("oss", "bob")
	w.active = "alice"
	if code := w.run("--check"); code != term.ExitOK {
		t.Fatalf("exit %d\n%s%s", code, w.stdout.String(), w.stderr.String())
	}
	for _, s := range []string{
		"Checking 2 GitHub account(s)", "━━━ oss (bob) ━━━", "✓ Auth: OK", "✓ Scopes: OK",
		"✓ SSH key: exists", "✓ GitHub key: registered", "✓ SSH verified: oss → bob", "✓ Overall: healthy", "✓ All accounts healthy",
	} {
		w.mustContain("stdout", s)
	}
	for _, p := range []string{"gh auth switch", "FG ", "ssh-keygen", "gh ssh-key add"} {
		if w.ran(p) {
			t.Errorf("--check ran %s", p)
		}
	}
}

func TestCheckFindsEveryIssueAndGoesOn(t *testing.T) {
	w := newWorld(t)
	w.config("work:alice", "oss:bob", "x:carol")
	w.logIn("alice").missing = "beta"
	w.key("work", "alice")
	w.ready("oss", "bob")
	w.keysFail = true
	if code := w.run("--check"); code != term.ExitFailure {
		t.Fatalf("exit %d", code)
	}
	for _, s := range []string{
		"✗ Scopes: missing beta", "✗ GitHub key: could not be read", "✗ Auth: not authenticated as carol",
		"✗ SSH key: missing (~/.ssh/github_x)", "✗ Overall: issues found",
	} {
		w.mustContain("stdout", s)
	}
	w.mustContain("stdout", "✗ Some accounts have issues — run with --setup-all to fix")
}

// E12: --check without a passphrase warns, and tests the key in batch mode.
func TestCheckWithoutAPassphrase(t *testing.T) {
	w := newWorld(t)
	w.config("work:alice")
	w.ready("work", "alice")
	w.vaultFails = true
	if code := w.run("--check"); code != term.ExitOK {
		t.Fatalf("exit %d\n%s", code, w.stderr.String())
	}
	w.mustContain("stderr", "Could not decrypt the passphrase — SSH tests may be limited")
	w.mustContain("stderr", "⚠ Failed to decrypt github_ssh_passphrase from the vault")
	if strings.Contains(w.stderr.String(), "✗") {
		t.Errorf("a run without a passphrase may only warn:\n%s", w.stderr.String())
	}
	ssh := w.mustCall("ssh ")
	if !slices.Contains(ssh.Args, "BatchMode=yes") || len(ssh.Secrets) != 0 || hasEnv(ssh, "SSH_ASKPASS_REQUIRE=force") {
		t.Errorf("ssh %q env %v", ssh.Line(), ssh.Env)
	}
}

// E26.
func TestTheActiveAccountIsPutBack(t *testing.T) {
	w := newWorld(t)
	w.config("oss:bob")
	w.logIn("alice")
	w.active = "alice"
	w.loginAs = "bob"
	w.addFails = true // the run fails after the login made bob active
	if code := w.run("--setup-all"); code != term.ExitFailure {
		t.Fatalf("exit %d", code)
	}
	if w.active != "alice" {
		t.Errorf("active %q after a failed run", w.active)
	}

	w = newWorld(t)
	w.config("oss:bob")
	w.loginAs = "bob"
	if code := w.run("--setup-all"); code != term.ExitOK {
		t.Fatalf("no account at the start: exit %d\n%s", code, w.stderr.String())
	}
	if w.ran("gh auth switch") {
		t.Error("switched back to nothing")
	}
}

func TestARestoreThatFailsFailsTheRun(t *testing.T) {
	w := newWorld(t)
	w.config("oss:bob")
	w.logIn("alice")
	w.active = "alice"
	w.loginAs = "bob"
	w.fake.OnFunc([]string{"gh", "auth", "switch"}, func(execx.Cmd) (execx.Result, error) {
		return execx.Result{Stderr: []byte("keyring locked"), ExitCode: 1}, nil
	})
	if code := w.run("--setup-all"); code != term.ExitFailure {
		t.Fatalf("exit %d", code)
	}
	w.mustContain("stderr", "Run: gh auth switch --hostname github.com --user alice")
}

// E27: a signal ends the run after the step under way, the account is put back, 128+n.
func TestASignalEndsTheRunAndPutsTheAccountBack(t *testing.T) {
	w := newWorld(t)
	w.config("oss:bob")
	w.logIn("alice")
	w.active = "alice"
	w.loginAs = "bob"
	ctx, cancel := context.WithCancelCause(context.Background())
	defer cancel(nil)
	w.fake.OnFunc([]string{"gh", "auth", "login"}, func(execx.Cmd) (execx.Result, error) {
		w.logIn("bob")
		w.active = "bob"
		cancel(&execx.SignalError{Signal: syscall.SIGINT})
		return execx.Result{ExitCode: 130}, nil
	})
	if code := w.runCtx(ctx, "--setup-all"); code != 130 {
		t.Fatalf("exit %d", code)
	}
	if w.active != "alice" {
		t.Errorf("active %q", w.active)
	}
	if w.ran("ssh-keygen") {
		t.Error("went on after the signal")
	}
}

// E28.
func TestColourOnlyOnATerminalWithoutNoColor(t *testing.T) {
	w := newWorld(t)
	w.config("work:alice")
	w.ready("work", "alice")
	env := w.newEnv()
	env.StderrIsTerminal = true
	if code := (App{Env: env}).Run(context.Background(), []string{"--setup-all"}); code != term.ExitOK {
		t.Fatalf("exit %d", code)
	}
	if !strings.Contains(w.stderr.String(), "\x1b[") {
		t.Error("no colour on a terminal")
	}
	w.stderr.Reset()
	w.env["NO_COLOR"] = "1"
	env = w.newEnv()
	env.StderrIsTerminal = true
	if code := (App{Env: env}).Run(context.Background(), []string{"--setup-all"}); code != term.ExitOK {
		t.Fatalf("exit %d", code)
	}
	if strings.Contains(w.stderr.String(), "\x1b[") {
		t.Error("colour with NO_COLOR")
	}
}

func TestAWriteThatFailsIsAFailure(t *testing.T) {
	w := newWorld(t)
	w.config("work:alice")
	w.ready("work", "alice")
	env := w.newEnv()
	env.Stdout = failingWriter{}
	if code := (App{Env: env}).Run(context.Background(), []string{"--check"}); code != term.ExitFailure {
		t.Fatalf("exit %d", code)
	}
	env = w.newEnv()
	env.Stdout = failingWriter{}
	if code := (App{Env: env}).Run(context.Background(), []string{"--help"}); code != term.ExitFailure {
		t.Fatalf("help: exit %d", code)
	}
}
