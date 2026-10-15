# Plan 00202: ccy claude code mod

**Status**: Not Started
**Created**: 2026-10-16
**Owner**: joseph
**Priority**: Medium

## Overview

Claude Code has **mods**: plugins that carry a JavaScript or TypeScript hooks module
running inside Claude Code
([overview](https://code.claude.com/docs/en/plugins/mods/overview)). They are on by
default from v2.1.287. A mod can do things that settings hooks, skills and status-line
commands cannot:

- draw a pane, a band above the prompt, a per-mod status entry and toasts;
- add `/commands` that run code with no Claude turn;
- read session usage and the agent list.

ccy has no mod today. What it does have is spread across mechanisms, each wired by hand:

- the pingbus plugin, loaded through `--plugin-dir` by three separate launchers;
- the phpantom plugin, copied in and enabled with `jq`;
- a status line that takes the single settings slot.

The pingbus kit is installed twice. Changes under `files/opt/claude-yolo/`, and to
`helpers/pingbus`, slip past the container version gate. Live team delivery already goes
in-process through the Claude Code inbox socket. The supervisor types messages in only in
`relay` mode.

This plan designs one **`ccy` mod**. It becomes the place where ccy extends the Claude Code
user experience: team UI, container awareness, `/ccy doctor`, deploy-pending and plan
status, and session helpers. It ships from one source tree to ccy containers and cc host
sessions. Enforcement stays with the hooks-daemon, launch decisions with the launcher,
delivery with the socket, and host runs with `meta-deploy.bash`.

[RESEARCH.md](RESEARCH.md) holds the evidence:

- §1, the map of what reaches a session today;
- §2, the cited mod capabilities and limits;
- §3, the brainstorm;
- §4, the architecture.

The adversarial review is
[subagent-reports/261016-design-review-fable.md](subagent-reports/261016-design-review-fable.md),
and this revision addresses it.

## Goals

- One `ccy` mod, loaded in place from a directory marketplace, with no network, in every
  ccy container and later in cc host sessions. One tree in the repo serves both.
- A small M1: close the version gate first, then ship the marketplace, the status entry
  and `/ccy doctor`.
- Contract tests on `claude plugin validate --json` that pin the forbidden hooks and
  calls, and `claude plugin test` fixtures for behaviour.
- A path that absorbs the pingbus plugin's skill, hooks and status line with no
  breakage for running teams or `agent-bus-claude` members.

## Non-Goals

- Moving any enforcement out of the hooks-daemon.
- Any delivery route into the model. The inbox socket is the route.
- Any route by which the container runs something on the host.
- Changing the pingbus protocol, or how seats and teams are chosen at launch.
- Building anything in this plan's research phase.

## Recommended architecture

1. **One plugin, `ccy`**, with one hooks module that imports feature modules (`core`,
   `teams`, `plans`, `lifecycle`). Each module is switched by `userConfig`, and the mod
   chooses modules by an image-baked marker.
2. **Source:** `files/opt/claude-yolo/mods/`, a directory marketplace listing `./ccy`.
   That tree is already the image build context and the host's `/opt/claude-yolo`.
3. **Enabled** through `extraKnownMarketplaces` plus `enabledPlugins`. The entrypoint also
   runs `claude plugin marketplace add /opt/claude-yolo/mods` before `exec claude`, so
   the first session of a new project has the mod. It is loaded in place, with no
   network.
4. **Owners:** the mod owns UI and commands. The hooks-daemon owns enforcement and the
   settings `statusLine`. pingbus owns the protocol, the CLI and delivery (the socket).
   The supervisor and launcher own restarts.
5. **What the mod may not do:** hook `tool.call`, `tool.check`, `prompt.submit`,
   `session.append`, `session.receive`, `session.send` or `classic.*`; call
   `$.http.fetch`, `$.settings.read`, `$.env.set`, `$.prompt.submit`, `$.session.append`
   or `$.session.send`; or put peer-authored text into a command's `{ text }`, which the
   model reads. A contract test pins all of this.

## Top features

These are ranked by value for effort. The full list is in [RESEARCH.md §3](RESEARCH.md#3-brainstorm-what-a-ccy-mod-could-give-a-session).

| Rank | Feature                                                                                                                                                     | Milestone         |
| ---- | ----------------------------------------------------------------------------------------------------------------------------------------------------------- | ----------------- |
| 1    | `/ccy doctor`: versions, mounts, seat and watcher, inbox socket bound and token exported, hooks-daemon, supervisor, network; each PASS or FAIL with the fix | M1                |
| 2    | ccy status entry, from image-baked facts: image version, team, context %                                                                                    | M1                |
| 3    | Update-needed band with the relaunch command (needs a launcher change and a mount)                                                                          | M2                |
| 4    | Deploy-pending band and `/ccy deploy-needed` (reads `meta-deploy.list`)                                                                                     | M2                |
| 5    | Team roster band, and `/team status` and `/team who` that return `{}` and show peer text only in the band or `$.ui.log`                                     | M2                |
| 6    | Team messages styled by `origin.kind` and pingbus-verified fields only, never by sender name                                                                | M2                |
| 7    | pingbus status line moved to the mod, freeing the settings slot (after D5)                                                                                  | M2 / M3           |
| 8    | Shutdown coordination UX: **blocked** on feat/teams-shutdown, which has no commits yet                                                                      | after that branch |

Later work: the `/plan` pane, `/handover`, the post-reboot resume card, and the usage
meter.

## Decisions for the owner

Each decision carries a recommendation.

01. **D1: Mod or plugin features?** Build a mod: a plugin with a hooks module.
    **Recommend: mod.**

02. **D2: One mod or several?** One `ccy` mod with feature modules. `$.ui.status` gives
    one line per mod, and there is one hooks module per plugin. **Recommend: one.**

03. **D3: Where does the source live?** `files/opt/claude-yolo/mods/ccy`, root-owned on
    the host. **Recommend: yes**, after Task 1.1 closes the gate.

04. **D4: How is it enabled in the container?**

    - (a) The entrypoint merges the keys into the container user's settings. That file is
      session-writable (`/root/.claude` links into `/workspace/.claude/ccy`), so the
      session controls whether its own mod loads. This gives **no integrity guarantee**.
    - (b) An image-owned `/etc/claude-code/managed-settings.json` puts the mod's load
      outside the session's control and makes it an organisation-tier mod. It also loads
      the built-in `sec-default` guard, which is harmless for a mod that never approves
      tool calls.

    **Recommend: (a) for M1 to prove loading, then (b) by M2.**

05. **D5: Does cc on the host get the mod?** Yes: Ansible registers the same root-owned
    directory, with the container modules off. Re-check the argv list against host paths,
    since pingbus lives in the host kit there. **Recommend: yes, in M2.**

06. **D6: How does the mod tell host from container?** By an image-baked marker that the
    Dockerfile writes, not `/run/.containerenv` (podman writes it, docker does not).
    `userConfig` overrides. **Recommend: yes.**

07. **D7: Does the mod take any delivery role?** No. `pingbus watch` already writes into
    the inbox socket by default, so the mod must never add `$.prompt.submit` or
    `$.session.append` as a second route. M4 is kept only as a question, with the default
    answer no. **Recommend: no delivery role.**

08. **D8: When are the pingbus skill and hooks absorbed?** In M3, after D11.
    **Recommend: M3.**

09. **D9: Is TypeScript allowed, and is the `claude` binary a QA dependency?** The mod's
    tests are `claude plugin test` fixtures. The Python contract test only reads
    `validate --json`. Add `claude` to the QA tool inventory through IaC where CI lacks it.
    TypeScript is the language Claude Code loads mods in, so it is not bash tooling.
    **Recommend: yes.**

10. **D10: How is API drift guarded?** Mods are generally available, but the API is
    version-gated and growing (fields marked v2.1.289 to v2.1.300), and the image does
    not pin Claude Code: it self-updates daily, and cc updates on every launch. The guard
    is a runtime floor: `session.start` checks `$.session.version()` against the version
    the types were generated for, and degrades to a toast. `tsc` against fresh types
    runs when that floor is bumped. **Recommend: runtime floor, plus `tsc` at floor
    bumps (from M2). No pin.**

11. **D11: How do `agent-bus-claude` members get `ccy`?** Those members (LXC, VM, other
    hosts) take the kit from `agent-bus-install`, not from `/opt/claude-yolo`. Either:

    - (a) the kit carries the `ccy` mod as well, which is a second copy, checked by the
      deployed-drift QA; or
    - (b) the `pingbus` plugin stays permanently for those members, and only ccy and cc
      move to `ccy`.

    **Recommend: (b)**, decided before M3, because those members have no ccy features
    to show.

## Tasks

### Phase 0: Research and decisions

- [x] ✅ **Task 0.1**: Map what reaches a session today ([RESEARCH.md §1](RESEARCH.md#1-what-ccy-puts-into-a-session-today))
- [x] ✅ **Task 0.2**: Verify the mod and plugin extension points with citations ([RESEARCH.md §2](RESEARCH.md#2-claude-codes-extension-points-centred-on-mods))
- [x] ✅ **Task 0.3**: Brainstorm features, including what does not belong in a mod ([RESEARCH.md §3](RESEARCH.md#3-brainstorm-what-a-ccy-mod-could-give-a-session))
- [x] ✅ **Task 0.4**: Recommend an architecture ([RESEARCH.md §4](RESEARCH.md#4-architecture))
- [x] ✅ **Task 0.5**: Adversarial design review (REVISE), with every point addressed in this revision
- [ ] ⬜ **Task 0.6**: Owner answers D1 to D11

### Phase 1: M1, version gate then a minimal mod in the container

- [ ] ⬜ **Task 1.1**: **Version gate first, in its own commit.** Gate 2 of `scripts/git-hooks/pre-commit` gets an explicit list of repo paths that require a container bump: `files/opt/claude-yolo/**` and `helpers/pingbus/**`, plus the existing `files/var/local/claude-yolo` COPY sources. A test checks that list against the copy tasks in `play-claude-yolo.yml`. (The alternative is a second context root per source, plus `helpers/pingbus/**`.) This overlaps Plan 00093
- [ ] ⬜ **Task 1.2**: Contract test first (`tests/helpers/ccy_mod/test_mod_contract.py`). It pins `hooks:`, `calls:` and `env reads:` from `validate --strict --json` separately, as set out in RESEARCH §4.4. It also checks that the version is in the changelog and that no install-specific text appears
- [ ] ⬜ **Task 1.3**: Write `files/opt/claude-yolo/mods/` (marketplace) and `ccy/` (manifest, `hooks.json`, `register.ts`, `core` module, `*.test.ts`), with C1 (status entry from image-baked facts) and D1 (`/ccy doctor`, local checks only). Commands are registered last, and their names are checked against the built-ins
- [ ] ⬜ **Task 1.4**: Wire `claude plugin validate --strict` and `claude plugin test` into `qa-changed.bash` and `qa-all.bash`, with `claude` provisioned through IaC where missing
- [ ] ⬜ **Task 1.5**: The entrypoint runs `claude plugin marketplace add /opt/claude-yolo/mods`, then enables `ccy@<marketplace>` (D4 option a). Bump the container version and add a changelog line
- [ ] ⬜ **Task 1.6**: Verify in a relaunched ccy session, **including the first session of a brand-new project**: `/plugin` shows `1 mod active · ccy`, the status entry appears (and its `⚠` prefix is checked), and `/ccy doctor` runs. Record it in the journal
- [ ] ⬜ **Task 1.7**: Run the qa-reviewer agent

### Phase 2: M2, read-only team UI, host cc, managed settings

- [ ] ⬜ **Task 2.1**: The `teams` module: the roster band (T1), and `/team status` and `/team who` returning `{}` (T4). T3 row styling keyed on `origin.kind` and verified fields only
- [ ] ⬜ **Task 2.2**: Update-needed band (C2): the launcher passes the ccy version in through a read-only mount, with a `CCY_VERSION` bump. Then the deploy-pending band and `/ccy deploy-needed` (H1, H2)
- [ ] ⬜ **Task 2.3**: Move the container enable to image-owned managed settings (D4 option b)
- [ ] ⬜ **Task 2.4**: An Ansible task registers the marketplace for cc host sessions (D5), with the container modules off
- [ ] ⬜ **Task 2.5**: Move the phpantom `jq` wiring onto the same directory marketplace
- [ ] ⬜ **Task 2.6**: Add `tsc` against fresh types at floor bumps (D10)
- [ ] ⬜ **Task 2.7**: T7, the status line on `$.ui.status`, after Task 2.4. The kit keeps `statusLine` for `agent-bus-claude` (D11)

### Phase 3: M3, absorb pingbus for ccy and cc

- [ ] ⬜ **Task 3.1**: D11 decided
- [ ] ⬜ **Task 3.2**: Move the pingbus skill and settings-style hooks into `ccy` for ccy and cc. The contract-test assertions move with them
- [ ] ⬜ **Task 3.3**: The entrypoint and `cc-teams.bash` stop passing `--plugin-dir` for pingbus

### Blocked

- [ ] ⬜ **Task B.1**: Shutdown coordination UX (T6). Blocked on feat/teams-shutdown, which has no commits beyond F44 yet

### Open question (M4)

- [ ] ⬜ **Task 4.1**: Confirm the mod needs no delivery role (D7). The default answer is no; close this with a journal note

## Dependencies

- Plan 00093 (version gate): Task 1.1.
- Plan 00161 (agent team bus) and Plan 00194 (live delivery): M2 and M3 touch their
  surfaces. This plan adds UI and never a delivery route.
- Plan 00201 and feat/teams-shutdown: Task B.1.
- Plan 00169 (image keeps its tools current): D10's no-pin premise.
- Plan 00135 (sessions survive a reboot): the later resume card.

## Success Criteria

- [ ] A commit that edits `files/opt/claude-yolo/**` or `helpers/pingbus/**` without a
  container bump is refused.
- [ ] A relaunched ccy session, including the first session in a new project, shows
  `1 mod active · ccy` and runs `/ccy doctor` with no network.
- [ ] The contract test fails on any forbidden hook or call listed under Recommended
  architecture.
- [ ] After M3, no ccy or cc launcher passes `--plugin-dir` for pingbus, and running teams
  kept working through the change.

## Delivery & Milestones

- Research, design and review revision: the branch `plan/ccy-claude-code-plugin`. The
  branch name predates the rename to "mod".
