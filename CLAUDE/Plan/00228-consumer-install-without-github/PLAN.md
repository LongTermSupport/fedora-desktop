# Plan 00228: consumer install without GitHub

**Status**: In Progress (Phases 1, 2 and 4 built; VM acceptance and review open)
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

- An attended consumer install, `./run.bash --consumer`, that needs no GitHub account:
  it never runs `gh` or `ssh-keygen` and never makes an authenticated GitHub call.
  Unauthenticated public downloads from GitHub are fine (the repository itself, Galaxy's
  role clone, and the release lookups of the Qobuz, photography and DisplayLink plays).
  A headless consumer run is refused.
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

**Choosing the audience.** `./run.bash --consumer` (the docs' one-liner passes it as
`(source <(curl …) --consumer)`); there is no headless form, and `--headless --consumer` is
refused. The first run writes `install_audience: consumer` into `localhost.yml`; a later
plain `./run.bash` or `--optional-only` reads it from there, so an update needs no flag.
`--consumer` on a developer box is refused before run.bash changes anything (Decision 6).
Plays read `install_audience` with a `developer` default from
`environment/localhost/group_vars/desktop.yml`, so every existing machine is unchanged.

**Selecting plays.** Each play declares `audience: all|developer` beside `scope`, and a
shared guard ends a `developer` play on a consumer box with a message naming why. One
`playbook-main.yml` stays the single list, the play ledger keeps one record per play, and
`run.bash --changed` needs no second list (Decision 2). Mixed plays gate their developer
tasks on `install_audience == 'developer'` inside the play. A QA gate refuses a play with no
`audience` declared.

**run.bash hands over to Go.** On a consumer run, run.bash installs git and Go, clones or
pulls the checkout over HTTPS, and execs `scripts/consumer-install.bash`, which builds and
runs the Go command `consumer-install` (`src-go/internal/consumer`, Decision 10). Go is
therefore installed on every consumer box: it is the installer's own build toolchain (and
`play-golang.yml` is `audience: all` because shared plays build Go helpers), not a
developer language offered to the user. None of run.bash's GitHub steps run on that path.
A single play, `--changed` and `--rerun` stay with run.bash's own play runner, which on a
consumer box never offers to file a GitHub issue: it says the log is the output on screen
and gives the public issues URL.

**Vault and secrets.** A consumer `localhost.yml` holds no `!vault` values: identity
(`user_login`, `user_name` from the passwd entry), `install_audience: consumer`,
`github_accounts: {}`. `ansible.cfg` still needs a readable `vault-pass.secret`, so
`consumer-install` generates a random one with no prompt, mode 0600, and never asks for it again
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
- [x] ✅ **Task 1.2**: every play classified: `play-golang.yml` is `all` because
  shared plays import it to build their Go helpers; Slack (`play-comms.yml`), ftp-camera,
  laptop thermal diagnostics, the dev diagnostics play and every experimental play are
  `developer`. The consumer optional set is the research doc's list.
- [x] ✅ **Task 1.3 / 1.4 / 1.5**: `install_audience` default in group_vars;
  `vars.audience` in every play; a one-task guard (`meta: end_play`) in every developer play
  instead of a shared task file; the preflight assert; NOPASSWD sudo removed for consumers;
  the gate is Check 4b of `qa-ansible` (`src-go/qa/ansible/audience.go`), red and green in its
  tests. Linger and developer-only GNOME extensions are not gated yet (Task 6.2).

### Phase 2: the consumer path (as built: a Go command run.bash hands over to)

The owner's instruction moved the consumer flow to Go, so the headless predicate refactor
(old Task 2.1) and the in-bash attended flow are not needed: the developer path of run.bash
is untouched apart from the hand-off lines. Headless consumer runs are refused, not built.

- [x] ✅ **Task 2.1**: `run.bash --consumer`, or a `localhost.yml` holding
  `install_audience: consumer`, installs git and Go, clones or pulls the checkout over HTTPS
  and execs `scripts/consumer-install.bash` with `--optional-only` passed on; a single play,
  `--changed`, `--rerun` and root stay with run.bash; `--headless` with it is refused. Paid
  for by deleting the optional menu's always-empty auto-run loop; the bash-limits record went
  down (statements 2821 to 2818).
- [x] ✅ **Task 2.2**: the Go command `consumer-install` (`src-go/internal/consumer`): refuses
  root and a non-terminal; Fedora version must match `vars/fedora-version.yml`; a fresh
  consumer `localhost.yml` (login, GECOS name, `install_audience: consumer`,
  `github_accounts: {}`), a developer one refused; dnf base packages through
  `internal/sudo`; pipx ansible with only the missing injects; the vault password file that
  `ansible-config` resolves made at random (32 bytes, base64, 0600) when absent; galaxy; the
  main playbook (root as Task 2.7 says); a numbered menu
  of the optional plays whose every play is `audience: all`; the setup marker; a reboot
  advised. No gh, no ssh-keygen, no git. Unit tests through `execx.Fake` (coverage 91%),
  including a test over this checkout's real optional plays.
- [x] ✅ **Task 2.3**: `scripts/test-run-bash-consumer.bash` drives the real run.bash under
  stubs: `--consumer` hands over with `--optional-only=false`, a consumer box hands over
  with no flag, a developer box takes the developer path, `--consumer` on a developer box
  is refused with nothing run, a failed single play and `--changed` on a consumer box say
  where the log is and never offer a GitHub issue or reach `gh` (a developer box still
  offers it), `--headless --consumer` is refused, and the real wrapper builds the Go
  command. The nine existing `scripts/test-run-bash-*.bash` suites pass unchanged.
- [x] ✅ **Task 2.5**: review round 1 fixed
  ([subagent-reports/261026-review-fixes.md](subagent-reports/261026-review-fixes.md)); the
  audience gate also requires the guard to be the first task the play runs.
- [x] ✅ **Task 2.4**: `RUN_BASH_VERSION` 1.33.0 and its changelog entry.
- [x] ✅ **Task 2.6**: a failed optional play no longer ends the consumer run (owner report:
  "it's not clear which of the optional ones were installed"). Every chosen play runs in menu
  order; a failure is recorded and the next one runs; Ctrl-C, or a play ended by a signal,
  stops at once. A summary on stderr always follows: per play installed, failed (exit status;
  its output is the terminal's, not saved), stopped part way, or not run; then one
  `run.bash <play>.yml` line per play to retry and the `--optional-only` menu. Exit 1 naming
  how many failed, 128+n on a signal, 0 only when every chosen play passed (the setup marker
  only then). The main playbook stays fail-fast. Red and green in `app_test.go`.
- [x] ✅ **Task 2.7**: the password asked once per run (owner report: "asks for sudo password
  for every single play"). Each play used to run with `--ask-become-pass`, so Ansible asked
  before the main play and again before every optional one. Now, unless sudo is truly
  passwordless, the login password is typed once with the echo off (`ui.Lines.Secret`,
  `ttyecho.Off`), checked at once by `sudo -k -S -v` with the password fed on stdin
  (`sudo.CheckPassword`), asked again on a refusal (three tries, then exit 1 before anything
  is installed), and written (`execx.Secret.WriteFile`) to a 0600 file in a 0700 directory of
  its own under `XDG_RUNTIME_DIR` (memory only); every `ansible-playbook` gets
  `--become-password-file` naming it. Never argv, never an environment, never printed. Run
  removes the directory on every exit path (success, a failed play, the input ending, Ctrl-C
  and TERM through the signal context). The checked password also leaves sudo's credential
  cached, so the dnf step asks nothing. The VM lab's consumer driver refuses the new question
  too (its sudo is passwordless). Red and green in the four packages' tests.

### Phase 3: VM lab acceptance

- [x] ✅ **Task 3.1** (built and tested here; its host run is Task 3.2's): Add a `desktop-consumer-install` scenario to
  `vars/vm-test-scenarios.yml` on the `desktop` base, driving an attended
  `./run.bash --consumer` (the consumer path refuses headless, so the lab needs a pty
  driver for it), plus its guest checker. As built: the manifest key `audience: consumer`
  (Python validator and the `vmtest` marker, closed list, desktop only, no `run_env` or
  `host_only`); vmtest had no pty driver, so `src-go/internal/vmtest/attend.go` drives
  `ssh -tt` and answers a prompt only when it appears (a password or GitHub credential
  prompt ends the run as a fail); `consumer.go` waits for the session, pins an HTTPS clone
  at the run's commit, runs `~/run.bash --consumer` and then the re-run (output kept in
  `/var/lib/vmtest/rerun.log`). The checker is named for the scenario,
  `guest-acceptance-desktop-consumer-install.bash` (17 checks), because `vmtest` finds a
  scenario's own checker by its id. Host run pending.
  - Acceptance: the checker asserts no `~/.ssh/id`, no `gh` auth state, origin is HTTPS,
    no `NOPASSWD` block, the consumer apps present, Go present (the installer's own build
    toolchain, so it is expected), the developer tools (Node, Claude Code, ccy, VS Code,
    LXC, Podman) absent, and a second `run.bash` run changes nothing;
    `qa-vmtest-manifest.bash` passes.
- [x] ✅ **Task 3.2**: Plan-local `deploy.bash` and `acceptance.bash` (on `_planlib.inc.bash`)
  that deploy the lab change and run `desktop-consumer-install`, `desktop-fresh-install` and
  `server-fast-provision`, so the developer and server paths are proved unchanged; add the
  plan to `CLAUDE/Plan/meta-deploy.list`.
  - Acceptance: `acceptance.bash` reports full coverage and a pass on the host.
  - Built here: `deploy.bash` (play-vm-test-lab.yml) and `acceptance.bash` (ensures the
    desktop and server-fast bases, runs the three scenarios from the pushed HEAD, `COVERAGE: N of 3`, exit 0/1/2), both lint-clean and refusing the container; queued in
    `meta-deploy.list`.
  - Host run at 3781e559f: COVERAGE 2 of 3, REJECTED. desktop-fresh-install passed 16 of 16
    and server-fast-provision 13 of 13. desktop-consumer-install failed 14 of 17:
    play-basic-configs asked a consumer for the prompt colour, the attendant had no answer,
    and the run waited out provisionLimit (exit 124). The 3 failed checks all follow from
    that. Fixed in bd342c63d (a consumer is never asked). Re-run after Task 3.3.
  - Re-run at 525560cfb: COVERAGE 2 of 3, REJECTED. desktop-fresh-install 16 of 16 and
    server-fast-provision 13 of 13 passed. desktop-consumer-install passed 16 of 17 in about
    18 minutes, with no prompt hang (the colour question was skipped on both runs). The one
    failure, rerun-changed-nothing, was play-podman.yml's tempdirs clean: it deleted
    overlay/tempdirs on every run, and podman recreates it on each use, now on every consumer
    install because ccc's image is built there (Plan 00233). Fixed: removed only when
    `podman info` fails. Re-run after that fix is pushed.
  - Re-run at acfcb6814: COVERAGE 3 of 3, ACCEPTED. desktop-consumer-install 17 of 17
    (rerun-changed-nothing included), desktop-fresh-install 16 of 16, server-fast-provision
    13 of 13.
- [x] ✅ **Task 3.3**: The vmtest attendant fails an attended run on an unanswered prompt within
  two minutes, not at provisionLimit (90 minutes): an idle-output bound once a prompt-shaped
  line is the last output, so the next unknown question costs 60 to 120 s. Tests first, in
  `src-go/internal/vmtest` (attend.go, steps.go).
  - As built: a partial line ending in `:`, `?`, `]`, `)` or `>` with nothing after it for
    `promptIdle` (60s, on `Env.Sleep`) ends the run as a fail, the transcript quoting its
    last 3 lines. The colour prompt ended in a bare `:`, not `: `. Its host proof is Task
    3.2's re-run.

### Phase 4: Docs for a non-developer

- [x] ✅ **Task 4.1**: "Install for Someone Who Does Not Write Code" in
  `docs/installation.md`: the one command, what it leaves out, the app menu, how to update,
  and that settings live only on this machine; `run.bash --help` names `--consumer`.
- [x] ✅ **Task 4.2**: Linked from `README.md` (its own Quick Start subsection), and the
  install audience named in `docs/playbooks.md` beside the provisioning profile.

### Phase 6: Later

- [ ] ⬜ **Task 6.1**: run.bash fully becomes a thin wrapper over Go, as its own plan: a
  fresh box has no Go and no checkout until the bash bootstrap has run, so that plan owns
  the bootstrap's shape.
- [ ] ⬜ **Task 6.2**: Gate the developer parts of mixed plays on a consumer box: linger
  (`play-systemd-user-tweaks.yml`) and any developer-only GNOME extension.
- [ ] ⬜ **Task 6.3**: A consumer `--changed` path in Go (today `--changed` on a consumer
  box runs run.bash's own play runner, which offers no GitHub issue there).
- [ ] ⬜ **Task 6.4**: A consumer choice in the kickstart (`fedora-install/ks.cfg`).
- [ ] ⬜ **Task 6.5**: Speech-to-text on a consumer box. `play-speech-to-text.yml` is
  `audience: all`, so the consumer menu offers it, and local dictation needs only Go and Whisper.
  Its Claude post-processing (Ctrl+Insert, the panel's Corporate and Natural items) runs through
  `wsi-claude-token`, which reads ccy's token pool, and a consumer box has no ccy: those items
  would fail there, ungated. Open, the owner's decision: post-processing for consumers (a Claude
  login without ccy) or none. Either way the items fail clearly or are hidden on a consumer box,
  and the consumer VM scenario installs speech-to-text and checks plain dictation is set up.
- [ ] ⬜ **Task 6.6**: The developer path's optional runs, as Task 2.6 did for the consumer:
  headless `RUN_BASH_OPTIONAL_PLAYBOOKS` stops at the first failed play (`hl_abort` in
  `hl_run_optional_playbooks`), so the plays after it never run and are not named; the
  attended menu (`show_menu`, its A, W and B choices) goes on after a failure but prints no
  summary of what installed; `--rerun` (`play_batch_run`) stops at the first failure by its
  documented design, and mixes main and optional plays, so it is the owner's call whether
  optional plays there should go on.

### Phase 5: Review

- [x] ✅ **Task 5.1**: `./scripts/qa-all.bash` passes (at acfcb6814, the tree the VM run
  accepted).
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
| A consumer hits a failure they cannot read              | M      | M           | failure says the log is on screen, with the issues URL; Task 4.1 |

## Delivery & Milestones

<!-- Curated milestones + delivery commit hashes only (git is the SSoT for
     "when" — do not add dates). The blow-by-blow activity log lives in
     JOURNAL/00228-Journal-YY-MM-DD.md — see CLAUDE/PlanJournalling.md. -->

- Plan filed.
