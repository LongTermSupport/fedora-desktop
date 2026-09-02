# Plan 00063 — Headless `run.bash` design (frozen spec)

Supporting document for [PLAN.md](PLAN.md). This is the implementation spec that
the hostile-review loop converged on: Design v2 (D1–D11) plus the round-2 and
round-3 hardening deltas (V3.1–V3.15) and the canonical invocation. The owner
decisions it rests on are in [DECISIONS.md](DECISIONS.md); the audit narrative
that produced it is in `JOURNAL/`; the original unsplit text is in
[PLAN_archive.md](PLAN_archive.md).

Line numbers below refer to `run.bash` as it was at plan creation (v1.7.3,
1941 lines) and are historical anchors, not current positions.

## Context: where the interactive walls were

- `run.bash` is wrapped in `main()` in a subshell; `set -e -u -o pipefail`,
  `IFS=$'\n\t'`. `RUN_BASH_VERSION` (line 6) must be bumped on every change.
- Read-based prompt helpers: `confirm()` (220), `promptForValue()` (676),
  `promptChoice()` (760), `promptSecretConfirmed()` (791), `promptDefault()`
  (818), `prompt_verified_vault_password()` (860),
  `prompt_github_accounts_yaml()` (329).
- Identity gather 1343–1345 / 1373–1375; GitHub accounts 1355/1381; vault
  1405–1436; SSH passphrase 1462–1470; hard SSH-for-GitHub gate 1072;
  config-repo choice 1266/1327; main playbook 1540/1543; projects restore 1568;
  optional menu 1773–1906; reboot 1927.
- `connection: local`: `run.bash` provisions the box it runs on. Server/cloud
  use means copying `run.bash` onto the box (or fetching it in cloud-init) and
  running it there.
- Prior art: `scripts/gh-account-setup.bash` already takes
  `GITHUB_SSH_PASSPHRASE`, `LOCALHOST_YML`, `VAULT_PASS_FILE` from env.
- Binding standards: `CLAUDE/InteractiveScripts.md` rules 03, 08, 10, 11;
  `CLAUDE/StderrHygiene.md`; the fail-fast rule; `CLAUDE/SecurityRules.md`
  (never echo secrets or place them in `argv`).

## Round-1 premise correction

"Make the bash prompt helpers headless-aware" covers only the easy half. The
load-bearing interactive walls are not bash reads: `gh auth login`/`auth refresh`
device-code OAuth (`run.bash:1076/1111`, `gh-account-setup.bash:297/328`), direct
`sudo` password from `run.bash:906` (long before the Ansible `--ask-become-pass`
at 1543), and the passphrase-protected `~/.ssh/id` clone with no ssh-agent
(`965-985` then `1159`). Plus secret exposure via cloud-init user-data, a failed
playbook run that exits 0, and vault-password footguns. D1–D11 give each an
explicit contract.

## Design v2 (D1–D11)

### D1. Headless trigger

Headless is ON when `RUN_BASH_HEADLESS=1` / `--headless`, or (`[ ! -t 0 ]` and at
least one `RUN_BASH_*` set). `--interactive` forces OFF. Piped-stdin smoke tests
must set `--interactive` or export no `RUN_BASH_*`, else they trip headless.

### D2. Env-var contract: non-secret env plus file-pointer secrets

Secrets are never literal env values by default: a literal secret in cloud-init
`user-data` persists at `/var/lib/cloud/instance/user-data.txt` and the metadata
service (`169.254.169.254`), world-readable forever (`unset` cannot unwrite it),
and every child inherits it via `/proc/PID/environ`. So secret bytes arrive via
`0600` files and only the path is an env var. Mirrors the existing
`VAULT_PASS_FILE` precedent. `run.bash` reads each file into a `local` and
deletes it after use. (Decision 4 later allowed the literal form too, guarded by
V3.10.)

Non-secret config:

| Variable                        | Purpose                                       | Required (headless) | Default       |
| ------------------------------- | --------------------------------------------- | ------------------- | ------------- |
| `RUN_BASH_HEADLESS`             | Force headless                                | —                   | unset         |
| `RUN_BASH_USER_LOGIN`           | System login                                  | no                  | `$(whoami)`   |
| `RUN_BASH_USER_NAME`            | Full name                                     | no                  | = login       |
| `RUN_BASH_USER_EMAIL`           | Git email                                     | **yes**             | — (fail fast) |
| `RUN_BASH_HOSTNAME`             | Hostname when box is still `fedora`           | no                  | keep current  |
| `RUN_BASH_GITHUB_ACCOUNTS`      | Comma-sep gh usernames (mandatory)            | **yes**             | — (fail fast) |
| `RUN_BASH_CONFIG_SOURCE`        | Config-repo host file to import, or `none`    | no                  | `none`        |
| `RUN_BASH_PROVISIONING_PROFILE` | Force `desktop`/`server`, passed via `-e`     | no                  | auto-detect   |
| `RUN_BASH_OPTIONAL_PLAYBOOKS`   | Space/comma list of optional plays, or `none` | no                  | `none`        |
| `RUN_BASH_RESTORE_PROJECTS`     | `1`/`0` restore from config manifest          | no                  | `0`           |
| `RUN_BASH_REBOOT`               | `1`/`0` reboot at end                         | no                  | `0`           |

Secret file-pointers (`0600` file paths):

| Variable                              | Purpose                                             | Required (headless v1)                    |
| ------------------------------------- | --------------------------------------------------- | ----------------------------------------- |
| `RUN_BASH_GITHUB_TOKEN_FILE`          | Scoped PAT for `gh auth login --with-token` (D3)    | **yes** (single account)                  |
| `RUN_BASH_VAULT_PASSWORD_FILE`        | Ansible vault password                              | **yes** (V3.3; never auto-generated)      |
| `RUN_BASH_GITHUB_SSH_PASSPHRASE_FILE` | Passphrase for generated SSH keys incl. `~/.ssh/id` | **yes** (v1, after the Task 1.6 deferral) |

### D3. GitHub non-interactive auth via scoped token

GitHub auth is a `gh` OAuth device flow, not a bash prompt. In headless mode,
before `gh auth status` (`run.bash:1063`), authenticate with
`gh auth login --hostname github.com --git-protocol ssh --with-token` reading the
token from the file. A valid token makes `gh auth status` pass, so the interactive
`gh auth login` (1076), `gh auth refresh` (1111) and the SSH-confirm loop (1072)
are structurally skipped. Token scope must include `admin:public_key` plus the
repo's pinned `REQUIRED_SCOPES` (`vars/github-required-scopes.yml`). Fail fast if
the token file is missing, empty, or under-scoped. `gh-account-setup.bash` needs
the same token path for its `--web` device flows (`:297/:328`). Multi-account:
v1 supports a single account and fails fast on more than one, never a silent
partial auth.

### D4. Sudo: headless requires NOPASSWD sudo, probed at startup

`run.bash` calls `sudo` directly from step 1 (`:906`), long before the Ansible
`--ask-become-pass` logic. Headless probes `sudo -k -n true` at startup and fails
fast with guidance if it fails. With NOPASSWD guaranteed, the playbook runs
without `--ask-become-pass`. See V3.7 for the `-k` and NOPASSWD:ALL details.

### D5. SSH keys: passphrase from file plus ssh-agent

`~/.ssh/id` (generated `965-985`) authenticates the clone (`1159`). A
passphrase-protected key with no agent prompts and hangs. Headless generates the
key with the passphrase from `RUN_BASH_GITHUB_SSH_PASSPHRASE_FILE` and starts an
ssh-agent for the git operations' lifetime. Decision 6 settled on keeping the
passphrase (no passphraseless fallback); see V3.6, V3.12, V3.13.

### D6. Vault reconciliation: never auto-generate over an imported vault

After the optional config import, if the resolved `localhost.yml` contains any
`!vault` value, the vault password is required and verified
(`verify_vault_password`, `run.bash:846`); a mismatch fails fast, never the
interactive abort-loop. V3.3 tightened this further: headless never auto-generates
a vault password at all.

### D7. Failure and public-post semantics

Headless plus a non-zero `main_exit_code` means `exit $main_exit_code`; the
desktop "continue despite failure?" default-yes (1557) must not apply headless.
Both "create a GitHub issue?" gates (603/661/1552) resolve to No headless, so no
unattended path posts box specifics to the public tracker.

### D8. Non-root entrypoint

`run.bash` refuses root (`:124`); cloud-init `runcmd` is root. Headless must run
as the non-root target user (`sudo -u <user> -i …`). Secret files cross that
boundary cleanly, a further reason for the file-pointer model.

### D9. Read-prompt neutralisation and `headless_fail`

For the remaining read-based prompts (identity, hostname, accounts validation,
config-repo choice, optional menu, projects restore, reboot) each helper is
headless-aware: return the env value or default, or call
`headless_fail <VAR> <what>`. `RUN_BASH_GITHUB_ACCOUNTS` runs the same validation
as `prompt_github_accounts_yaml` and fails fast on a bad entry. `confirm()` gets
an EOF guard. `headless_fail` and all status go to stderr; helper stdout stays
the captured value only.

### D10. Implementation constraints

No new `2>/dev/null`, `|| true` or `sed` (the hooks daemon blocks them on new
content; use capture-to-var probes). Bump `RUN_BASH_VERSION` on every edit. QA
via `./scripts/qa-all.bash`.

### D11. `--help` and `--help-run-headless`

`--help` gains a one-line pointer to server/cloud and `--help-run-headless`.
`--help-run-headless` prints the full env contract, the NOPASSWD-sudo and
non-root requirements, the token scopes, a copy-paste cloud-init example, the
fail-fast rules and the desktop-vs-server model. The cloud example must not put
secrets in `write_files` (V3.1).

## Design v3: round-2 deltas (V3.1–V3.9)

- **V3.1 — Secrets out-of-band on cloud, never `write_files`.** `write_files`
  content lives inside user-data, which the metadata service serves forever. On
  cloud, secrets are fetched inside `runcmd` from a secrets manager, vendor-data
  or attached media. On a plain server the operator places the `0600` files
  directly.
- **V3.2 — Token-scope completeness gate before both refresh sites.** Token
  pre-auth skips `gh auth login` but not the scope-triggered `gh auth refresh` at
  `run.bash:1111` and `gh-account-setup.bash:328`. Check the token against the
  full `vars/github-required-scopes.yml` plus `admin:public_key` before both
  sites, failing fast and naming missing scopes. Single-account also routes
  through `--setup-all` (guard at 1487), so it needs the same gate.
- **V3.3 — Vault: require the password, never auto-generate or print headless.**
  Printing a generated password to stderr leaks it into
  `/var/log/cloud-init-output.log` and the serial console. Headless requires the
  vault password (either form per Decision 4, file preferred), never
  auto-generates it, and never prints secret bytes.
- **V3.4 — EXIT trap cleans every secret file.** The original trap (`:56`) only
  removed `/tmp/.github_ssh_pp`. Extend it to remove all secret paths via a
  trap-visible array. Also trap-guard the passphrase-stripped key copy at
  `gh-account-setup.bash:211-232`.
- **V3.5 — gh persists the token in `~/.config/gh/hosts.yml`.** Deleting the
  token file is false assurance. Recommend a fine-grained short-lived PAT and
  `gh auth logout` at the end; document `vault-pass.secret` as a standing key on a
  persistent box.
- **V3.6 — ssh-agent lifecycle and argv passphrase.** `ssh-add` has no argv
  passphrase flag, so it needs an `SSH_ASKPASS` helper; the agent must live from
  keygen through the `git pull` at `:1508` and be torn down; `ssh-keygen -P/-N`
  expose the passphrase in `/proc/PID/cmdline` regardless of source. Resolved by
  Decision 6 (keep passphrase; agent plus askpass; residuals accepted).
- **V3.7 — `sudo -k -n true`, document NOPASSWD:ALL.** `-k` avoids a
  cached-timestamp false pass; a command-scoped NOPASSWD passes `true` but fails
  `dnf`, so NOPASSWD:ALL is a documented assumption.
- **V3.8 — Optional-playbook failures propagate headless.** `run_playbook`
  (1593-1600) swallowed failures; headless optional failures must reach a
  non-zero final exit.
- **V3.9 — Passphrase hand-off to gh-account-setup via file, not env.**
  `run.bash:1492` passed `GITHUB_SSH_PASSPHRASE=` as literal env, inherited via
  `/proc/PID/environ`. Teach `gh-account-setup.bash` a `*_FILE` input.

## Design v3: round-3 hardening (V3.10–V3.15)

- **V3.10 — Literal-secret form needs active guardrails.** Supporting the literal
  form (Decision 4) re-opens the user-data and `/proc` leak unless guarded. The
  literal form must: (a) hard-fail on a detected cloud box
  (`/var/lib/cloud/instance` present); (b) warn loudly on stderr otherwise;
  (c) fail fast when both the literal and its `_FILE` are set; (d) never fall
  back on an unreadable `_FILE`; (e) be read into a non-exported local and
  `unset` before the first child spawn.
- **V3.11 — `set -u`-safe cleanup trap.** A trap referencing unset secret-path
  vars aborts under `set -u` on early abort and skips the cleanup it exists for.
  Initialise every trap-visible var to empty before the trap; expand with
  `${x:-}` / `"${arr[@]:-}"`.
- **V3.12 — ssh-agent killed right after the last git op, not at EXIT.** An
  EXIT-scoped teardown leaves the unlocked key reachable via `$SSH_AUTH_SOCK`
  across galaxy, the whole main playbook, optional playbooks and reboot. Kill the
  agent immediately after the pull at `:1508`; the EXIT trap is only a backstop.
- **V3.13 — askpass helper hardening.** `mktemp` at `0700`; the helper reads the
  passphrase from the `0600` file at runtime (never inlined); carries only the
  non-secret path in its env/argv; is trap-cleaned. The passphrase file survives
  until `ssh-add` has consumed it.
- **V3.14 — Empty-GitHub path requirements (moot for v1 after Task 1.6).** If the
  `RUN_BASH_GITHUB_ACCOUNTS=none` path is ever enabled it must: clone with the
  HTTPS url at `:1156-1159` and skip the SSH-origin migration at `:1164-1171`;
  still write identity and vault (`:1371-1449`); gate roughly six blocks
  individually rather than a line range, guarding `primary_gh_username` at
  `:1200`/`:1569`; and guard two general-scoped core plays that hard-depend on
  GitHub (`play-github-cli-multi.yml:42` ungated `gh --version`;
  `play-lxc-install-config.yml:240` `git@` clone). Carried to the follow-up plan.
- **V3.15 — Canonical cloud example shows the out-of-band fetch inline and pins
  the fetch.** The `runcmd` example shows the secret fetched out-of-band
  immediately above the `run.bash` line, so operators do not reflexively
  `write_files` it. Pin the fetch to a commit SHA or add a checksum step, and use
  the download-inspect-run form rather than `bash -c "$(curl …)"`, which is a
  silent no-op when curl fails.

## Canonical headless invocation (v1: GitHub token required)

The one canonical way to headlessly provision the latest repo. `run.bash`
self-updates (clones or pulls the repo), so this always runs branch-latest
source. Run as the non-root target user; on cloud-init, drop from root in
`runcmd`.

```bash
RUN_BASH_HEADLESS=1 \
RUN_BASH_USER_EMAIL=name@example.com \
RUN_BASH_GITHUB_ACCOUNTS=<gh-username> \
RUN_BASH_GITHUB_TOKEN_FILE=/run/secrets/gh-token \
RUN_BASH_GITHUB_SSH_PASSPHRASE_FILE=/run/secrets/ssh-pass \
RUN_BASH_VAULT_PASSWORD_FILE=/run/secrets/vault-pass \
  bash /tmp/run.bash
```

Preconditions (fail fast if unmet): NOPASSWD:ALL sudo; non-root user; on cloud,
secret files fetched out-of-band in `runcmd` (never `write_files`/user-data); the
token carries the full `vars/github-required-scopes.yml` plus `admin:public_key`.
The user-facing form of this contract lives in `docs/headless-provisioning.md`
and the runbook `docs/headless-server-install.md`.

## Risks and mitigations

| Risk                                                     | Impact | Probability | Mitigation                                                                                          |
| -------------------------------------------------------- | ------ | ----------- | --------------------------------------------------------------------------------------------------- |
| GitHub device-code auth cannot run headless              | H      | mitigated   | D3: `gh auth login --with-token` from a scoped token file in both scripts; scope gate (V3.2)        |
| Secret leak via cloud-init user-data / `/proc/environ`   | H      | mitigated   | D2/Decision 4: `0600` file pointers, out-of-band on cloud (V3.1), literal-form guardrails (V3.10)   |
| Password-sudo box: first direct `sudo` hangs headless    | H      | mitigated   | D4/V3.7: startup `sudo -k -n true` probe, fail fast; no `--ask-become-pass` headless                |
| Auto-generated vault password corrupts an imported vault | H      | mitigated   | D6/V3.3: password must be provided and verified; never auto-generated headless                      |
| Failed main playbook reports success                     | H      | mitigated   | D7/V3.8: headless failure exits non-zero, including optional playbooks                              |
| ssh-agent-less passphrase clone hangs                    | H      | mitigated   | D5/Decision 6: passphrase from file, ssh-agent plus `SSH_ASKPASS`, agent killed after the last pull |
| Headless auto-detect fires on an accidental desktop pipe | H      | L           | Explicit `RUN_BASH_*` (or `--headless`) required alongside no-TTY; `--interactive` override         |
| Desktop regression from shared helpers                   | H      | L           | Headless branch additive and guarded; desktop path unchanged; host regression check (Task 3.2)      |
