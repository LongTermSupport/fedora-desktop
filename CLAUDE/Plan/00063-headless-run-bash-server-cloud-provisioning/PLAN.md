# Plan 00063: Headless `run.bash` — Server & Cloud Provisioning

**Status**: In Progress — code-complete and **in production use**: the owner reports
the headless path provisions the hosts of a separate infrastructure repository.
Phase 3 is partly discharged by Plan 00110's VM acceptance lab (end-to-end run and
failure propagation, each tick carrying its run id). Tasks 3.3 and 3.4 stay open
and are to be discharged properly rather than asserted: 00110's `DESIGN.md:1554-1555`
names the opt-in, human-gated `server-github-token` scenario as the route for exactly
those criteria. That scenario is now **built** (Plan 00121, `e40d5c60`); these tasks
wait only on a human running it on the HOST with a scoped PAT on a throwaway account.
**Created**: 2026-07-23
**Owner**: joseph
**Priority**: Medium

> Lean plan. The full pre-slimming text is preserved verbatim in
> [PLAN_archive.md](PLAN_archive.md). The frozen implementation spec is in
> [DESIGN.md](DESIGN.md), the owner decisions in [DECISIONS.md](DECISIONS.md),
> and the round-by-round audit and implementation narrative in `JOURNAL/`.

## Overview

Plan 00061 made the Ansible layer headless-capable: every play declares a
`scope:` and self-guards on an auto-detected `provisioning_profile`, so
`playbook-main.yml` on a Fedora Server or Cloud box already installs only the
general plus server subset.

The remaining blocker was `run.bash`, the bootstrap installer. It was
desktop-shaped and fully interactive: identity, GitHub accounts, vault password,
SSH passphrase, config-repo choice, optional-playbook menu and reboot were all
gathered from terminal prompts, so it could not provision a headless server or an
unattended cloud instance and violated `CLAUDE/InteractiveScripts.md` rule 11.

This plan adds a headless execution mode to `run.bash`, driven by `RUN_BASH_*`
environment variables (non-secret values) and `0600` secret files (paths via
env), so the same script provisions a desktop interactively or a server/cloud box
unattended. GitHub setup remains mandatory and is fed from a scoped token. The
design was hardened through three rounds of hostile review before implementation
(Decision 3) and was code-complete at `run.bash` v1.10.0. Verification is now
partly discharged: Plan 00110's VM lab proves the headless end-to-end run and the
failure-propagation criteria (see the Phase 3 ticks and their run ids). What
remains needs a secret no VM scenario carries — see the Status note above.

## Goals

- A headless, non-interactive mode for `run.bash` that provisions a Fedora
  Server or Cloud box end-to-end with zero prompts, driven by env vars and
  secret files.
- Fail fast, never hang, when a required value is missing or a precondition is
  unmet, naming the exact env var or fix.
- GitHub setup stays mandatory in every mode; headless supplies the account,
  token, SSH passphrase and vault password non-interactively.
- The interactive desktop path is unchanged (zero regression).
- Headless auto-detects the same way the Ansible layer does and passes a
  `provisioning_profile` override through to the playbook.
- Expanded `--help` plus a `--help-run-headless` deep-dive documenting the env
  contract, a cloud-init example and the fail-fast rules.
- User-facing docs for the server/cloud story, cross-linked from `README.md`.

## Non-Goals

- No new play `scope`: Fedora Cloud correctly resolves to
  `provisioning_profile: server`.
- No changes to the Ansible layer, play scoping or `provisioning_profile`
  detection (Plan 00061's delivery, reused verbatim).
- No re-architecture of the interactive desktop flow; headless is added alongside
  it through the existing prompt-helper chokepoints.
- No remote-driven provisioning: `run.bash` is `connection: local` and runs on
  the target.
- No kickstart or ISO changes (Plan 00018/00022 territory).
- No answers-file mechanism (Decision 1).
- The `RUN_BASH_GITHUB_ACCOUNTS=none` HTTPS-only path was out of scope here
  (Decision 2, Task 1.6). It has since **shipped** under Plan 00082
  (`run.bash:293`), and every VM acceptance scenario now depends on it — so this
  is a record of what this plan did not do, not a description of a missing
  feature.

## Tasks

### Phase 1: Plan and hostile review

- [x] ✅ **Task 1.1**: Confirm Fedora Cloud needs no new scope; establish that the
  gap is `run.bash`'s interactive-only bootstrap, not the Ansible layer.
- [x] ✅ **Task 1.2**: Owner decisions captured: env-var input, GitHub always
  required, plan-first plus hostile review loop ([DECISIONS.md](DECISIONS.md)
  1–3).
- [x] ✅ **Task 1.3**: Author the plan (problem, goals, non-goals, draft design).
- [x] ✅ **Task 1.4 round 1**: Two independent auditors (coverage and security
  lenses) found the draft not implementation-ready; design hardened to v2
  (D1–D11 in [DESIGN.md](DESIGN.md)).
- [x] ✅ **Task 1.4 round 2**: Re-audit of v2; architecture sound, residual hangs
  and one cloud secret-delivery blocker folded into V3.1–V3.9.
- [x] ✅ **Task 1.5a**: Three owner decisions resolved (Decisions 2, 4, 6);
  canonical invocation documented.
- [x] ✅ **Task 1.5b**: Round-3 focused review; no new architecture breaks, only
  bounded hardening V3.10–V3.15.
- [x] ✅ **Task 1.5c**: Design frozen, then re-opened when the delayed round-3
  coverage audit found the empty-GitHub path breaks `playbook-main.yml` via two
  latent server-profile play bugs (V3.14). No bug shipped.
- [x] ✅ **Task 1.6**: Owner decision: defer the empty-GitHub path. v1 is
  GitHub-token-required; `RUN_BASH_GITHUB_ACCOUNTS=none` fails fast in
  `headless_preflight` naming the follow-up (run.bash v1.9.1). Design re-frozen.
  **Superseded**: Plan 00082 implemented that path; `none` is now an accepted
  value with its own guards (`run.bash:293`), not a fail-fast.

### Phase 2: Implementation

- [x] ✅ **Task 2.1**: Headless trigger, arg parsing (`--headless` /
  `--interactive`, auto-detect), version bump, and the startup `sudo -k -n true`
  NOPASSWD probe plus non-root check in `headless_preflight` (v1.8.0, v1.9.0).
- [x] ✅ **Task 2.2**: Secret file-pointer plumbing (D2). `hl_resolve_secret` with
  the V3.10 guardrails, literal-env `unset` before the first child, the
  `set -u`-safe `HL_SECRET_FILES` trap, stderr-clean `headless_fail`.
  Delete-after-use and ssh-agent teardown discharged by Task 3.4.
- [x] ✅ **Task 2.3**: GitHub token auth (D3). Headless
  `gh auth login --with-token` block in `run.bash` (PAT via stdin, never argv)
  ahead of the interactive one (v1.9.4); `gh-account-setup.bash` fails loud under
  `RUN_BASH_HEADLESS` instead of opening a device flow (v1.10.0). Discharged by
  Task 3.3.
- [x] ✅ **Task 2.4**: SSH keys and vault (D5, D6). Passphrase file required in
  preflight (v1.9.2); `hl_ssh_agent_start` / `hl_ssh_agent_stop` with the transient
  `SSH_ASKPASS` helper and `hl_cleanup` EXIT trap (v1.9.4); `hl_reconcile_vault`
  provided-or-fail, never auto-generate (v1.9.5). The agent load, clone and
  teardown end-to-end discharged by Task 3.4.
- [x] ✅ **Task 2.5**: Read-prompt neutralisation (D9). Done: `hl_abort` backstop
  at the top of every shared prompt helper (v1.9.3); every call site
  headless-branched: hostname, `hl_write_localhost_yml`, vault,
  `hl_run_optional_playbooks`, projects restore, reboot (v1.10.0). End-to-end
  verified by Plan 00110's lab: a headless run over SSH with no TTY completed
  with `RUN-BASH-EXIT 0` (run `20260913T170901Z-server-fast-provision`).
- [x] ✅ **Task 2.6**: Failure semantics (D7): a headless main-playbook failure
  aborts loud and exits non-zero, no public-tracker prompt, no continue-anyway;
  `RUN_BASH_PROVISIONING_PROFILE` forwarded via `-e` (v1.10.0).
- [x] ✅ **Task 2.7**: `--help` expanded and `--help-run-headless` documents the
  full v1 token-required contract with an out-of-band cloud-init example (v1.8.0,
  retuned v1.9.1).
- [x] ✅ **Task 2.8**: QA and acceptance. Done: `./scripts/qa-all.bash` green each
  slice; plan-local [acceptance.bash](acceptance.bash) passes 10 preflight
  fail-fast gates in-container via `runuser -u nobody`; no new `2>/dev/null`,
  `|| true` or `sed` (D10). The end-to-end execution assertions now run in
  Plan 00110's lab (`server-fast-provision` and the three negative scenarios;
  `acceptance.bash` there: VERDICT PASS, 2026-09-13).
- [x] ✅ **Task 2.9**: Docs: `docs/headless-provisioning.md` (reference) and
  `docs/headless-server-install.md` (runbook), cross-linked from `docs/README.md`,
  `docs/installation.md` and the root `README.md`.

### Phase 3: Verification (HOST, not the CCY container)

- [x] ✅ **Task 3.1**: On a real or VM Fedora Server or Cloud box, run `run.bash`
  headless via env and confirm end-to-end provisioning with zero prompts.
  Done by Plan 00110: a fresh Fedora 44 Cloud Base guest, `run.bash` fetched at
  a pinned pushed commit and run headless over a non-interactive SSH session
  (`RUN_BASH_HEADLESS=1`, `RUN_BASH_GITHUB_ACCOUNTS=none`, a `0600` vault
  password file), `RUN-BASH-EXIT 0`, PLAY RECAP with work done, 13 in-guest
  checks green — run `20260913T170901Z-server-fast-provision`; repeated through
  the container-to-host bridge as `20260913T181420Z-server-fast-provision`.
  The Anaconda-installed Server variant is `server-full-provision` (Plan 00110
  Phase 3b).
- [ ] 🧑 **Task 3.2 — HUMAN AT A TERMINAL, permanently**: Confirm the desktop
  interactive path is unchanged. **This task will never be discharged by a script, and
  should not be read as work waiting to be automated.** An automated scenario cannot
  answer "does this still prompt correctly", because driving the prompts is what makes a
  run non-interactive. `desktop-fresh-install` does not cover it: the lab drives every
  guest with `RUN_BASH_HEADLESS=1` (`files/home/.local/bin/vmtest:918`), so it exercises
  the interactive path exactly as little as the server scenarios do. The only thing that
  closes it is a person running `run.bash` on a desktop and reading the prompts
- [ ] 🔄 **Task 3.3**: The GitHub token path (Task 2.3). `gh auth login --with-token`
  with a scoped PAT on stdin, then `gh-account-setup.bash` failing loud under
  `RUN_BASH_HEADLESS` instead of opening a device flow.
  - [x] ✅ The device-flow guard, by inspection: `gh-account-setup.bash:287-291`
    errors, names `RUN_BASH_GITHUB_TOKEN_FILE`, and returns **before** the
    device-code flow at `:305`
  - [x] ✅ The PAT reaches `gh` on **stdin, never argv** — `run.bash:2284`
  - [ ] 🔄 End-to-end with a real token. **Route: the opt-in `server-github-token`
    scenario**, now **built** by Plan 00121 (`e40d5c60`) and awaiting a human run on
    the host. The default six cannot reach it — all run
    `RUN_BASH_GITHUB_ACCOUNTS=none` (`vmtest:920`, `:1229`), the branch that skips this
    code, while the opt-in route supplies the account, token and passphrase files at
    `:1230-1234` — but that is a property of those scenarios, not of the lab: 00110's
    `DESIGN.md:1554` already assigns this criterion to `server-github-token`. No agent
    creates or handles the PAT
- [ ] 🔄 **Task 3.4**: The SSH path (Tasks 2.2 and 2.4). Production use proves the
  clone works; it does **not** prove any of the obligations below, each of which is
  invisible from a successful run and needs its own assertion:
  - [ ] 🔄 A passphrase file accepted in preflight
  - [ ] 🔄 `hl_ssh_agent_start` / `hl_ssh_agent_stop` bracketing the run, with the
    agent gone afterwards. The **hole is closed** in run.bash 1.21.0 (`ee501aaf`):
    `hl_ssh_agent_stop:485` now uses `/proc/<pid>` to tell "already gone" from "the
    kill failed and it still holds an unlocked key", and aborts on the second.
    Unit-tested by `scripts/test-run-bash-ssh-agent-teardown.bash`. What remains is
    the end-to-end observation in a guest, which `server-github-token` check 6 makes
  - [ ] 🔄 The transient `SSH_ASKPASS` helper removed afterwards
  - [ ] 🔄 `hl_cleanup` firing on EXIT, and the secret files unlinked after use
    (the wording half of this is **settled**: `hl_cleanup` used `rm -f` while four
    comments and three docs said "shred". Resolved in favour of the CODE, not the word —
    coreutils' own caution is that "shred assumes the file system and hardware overwrite
    data in place", and neither platform here does: these files come from `mktemp`, /tmp
    on Fedora is tmpfs, and the default root filesystem is btrfs, which is copy-on-write.
    `shred` would rewrite blocks that are not where the secret is and report success, so
    calling it to make the word true while the effect stayed false would have been worse
    than the mismatch. The protection is 0600 plus a short lifetime, and the docs now say
    that. What still needs the guest is the OBSERVATION that the unlink actually happened
    on every exit path)
  - Same route as 3.3: with `GITHUB_ACCOUNTS=none` no key is ever loaded, so the
    default scenarios never start the agent and the assertions would pass by
    absence — 00110 `DESIGN.md:1604-1607` says exactly this

> **Where Phase 3 stands.** Task 3.1 and the failure-propagation criteria are
> discharged by Plan 00110's lab. Tasks 3.3 and 3.4 are **not** discharged by
> production use: a real deployment proves the clone works, which is not the same as
> proving the agent was torn down, the askpass helper removed, or the secret files
> unlinked — none of which is visible from a run that succeeded.
>
> The **default six** scenarios cannot reach them, because each hardcodes
> `RUN_BASH_GITHUB_ACCOUNTS=none` and the assertions would pass by absence (00110
> `DESIGN.md:1590-1596`, `:1604-1607`). That is a property of those scenarios, not of
> the lab: 00110 `DESIGN.md:1554-1555` already names the opt-in, human-gated
> `server-github-token` scenario as the route for exactly these criteria. It is now
> **built** — Plan 00121, `e40d5c60` — and what these tasks wait on is a human running
> it on the host with a scoped PAT on a throwaway account. Its checker asserts each
> obligation directly: the agent gone, the socket gone, the askpass helper gone, the
> transient passphrase file gone, and no secret bytes in any process environment, in
> cloud-init `user-data`, or on disk.
>
> There is no desktop-versus-server asymmetry: the desktop scenario sets `none` too
> (`vmtest:749`), so it exercises the GitHub paths exactly as little as the server
> ones do — and sets `RUN_BASH_HEADLESS=1`, so it does not cover Task 3.2 either.

## Dependencies

- Depends on Plan 00061 (Ansible-layer scope split and `provisioning_profile`
  auto-detect), reused verbatim.
- Follow-up: a separate plan re-enables the empty-GitHub path after fixing
  `play-github-cli-multi.yml` and `play-lxc-install-config.yml` for the server
  profile (V3.14).

## Success Criteria

- [x] `run.bash` provisions a headless Fedora Server or Cloud box end-to-end with
  zero interactive prompts, driven by `RUN_BASH_*` env plus `0600` secret files.
  (Plan 00110 run `20260913T170901Z-server-fast-provision`, Cloud Base guest.)
- [x] Every missing required value or unmet precondition (email, GitHub account,
  token file, SSH passphrase file, vault password, NOPASSWD sudo) fails fast
  naming the exact fix, never hangs. (Exercised 2026-09-14: three
  missing-value invocations each exited 1 immediately — no hang — naming the
  problem, the exact fix *and* its cloud-init form, then pointing at
  `--help-run-headless`. Verified by run and by inspection, **not** by a test; see
  the follow-up note in Phase 3.)
- [x] GitHub auth works non-interactively via a scoped token; SSH-only git auth.
  (Production use, Tasks 3.3 and 3.4.)
- [x] A failed main or optional playbook makes a headless run exit non-zero.
  (Plan 00110 negative scenarios: `server-main-playbook-fails`,
  `server-optional-playbook-fails` and `server-optional-play-missing` each
  produced `RUN-BASH-EXIT 1` for their own reason, 2026-09-13.)
- [ ] 🔄 No secret bytes enter the environment or cloud-init `user-data`. The
  environment half is evidenced: values come from `0600` file pointers,
  `run.bash:373` unsets the literal `RUN_BASH_*` forms after resolving them at
  `:368`, and the PAT reaches `gh` on **stdin**, never argv (`run.bash:2284`). The
  `user-data` half is not, and `hl_resolve_secret:134-140` still accepts a literal
  on a non-cloud box. 00110 `DESIGN.md:1553` assigns this criterion to
  `server-github-token`: with no PAT and no passphrase in the guest, grepping the
  default scenarios for secrets proves nothing, because the thing being searched for
  was never supplied
- [ ] ⬜ Desktop interactive `./run.bash` is unchanged. Needs a human at a terminal;
  no scenario covers it, since the lab forces `RUN_BASH_HEADLESS=1`
- [x] `--help` points to it; `--help-run-headless` documents the full contract
  and an out-of-band cloud-init example. (Checked 2026-09-14: three cross-references
  from `--help`; the deep-dive runs to 137 lines and documents **every** `RUN_BASH_*`
  input — cross-checked against the full set extracted from `run.bash` — plus the
  cloud-init example and the `0600` requirement.)
- [x] `./scripts/qa-all.bash` passes; `RUN_BASH_VERSION` bumped; no new
  `2>/dev/null`, `|| true` or `sed`. (QA green at 860 files; version 1.20.2;
  `|| true` zero occurrences. The `2>/dev/null` and `sed` counts are pre-existing
  rather than new — the criterion is about additions.)

## Delivery & Milestones

<!-- Curated milestones + delivery commit hashes only. Blow-by-blow in JOURNAL/. -->

- Plan created; no-new-scope confirmed; hostile review loop commissioned
  (`47e3e757`).
- Design v2 after round 1 (`ccff44f9`); v3 deltas after round 2 (`d379b5b9`);
  convergence and freeze after round 3 (`a58bddec`).
- Empty-GitHub path deferred, v1 token-required (`83cdb2c1`, run.bash v1.9.1).
- Headless slices: help and trigger v1.8.0 (`79742fa4`); preflight v1.9.0
  (`68c9305b`); backstop v1.9.3 (`0ef1a5be`); GitHub/SSH mechanics v1.9.4
  (`9f3195e4`); localhost.yml and vault v1.9.5 (`b88bc544`); stop flipped,
  headless live v1.10.0 (`c62c180a`).
- User-facing docs (`383365b0`, `c0854cf9`).
- Plan slimmed: history to PLAN_archive.md, spec to DESIGN.md and DECISIONS.md.
