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
