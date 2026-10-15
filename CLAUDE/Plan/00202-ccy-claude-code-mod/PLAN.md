# Plan 00202: ccy claude code mod

**Status**: Not Started
**Created**: 2026-10-16
**Owner**: joseph
**Priority**: Medium

## Overview

Claude Code now has **mods**: plugins that carry a JavaScript or TypeScript hooks module
running inside Claude Code
([overview](https://code.claude.com/docs/en/plugins/mods/overview)). A mod can do things
that settings hooks, skills and status-line commands cannot:

- draw a pane, a band above the prompt, a per-mod status entry and toasts;
- add `/commands` that run code with no Claude turn;
- read session usage and the agent list;
- observe deliveries between sessions.

ccy has no mod today. What it does have is spread across several mechanisms, each wired by
hand:

- the pingbus plugin, loaded through `--plugin-dir` by three separate launchers;
- the phpantom plugin, copied in and enabled with `jq`;
- a status line that takes the single settings slot;
- supervisor keystroke injection.

The pingbus kit is installed twice, and changes under `files/opt/claude-yolo/` slip past
the container version gate.

This plan designs one **`ccy` mod**. It becomes the place where ccy extends the Claude Code
user experience: team UI, container awareness, `/ccy doctor`, deploy-pending and plan
status, and session lifecycle helpers. It ships from one source tree to both ccy
containers and cc host sessions. Enforcement stays with the hooks-daemon, launch decisions
with the launcher, and host runs with `meta-deploy.bash`.

[RESEARCH.md](RESEARCH.md) holds the evidence:

- §1, the map of what reaches a session today;
- §2, the cited mod capabilities and limits;
- §3, the feature brainstorm;
- §4, the architecture.

## Goals

- One `ccy` mod, loaded in place from a directory marketplace, with no network, in every
  ccy container and in cc host sessions. One tree in the repo serves both.
- A first milestone (M1) that is small and read-only: a status entry, a `/ccy doctor`
  command, an update-needed band, and a deploy-pending band.
- A path that absorbs the pingbus plugin's skill, hooks and status line into `ccy` with no
  breakage for running teams.
- Contract tests built on `claude plugin validate --json` and `claude plugin test`, which
  pin what the mod may hook and call.

## Non-Goals

- Moving any enforcement (guards, secret checks, plan QA, stop gates) out of the
  hooks-daemon.
- Any route by which the container runs something on the host.
- Changing the pingbus protocol, or how seats and teams are chosen at launch.
- Building anything in this plan's research phase.

## Recommended architecture

1. **One plugin, `ccy`**, with one hooks module that imports feature modules (`core`,
   `teams`, `plans`, `lifecycle`). Each is switched by a `userConfig` option, and the mod
   detects whether it runs in a container or on the host.
2. **Source:** `files/opt/claude-yolo/mods/`, a directory marketplace listing `./ccy`.
   That tree is already the image build context and the host's `/opt/claude-yolo`, so it
   reaches both with no second copy.
3. **Enabled** by the entrypoint (container) and by Ansible (host) through
   `extraKnownMarketplaces` plus `enabledPlugins`. It is loaded in place, needs no
   network, and its version is shown but not used for loading.
4. **Owners:** the mod owns UI and commands, the hooks-daemon owns all enforcement and the
   settings `statusLine`, pingbus owns the protocol and CLI, and the supervisor and
   launcher own restarts. The mod never hooks `tool.call`, `tool.check`, `prompt.submit` or
   `session.append`, and never calls `$.http.fetch`. A contract test pins this.
5. **Version:** a mod change is an image change. Bump the container version, the
   `plugin.json` SemVer and the changelog. The pre-commit gate is extended to
   `files/opt/claude-yolo/` first.

## Top features

These are ranked by value for effort. The full list is in [RESEARCH.md §3](RESEARCH.md#3-brainstorm-what-a-ccy-mod-could-give-a-session).

| Rank | Feature                                                                                                              | Milestone |
| ---- | -------------------------------------------------------------------------------------------------------------------- | --------- |
| 1    | `/ccy doctor`: versions, mounts, seat and watcher, hooks-daemon, supervisor, network, each PASS or FAIL with the fix | M1        |
| 2    | ccy status entry: ccy and image version, team, unread count, context %                                               | M1 / M2   |
| 3    | Update-needed band with the relaunch command                                                                         | M1        |
| 4    | Deploy-pending band and `/ccy deploy-needed` (reads `meta-deploy.list`)                                              | M1        |
| 5    | Team roster band and read-only `/team status` and `/team who`                                                        | M2        |
| 6    | pingbus status line moved to the mod, freeing the settings slot                                                      | M2        |
| 7    | Team messages drawn by origin: human, peer or ping                                                                   | M2        |
| 8    | Shutdown coordination UX for feat/teams-shutdown: a band, `/team shutdown` with `$.ui.ask`                           | M3        |

Later work: in-process delivery that replaces supervisor typing (M4, high risk, needs a
spike), the `/plan` pane, `/handover`, and the post-reboot resume card.

## Decisions for the owner

Each decision carries a recommendation.

01. **D1: Mod or plugin features?** Build a mod: a plugin with a hooks module.
    Skills and settings hooks stay available in the same plugin. **Recommend: mod.**
02. **D2: One mod or a marketplace of several?** Build one `ccy` mod with feature
    modules. `$.ui.status` gives one line per mod, and there is one hooks module per
    plugin. **Recommend: one.**
03. **D3: Where does the source live?** `files/opt/claude-yolo/mods/ccy`, as a directory
    marketplace. **Recommend: yes**, with the version gate extended to cover it (Task 1.3).
04. **D4: How is it enabled in the container?** Either (a) the entrypoint merges
    `extraKnownMarketplaces` and `enabledPlugins` into the container user's settings, or
    (b) an image-owned `/etc/claude-code/managed-settings.json` makes it an "organization"
    mod that runs first. Option (b) also loads the built-in `sec-default` guard, which
    changes what other mods may approve. **Recommend: (a)** for M1, and revisit (b) if a
    session-installed mod ever needs policing.
05. **D5: Does cc on the host get the mod?** Yes: the same directory, registered by
    Ansible in the owner's user settings, with the container-only modules off. On the host
    it runs as the owner, with the allow-listed read-only argv. **Recommend: yes, from
    M2** (M1 is container-only, to prove it).
06. **D6: Do host and container differ only by detection?** The mod turns modules on or
    off by detecting `/run/.containerenv` and the ccy marker, and `userConfig` overrides
    it. **Recommend: yes.**
07. **D7: Does the mod take over live delivery from the supervisor (Plan 00194)?** Not
    yet. Run a spike in M4 to check that `session.receive`, `$.session.append` and
    `$.prompt.submit` keep Plan 00161's HUMAN/PEER framing. Then *replace* the supervisor's
    typing, never add a second route. **Recommend: defer to M4.**
08. **D8: When are the pingbus skill and hooks absorbed?** In M3. Keep a `pingbus` stub
    for one release for `agent-bus-claude` members, then retire it. **Recommend: M3.**
09. **D9: Is TypeScript allowed, and is the `claude` binary a QA dependency?**
    `claude plugin test` and `tsc` run in QA. Add `claude` to the QA tool inventory
    through IaC where CI lacks it. **Recommend: yes.** TypeScript is the language mods
    are typed in. It is not bash tooling, so the Go-only rule for tooling does not apply
    to a module Claude Code loads, just as Ansible-loaded Python stays Python.
10. **D10: Is the API's "early access" status acceptable?** The API can move between
    releases. Pin the image's Claude Code version and run `tsc` against the per-build
    types in QA, so a break fails QA and never reaches a session. **Recommend: accept,
    with the type check as the guard.**

## Tasks

### Phase 0: Research and decisions

- [x] ✅ **Task 0.1**: Map what reaches a session today ([RESEARCH.md §1](RESEARCH.md#1-what-ccy-puts-into-a-session-today))
- [x] ✅ **Task 0.2**: Verify the mod and plugin extension points with citations ([RESEARCH.md §2](RESEARCH.md#2-claude-codes-extension-points-centred-on-mods))
- [x] ✅ **Task 0.3**: Brainstorm features, with what does not belong in a mod ([RESEARCH.md §3](RESEARCH.md#3-brainstorm-what-a-ccy-mod-could-give-a-session))
- [x] ✅ **Task 0.4**: Recommend an architecture ([RESEARCH.md §4](RESEARCH.md#4-architecture))
- [ ] ⬜ **Task 0.5**: Owner answers D1 to D10

### Phase 1: M1, a read-only core mod in the container

- [ ] ⬜ **Task 1.1**: Contract test first (`tests/helpers/ccy_mod/test_mod_contract.py`). It pins the allowed hooks and calls, the `$.process.run` argv allow-list, no `http.fetch`, the version in the changelog, and no install-specific text
- [ ] ⬜ **Task 1.2**: Add `claude plugin validate --strict --json`, `claude plugin test` and `tsc` to `qa-changed.bash` and `qa-all.bash`, with the `claude` binary provisioned through IaC where missing
- [ ] ⬜ **Task 1.3**: Extend the pre-commit container-version gate to `files/opt/claude-yolo/` (overlaps Plan 00093)
- [ ] ⬜ **Task 1.4**: Write `files/opt/claude-yolo/mods/` (marketplace) and `ccy/` (manifest, `hooks.json`, `register.ts`, `core` module, `*.test.ts`)
- [ ] ⬜ **Task 1.5**: Features: status entry (C1), `/ccy doctor` (D1), update-needed band (C2), deploy-pending band and `/ccy deploy-needed` (H1, H2)
- [ ] ⬜ **Task 1.6**: The entrypoint enables `ccy@ccy-local` (D4 option a). Bump the container and ccy versions, and add a changelog line
- [ ] ⬜ **Task 1.7**: Verify in a relaunched ccy session: `/plugin` shows `1 mod active · ccy`, and each feature works. Record it in the journal
- [ ] ⬜ **Task 1.8**: Run the qa-reviewer agent

### Phase 2: M2, team read-only UI and cc on the host

- [ ] ⬜ **Task 2.1**: The `teams` module: roster band (T1), `/team status` and `/team who` (T4, read-only), origin-styled rows (T3)
- [ ] ⬜ **Task 2.2**: Status line moved to `$.ui.status` (T7). The kit `settings.json` drops `statusLine`
- [ ] ⬜ **Task 2.3**: An Ansible task registers the marketplace for cc host sessions (D5), with container modules off

### Phase 3: M3, absorb pingbus and add shutdown UX

- [ ] ⬜ **Task 3.1**: Move the pingbus skill and settings-style hooks into `ccy`, with the contract-test assertions moving with them. Keep a `pingbus` stub for one release
- [ ] ⬜ **Task 3.2**: Collapse the three hand-wired launchers onto `enabledPlugins`
- [ ] ⬜ **Task 3.3**: Shutdown coordination UX (T6), aligned with feat/teams-shutdown

### Phase 4: M4, in-process delivery spike

- [ ] ⬜ **Task 4.1**: Spike T2 against Plan 00161's trust model, and decide with Plan 00194

## Dependencies

- Plan 00161 (agent team bus) and Plan 00194 (live delivery): M2 to M4 touch their
  surfaces. This plan adds UI and does not change their protocol.
- Plan 00201 and feat/teams-shutdown: M3 Task 3.3.
- Plan 00093 (version gate): Task 1.3.
- Plan 00135 (sessions survive a reboot): the later resume card.

## Success Criteria

- [ ] A relaunched ccy session shows `1 mod active · ccy` in `/plugin`, and `/ccy doctor`
  runs with no network.
- [ ] The contract test fails if the mod hooks `tool.call`, `tool.check`, `prompt.submit` or
  `session.append`, or calls `$.http.fetch`.
- [ ] Editing a file under `files/opt/claude-yolo/` without a container bump is refused
  at commit.
- [ ] After M3, no launcher passes `--plugin-dir` for pingbus, and running teams kept
  working through the change.

## Delivery & Milestones

- Research and design: the branch `plan/ccy-claude-code-plugin`
