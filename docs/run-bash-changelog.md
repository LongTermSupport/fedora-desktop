# `run.bash` changelog

`RUN_BASH_VERSION` is the version string printed in the installer banner and in every
`hl_abort` panic. This file is what it means.

**Why this file exists.** This history used to live in a single trailing comment on the
`RUN_BASH_VERSION` line in `run.bash`. By v1.12.0 that comment was **4,791 characters on one
line** — a changelog wearing a comment's clothes, unreadable in any editor and unreviewable in
a diff. Version history is documentation; it belongs in a document. The comment is now a
pointer here.

Each entry names its governing plan. The plan's `PLAN.md` carries the design and the
`JOURNAL/` carries the blow-by-blow, so nothing below needs to re-argue a decision — this is
the index, not the record.

---

## 1.22.0 — one play can run unattended, and play runs on a host take one shared lock (Plan 00137)

`--headless <play>.yml` (or `RUN_BASH_HEADLESS=1` through a play's shebang) used to be
refused: headless meant the whole provisioning contract, and a single play needs none of it.
It now runs that one play unattended. Its preflight checks only the sudo credential (NOPASSWD,
or `RUN_BASH_SUDO_PASSWORD_FILE`, which may be `/dev/fd/N` so a root caller can hand over a
root-only file as an inherited descriptor). PATH gains `~/.local/bin`, stdin is closed, and a
failure exits with the play's status instead of offering to file an issue. The auto-detected
case changes with it: a piped single play with a `RUN_BASH_*` variable set used to be refused,
and now runs unattended.

Every single-play run, interactive or not, now takes the host's play lock
(`helpers/play_lock/lock.py`) before it starts, so two play runs cannot overlap; the panel's
`--run-play` goes through the same route and is covered by it. A run that finds the lock held
exits 75 and names the holder. A holder can pass the lock to its child through
`FEDORA_DESKTOP_PLAY_LOCK_FD`, which is proven before it is trusted.

Minor: new behaviour on a path that used to refuse; an interactive single play with the lock
free prints exactly what it did before.

## 1.21.1 — a headless run without `admin:public_key` aborts instead of waiting at a browser prompt; key titles carry the hostname (Plan 00063)

Raised by a downstream headless consumer against Plan 00063's promise that every interactive
point has a headless branch that fails loud. When the GitHub token lacks `admin:public_key`,
the SSH-access step runs `gh auth refresh`, which opens a device-code flow in a browser. On a
headless box there is no browser and no human, so the run did not fail fast: it waited at that
prompt for ever, with the banner already saying provisioning was unattended. The step now takes
the same guard every other prompt in this file takes — `hl_abort` under `HEADLESS`, naming the
missing scope and the two ways to supply it — and the interactive path is unchanged. The
upload-failure branch of the same step goes through `fatal`, so a failed `gh ssh-key add` also
ends a headless run with the banner rather than one red line.

The uploaded key was titled `fedora-desktop setup <date>`, so two boxes set up on the same
day were indistinguishable in `/user/keys` and revoking one meant guessing. The title now
carries the short hostname as well as the date — distinguishing when the box has been given a
hostname (`RUN_BASH_HOSTNAME` headless), since boxes left at the default share one.

Patch: no run that previously completed now fails; the title change is cosmetic.

## 1.21.0 — an ssh-agent that survives teardown aborts the run instead of warning (Plan 00063)

`ssh-agent -k` returns non-zero for two states that are not alike: the agent was already gone,
or the kill **failed** and it is still running. `hl_ssh_agent_stop` reported both as
"agent may already be gone", warned, and continued — so a surviving agent left an unlocked key
reachable through `$SSH_AUTH_SOCK` for every remaining step of the run (ansible-galaxy, the
main playbook, each optional playbook, the reboot) while the run exited 0. That is the exact
exposure the function exists to close.

The two states are now told apart by `/proc/<pid>`, which answers without signalling anything
and without a redirect that would hide the answer. An already-gone agent stays silent and
returns 0; a survivor aborts via `headless_fail`, naming the pid and what is exposed.
`HL_SSH_AGENT_PID` is deliberately left set on that path so the `hl_cleanup` EXIT trap still
gets its attempt at the agent this could not kill.

Minor rather than patch: a run that previously succeeded with a warning now fails.
`scripts/test-run-bash-ssh-agent-teardown.bash` covers all five cases and is wired into
`qa-all.bash`; against the previous body it fails 5 of its 10 assertions.

## 1.20.2 — the localhost.yml reconcile compares without `diff` (Plan 00119)

1.20.1's reconcile read the file's current GitHub half through `diff`, which exits 1 on any
difference — exactly the case the reconcile exists for — and `set -e` took that as a failure,
so the first run that needed the reconcile died silently at "Loading Personal Configuration".
One awk classifier now yields both views (strip and extract) with no diff involved.

## 1.20.1 — headless: the GitHub inputs are reconciled into an existing localhost.yml; 443 applied before the first SSH use (Plan 00119)

Two defects found on the first real headless run with an account. (1) An existing
`localhost.yml` was kept untouched, so a box first provisioned with
`RUN_BASH_GITHUB_ACCOUNTS=none` and later declared an account kept `github_accounts: {}`:
the multi-account play generated no key and no Host block while `gh` was logged in. With the
default `RUN_BASH_CONFIG_SOURCE=none` the inputs are the declaration, so the file's GitHub
half (`github_accounts`, `github_ssh_over_443`, their comments) is now reconciled to them on
every run — rewritten only on a difference, everything else in the file preserved. A declared
config source keeps the old keep-if-configured behaviour. (2) With `RUN_BASH_GITHUB_SSH_443=1`,
step 10 switched the repository's origin to SSH and pulled over `github.com:22` before
`play-github-cli-multi.yml` had written the 443 override, so a port-22-blocked box timed out
there. Step 10 now applies the override first, through the repository's own `helpers/github443`
module (same managed blocks the play reconciles), bootstrapping the checkout over HTTPS on a
fresh box.

## 1.20.0 — `RUN_BASH_GITHUB_SSH_443`: a headless box declares the always-on 443 route (Plan 00119)

Headless provisioning with a GitHub account had no input for `github_ssh_over_443`, so a box
whose egress blocks port 22 could upload its key (HTTPS) and then hang on every SSH use of it.
`RUN_BASH_GITHUB_SSH_443=1` writes `github_ssh_over_443: true` into the fresh `localhost.yml`,
and `play-github-cli-multi.yml` installs the always-on `ssh.github.com:443` route in the same
run. Strictly `0`/`1`; refused together with `RUN_BASH_GITHUB_ACCOUNTS=none`, where there is no
key to route.

## 1.19.0 — `RUN_BASH_PS1_COLOUR`: a headless box gets a prompt colour (Plan 00108)

A headless run reached `play-basic-configs.yml`'s interactive colour prompt with no tty, the
prompt returned an empty answer, and the box was left with `PS1_COLOUR=` — every prompt on it
silently fell back to the prompt script's own colour, indistinguishable from a desktop's default.
`RUN_BASH_PS1_COLOUR` names the colour and is forwarded to the main playbook as the `PS1_Colour`
extra-var, validated against the functions `/var/local/colours` defines. The empty-answer path
now takes the documented default.

## 1.18.1 — headless optional playbooks receive the become password (Plan 00107)

`hl_run_optional_playbooks` ran each optional play bare, while the main playbook and the
interactive runner pass `--become-password-file` on the password-sudo path. It worked only
because the main playbook had just granted `NOPASSWD:ALL`; with the server profile no longer
granting it, the first optional play's `become` failed with `a password is required`. The
optional runner now uses the same become contract as the main playbook.

## 1.18.0 — `RUN_BASH_GIT_REF`: provision from a declared branch or commit (Plan 00106)

Headless HTTPS path only. A branch name puts the checkout on that branch at its origin tip
(`checkout -B`, so every run tracks the tip); a 40-hex commit pins it, detached. Unset keeps
the previous behaviour, the default branch's tip. Replaces `git pull` when set, since a pull
cannot run on a detached pin. A ref that does not resolve aborts rather than provisioning
from whatever was already checked out.

## 1.17.0 — resync with `plan-00066-ccy-ci-runner` (Plan 00090)

That branch (internally Plan 00068) diverged from this line at **1.10.0** — not 1.11.0, an
earlier version of this note misstated the divergence point — and both lines independently
minted a 1.11.0 of their own from there: this line's was Plan 00065 Phase 5
(`server-recommended`), the branch's was Plan 00073 (sudo password). The same happened one
version later, at 1.12.0: this line's was Plan 00082 (`GITHUB_ACCOUNTS=none`), the branch's
was Plan 00074 (the legacy-grub fix).

Merging both required renumbering this line's post-1.10.0 versions so neither collision
survives — see 1.13.0–1.16.0 below, each carrying its real governing plan number (the
version bump was independent of history; the plan number is the actual identity). The
branch's 1.11.0 and 1.12.0 are kept as-is, since renumbering the smaller side of a
2-vs-4-version collision moves fewer entries.

## 1.16.0 — headless PATH gap for pipx-installed tools (Plan 00085)

`pipx`-installed tools land in `~/.local/bin`, which a fresh headless shell has not yet
exported onto `PATH`. Exported before any pipx-installed tool is used headlessly.

## 1.15.0 — headless no longer requires `NOPASSWD:ALL` (Plan 00084, porting Plan 00073)

Plan 00073 built `RUN_BASH_SUDO_PASSWORD[_FILE]` as a second, equally supported sudo
credential on the (at the time) 1.11.0 line that later became `plan-00066-ccy-ci-runner`.
Plan 00084 ports the same design onto this line (lts-infra Plan 00045) so it composes with
1.14.0's `RUN_BASH_GITHUB_ACCOUNTS=none`:

- Preflight **asserts** one of NOPASSWD:ALL or the password file and decides
  `HL_SUDO_OPTS` once (D1).
- `hl_sudo_askpass_start` writes a `0600` password file plus a `0700` `SUDO_ASKPASS`
  helper — the sudo twin of `hl_ssh_agent_start`, unlinked by the same EXIT trap (D4).
- `hl_sudo_probe_password` **proves** the password authenticates during preflight rather
  than mid-provision.
- Every privileged call site goes through `_sudo` (D2), byte-identical to bare `sudo`
  whenever `HL_SUDO_OPTS` is empty — i.e. on every pre-existing path.
- The two Ansible invocations gain a third branch using ansible-core's native
  `--become-password-file` (D3).

**Known limitation, stated rather than implied:** `sudo -k -n true` is a *weak* probe — a
command-scoped rule passes `true` and still fails `dnf` — and the password probe is exactly
as weak. `ALL`-scoped sudo remains the documented requirement for **both** credentials.

## 1.14.0 — `RUN_BASH_GITHUB_ACCOUNTS=none` in headless (Plan 00082)

Preflight now accepts `none`: skips the `GITHUB_TOKEN_FILE`/`GITHUB_SSH_PASSPHRASE_FILE`
requirement, and rejects it combined with `RUN_BASH_CONFIG_SOURCE` or
`RUN_BASH_RESTORE_PROJECTS=1` — both need a GitHub identity. The SSH keygen block, the
gh-install/auth/SSH-key-upload/known-hosts/self-clone block (now an HTTPS-only clone in the
empty branch), the "GitHub SSH Key Passphrase" vault-encrypt step, and the "Setting Up
GitHub Multi-Account Access" step all gain an explicit `HL_GITHUB_ACCOUNTS=none` branch that
skips GitHub/SSH setup entirely. `hl_write_localhost_yml` writes an explicit
`github_accounts: {}` (not an omitted key) so both `github_accounts_configured` and the
function's own idempotency re-run check read it correctly.

Revives the design `--help-run-headless` documented before it was deferred at 1.9.1 — the
two "latent server-profile playbook bugs" cited as blockers there were re-verified against
current files and are (1) already fixed (`play-lxc`'s `git@` clone) and (2) not reproducible
from current file state (`play-git-configure-and-tools.yml` installs `gh` unconditionally,
before `play-github-cli-multi.yml` runs, regardless of GitHub config). No change to the
GitHub-configured headless path or the interactive path. See
`CLAUDE/Plan/00082-run-bash-github-accounts-none/PLAN.md`.

## 1.13.0 — `server-recommended` optional-play bundle (Plan 00065 Phase 5)

`RUN_BASH_OPTIONAL_PLAYBOOKS` accepts the reserved keyword `server-recommended`, expanded
from the tracked manifest `playbooks/imports/optional/server-recommended.bundle` into its
listed plays before the existing per-token resolver runs. Composes with explicit tokens, and
the resolved token list is de-duplicated — a play named twice (via the bundle plus an
explicit token, or two explicit tokens) runs once. Unknown-token and failed-play handling
unchanged.

## 1.12.0 — the legacy-grub check gets four outcomes (Plan 00074)

The "Checking for Legacy Grub Configurations" step held two opposite defects.

- **It reported an absence it could not prove.** `grubby --info=ALL 2>/dev/null | grep -q …`
  turned a *failing* grubby into `✓ No legacy cgroup configuration found`: after the pipe,
  empty stdout and a genuine negative are indistinguishable, and `pipefail` does not
  separate them either (both are non-zero pipelines).
- **It reported a failure it had proven, and continued.** `error()` is `echo -e` and does not
  exit, so a verifiably-failed removal printed a message, printed manual instructions, and
  let the installer run on and exit `0`.

`check_legacy_grub_cgroup` now produces four distinct outcomes. Only a **non-zero** grubby
exit is fatal — exit 0 with no legacy args is still a valid negative answer, so boxes with
unusual boot entries are unaffected. Both failure states call the new `fatal`.

**New:** `fatal <step> <what> [debug]` — the both-modes abort (`hl_abort` when headless,
`error` + `exit 1` otherwise) that interactive code had never had. Its absence is *how* the
skip-and-warn above arose: by accident, not by choice.

Extracted to a top-level function so a stub `grubby` can drive all four states. Both defects
had survived because inline code inside `main()` could not be tested at all.

## 1.11.0 — headless no longer requires `NOPASSWD:ALL` (Plan 00073)

`RUN_BASH_SUDO_PASSWORD[_FILE]` becomes a second, equally supported sudo credential.

- Preflight **asserts** one of the two and decides `HL_SUDO_OPTS` once (D1).
- `hl_sudo_askpass_start` writes a `0600` password file plus a `0700` `SUDO_ASKPASS` helper —
  the sudo twin of `hl_ssh_agent_start`, unlinked by the same EXIT trap.
- `hl_sudo_probe_password` **proves** the password authenticates during preflight rather than
  mid-provision.
- Every privileged call site goes through `_sudo` (D2), byte-identical to bare `sudo`
  whenever `HL_SUDO_OPTS` is empty — i.e. on every pre-existing path.
- The two Ansible invocations gain a third branch using ansible-core's native
  `--become-password-file` (D3).

**Known limitation (D5), stated rather than implied:** `sudo -k -n true` is a *weak* probe — a
command-scoped rule passes `true` and still fails `dnf` — and the password probe is exactly as
weak. `ALL`-scoped sudo remains the documented requirement for **both** credentials.

## 1.10.0 — headless flows through the full body (Plan 00063)

Flips the honest-stop: a headless run now executes the whole installer rather than stopping
early. `gh-account-setup` receives `RUN_BASH_HEADLESS` and fails loud on any interactive `gh`
web/scope-refresh; the main playbook gets `RUN_BASH_PROVISIONING_PROFILE` passthrough plus a
D7 loud-fatal on failure; optional playbooks via `RUN_BASH_OPTIONAL_PLAYBOOKS`; project
restore via `RUN_BASH_RESTORE_PROJECTS`; reboot via `RUN_BASH_REBOOT`.

> End-to-end execution is **host-verified on a real server**. In a container this is
> `bash -n` + shellcheck + preflight acceptance only.

## 1.9.5 — `localhost.yml` assembly (Plan 00063)

`hl_write_localhost_yml` (idempotent keep, else `RUN_BASH_CONFIG_SOURCE` pull from the private
config repo, else fresh from `RUN_BASH_*` identity + `github_accounts`), `hl_pull_config_source`
(private-repo gate + loud 404), and `hl_reconcile_vault` (D6: provided-or-fail, verified against
encrypted values, **never** auto-generating over `!vault`). Headless branch for
`github_ssh_passphrase` reuses the resolved passphrase and vault-encrypts it. Interactive
config/vault blocks are wrapped under `if HEADLESS != true`.

## 1.9.4 — GitHub/SSH execution mechanics (Plan 00063)

`hl_ssh_agent_start` (ssh-agent + a transient `0700` `SSH_ASKPASS` reading a `0600` passphrase
file, V3.13), `hl_ssh_agent_stop` (killed after the last git op, V3.12), and the `hl_cleanup`
EXIT trap (unlink secret files + backstop agent kill, V3.11). Headless branches for keygen
(`-P` from the resolved passphrase, then agent load), hostname (`RUN_BASH_HOSTNAME` or leave
the default), and `gh` token auth (`gh auth login --with-token` from stdin, `git_protocol=ssh`).
All fail loud via `hl_abort`.

## 1.9.3 — the execution slice begins (Plan 00063)

Adds `hl_abort` (big loud banner, exit 1) for headless *execution* failures, and a headless
backstop at the top of every shared interactive prompt helper (`confirm`, `promptForValue`,
`promptChoice`, `promptSecretConfirmed`, `promptDefault`, `prompt_verified_vault_password`,
`prompt_github_accounts_yaml`) so a headless run that ever reaches a prompt fails loud instead
of hanging (fail-fast rule 11).

## 1.9.2 — the login SSH key stays passphrase-protected (Plan 00063)

`RUN_BASH_GITHUB_SSH_PASSPHRASE_FILE` becomes required in v1 (D6), mirroring the interactive
no-empty-passphrase rule, because headless loads the key non-interactively via
ssh-agent/`SSH_ASKPASS` (D5) and has no TTY to prompt from later.

## 1.9.1 — defer the GitHub-empty path (Plan 00063)

Headless v1 requires a single GitHub account plus a token file, and fails fast on
`RUN_BASH_GITHUB_ACCOUNTS=none`. Help text and acceptance aligned. The empty-GitHub path is
blocked by two latent server-profile playbook bugs, so failing fast beats provisioning a box
that would break at the playbook stage.

## 1.9.0 — headless preflight (Plan 00063)

`headless_preflight` validates and resolves all `RUN_BASH_*` input up front: non-root check,
NOPASSWD-sudo probe, required email/accounts, and secret `*_FILE` resolution with the V3.10
guardrails (file-precedence; fail fast on both-set, unreadable, or a literal on a cloud box;
warn on a literal elsewhere; unset literals before the first child process). Adds the
`set -u`-safe secret-file EXIT trap.

---

## See also

- [Headless / Unattended Provisioning](headless-provisioning.md) — the reference contract
- [Headless Server Install](headless-server-install.md) — the step-by-step walkthrough
- `./run.bash --help-run-headless` — the authoritative, always-current contract in the script
