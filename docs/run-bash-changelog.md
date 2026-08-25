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
  helper — the sudo twin of `hl_ssh_agent_start`, shredded by the same EXIT trap (D4).
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
  the sudo twin of `hl_ssh_agent_start`, shredded by the same EXIT trap.
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
EXIT trap (shred secret files + backstop agent kill, V3.11). Headless branches for keygen
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
