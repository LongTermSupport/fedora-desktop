# Plan 00063: Headless `run.bash` — Server & Cloud Provisioning

**Status**: In Progress
**Created**: 2026-07-23
**Owner**: joseph
**Priority**: Medium

## Overview

Plan 00061 made the **Ansible layer** headless-capable: every play declares a
`scope:` (`general` | `gnome` | `server`) and self-guards on an auto-detected
`provisioning_profile` (`systemctl get-default` → `graphical.target` = desktop,
else server, server-biased). Running `./playbooks/playbook-main.yml` on a Fedora
**Server** or **Cloud** box already skips all GNOME plays and installs only the
general + server subset. That half is done.

The remaining blocker is **`run.bash`** — the bootstrap installer that a user (or
the kickstart pipeline) runs to go from a fresh Fedora to a fully-provisioned box.
It is desktop-shaped and **100% interactive**: every configuration value (user
identity, GitHub accounts, vault password, SSH passphrase, config-repo choice,
optional-playbook menu, reboot) is gathered from a terminal `read`/prompt. There
is **no non-interactive path**, which means `run.bash` cannot provision a headless
server or an unattended cloud instance — the prompts either EOF-abort or hang when
no TTY is present. This also violates `CLAUDE/InteractiveScripts.md` rule 11 (a
non-interactive escape hatch must exist and must fail fast, never hang).

This plan adds a **headless execution mode** to `run.bash`, driven entirely by
**environment variables** (12-factor; drops straight into cloud-init user-data and
CI), so the same script can provision a desktop interactively **or** a
server/cloud box unattended via IaC. GitHub setup **remains mandatory** (fed from
env in headless mode; missing → fail fast). It also expands `--help` and adds a
dedicated `--help-run-headless` deep-dive documenting the full env-var IaC
contract without flooding the normal help output.

## Goals

- Add a **headless / non-interactive mode** to `run.bash` that provisions a Fedora
  **Server or Cloud** box end-to-end with **zero interactive prompts**, driven by
  environment variables.
- **Fail fast** when a required value is missing and no TTY is available — a clear
  message naming the exact env var to set, never a hang (satisfies
  `CLAUDE/InteractiveScripts.md` rules 03 & 11).
- Keep **GitHub setup mandatory** in every mode; headless supplies the accounts
  (and SSH passphrase / vault password) from env. SSH remains the sole GitHub auth
  path.
- Preserve the **interactive desktop path with zero regression** — a bare
  `./run.bash` on a workstation behaves exactly as today.
- Make headless mode **auto-detect** the target the same way the Ansible layer
  does (no TTY and/or `RUN_BASH_HEADLESS=1`), and pass/allow the
  `provisioning_profile` override through to the playbook.
- Expand `--help`; add **`--help-run-headless`** documenting the complete env-var
  contract, a cloud-init example, and the fail-fast rules.
- Document the headless/server/cloud story in `docs/` and cross-link from
  `README.md`.

## Non-Goals

- **Not** adding a new play `scope` — Fedora Cloud correctly resolves to
  `provisioning_profile: server` via Plan 00061's auto-detection. The
  general+server subset is the right coverage for Cloud; a `cloud` scope would be
  an empty-bucket taxonomy bloat. (Confirmed: `VERSION_ID` is edition-independent,
  so the version gate already passes on Server/Cloud editions.)
- **Not** changing the Ansible layer / play scoping / the `provisioning_profile`
  detection — that is Plan 00061's delivery and is reused verbatim.
- **Not** re-architecting `run.bash`'s interactive desktop flow — headless mode is
  added *alongside* it through the existing prompt-helper chokepoints, not a
  rewrite.
- **Not** building remote-driven provisioning (`run.bash` is `connection: local`;
  it runs *on* the target). "Run against servers" = run `run.bash` on the server /
  via cloud-init user-data, consistent with the repo's local-transport design.
- **Not** changing the kickstart `ks.cfg` / ISO build (Plan 00018/00022 territory),
  though this plan makes `run.bash` *callable* unattended from that pipeline.
- **Not** adding an answers-file mechanism — env vars are the chosen single input
  channel (owner decision). An answers file can layer on later if needed.

## Context & Background

- **Entry point**: `run.bash` (1941 lines, `RUN_BASH_VERSION` at line 6 — **must
  be bumped on every change**, same discipline as CCY). Whole body is wrapped in
  `main()` run inside a subshell; `set -e -u -o pipefail`, `IFS=$'\n\t'`.
- **Interactive chokepoints** (the surface headless mode must neutralise):
  - `confirm()` (220), `promptForValue()` (676), `promptChoice()` (760),
    `promptSecretConfirmed()` (791), `promptDefault()` (818),
    `prompt_verified_vault_password()` (860), `prompt_github_accounts_yaml()` (329).
  - Identity gather: lines 1343–1345 / 1373–1375 (`user_login`, `user_name`,
    `user_email`), GitHub accounts 1355/1381, vault 1405–1436, SSH passphrase
    1462–1470.
  - **Hard SSH-for-GitHub gate**: line 1072 (loops until confirmed).
  - Config-repo import choice: `promptChoice` 1266/1327.
  - Main playbook invocation: 1540/1543 (NOPASSWD detection → `--ask-become-pass`).
  - Post-main: projects restore 1568, optional-playbook menu 1773–1906, reboot
    1927\.
- **`connection: local`**: `run.bash` provisions the box it runs on. Server/cloud
  use = copy `run.bash` onto the box (or curl it in cloud-init user-data) and run.
- **Prior art in-repo**: `scripts/gh-account-setup.bash` already takes
  `GITHUB_SSH_PASSPHRASE`, `LOCALHOST_YML`, `VAULT_PASS_FILE` from env (invoked at
  line 1492) — the env-driven pattern is established; this plan extends it to the
  whole bootstrap.
- **Standards that bind this work**:
  - `CLAUDE/InteractiveScripts.md` — rules 03 (clean EOF exit), 08 (stderr
    hygiene), 10 (`-h` always works, unknown opts fail fast), 11 (non-interactive
    escape hatch, fail-not-hang).
  - `CLAUDE/StderrHygiene.md` — prompts/diagnostics → stderr; captured values →
    stdout.
  - `CLAUDE.md` #1 Fail-Fast; `CLAUDE/QA.md` (`./scripts/qa-all.bash` before every
    commit); CCY container rule (edit + commit only; deploy/verify on HOST).
  - `CLAUDE/SecurityRules.md` — headless must never echo secrets or place them in
    `argv`; env-var secrets read once and unset.

## Design v2 — HARDENED after hostile review round 1

> **Round 1 finding (both auditors, independently): the round-1 premise was wrong.**
> "Make the bash prompt helpers headless-aware" (old D3) covers only the *easy
> half* — `read`-based prompts. The **load-bearing** interactive walls are NOT
> bash reads and are untouched by helper neutralisation: `gh auth login`/`auth refresh` **device-code OAuth** (`run.bash:1076/1111`, `gh-account-setup.bash:297/328`),
> direct **`sudo` password** (from `run.bash:906`, long before the Ansible
> `--ask-become-pass` at 1543), and the passphrase-protected **`~/.ssh/id` clone
> with no ssh-agent** (`965-985`→`1159`). Plus secret exposure via cloud-init
> user-data, a failed-playbook run that exits 0, and vault-password footguns. The
> design below adds explicit contracts for each. See JOURNAL round-1 judgement.

### D1. Headless trigger

Headless is ON when: `RUN_BASH_HEADLESS=1` / `--headless`, **or** (`[ ! -t 0 ]`
**and** ≥1 `RUN_BASH_*` set). `--interactive` forces OFF. **Smoke-test caveat
(round-1 S5):** piped-stdin smoke tests must set `--interactive` or export no
`RUN_BASH_*`, else they trip headless.

### D2. Env-var contract v2 — non-secret env + **file-pointer** secrets

**Secrets are NEVER literal env values** (round-1 B2/B3 + secret verdict): a literal
secret in cloud-init `user-data` persists at `/var/lib/cloud/instance/user-data.txt`

- the metadata service (`169.254.169.254`), world-readable **forever** — `unset`
  cannot unwrite it — and every child inherits it via `/proc/PID/environ` during the
  multi-minute run. So secret **bytes** arrive via `0600` **files** (cloud-init
  `write_files`, `permissions: '0600'`); only the **path** is an env var. This keeps
  the owner's env-driven contract (Decision 1) — path via env — while the secret
  never enters the environment or user-data. Mirrors the existing `VAULT_PASS_FILE`
  precedent (`gh-account-setup.bash:21`). run.bash reads each file into a `local` and
  deletes it after use.

**Non-secret config (plain `RUN_BASH_*` env):**

| Variable                        | Purpose                                       | Required (headless) | Default       |
| ------------------------------- | --------------------------------------------- | ------------------- | ------------- |
| `RUN_BASH_HEADLESS`             | Force headless                                | —                   | unset         |
| `RUN_BASH_USER_LOGIN`           | System login                                  | no                  | `$(whoami)`   |
| `RUN_BASH_USER_NAME`            | Full name                                     | no                  | = login       |
| `RUN_BASH_USER_EMAIL`           | Git email                                     | **yes**             | — (fail fast) |
| `RUN_BASH_HOSTNAME`             | Hostname when box is still `fedora` (S3)      | no                  | keep current  |
| `RUN_BASH_GITHUB_ACCOUNTS`      | Comma-sep gh usernames (mandatory)            | **yes**             | — (fail fast) |
| `RUN_BASH_CONFIG_SOURCE`        | Config-repo host file to import, or `none`    | no                  | `none`        |
| `RUN_BASH_PROVISIONING_PROFILE` | Force `desktop`/`server` → `-e` passthrough   | no                  | auto-detect   |
| `RUN_BASH_OPTIONAL_PLAYBOOKS`   | Space/comma list of optional plays, or `none` | no                  | `none`        |
| `RUN_BASH_RESTORE_PROJECTS`     | `1`/`0` restore from config manifest          | no                  | `0`           |
| `RUN_BASH_REBOOT`               | `1`/`0` reboot at end                         | no                  | `0`           |

**Secret file-pointers (`0600` file paths, bytes never in env/user-data):**

| Variable                              | Purpose                                                  | Required (headless)            |
| ------------------------------------- | -------------------------------------------------------- | ------------------------------ |
| `RUN_BASH_GITHUB_TOKEN_FILE`          | Scoped PAT for `gh auth login --with-token` (D3)         | **yes** (see D3 multi-account) |
| `RUN_BASH_VAULT_PASSWORD_FILE`        | Ansible vault password                                   | conditional (D6)               |
| `RUN_BASH_GITHUB_SSH_PASSPHRASE_FILE` | Passphrase for generated SSH keys incl. `~/.ssh/id` (D5) | no (empty ⇒ passphraseless)    |

### D3. GitHub **non-interactive auth** via scoped token (round-1 B1 — THE blocker)

GitHub auth is a `gh` OAuth device flow, not a bash prompt — no read-helper can
neutralise it. In headless mode, **before** `gh auth status` (`run.bash:1063`),
authenticate each account with `gh auth login --hostname github.com --git-protocol ssh --with-token < "$RUN_BASH_GITHUB_TOKEN_FILE"`. A valid token
makes `gh auth status` pass → the interactive `gh auth login` (1076),
`gh auth refresh` (1111), and the SSH-confirm loop (1072) are all **structurally
skipped** (old D4 solved the wrong problem). Token scope must include
`admin:public_key` (for `gh ssh-key add`, `run.bash:1123`) plus the repo's pinned
`REQUIRED_SCOPES` (`vars/github-required-scopes.yml` / `gh-account-setup.bash`).
Fail fast if headless + a token file is missing/empty or lacks scopes.
**`gh-account-setup.bash` also needs a token path** — its `--web` device flows
(`:297/:328`) must accept per-account token files.
**Multi-account (v1 decision):** headless supports **single account** cleanly; for

> 1 account require one token file per alias (`RUN_BASH_GITHUB_TOKEN_FILE__<alias>`)
> and **fail fast** if multiple accounts are requested without them — never a silent
> partial auth.

### D4. Sudo/become — headless REQUIRES NOPASSWD sudo, enforced at startup (B2/S2)

`run.bash` calls `sudo` directly from step 1 (`:906` `sudo dnf …`), long before the
Ansible `--ask-become-pass` logic (1539-1543). On a password-sudo box with no TTY
that first `sudo` fails "no tty present"/hangs. So headless **probes `sudo -n true`
at startup and fails fast** with clear guidance if it fails (the default cloud user
has NOPASSWD; a plain Server install may not). With NOPASSWD guaranteed, the
playbook runs **without** `--ask-become-pass` in headless. `sudo reboot` (1929) is
likewise safe only under NOPASSWD.

### D5. SSH keys — passphrase from file + ssh-agent, or passphraseless (S1)

`~/.ssh/id` (generated `965-985`, currently forces a non-empty passphrase, **no
agent anywhere**) authenticates the clone (`git clone git@github.com:` 1159). A
passphrase-protected key with no agent prompts → hang. Headless: if
`RUN_BASH_GITHUB_SSH_PASSPHRASE_FILE` is set, generate the keys with that
passphrase and **start an ssh-agent + `ssh-add`** for the clone's lifetime; if
empty/unset, generate **passphraseless** (acceptable — the key is box-local to a
dedicated provision). Never put the passphrase in `argv` where avoidable
(pre-existing S3 caveat noted).

### D6. Vault reconciliation — never auto-generate over an imported vault (B3/S1)

After the optional config import, if the resolved `localhost.yml` contains any
`!vault` value, `RUN_BASH_VAULT_PASSWORD_FILE` is **REQUIRED** and is **verified**
(`verify_vault_password`, `run.bash:846`) — mismatch ⇒ fail fast, never the
interactive `prompt_verified_vault_password` abort-loop (1405/1418).
Auto-generate is allowed **only** when no `!vault` is present; the generated
password (written `0600` to `vault-pass.secret`, 1444) is **surfaced** headless
(stderr + stated path) so an ephemeral box's encrypted config is not left
permanently undecryptable.

### D7. Failure & public-post semantics — fail fast, never auto-post (B4/S4)

Headless + non-zero `main_exit_code` ⇒ **`exit $main_exit_code`** (the desktop
"continue despite failure?" default-yes at 1557 must NOT apply headless — else a
failed provision reports success, `exit 0` at 1933). Both "create a GitHub issue?"
gates (603/661/1552, default `n`) resolve to **No** headless — no unattended path
posts box specifics to the **public** tracker (`sanitize_error_log` does not scrub
`RUN_BASH_*`/vault values anyway).

### D8. Non-root entrypoint (B4-security)

`run.bash` refuses root (`:124`); cloud-init `runcmd` is root. Headless must run as
the **non-root** target user (`sudo -u <user> -i …`). Secret **files** (D2) cross
that boundary cleanly with no env-stripping issue — a further reason for the
file-pointer model. `--help-run-headless` shows the exact cloud-init `write_files`
(0600 secrets) + `runcmd` drop-to-user invocation.

### D9. Read-prompt neutralisation + `headless_fail` (stderr-clean)

For the remaining **`read`-based** prompts (identity 1343-1345/1373-1375, hostname
1995, `RUN_BASH_GITHUB_ACCOUNTS` validation, config-repo `promptChoice`, optional
menu, projects-restore, reboot): make each helper headless-aware — return the env
value/default, or call `headless_fail <VAR> <what>` (fail fast, rule 11).
`RUN_BASH_GITHUB_ACCOUNTS` runs the **same** validation as
`prompt_github_accounts_yaml` and fails fast on a bad entry (never loops, S6).
`confirm()` gets an EOF guard (S2). **Stderr hygiene (S5):** `headless_fail` and all
status go to **stderr**; helper stdout stays the captured value only, or
`user_email=$(…)`/the YAML write (1381) get corrupted.

### D10. Implementation constraints

No **new** `2>/dev/null` / `|| true` / `sed` (hooks daemon blocks them on new
content — use capture-to-var probes, S7). **Bump `RUN_BASH_VERSION`** (line 6) on
every edit. QA: `./scripts/qa-all.bash`.

### D11. `--help` / `--help-run-headless`

`--help` gains a one-line pointer to server/cloud + `--help-run-headless`.
`--help-run-headless` prints: the full env contract (non-secret env + secret
file-pointers), the NOPASSWD-sudo + non-root requirements, the GitHub-token
scopes, a copy-paste cloud-init example, the fail-fast rules, and the
desktop-vs-server/cloud model. **(v3 correction:** the cloud example must NOT put
secrets in `write_files` — see V3.1.)

## Design v3 — round-2 deltas (bounded fixes on the v2 architecture)

Round 2 confirmed the v2 *architecture* is sound but found residual hangs, a leak
v2 reintroduced, and a blocker in v2's own cloud secret-delivery. These deltas
apply on top of D1-D11:

- **V3.1 — Secrets OUT-OF-BAND on cloud, never `write_files` (BLOCKER, AuditSec #1).**
  cloud-init `write_files` content lives *inside* user-data, which the metadata
  service serves world-locally **forever** — so v2's "write the 0600 file via
  write_files" re-persists the plaintext exactly where Decision 4 forbids. **Cloud:**
  secrets are fetched **out-of-band inside `runcmd`** (secrets manager / vendor-data
  / attached media), never user-data/write_files. **Plain Server (SSH-in):** the
  operator places the `0600` files directly — no user-data involved — which is fine.
  `--help-run-headless`'s cloud example shows the out-of-band pattern.
- **V3.2 — Token-scope completeness gate before BOTH refresh sites (both auditors'
  #1 pick).** Token pre-auth (D3) skips `gh auth login` but NOT the scope-triggered
  interactive `gh auth refresh` at `run.bash:1111` **and** `gh-account-setup.bash:328`
  → those still HANG on an under-scoped token. Add an explicit headless check of the
  token against the **full** `vars/github-required-scopes.yml` + `admin:public_key`,
  **before both** sites, fail-fast naming missing scopes. Single-account also routes
  through `--setup-all` (guard at 1487 is `grep github_accounts`, always written) so
  it needs the same gate.
- **V3.3 — Vault: require file, never auto-generate/print headless (both #4).** D6's
  "surface the generated password to stderr" leaks it into
  `/var/log/cloud-init-output.log` + the serial console. Headless **requires**
  `RUN_BASH_VAULT_PASSWORD_FILE` and fails fast; **no auto-generate**, never print
  secret bytes (path only). Resolves the S5/D6 tension.
- **V3.4 — EXIT-trap cleans every secret file (AuditSec #2).** The trap (`:56`) only
  removes `/tmp/.github_ssh_pp`; a `set -e` abort leaves the new `*_FILE` secrets on
  a persistent box. Extend the trap to `rm -f` all secret paths (trap-visible
  array). Also fix the pre-existing un-trapped passphrase-stripped key copy at
  `gh-account-setup.bash:211-232`.
- **V3.5 — gh persists the token in `~/.config/gh/hosts.yml` (AuditSec #3).**
  Deleting the token file is false assurance. Recommend a **fine-grained short-lived
  PAT + `gh auth logout` at end**; document `vault-pass.secret` as a standing key on
  a persistent box.
- **V3.6 — ssh-agent lifecycle + argv passphrase (AuditCoverage a / S3 — OWNER
  TRADEOFF).** `ssh-add` has no argv passphrase flag → needs an `SSH_ASKPASS` helper
  (itself a passphrase-echoing surface) or it hangs; the agent must live from keygen
  through the `git pull` at `:1508` (not just the clone `:1159`) and be torn down
  (`ssh-agent -k`); `ssh-keygen -P/-N` expose the passphrase in `/proc/PID/cmdline`
  regardless of source. Two options — **(a)** ssh-agent + `SSH_ASKPASS`, keep the
  passphrase; **(b)** headless generates a **passphraseless** `~/.ssh/id` (simpler,
  no askpass/agent, no argv leak) — but that leaves a standing passphraseless GitHub
  **auth** key at rest. **Owner decision required** (see below).
- **V3.7 — `sudo -k -n true`, document NOPASSWD:ALL (both #5).** Use `-k` (avoid a
  cached-timestamp false pass); a command-scoped NOPASSWD passes `true` but fails
  `dnf` → document the NOPASSWD:ALL assumption.
- **V3.8 — Optional-playbook failures propagate headless (AuditCoverage B4-resid).**
  `run_playbook` (1593-1600) swallows failures; D7 covers only the main playbook.
  Headless optional failures must reach a non-zero final exit.
- **V3.9 — Passphrase handoff to gh-account-setup via file, not env (AuditCoverage
  e).** `run.bash:1492` passes `GITHUB_SSH_PASSPHRASE=` as literal env → child
  inherits via `/proc/PID/environ` (the Decision-4b exposure). Teach
  `gh-account-setup.bash` a `*_FILE` input (or a documented, scoped env exception).

**Open owner decisions surfaced by round 2** (do not block writing v3; block
freeze/execute):

1. **Confirm Decision 4 + V3.1** — secrets as `0600` file-pointers, and on cloud
   fetched out-of-band (never user-data/write_files).
2. **V3.6 ssh key at rest** — (a) ssh-agent+askpass keep passphrase, or (b)
   passphraseless key. Recommend (b) for a dedicated provision box (simplest, no new
   askpass exposure), documented.
3. **Reconsider Decision 2 (GitHub-mandatory) for headless** — the loop shows every
   hardest residual (token-scope gate, PAT at rest in gh config, SSH key at rest,
   passphrase-in-argv, askpass helper) stems from GitHub being mandatory. For a pure
   cloud/server box that needs no GitHub identity, **skip-GitHub-when-unset** removes
   most of the remaining complexity and attack surface. New evidence justifies
   re-raising.

## Tasks

### Phase 1: Plan & hostile review (this session)

- [x] ✅ **Task 1.1**: Confirm Fedora Cloud needs no new scope; establish that the
  gap is `run.bash`'s interactive-only bootstrap, not the Ansible layer.
- [x] ✅ **Task 1.2**: Owner decisions captured — env-var input, GitHub always
  required, plan-first + hostile opus review loop + execute (see Technical
  Decisions).
- [x] ✅ **Task 1.3**: Author this plan (problem, goals, non-goals, draft design).
- [x] ✅ **Task 1.4 round 1**: Two independent Opus auditors (coverage lens +
  security lens) hostile-audited the draft. **Both independently returned
  NOT-implementation-ready**, converging on the same load-bearing gaps (GitHub
  device-code auth, direct-sudo password, ssh-agent-less clone, user-data secret
  persistence, failed-playbook-exits-0, vault footguns). Judged in JOURNAL; design
  hardened to **v2** (D1-D11). No finding dismissed as invalid except "git commit
  identity" (correctly unfounded — run.bash does no local commit).
- [x] ✅ **Task 1.4 round 2**: Re-ran both auditors against **Design v2**. Verdict:
  architecture sound; bounded residual hangs + one reintroduced leak + a blocker in
  v2's own cloud secret-delivery (`write_files` = user-data). All confirmed and
  folded into **Design v3 deltas (V3.1-V3.9)**. Convergence is close — remaining
  items are bounded edits, except 3 genuine **owner decisions** the loop surfaced.
- [ ] 🔄 **Task 1.5**: Resolve the 3 owner decisions (Decision 4/V3.1 confirm;
  V3.6 ssh-key-at-rest; reconsider Decision 2 GitHub-mandatory), then either a final
  confirming round or freeze the design as the implementation spec.

### Phase 2: Implementation (post-convergence)

- [ ] ⬜ **Task 2.1**: Headless trigger + arg parsing (`--headless`/`--interactive`);
  startup **NOPASSWD-sudo probe → fail fast** (D4); bump `RUN_BASH_VERSION`.
- [ ] ⬜ **Task 2.2**: Secret **file-pointer** plumbing (D2) — read `*_FILE` into
  locals, `0600`-verify, delete after use; `headless_fail` (stderr-clean, D9).
- [ ] ⬜ **Task 2.3**: **GitHub token auth** (D3) — `gh auth login --with-token` in
  run.bash **and** `gh-account-setup.bash`; scope check; single-account v1 +
  fail-fast on unsupported multi-account.
- [ ] ⬜ **Task 2.4**: SSH keys (D5) — passphrase-from-file + ssh-agent for the
  clone, or passphraseless; vault reconciliation (D6, verify-or-fail, no
  auto-generate over `!vault`).
- [ ] ⬜ **Task 2.5**: Read-prompt neutralisation (D9) — identity, hostname
  (`RUN_BASH_HOSTNAME`), `RUN_BASH_GITHUB_ACCOUNTS` validation, config import,
  optional menu, projects restore, reboot; env-gate + safe defaults.
- [ ] ⬜ **Task 2.6**: Failure semantics (D7) — headless main-playbook failure ⇒
  non-zero exit; both public-tracker gates resolve No; pass
  `RUN_BASH_PROVISIONING_PROFILE` to `playbook-main.yml` when set.
- [ ] ⬜ **Task 2.7**: `--help` + `--help-run-headless` (D11) with the cloud-init
  `write_files`+`runcmd` drop-to-user example.
- [ ] ⬜ **Task 2.8**: `./scripts/qa-all.bash`; plan-local `acceptance.bash`
  (headless fail-fast proofs — missing token/email/accounts, non-NOPASSWD probe,
  failed-playbook exit code; desktop path unchanged). No new
  `2>/dev/null`/`|| true`/`sed` (D10).
- [ ] ⬜ **Task 2.9**: Docs — `docs/` headless/server/cloud section + `README.md`
  cross-link.

### Phase 3: Verification (HOST — not CCY container)

- [ ] ⬜ **Task 3.1**: On a real/VM Fedora **Server or Cloud** box, run `run.bash`
  headless via env and confirm end-to-end provisioning with zero prompts.
- [ ] ⬜ **Task 3.2**: Confirm desktop interactive path unchanged (zero regression).

## Dependencies

- **Depends on**: Plan 00061 (Headless / Server Provisioning — Ansible-layer scope
  split + `provisioning_profile` auto-detect). Reused verbatim; not modified here.

## Technical Decisions

### Decision 1: Env vars as the single headless input channel

**Context**: Headless mode needs config without a TTY; options were env vars, an
answers file, or Ansible extra-vars passthrough.
**Decision** (owner, 2026-07-23): **Environment variables** (`RUN_BASH_*`). Rationale:
12-factor, drops directly into cloud-init `user-data` and CI, matches the existing
`gh-account-setup.bash` env pattern, no new file format to parse or secure. An
answers file is an explicit **Non-Goal** for now.
**Date**: 2026-07-23

### Decision 2: GitHub setup stays mandatory in every mode

**Context**: A bare server arguably needs no GitHub identity; option was to skip
GitHub if unset headless.
**Decision** (owner, 2026-07-23): **Always require GitHub.** Headless supplies
accounts via `RUN_BASH_GITHUB_ACCOUNTS`; missing → fail fast. SSH stays the only
auth path. Keeps behaviour uniform with the desktop flow and every provisioned box
gets a working GitHub identity.
**Date**: 2026-07-23

### Decision 3: Plan-first, then hostile Opus review loop, then execute

**Context**: `run.bash` is a 1941-line critical bootstrap; a regression bricks a
provision.
**Decision** (owner, 2026-07-23): Write this plan, then run an **adversarial review
loop with independent Opus agents** (mirroring Plan 00061's Sonnet↔Fable↔judge
convergence) hostile-auditing the design before any code is written. Only execute
once the loop converges.
**Date**: 2026-07-23

### Decision 4: Secrets travel as `0600` **file pointers**, not literal env values (round-1 refinement of Decision 1)

**Context**: Round-1 security audit proved that literal secrets in env are exposed
two ways on exactly the cloud-init use case Decision 1 targets: (a) cloud-init
persists `user-data` to `/var/lib/cloud/instance/user-data.txt` + the metadata
service, world-readable **indefinitely** (`unset` cannot unwrite it); (b) every
child process inherits an exported secret via `/proc/PID/environ` during the
multi-minute run.
**Decision**: Keep Decision 1's env-driven contract, but for **secret material**
(GitHub token, vault password, SSH passphrase) the env var carries a **path to a
`0600` file**, not the secret bytes. cloud-init lays the files down via
`write_files` (`permissions: '0600'`); run.bash reads each into a `local` and
deletes it after use. Mirrors the repo's existing `VAULT_PASS_FILE`
(`gh-account-setup.bash:21`). This is a **refinement, not a reversal** — the path
still arrives via env; only the secret bytes stay out of the environment and
user-data. **Owner FYI to confirm at round-2 sign-off** (does not block the loop).
**Date**: 2026-07-23

### Decision 5: Headless requires NOPASSWD sudo + a scoped GitHub token; single-account in v1

**Context**: Round-1 coverage audit proved the load-bearing interactive walls are
not bash prompts: direct `sudo` (password) and `gh` device-code OAuth.
**Decision**: Headless **requires** (a) **NOPASSWD sudo** — probed at startup,
fail-fast if absent (the default cloud user has it); and (b) a **scoped GitHub
token file** (`gh auth login --with-token`) — GitHub auth cannot be done via any
`read` helper. Headless v1 supports a **single GitHub account**; multiple accounts
require one token file per alias and **fail fast** otherwise (no silent partial
auth). Both are documented preconditions in `--help-run-headless`, not silent
assumptions.
**Date**: 2026-07-23

## Success Criteria

- [ ] `run.bash` provisions a headless Fedora **Server/Cloud** box end-to-end with
  **zero interactive prompts**, driven by `RUN_BASH_*` env (non-secret) + `0600`
  secret files.
- [ ] Every missing **required** value / unmet precondition (email, GitHub
  accounts, token file, NOPASSWD sudo, vault-over-`!vault`) **fails fast** naming
  the exact fix — **never hangs**.
- [ ] GitHub auth works non-interactively via a scoped token; SSH-only git auth.
- [ ] A **failed** main playbook makes a headless run **exit non-zero** (never
  reports success).
- [ ] No secret bytes enter the environment or cloud-init `user-data`.
- [ ] Desktop interactive `./run.bash` is **unchanged** (zero regression).
- [ ] `--help` points to it; `--help-run-headless` documents the full contract +
  cloud-init `write_files`/`runcmd` example.
- [ ] `./scripts/qa-all.bash` passes; `RUN_BASH_VERSION` bumped; no new
  `2>/dev/null`/`|| true`/`sed`.

## Risks & Mitigations

| Risk                                                     | Impact | Probability | Mitigation                                                                                                       |
| -------------------------------------------------------- | ------ | ----------- | ---------------------------------------------------------------------------------------------------------------- |
| GitHub device-code auth cannot run headless (round-1 B1) | H      | H→mitigated | D3: `gh auth login --with-token` from a scoped token file, in run.bash + gh-account-setup.bash; fail-fast checks |
| Secret leak via cloud-init user-data / `/proc/environ`   | H      | H→mitigated | D2/Decision 4: `0600` file pointers, bytes never in env/user-data; read-to-local, delete after use               |
| Password-sudo box: first direct `sudo` hangs headless    | H      | M→mitigated | D4: startup NOPASSWD probe → fail fast; playbook run drops `--ask-become-pass` headless                          |
| Auto-generate vault password corrupts imported `!vault`  | H      | M→mitigated | D6: require+verify vault password when `!vault` present; auto-generate only on a fresh box, surfaced             |
| Failed main playbook reports success (`exit 0`)          | H      | M→mitigated | D7: headless failure ⇒ `exit $main_exit_code`; continue-anyway is desktop-only                                   |
| ssh-agent-less passphrase clone hangs                    | H      | M→mitigated | D5: passphrase-from-file + ssh-agent for the clone, or passphraseless                                            |
| Headless auto-detect fires on an accidental desktop pipe | H      | L           | Explicit `RUN_BASH_*` (or `--headless`) required alongside no-TTY; `--interactive` override; smoke-test caveat   |
| Desktop regression from shared helpers                   | H      | L           | Headless branch additive/guarded; desktop path byte-unchanged; QA + host regression (Task 3.2)                   |

## Delivery & Milestones

<!-- Curated milestones + delivery commit hashes only. Blow-by-blow in JOURNAL/. -->

- Plan created; Fedora-Cloud/no-new-scope confirmed; owner decisions recorded;
  hostile Opus review loop commissioned.
