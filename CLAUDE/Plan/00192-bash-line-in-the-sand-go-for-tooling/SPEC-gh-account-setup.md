# gh-account-setup: specification of the Go port

Programme P6 of [INVENTORY.md](INVENTORY.md) ("Interactive account/setup tools"), its third tool
after [`open`](SPEC-open.md) and [`rclone-cache-status`](SPEC-rclone-cache-status.md):
`gh-account-setup`, which logs each configured GitHub account into `gh` with the required OAuth
scopes, makes and uploads its SSH key, and proves the key logs in as that account. **Method: the
legacy bash is not an oracle.** This document states the intended behaviour as numbered, testable
expectations (`E*`). The bash (`scripts/gh-account-setup.bash`, 546 code lines, the `data` flag,
at `58d531ed`) was read only to find the requirements; where it was wrong, the expectation states
the correct behaviour and section 5 lists the departure (`B*`). The proof is the Go tests in
`src-go/internal/ghaccount` (every flow over `execx.Fake` and temporary directories, the askpass
answer against the real `ssh-keygen`) and `src-go/cmd/gh-account-setup`, and the black-box cases of
`scripts/test-run-bash-gh-scopes.bash`, which build and drive the command through its wrapper. What
only the host with real GitHub accounts can prove is section 7.

## 1. Why this one next among P6

P6 lists `gh-account-setup.bash` first, and it is the largest P6 tool left after `open` and
`rclone-cache-status`. The others left are `setup-rclone`, `nord`, the `ssh-with-key` family,
`reclaim` (root and deletions), `nvidia-status` (hardware) and `rclone-cache-warm`. This one has the
most to gain from Go: three embedded Python programs (the `data` flag), a hand-rolled YAML line
editor, and a passphrase handed to `ssh-keygen` in argv, which the Go rule "secrets never in argv"
(`src-go/CLAUDE.md`) forbids and `execx` refuses. It handles credentials, so the whole flow is
pinned over fakes and the passphrase path is proven against the real `ssh-keygen`; the GitHub side
is the owner's host check (section 7). It had a white-box suite (functions extracted from the bash
with awk); those cases move to Go tests, and the suite drives the built command instead (D4).

## 2. Scope decisions

- **D1. Run from the checkout, built on each run.** It is not installed by a play: `run.bash` runs
  it before any play (to log the accounts in), and the playbook's scope failure names it as the
  remedy. `scripts/gh-account-setup.bash` stays, as a build-and-run wrapper like the QA gates':
  `scripts/lib/go-tool.bash` gains `run_go_command`, which builds `src-go/cmd/<name>` from the main
  module (`go build -trimpath -buildvcs=false`, as `tasks/build-go-command.yml` builds, so a `git`
  that is not git on `PATH`, such as a test stub, cannot break the build; `GOFLAGS=-mod=vendor`, `GOTOOLCHAIN=local`, so offline from
  `src-go/vendor`) into a private directory under the cache, runs it, removes the directory and
  returns its status. The wrapper gives the binary the checkout as `GH_ACCOUNT_SETUP_ROOT`. Its
  allowlist row leaves `scripts/bash-limits.allowlist`; `scripts/go-binary-suites.tsv` gets
  `gh-account-setup	scripts/test-run-bash-gh-scopes.bash`.
- **D2. Go before the GitHub step (the bootstrap).** On a fresh host nothing has installed Go when
  `run.bash` reaches "Setting Up GitHub Multi-Account Access". `run.bash` adds `golang` to the
  system dependencies it already installs with dnf at the start (git, python3, jq...), in the same
  block as the GitHub step, so every run that reaches the step has the toolchain. That is the
  smallest answer to INVENTORY P4's open question (how Go gets on a fresh host) for this one tool,
  and `play-golang.yml` still owns the version floor: it installs the same package at Fedora's
  newest build and asserts `golang_min_version`. A Fedora golang older than `src-go/go.mod`'s `go`
  line fails the build loudly (`GOTOOLCHAIN=local` never downloads another toolchain).
- **D3. External programs stay the tool.** `gh` does the GitHub work, `ssh-keygen` makes the key,
  `ssh` tests it, `ansible` decrypts the vaulted passphrase, and `helpers/github_scopes` stays the
  one judge of the scopes (its `required` and `audit` commands, run as
  `python3 -m helpers.github_scopes.cli` from the checkout), so `run.bash`, this tool and
  `play-github-cli-multi.yml` still ask the same code. Done in Go: the YAML read of
  `localhost.yml` (`go.yaml.in/yaml/v3`, newly allowlisted for this module, the version the `qa`
  module already uses), its edit, the JSON of `gh auth status`, the vault output and the key
  comparison. No embedded Python is left.
- **D4. The suite.** `scripts/test-run-bash-gh-scopes.bash` keeps its `run.bash` cases. Its
  `gh-account-setup.bash` cases extracted bash functions (`audit_all_accounts_headless`,
  `restore_active_account`) and read the bash's traps; they are Go tests now (E24, E26), and the
  suite instead builds and runs the wrapper over its `gh` stub (headless audit of every account,
  the active account put back), so the wiring from the wrapper to the binary is proven end to end.
- **D5. One seam for processes and secrets.** Every program runs through `internal/execx`; the
  login and the scope refresh in the foreground (`execx.Foregrounder`, the terminal's own stdin
  and Ctrl-C); the passphrase and each account's token only as `execx.Secret`s in one child's
  environment.

## 3. Expectations

### Command line

- E1. `gh-account-setup --add=ALIAS:USERNAME | --setup-all | --check`. `-h`/`--help` anywhere
  prints the usage on stdout and exits 0, whatever else the line holds; no argument prints the
  usage and exits 0. Exactly one mode. The whole command line is read before anything runs.
- E2. An unknown argument, a second mode, `--add=` without a `:`, an empty alias or username, or
  one that fails E3 is a usage error: `gh-account-setup: <text> — see 'gh-account-setup --help'.`
  on stderr, exit 64.
- E3. An alias is `[A-Za-z0-9][A-Za-z0-9_-]*` (it is a file name, `~/.ssh/github_<alias>`, an SSH
  host `github.com-<alias>` and a shell function name in the play). A username is a GitHub login:
  1 to 39 letters, digits and hyphens, starting and ending with a letter or digit.
  Aliases, like logins, compare without regard to case: ssh matches `Host` names without case,
  so `Work` and `work` would be one host alias, and an alias given twice that way is refused.
- E4. `GH_ACCOUNT_SETUP_ROOT` (set by the wrapper) is the checkout, an absolute directory; unset,
  relative or not a directory is exit 1 telling the user to run `scripts/gh-account-setup.bash`.
  The configuration is `LOCALHOST_YML` (default `<root>/environment/localhost/host_vars/localhost.yml`),
  the scope list `SCOPES_FILE` (default `<root>/vars/github-required-scopes.yml`).
  `RUN_BASH_HEADLESS=true` is a headless run. The vault password file has no default here:
  `<root>/ansible.cfg` names it (`vault_password_file`, with `vault_identity`), and E11 runs
  ansible under that file. `VAULT_PASS_FILE`, when set, overrides it (E11).

### Preflight

- E5. `gh` not on `PATH` is `GitHub CLI (gh) is not installed`, exit 1. The first `x.y.z` in
  `gh --version` must be 2.81.0 or newer, the first gh whose `auth status` takes `--json` (E6;
  B15); older, or no version found, is exit 1 naming what was found and the floor.
- E6. The accounts gh holds are `gh auth status --hostname github.com --json hosts` (which exits 0
  whatever the tokens' state). A non-zero exit, or stdout that is not a JSON object, is exit 1
  naming gh's output. An account is logged in when its entry's `state` is `success`; the active
  account is the logged-in entry with `active: true`, or none. Logins are compared without regard
  to case everywhere (GitHub's are case-insensitive).
- E6a. The active account at the start is recorded before any mode runs (E26).

### Configuration

- E7. `localhost.yml` (missing is `Config not found: <path>`, exit 1) is read as YAML; tags such as
  `!vault` are data and nothing is decrypted. The top-level `github_accounts` mapping, in file
  order, gives the accounts `alias -> username`; absent, null or `{}` is no account. Exit 1,
  naming the alias, for: a `github_accounts` that is not a mapping, a username that is not a plain
  scalar (null, a `!vault` value, a list), an alias given twice, or an alias or username failing
  E3. A file that is not YAML is exit 1 with the parser's reason.
- E8. `--setup-all` and `--check` with no account: `⚠ No github_accounts entries found in <path>`,
  exit 0.

### Scopes (the helper)

- E9. The required scopes are `python3 -m helpers.github_scopes.cli required --scopes-file <file>`
  run in the checkout, read only when a login needs them. Each account's scopes are
  `... audit --user <login> [--user <login>...] --scopes-file <file>`, which reads each account
  with its own token and switches nothing. A helper that fails, or prints nothing for `required`,
  is exit 1 naming it.
- E10. An audit answer is one line per asked login, in order: `SCOPES-OK <login>`,
  `SCOPES-MISSING <login> <scopes>`, `SCOPES-NOT-AUTHENTICATED <login>` or
  `SCOPES-UNREADABLE <login> <why>`. A line of another form, for a login not asked, or a login
  with no line, is exit 1: the helper's answer is never guessed at.

### The passphrase

- E11. A non-empty `GITHUB_SSH_PASSPHRASE` (set by `run.bash` on a fresh install) is the
  passphrase, taken out of the environment before any program runs, so no child (not even
  `gh --version`) inherits it. Otherwise it is decrypted
  from the vault: `ansible localhost -c local -e @<localhost.yml> -m debug -a 'msg={{ github_ssh_passphrase }}'` runs in the checkout with `ANSIBLE_CONFIG=<root>/ansible.cfg`,
  `ANSIBLE_STDOUT_CALLBACK=ansible.builtin.minimal` and `ANSIBLE_NOCOLOR=1`, and no `--vault-id`
  argument: the password file and the vault identity are the ones `ansible.cfg` names, so this
  tool never names that file. Only when `VAULT_PASS_FILE` is set is that path checked (missing is
  `Vault password file not found: <path>`, exit 1) and given to ansible as
  `ANSIBLE_VAULT_PASSWORD_FILE` (ansible labels it with the configured `vault_identity`). Its
  stdout after the first `=> ` must be a JSON object whose `msg` is a
  non-empty string. Ansible failing (its stderr is shown), output of another shape, or an empty
  value is `Failed to decrypt github_ssh_passphrase from the vault`, exit 1. The passphrase is never
  printed and never in an argv.
- E12. `--add` and `--setup-all` without a passphrase stop (exit 1). `--check` reports the
  vault's failure as a warning, warns `Could not decrypt the passphrase — SSH tests may be limited`
  and goes on without one; a `VAULT_PASS_FILE` that is not there is a wrong setting, not a
  passphrase it could not get, and ends `--check` too (exit 1, E11).

### One account's setup (`--add` and `--setup-all`)

- E13. Each account starts with the heading `━━━ <alias> (<username>) ━━━` and runs E14 to E20 in
  order; the first failure ends the run with exit 1.
- E14. The browser helper is `/usr/local/bin/gh-print-auth-url` (the play's copy), else
  `<root>/files/usr/local/bin/gh-print-auth-url`, else exit 1 naming both. gh runs it as
  `GH_BROWSER=bash "<path>"` (shell-split by gh, so it works whatever its exec bit); a path holding
  `"`, `\`, `$` or a backtick is refused, exit 1. Only the login and the refresh get it.
- E15. Logged in (E6) is `✓ Authenticated: <username>`. Otherwise, headless is exit 1 (the browser
  flow cannot run unattended; the message names `RUN_BASH_GITHUB_TOKEN_FILE`). Otherwise the
  four-step instructions are printed and `gh auth login --hostname github.com --git-protocol ssh --web --skip-ssh-key --scopes <required>` runs in the foreground. A non-zero exit is
  `Authentication failed for <username>`, exit 1. The status is then read again: the active
  account must be `<username>`, logged in; another is `Authenticated as '<login>' but expected '<username>'` with the `gh auth logout` to run, exit 1.
- E16. The account's scopes (E9, E10): OK is `✓ Scopes OK`. Missing: headless is exit 1 naming
  them; otherwise gh is switched to the account (E19), `⚠ Missing scopes: <scopes>` and
  `gh auth refresh --hostname github.com --scopes <scopes>` runs in the foreground (one browser
  flow for every missing scope); a non-zero exit is exit 1; the account is audited again, and
  anything still missing is exit 1 naming it; else `✓ Scopes updated`. Not authenticated or
  unreadable is exit 1.
- E17. The key is `~/.ssh/github_<alias>`. Present is `✓ SSH key exists`, and its `.pub` must
  exist (exit 1 naming the `ssh-keygen -y` that recreates it). Absent: `~/.ssh` is created mode 0700
  when missing, `ssh-keygen -q -t ed25519 -C <username>@github -f <key>` makes it with the
  passphrase through askpass (E21), the key is made mode 0600, and both files must then exist.
- E18. The key is on GitHub when a key listed by `gh api user/keys --paginate --jq '.[].key'`
  has the same second field (the base64 key) as the `.pub`, which must be `<type> <key> [comment]`.
  Both this and the upload act as the account with its own token (`gh auth token --hostname github.com --user <username>`, given as `GH_TOKEN`), not as gh's active account. A listing
  that fails is exit 1, not "not registered". Not there: `gh ssh-key add <key>.pub --title <hostname>-<alias> --type authentication`; failing is exit 1 with the command to run by
  hand; else `✓ Key uploaded`.
- E19. Only the refresh acts on gh's active account. gh is switched
  (`gh auth switch --hostname github.com --user <username>`) only when another account is active,
  and a switch that fails is exit 1, never a warning.
- E20. The SSH test: `ssh -F /dev/null -o IdentityAgent=none -o IdentitiesOnly=yes -o ConnectTimeout=10 -o StrictHostKeyChecking=accept-new -o PreferredAuthentications=publickey -o PasswordAuthentication=no -o KbdInteractiveAuthentication=no -i <key> -T git@github.com`, stdin
  `/dev/null`, at most 15 seconds; with no passphrase also `-o BatchMode=yes`. Its output naming
  `Hi <username>!` is `✓ SSH verified: <alias> → <username>`. Another login is
  `SSH key github_<alias> authenticates as '<login>', expected '<username>'` with the advice to
  delete it from that account: exit 1 in setup. No login (timeout, refusal, no network) is
  `⚠ SSH test inconclusive for <alias>` with the manual command, and setup goes on.
- E21. The passphrase reaches `ssh-keygen` and `ssh` only through askpass, never in argv and never
  as an unencrypted copy of the key: the child gets `SSH_ASKPASS=<this binary>`,
  `SSH_ASKPASS_REQUIRE=force`, `GH_ACCOUNT_SETUP_ASKPASS=<keygen|ssh>` and the passphrase as the
  secret `GH_ACCOUNT_SETUP_PASSPHRASE`. Run so, the binary is the askpass program: for `keygen` a
  prompt containing `passphrase`, for `ssh` one starting `Enter passphrase for`, is answered with
  the passphrase and a newline on stdout, exit 0; any other prompt (a host key question, a
  password), an unknown mode or no passphrase is exit 1 with nothing on stdout.

### `--add`

- E22. The new account is checked against the configuration first: the alias already there with
  the same username is `i Account '<alias>' is already in <path>`, and the file is left alone; with
  another username it is exit 1 naming both, before anything runs. Then the passphrase (E11), then
  the file is changed (E23), then the account is set up (E13), then
  `Done! Account <alias> is ready.` and the play to run, `<root>/playbooks/imports/play-github-cli-multi.yml`.
- E23. The entry is `<indent><alias>: "<username>"`. Under a block `github_accounts:` it goes after
  the block's last entry line (the indent is the first entry's, else two spaces); a
  `github_accounts:` with an empty, `null`, `~` or `{}` value becomes that block with the one entry;
  with no `github_accounts` key the block is appended at the end. The result is read back (E7) and
  must give the old accounts plus the new one, else exit 1 with the file unchanged. The file
  (through a symlink, its target) is replaced atomically with its mode kept.

### `--setup-all`

- E24. `Setting up <n> GitHub account(s)`. Headless, every account is audited first (E9), and any
  not OK fails the run once, before the passphrase and before any account's step, naming every
  account and what it lacks (`MISSING <login> <scopes>`, `NOT-AUTHENTICATED <login>`,
  `UNREADABLE <login> <why>`) and the scope file to satisfy. Then the passphrase, then each account
  in file order (E13), then `All accounts configured!`.

### `--check`

- E25. Read only: nothing logs in, refreshes, switches, makes or uploads. For each account: `Auth`
  (E6); when logged in, `Scopes` (E9); `SSH key` exists; when the `.pub` exists and the account is
  logged in, `GitHub key` registered (E18); when the key exists, the SSH test (E20); then
  `Overall: healthy` or `Overall: issues found`. A check that cannot be made (a helper or gh that
  fails) is an issue line, and the next check runs. The report is stdout. All healthy is
  `✓ All accounts healthy`, exit 0; else `✗ Some accounts have issues — run with --setup-all to fix`,
  exit 1.

### The active account, signals and output

- E26. Plain gh calls act as the active account, which `run.bash` makes the primary one. The
  account active at the start is made active again however the run ends after E6a: when it was
  not empty and another is active now, `gh auth switch --hostname github.com --user <it>`, then
  `i gh is back on <it>, the account active when this started`. A switch that fails names the
  command to run by hand, and a run that would have exited 0 exits 1.
- E27. SIGINT, SIGTERM, SIGHUP and SIGQUIT end the run after the step under way (a foreground login
  or refresh gets the terminal's Ctrl-C itself), E26 runs, and the exit is 128 + the signal.
- E28. Progress and diagnostics are stderr, as `i `, `✓ `, `⚠ ` and `✗ ` lines; the usage and the
  `--check` report are stdout. Colour only on a terminal and with `NO_COLOR` unset or empty. A write
  that fails is exit 1.

### The wrapper and the bootstrap

- E29. `scripts/gh-account-setup.bash` builds and runs the command (D1) and returns its status; no
  `go` is exit 2 naming `play-golang.yml`, a build that fails exit 2.
- E30. `run.bash` installs `golang` with its other system dependencies, before the GitHub step
  (D2).

## 4. Kept as it was

The three modes and their flag spelling, the environment it reads (`LOCALHOST_YML`,
`VAULT_PASS_FILE`, `SCOPES_FILE`, `GITHUB_SSH_PASSPHRASE`, `RUN_BASH_HEADLESS`), the key path and
upload title, the browser helper, the order of the steps, the one-pass scope promise (one refresh
asking for every missing scope, headless failing once naming everything), the SSH test's isolation
(`-F /dev/null`, no agent, only the key), the inconclusive test not stopping setup, and the wording
of the lines.

## 5. Departures from the bash

- B1. Usage errors exit 64 (the bash: 1) and a second mode is refused (the bash: the last won).
  `--help` reads nothing (the bash ran the scope helper first, so a broken helper broke `--help`);
  the usage names the scope file instead of listing its scopes.
- B2. Progress is stderr and the `--check` report stdout (the bash printed both on stdout).
- B3. Logged in is the JSON entry's `state` being `success` for that exact login (the bash grepped
  `account <name>` in gh's text, which matched `bobby` for `bob` and an account whose token had
  failed). The active account is read from the same local status (the bash: `gh api user`, a
  network call whose failure read as "no account").
- B4. A switch that fails is an error (the bash warned and went on, and its next scope check read
  whichever account was active). Only the refresh needs a switch; the scope check, the key listing
  and the upload act with the account's own token (the bash switched for each).
- B5. A key is registered when the base64 field matches exactly (the bash grepped it as a pattern
  in the raw JSON), and a listing that fails is an error (the bash read it as "not registered" and
  uploaded the key again).
- B6. After a login the account is always checked (the bash skipped the check when `gh api user`
  failed).
- B7. The passphrase goes through askpass (E21). The bash gave it to `ssh-keygen` in argv
  (`-N`, `-P`, readable in `/proc/<pid>/cmdline`) and wrote a passphrase-free copy of the key to
  the runtime directory for the SSH test; the test now uses the key itself, so `BatchMode` is set
  only without a passphrase and password and keyboard-interactive logins are off instead.
- B8. Logins compare without case (the bash compared case-sensitively except after a login).
- B9. Configured accounts are validated (E3, E7): a null or vaulted username and a repeated alias
  are errors (the bash's Python skipped the first two in silence).
- B10. `--add` refuses an alias configured with another username (the bash set up the new
  username under the alias while the file still named the old one), changes the file only after
  the passphrase is in hand (the bash wrote first), turns `github_accounts: {}` into a block (the
  bash added an indented line under it, which is not YAML), keeps the file's indent, reads the
  result back, and writes through a symlink and atomically (the bash rewrote it in place).
- B11. `~/.ssh` is created when missing (the bash's `ssh-keygen` failed), and a key without its
  `.pub` is a clear error (the bash's `awk` failed with its own message).
- B12. The closing advice names this checkout's play (the bash: a fixed `~/Projects` path).
- B13. The per-account scopes come from the helper's `audit` (the bash fed the active account's
  `gh api -i user` to `missing`), so the helper's `missing` command has one caller fewer.
- B14. The browser helper path is refused when gh's splitting could not keep it whole (E14).
- B15. The gh floor is 2.81.0 (the bash: 2.40.0, the first gh with several accounts). E6 reads
  the accounts with `gh auth status --json`, which gh 2.81.0 added: its release notes
  (`github.com/cli/cli/releases/tag/v2.81.0`, published 2025-10-01) open with "`gh auth status`
  Supports JSON Output" (cli/cli PR 11544), and no earlier release names the flag. Under the old
  floor a gh from 2.40.0 to 2.80.x passed E5 and failed at E6 with gh's own "unknown flag"
  instead of the floor message. `play-github-cli-multi.yml` keeps its own 2.40.0 check: the play
  needs only several accounts, and when it sends the user to this tool, an older gh gets this
  tool's floor message.

## 6. Proof

`go test -race` over `internal/ghaccount` and `cmd/gh-account-setup`: every expectation has a test
written first and seen to fail (journal). E21 is also proven against the real `ssh-keygen`: the
test binary is the askpass program, makes a key with a passphrase, and `ssh-keygen -y` reads it
back through the same askpass. `scripts/test-run-bash-gh-scopes.bash` builds and runs the wrapper
over its `gh` stub (D4).

## 7. Host checks (the owner's)

These need real GitHub accounts, a browser and the host's gh, so no container can make them.

- H1. `./scripts/gh-account-setup.bash --check` lists every account healthy, and
  `gh auth status --active` afterwards names the account that was active before.
- H2. `./scripts/gh-account-setup.bash --add=<alias>:<login>` for a new account: the device-code
  URL is printed (no browser opens), the key is made with the vault passphrase
  (`ssh-keygen -y -f ~/.ssh/github_<alias>` asks for it), it is uploaded, and the SSH test
  verifies it.
- H3. `./run.bash` reaches "Setting Up GitHub Multi-Account Access", builds the command and sets
  every account up (`--setup-all`), with the primary account active afterwards.
- H4. A headless `run.bash` on a fresh machine (the vmtest headless scenario) installs `golang`
  with the system dependencies and passes the GitHub step.
- H5. Ctrl-C during the login of H2 ends the run with exit 130 and the account active before is
  active again.

None of these is a scripted host run: H1 to H3 and H5 are by hand at the desktop, and H4 is the
VM lab's headless scenario. Nothing is queued in `meta-deploy.list` for this port, because no play
changed: the wrapper builds the command from the checkout on each run.
