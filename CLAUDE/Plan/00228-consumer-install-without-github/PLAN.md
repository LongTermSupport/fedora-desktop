# Plan 00228: consumer install without GitHub

**Status**: Not Started (owner Decisions 1-10 open; each has a default)
**Created**: 2026-10-26
**Owner**: joseph
**Priority**: Medium

## Overview

The owner wants to set up fedora-desktop for people who do not write code: run
`run.bash`, but with no GitHub account, no SSH key on GitHub, no private config repo, and
none of the developer tooling. The public repository is fetched anonymously over HTTPS and
only the plays a non-developer needs are installed.

Most of the no-GitHub path already exists, but only for unattended runs: Plan 00082 added
`RUN_BASH_GITHUB_ACCOUNTS=none` to headless mode (HTTPS clone, no keygen, no `gh` auth,
`github_accounts: {}`). An attended run still requires GitHub at a dozen points and rewrites
the checkout's origin to SSH on every run. Nothing yet selects plays by *who uses the
machine*: `provisioning_profile` (desktop|server) says what kind of machine it is, which is a
different question.

This plan adds an **install audience** (`developer`, the default and today's behaviour, or
`consumer`), chosen once, recorded in `localhost.yml`, and honoured by `run.bash`, by every
play, by the optional-play menu, and by later updates. The research behind it, including the
full list of GitHub touchpoints and the play-by-play classification, is in
[RESEARCH-github-touchpoints-and-plays.md](RESEARCH-github-touchpoints-and-plays.md).

## Goals

- A consumer install run through `run.bash`, attended or headless, that needs no GitHub
  account and never calls `gh`, `ssh-keygen` or the GitHub API.
- The repository is fetched and updated anonymously over HTTPS, and stays HTTPS on every
  re-run.
- Only the consumer and shared plays run; developer-only plays end themselves with a
  stated reason, the same way the scope guard does.
- Fail Fast and IaC only throughout: a contradictory request (consumer plus a GitHub
  account, consumer on a server profile, developer-to-consumer) stops with a clear message.
- Any new logic that does not fit `run.bash`'s ratchet record is written in Go.
- A non-developer can follow the docs from a fresh Fedora Workstation to a finished,
  updated machine.

## Non-Goals

- A consumer kickstart variant of `fedora-install/ks.cfg` (Decision 9 default; a follow-up).
- Creating local user accounts (Decision 4 default: the Fedora installer owns that).
- Unattended self-update for consumer desktops (Decision 5 default; would extend Plan 00137).
- Uninstalling developer tooling from a box that already has it (Decision 6).
- Changing the developer path in any way beyond sharing the predicate with it.

## Context & Background

Related plans, linked rather than duplicated:

- [Completed/00082-run-bash-github-accounts-none](../Completed/00082-run-bash-github-accounts-none/)
  built the headless no-GitHub path this plan generalises to attended runs.
- [00063-headless-run-bash-server-cloud-provisioning](../00063-headless-run-bash-server-cloud-provisioning/)
  owns headless mode; the consumer audience adds one knob to its input contract.
- [00061-headless-server-provisioning](../00061-headless-server-provisioning/) owns the
  `scope` guard and `provisioning_profile`; the audience guard copies its shape.
- [00119-headless-github-ssh-443-input](../00119-headless-github-ssh-443-input/) is
  GitHub SSH routing, which a consumer never uses; the only contact is that
  `RUN_BASH_GITHUB_SSH_443=1` is refused for a consumer, as it already is for `none`.
- [00137-unattended-server-self-update](../00137-unattended-server-self-update/) and
  [00214-update-all-changed-plays](../00214-update-all-changed-plays/) own the update
  machinery; this plan only makes `run.bash` re-runs and `run.bash --changed` work on an
  HTTPS consumer checkout (Decision 5).
- [00182-run-bash-private-plays-and-config-clone](../00182-run-bash-private-plays-and-config-clone/)
  assumes a private config repo; the consumer path must leave its hooks untouched.

Constraints the design must meet (detail in the research doc, section 3): `run.bash` is on
the bash-limits ratchet and cannot grow; `provisioning_profile` is a closed two-value axis
asserted in many plays; the desktop profile grants `NOPASSWD: ALL` sudo.

## Design

**Choosing the audience.** `run.bash --consumer` (attended; the README one-liner passes it
as `(source <(curl …) --consumer)`) or `RUN_BASH_INSTALL_AUDIENCE=consumer` (headless). The
first run writes `install_audience: consumer` into `localhost.yml`; later runs read it from
there, so an update needs no flag. A flag that contradicts the file fails fast (Decision 6).
Plays read `install_audience` with a `developer` default from
`environment/localhost/group_vars/desktop.yml`, so every existing machine is unchanged.

**Selecting plays.** Each play declares `audience: all|developer` beside `scope`, and a
shared guard ends a `developer` play on a consumer box with a message naming why. One
`playbook-main.yml` stays the single list, the play ledger keeps one record per play, and
`run.bash --changed` needs no second list (Decision 2). Mixed plays gate their developer
tasks on `install_audience == 'developer'` inside the play. A QA gate refuses a play with no
`audience` declared.

**run.bash.** The headless `HL_GITHUB_ACCOUNTS == none` checks become one predicate meaning
"no GitHub on this run", true for headless `none` and for any consumer run. Every GitHub
step in the research doc's section 2 list hangs off it. A consumer run does not prompt
for an email address, does not install `golang` in the bootstrap, and does not offer to file
a GitHub issue on failure; it prints the run log path and the public issues URL instead.
Profile resolution and its contradiction checks go into a Go command if they do not fit the
ratchet (Decision 10).

**Vault and secrets.** A consumer `localhost.yml` holds no `!vault` values: identity
(`user_login`, `user_name` from the passwd entry), `install_audience: consumer`,
`github_accounts: {}`. `ansible.cfg` still needs a readable `vault-pass.secret`, so
`run.bash` generates a random one with no prompt, mode 0600, and never asks for it again
(Decision 3). There is no config repo, so `localhost.yml` lives only on the machine; the
docs say so and say what is lost if the disk is (Decision 7).

**Sudo.** A consumer box does not get the `NOPASSWD: ALL` block; the play removes it as it
does for the server profile (Decision 8).

**Updates.** Re-running `run.bash` (or the desktop launcher Decision 5 names) pulls over
HTTPS and runs the main playbook; `run.bash --changed` runs only the stale plays. Fedora's
own package updates continue through GNOME Software as usual.

## Tasks

### Phase 1: Audience variable and play classification

- [x] ✅ **Task 1.1**: Owner answers Decisions 1-10, or accepts the defaults; record the
  answers in this plan. (The owner's instruction is recorded at the top of Technical Decisions.)
- [ ] ⬜ **Task 1.2**: Read every optional play and confirm or correct the research doc's
  section 4 first pass; record the final class for each.
  - Acceptance: every play under `playbooks/imports/` has one class, with a one-line reason.
- [x] ✅ **Task 1.2 (as built)**: every play classified: `play-golang.yml` is `all` because
  shared plays import it to build their Go helpers; Slack (`play-comms.yml`), ftp-camera,
  laptop thermal diagnostics, the dev diagnostics play and every experimental play are
  `developer`. The consumer optional set is the research doc's list.
- [x] ✅ **Task 1.3 / 1.4 / 1.5 (as built)**: `install_audience` default in group_vars;
  `vars.audience` in every play; a one-task guard (`meta: end_play`) in every developer play
  instead of a shared task file; the preflight assert; NOPASSWD sudo removed for consumers;
  the gate is Check 4b of `qa-ansible` (`src-go/qa/ansible/audience.go`), red and green in its
  tests. Linger and developer-only GNOME extensions are not gated yet (Task 6.2).
- [ ] ⬜ **Task 1.3**: Add `install_audience` (default `developer`) to
  `environment/localhost/group_vars/desktop.yml`, and an `audience` guard task file under
  `tasks/` that asserts the value is `developer|consumer` and ends a `developer` play on a
  consumer box.
  - Acceptance: an unknown value fails the play with the allowed values named;
    `./scripts/qa-changed.bash` passes.
- [ ] ⬜ **Task 1.4**: Declare `audience:` in every play and include the guard; gate the
  developer tasks of mixed plays (`play-basic-configs.yml` sudo block,
  `play-systemd-user-tweaks.yml` linger, any developer-only GNOME extension).
  - Acceptance: `--syntax-check` passes for all plays; on a developer box the run is
    unchanged (no task newly skipped).
- [ ] ⬜ **Task 1.5**: QA gate (Go, under `src-go/qa/`) refusing a play without `audience`
  or with a value other than `all|developer`, wired into `qa-all`, `qa-changed` and CI.
  - Acceptance: red on a fixture play missing the key, green on the tree.

### Phase 2: run.bash consumer path

- [ ] ⬜ **Task 2.1**: Replace the repeated headless `none` checks with one predicate, with
  no behaviour change.
  - Acceptance: the existing `scripts/test-run-bash-*.bash` suites pass; the bash-limits
    record for `run.bash` does not rise.
- [ ] ⬜ **Task 2.2**: `--consumer` and `RUN_BASH_INSTALL_AUDIENCE`, resolution against
  `localhost.yml`, and the contradiction checks (consumer with a GitHub account, with
  `RUN_BASH_CONFIG_SOURCE`, `RUN_BASH_RESTORE_PROJECTS=1`, `RUN_BASH_GITHUB_SSH_443=1`, a
  server profile, or a developer `localhost.yml`). In Go if the ratchet requires it.
  - Acceptance: each contradiction stops before any change with a message naming both
    inputs; unit tests cover each.
- [ ] ⬜ **Task 2.3**: Consumer attended flow: no keygen, no `gh`, no known-hosts fetch,
  HTTPS clone and HTTPS-preserving pull, no email prompt, no `golang` in the bootstrap
  packages, the consumer `localhost.yml` written, the vault password generated silently,
  no issue offer on failure.
  - Acceptance: a pty test in `scripts/test-run-bash-*.bash` drives an attended consumer
    run against stubs and proves no `gh`, `ssh-keygen` or `api.github.com` call is made and
    origin stays HTTPS on a second run.
- [ ] ⬜ **Task 2.4**: Optional-play menu lists only `audience: all` plays on a consumer box,
  and `RUN_BASH_OPTIONAL_PLAYBOOKS` refuses a developer play there.
  - Acceptance: a test proves both.
- [ ] ⬜ **Task 2.5**: Bump `RUN_BASH_VERSION` and add the `docs/run-bash-changelog.md`
  entry.

### Phase 3: VM lab acceptance

- [ ] ⬜ **Task 3.1**: Add a `desktop-consumer-install` scenario to
  `vars/vm-test-scenarios.yml` on the `desktop` base, `run_env` setting
  `RUN_BASH_INSTALL_AUDIENCE=consumer` and `RUN_BASH_GITHUB_ACCOUNTS=none`, plus its guest
  checker `files/home/.local/share/vmtest/guest-acceptance-desktop-consumer.bash`.
  - Acceptance: the checker asserts no `~/.ssh/id`, no `gh` auth state, origin is HTTPS,
    no `NOPASSWD` block, the consumer apps present, the developer tools (Go, Node, Claude
    Code, ccy, VS Code, LXC, Podman) absent, and a second `run.bash` run changes nothing;
    `qa-vmtest-manifest.bash` passes.
- [ ] ⬜ **Task 3.2**: Plan-local `deploy.bash` and `acceptance.bash` (on `_planlib.inc.bash`)
  that deploy the lab change and run `desktop-consumer-install`, `desktop-fresh-install` and
  `server-fast-provision`, so the developer and server paths are proved unchanged; add the
  plan to `CLAUDE/Plan/meta-deploy.list`.
  - Acceptance: `acceptance.bash` reports full coverage and a pass on the host.

### Phase 4: Docs for a non-developer

- [ ] ⬜ **Task 4.1**: A consumer install page under `docs/` written for someone who has
  never used a terminal: what it installs, the one command, what each prompt means, how to
  update, what to do on a failure, and that settings live only on this machine.
  - Acceptance: linked from `README.md` and `docs/README.md`; no developer jargon left
    unexplained; docs QA passes.
- [ ] ⬜ **Task 4.2**: `docs/installation.md`, `docs/playbooks.md` and the run.bash
  `--help` text name the audience and point at the consumer page instead of repeating it.

### Phase 5: Review

- [ ] ⬜ **Task 5.1**: `./scripts/qa-all.bash` passes.
- [ ] ⬜ **Task 5.2**: `qa-reviewer` over the full plan diff; every BLOCK and
  FIX-BEFORE-MERGE finding resolved.

## Technical Decisions

### Owner instruction (overrides the defaults below where they differ)

Verbatim: "lets get a basic consumer install system working. sounds like run.bash
--consumer. can internally export the none github accounts etc. and yes - excluide slack,
include other stuff. at the point of installing optoinal - exclude the dev type optional and
only show the "normal" stuff like qobuz etc. so no programming languages/tools. but do
include apps and stuff. i hitnk this is pretty easy, doesnt' need to be perfect. get it done"

Later, relayed by the coordinator: "feel free to make run.bash a thin wrapper around a go
toolset", then "dont fuck about". Read as: the consumer path is Go, run.bash only hands over
to it, and existing developer installs must behave exactly as before.

What that settles: Decision 1 is (b) without Slack (`play-comms.yml` is developer); Decision
2 is (a); Decision 3 is (a); Decision 10 is (b), the consumer flow is a Go command and
run.bash only hands over to it. The other decisions keep their defaults.

Open owner Decisions. Each states the default the plan proceeds on if the owner does not
answer.

### Decision 1: Which apps are "consumer level"

**Options**: (a) the shared plays plus Firefox, fonts, RPM Fusion codecs and one extra
browser; (b) as (a) plus every browser in `play-browsers.yml`, Slack (`play-comms.yml`), and
the media optional plays; (c) the owner names a list.
**Default**: (a), with Chrome as the extra browser, Slack excluded, and the optional menu
offering the consumer candidates in the research doc (photography, videography, Qobuz,
HD audio, collaboration, VPN clients, hardware-specific).

### Decision 2: How plays are selected

**Options**: (a) an `audience` key and guard in every play, one `playbook-main.yml`;
(b) a separate `playbook-consumer.yml` importing a subset.
**Default**: (a): one list for the ledger and `--changed`, and a new play cannot be
forgotten from a second list because the QA gate demands the key.

### Decision 3: Vault with no private config repo

**Options**: (a) generate a random `vault-pass.secret` silently and store nothing in the
vault; (b) prompt for a vault password as the developer path does.
**Default**: (a); a consumer has no secret to protect and no password manager entry to keep.

### Decision 4: Local user account and password

**Options**: (a) `run.bash` runs as the account Fedora's first-boot setup created, no
account prompt; (b) `run.bash` prompts for and creates an account.
**Default**: (a). The sudo password is still asked for by Ansible when needed.

### Decision 5: How a consumer updates

**Options**: (a) re-run `run.bash` (or a desktop launcher that runs it), HTTPS pull;
(b) extend Plan 00137's signed self-update timer to consumer desktops; (c) only Fedora's own
updates after install.
**Default**: (a) in this plan, with a desktop launcher entry named "Update fedora-desktop";
(b) is a follow-up raised against Plan 00137.

### Decision 6: Changing audience later

**Options**: consumer to developer: (a) supported by re-running with the developer flag,
which then runs the GitHub steps and moves origin to SSH; (b) refused. Developer to
consumer: (c) refused; (d) supported with uninstalls.
**Default**: (a) and (c): upgrading adds; downgrading would need every developer play to
learn to remove itself, which is out of scope.

### Decision 7: Backing up a consumer's `localhost.yml`

**Options**: (a) none, documented; (b) a local copy to a chosen folder.
**Default**: (a); the file holds only login, name and audience, all regenerated by a re-run.

### Decision 8: Passwordless sudo on a consumer box

**Options**: (a) absent; (b) granted as on a developer desktop.
**Default**: (a); a non-technical user's account should not be a silent path to root.

### Decision 9: Kickstart support

**Options**: (a) out of scope, follow-up plan; (b) add a consumer choice to `ks.cfg`'s
TUI in this plan.
**Default**: (a); the kickstart already clones over HTTPS, so a follow-up only has to pass
the audience through.

### Decision 10: Where the new run.bash logic lives

**Options**: (a) bash, only if Task 2.1's consolidation leaves room within the ratchet
record; (b) a Go command from the start for resolution and contradiction checks.
**Default**: (a) for the predicate, (b) for anything with more than one input to reconcile.

## Success Criteria

- [ ] `desktop-consumer-install` passes in the VM lab with no GitHub credential present.
- [ ] `desktop-fresh-install` and `server-fast-provision` still pass.
- [ ] A consumer re-run keeps origin on HTTPS and changes nothing on a converged box.
- [ ] Every play declares `audience`, enforced by a QA gate.
- [ ] The consumer docs page exists and is linked from the README.
- [ ] `./scripts/qa-all.bash` passes and `qa-reviewer` has no open BLOCK finding.

## Risks & Mitigations

| Risk                                                    | Impact | Probability | Mitigation                                                       |
| ------------------------------------------------------- | ------ | ----------- | ---------------------------------------------------------------- |
| A shared play quietly depends on a developer play       | M      | M           | the consumer VM scenario runs the whole main playbook            |
| `run.bash` cannot take the branch within its ratchet    | M      | M           | Task 2.1 consolidates first; Decision 10 sends the rest to Go    |
| A developer box loses a task through a wrong `audience` | H      | L           | Task 1.4 acceptance and `desktop-fresh-install` prove no change  |
| A consumer hits a failure they cannot read              | M      | M           | failure prints the log path and issues URL; Task 4.1 explains it |

## Delivery & Milestones

<!-- Curated milestones + delivery commit hashes only (git is the SSoT for
     "when" — do not add dates). The blow-by-blow activity log lives in
     JOURNAL/00228-Journal-YY-MM-DD.md — see CLAUDE/PlanJournalling.md. -->

- Plan filed.
