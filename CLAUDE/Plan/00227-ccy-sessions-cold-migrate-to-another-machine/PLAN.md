# Plan 00227: ccy sessions cold migrate to another machine

**Status**: Not Started
**Created**: 2026-10-26
**Owner**: joseph
**Priority**: Medium

## Overview

An overloaded laptop (`machine-A`) should be able to hand its ccy sessions, and whole agent
teams, to another Fedora laptop built by this repo (`machine-B`). The move is **cold**: stop
everything, migrate, resume on machine-B. Nothing is moved while it runs, and the tooling
refuses to start while any session or team member in scope is running.

This repo owns **both sides**: a send side on machine-A that gates, snapshots, packs and
fences, and a receive side on machine-B that verifies, preflights, unpacks and resumes. Both
are Go verbs on the existing binaries (`ccy-sessions`, `ccy-teams`), reusing `registry`,
`conversation`, `sessprobe`, `tmuxccy` and `restore`. Anything machine-B needs installed
(`age`, the receiving key) comes from a play.

Host members (ccy and cc sessions) resume by their **recorded conversation id**. LXC members
resume by `ccy --continue` run inside the LXC container, which the owner uses today and
expects to continue a session cleanly. Both rules are already settled for the same-machine
case by Plan 00203 Decision 13; this plan applies them across machines.

## Owner rulings (settled 2026-10-26)

- Cold only: "stop, migrate, resume on new machine". The send side gates on nothing running.
- This repo is in charge of the send and the receive side.
- `ccy --continue` run inside the LXC container resumes an LXC member.
- Host members resume by the recorded session id, never `--continue`.

## Goals

- `ccy-sessions export <checkout>` on machine-A refuses while any session of the checkout, or
  any member of any team its seats belong to, is running; otherwise it snapshots, writes one
  encrypted bundle and fences machine-A so neither machine can resume the same conversation.
- `ccy-sessions import <bundle>` on machine-B verifies the bundle, refuses with the play to run
  for anything machine-B lacks, unpacks owner-only and starts each session by its recorded id.
- `ccy-teams migrate <team>` drives a whole team: every member stopped, the homeserver moved
  with it (backup, play on machine-B, restore, remove on machine-A), members imported
  coordinator first, LXC members resumed by `ccy --continue` in their container.
- Every tool is Go in `src-go/`, unit tested through fakes; every install is a play edit.
- Proven end to end with a vmtest guest acting as machine-B.

## Non-Goals

- Live or warm migration of a running session.
- Moving one member while its team and homeserver stay on machine-A: that needs the overlay
  and v2 remote seats (Plans 00220, 00209); the tool refuses it and names them.
- A network transport between the laptops (ssh/rsync); a bundle file only (Decision 2).
- Carrying machine state: images, the Claude OAuth token, GH tokens, SSH keys,
  `ccy.env.local`, host_vars or the vault. Machine-B's own plays provide them.
- A different checkout path on machine-B in the first version (Decision 1).
- Encryption of transcripts at rest (Plan 00098).

## Context & Background

- Research, with every citation: [RESEARCH-cold-migration.md](RESEARCH-cold-migration.md).
  Its section 2 lists what a session depends on and the verdict for each item; section 4 is
  the design this plan follows.
- Inside ccy every transcript is under `<checkout>/.claude/ccy/projects/-workspace/` and every
  worktree path is `/workspace/...`, so the checkout is portable; the host-side state (registry
  record, tmux, tokens, images, seats and homeserver) is not.
- A copied transcript was verified to be **found** by `claude --resume <id>` in an empty config
  dir; that it **loads and continues** with a valid login is inferred and is closed by Task 5.2.
- [Plan 00135](../00135-ccy-sessions-survive-a-reboot/PLAN.md): the registry, its reader and
  writer, `registry.ResumeArgs` and the detached restore start. Same-machine only.
- [Plan 00203](../00203-teams-hot-control-and-robustness/PLAN.md): the conversation record
  (Task 3.10), resume by id (Task 3.11), team wind-down and shutdown (Tasks 3.14-3.16), the
  last-shutdown record and `ccy-teams resume` (Task 3.17), the LXC member connect and resume
  command (Task 3.18), and Decision 13. This plan builds on those and duplicates none of them.
- Related: Plan 00220 (overlay), 00209 (v2 seats keyed by checkout path), 00207 D25 (cc seats
  out of the checkout), 00098 (no sealer), 00178 (room history export).

## Tasks

### Phase 1: Shared foundations

- [ ] ⬜ **Task 1.1**: the scope resolver. Given a checkout, list every registry record whose
  `dir=` is the checkout, every seat under its `pingbus/seats/`, every team those seats
  belong to, and every member of those teams (other checkouts, cc sessions, LXC members).
  - Acceptance: unit tests over a fake registry, seat tree and team record; a member in a
    second checkout is listed; an unknown team is an error, not an empty list.
- [ ] ⬜ **Task 1.2**: the running gate, one Go function used by both sides. A member is
  running if any probe says so: tmux session on `-L ccy`, `restore.Judge` pane state, a
  `ccy=true` container through `sessprobe`, a held `seat.lock` (`hot.keeperAlive`), a live
  `cc` process, an LXC container whose member tmux session or service unit is up, a pending
  `state/restart-request.json`, a frozen session. It takes the union (Plan 00203 Decision 6).
  - Acceptance: each probe has a fake that reports running, and each alone makes the gate
    refuse, naming the member and the probe; a probe that cannot run (missing tmux, an
    unreachable LXC) refuses rather than counting as stopped.
- [ ] ⬜ **Task 1.3**: the migration manifest. Format, version, source repo commit, ccy
  version, highest Claude Code `version` in the transcripts, host path, each host member's
  rewritten record and recorded id, each LXC member's container, user and recorded working
  directory, each seat's team and handle, `HOOKS_DAEMON_HOSTNAME`, SHA-256 of every file.
  Built on the Task 3.17 last-shutdown record format, not beside it (Decision 13).
  - Acceptance: round-trip tests; a manifest naming a host member with no recorded id, or an
    id whose transcript is missing (`conversation.For`), is rejected at write time.
- [ ] ⬜ **Task 1.4**: IaC. `age` installed by the play that installs the ccy tooling, and a
  per-user receiving keypair generated on machine-B by a play (only the public key leaves
  it), private key 0600 outside every repo (Decisions 2, 3).
  - Acceptance: `--syntax-check` passes; the play is idempotent on a second run; the key is
    never written into a checkout; a `deploy.bash` and `acceptance.bash` in this plan folder,
    queued in `CLAUDE/Plan/meta-deploy.list` in the same commit.

### Phase 2: The send side (machine-A), solo sessions

- [ ] ⬜ **Task 2.1**: `ccy-sessions export <checkout> --to <recipient>` gate. Refuses on the
  Task 1.2 gate for the whole scope, an unpushed local branch with commits (Decision 5), a
  member type not yet supported, and a team in scope (until Phase 4). `--dry-run` prints
  the scope, the verdict per member and the bundle contents, and changes nothing.
  - Acceptance: tests show a refusal names every running member, not just the first; a dry
    run on a stopped checkout writes nothing.
- [ ] ⬜ **Task 2.2**: snapshot and pack. The registry records and ids are snapshotted before
  anything is removed; the archive excludes the fixed list in the research (sessions
  `*.json`, `shell-snapshots/`, `.last-launch.conf`, `ccy.env.local`, caches, hooks-daemon
  venvs, pid, sock and lock files, every `seat.lock`); `untracked/` per Decision 6; encrypted
  with `age` to the recipient; written owner-only, refused inside any repository.
  - Acceptance: a test archive contains none of the excluded paths; the manifest hashes match;
    a bundle path inside a checkout is refused; no plaintext archive touches disk.
- [ ] ⬜ **Task 2.3**: fence machine-A, only after the bundle is written and verified: remove
  the checkout's registry records and rename the checkout aside (Decision 7). The re-check of
  the gate runs immediately before the fence; a session started meanwhile aborts the fence.
  - Acceptance: after the fence `ccy-sessions restore` starts nothing from that checkout; a
    failure mid-fence leaves the bundle and prints what was and was not fenced.

### Phase 3: The receive side (machine-B), solo sessions

- [ ] ⬜ **Task 3.1**: `ccy-sessions import <bundle>` verify and gate. Decrypt with the play's
  key, verify every hash, refuse a non-empty target path, a path that differs from the
  manifest's (Decision 1), and any manifest member already running on machine-B (the
  Task 1.2 gate, never restore's silent skip). `--dry-run` as on the send side.
  - Acceptance: a tampered file, a wrong key and an occupied path each refuse before
    anything is unpacked.
- [ ] ⬜ **Task 3.2**: preflight, each refusal naming the play or command that fixes it:
  `allowed-hostnames` admits machine-B; every `mounts` source exists; every token named by
  `--token` exists (else `ccy --export-token NAME` on machine-A, Decision 4); the gh account
  and SSH keys (`play-github-cli-multi.yml`); the `--network`; ccy and Claude Code not older
  than the bundle's (Decision 12); `claude-yolo:latest` (`play-claude-yolo.yml`);
  `HOOKS_DAEMON_HOSTNAME` matches the manifest when seats travel.
  - Acceptance: one test per check; all failures reported in one run, then exit non-zero.
- [ ] ⬜ **Task 3.3**: unpack and resume. Unpack owner-only, write each rewritten registry
  record, start each host member detached through the restore path with `--resume <id>`,
  never `--continue`; no `set-going` unless asked (Decision 10). Print what started and what
  was refused.
  - Acceptance: tests through fake tmux and registry show the resume args carry the
    recorded id; a start failure for one member is reported and exits non-zero.

### Phase 4: Whole teams and LXC members

- [ ] ⬜ **Task 4.1**: `ccy-teams migrate <team> --to <recipient>` on machine-A. Gate on every
  member of the team stopped (Task 1.2), refuse while the homeserver would stay behind
  (Decision 9); `agent-bus-install backup-now --team <team>` through a root-run play step;
  the backup carried inside the encrypted bundle; members snapshotted coordinator first.
  - Acceptance: a running worker in another checkout refuses the whole migration; the
    homeserver secrets in the bundle are never in argv, logs or plaintext on disk.
- [ ] ⬜ **Task 4.2**: LXC members on the send side. Through the Task 3.18 connect setting,
  confirm the member's tmux session and service unit are stopped, then carry the member's
  checkout and the transcript store its `--continue` reads (Decision 14).
  - Acceptance: a fake LXC runner reporting a live member refuses; the carried store is the
    one Task 3.18's `transcripts:` setting names.
- [ ] ⬜ **Task 4.3**: the receive side for a team. Machine-B's host_vars carry the team with
  the same name, `server_name`, port and bus address (Decision 8; the owner moves them);
  `play-agent-bus.yml` installs it; `agent-bus-install restore` loads the backup; then
  `agent-bus-install remove` on machine-A is queued as the fence. Members are imported
  coordinator first: host members by recorded id, LXC members by the team's connect command,
  a `cd` into the recorded working directory, then `ccy --continue` (Plan 00203 Decision 13).
  - Acceptance: the installer is confirmed to accept the same bus address on two machines;
    a seat starts on machine-B without a reissue; a coordinator that fails to start stops
    the workers' start, with the reason.

### Phase 5: Proof and documentation

- [ ] ⬜ **Task 5.1**: user documentation in `docs/ccy.md` and `docs/agent-bus.md`: what moves,
  what machine-B must have, what is lost by design (background tasks, the warm prompt cache).
- [ ] ⬜ **Task 5.2**: acceptance with a vmtest guest as machine-B: a solo checkout and a team
  with one LXC member exported, imported and continued with a valid login, closing the
  research's one inferred step (a copied conversation loads and continues).
  - Acceptance: `acceptance.bash` with a `COVERAGE: n of m` line; anything not establishable
    in a guest is named for the owner.
- [ ] ⬜ **Task 5.3**: `qa-reviewer` over the full diff; every BLOCK and FIX-BEFORE-MERGE
  finding resolved.

## Dependencies

- Depends on: Plan 00203 Tasks 3.10, 3.11, 3.17 and 3.18 (conversation record, resume by id,
  last-shutdown record, LXC connect and resume) and its Decisions 18, 19 and 21.
- Depends on: Plan 00135 (registry and restore).
- Interacts with: Plan 00207 D25 and Plan 00209 (both move or re-key seats; export must
  follow when they land). Leaves single-member moves to Plans 00220 and 00209.

## Technical Decisions

Each open decision runs on its default until the owner decides. Reasons for each default are
in the research, section 4.

### Open: for the owner

01. **Checkout path on machine-B**: the same absolute path, a different one refused. The
    alternative: re-derive session names, rename `pingbus/sessions/<name>/`, rewrite `dir=`,
    rename a cc slug.
02. **Transport**: an encrypted bundle file the owner moves by any means. The alternative:
    ssh/rsync pull over the Plan 00220 overlay.
03. **Encryption**: `age` to a recipient key generated on machine-B by a play. The
    alternative: an `age` passphrase read from the terminal.
04. **The Claude OAuth token**: never in the bundle; `ccy --export-token` or machine-B's own.
    The alternative: carried inside the encrypted bundle.
05. **Uncommitted work**: carried verbatim; export refuses unless every branch with commits is
    pushed. The alternative: refuse any dirty tree and ship only `.claude/ccy`.
06. **How much of `untracked/` travels**: every worktree; `untracked/` except reference clones
    and plan run logs, with a per-run override. The alternative: none of `untracked/`.
07. **The fence on machine-A**: registry records removed and the checkout renamed aside. The
    alternative: a launcher tombstone check (a CCY version bump).
08. **Teams**: the homeserver moves with the team, same name, `server_name`, port and bus
    address. The alternative: wait for remote seats.
09. **One member while its team stays behind**: refused, naming Plans 00220 and 00209.
10. **After import**: sessions started detached at the prompt, no `set-going`. The
    alternative: set going, at the cost of a cold full-context first turn each.
11. **Where the verbs live**: `ccy-sessions export/import`, `ccy-teams migrate`. The
    alternative: one new `ccy-migrate` binary.
12. **Version skew**: machine-B's ccy and Claude Code not older than the bundle's. The
    alternative: require an exact match.
13. **Snapshot format**: built on Plan 00203 Task 3.17's last-shutdown record, so Decisions
    18, 19 and 21 there are settled first. The alternative: a separate migration record.
14. **What travels for an LXC member**: its checkout and its transcript store, into an LXC
    container machine-B's plays already built from the moved host_vars. The alternative:
    the whole container rootfs.

## Success Criteria

- [ ] Export and import both refuse while anything in scope is running, naming each member.
- [ ] A solo checkout and a team with an LXC member move to a vmtest guest and continue:
  host members by recorded id, the LXC member by `ccy --continue` in its container.
- [ ] After a migration, machine-A cannot resume any moved conversation or hold any moved seat.
- [ ] No secret in argv, logs, the repository or a plaintext file on disk.
- [ ] `./scripts/qa-all.bash` passes; `qa-reviewer` findings resolved.

## Risks & Mitigations

| Risk                                                    | Impact | Probability | Mitigation                                                     |
| ------------------------------------------------------- | ------ | ----------- | -------------------------------------------------------------- |
| A copied conversation is found but does not load        | H      | L           | Task 5.2 proves it on a guest before the tools are relied on   |
| Both machines resume one conversation                   | H      | M           | Gate re-checked immediately before the fence; fence by default |
| Root-of-trust homeserver secrets leak in transit        | H      | L           | `age` to a play-made key; never argv, logs or plaintext        |
| A probe that cannot run is read as "stopped"            | H      | M           | Task 1.2 refuses on any probe error                            |
| Plans 00207 D25 or 00209 move seats out of the checkout | M      | M           | Scope resolver owns seat discovery; one place to change        |

## Delivery & Milestones

<!-- Curated milestones + delivery commit hashes only (git is the SSoT for
     "when" — do not add dates). The blow-by-blow activity log lives in
     JOURNAL/00227-Journal-YY-MM-DD.md — see CLAUDE/PlanJournalling.md. -->

- Plan filed from the research.
