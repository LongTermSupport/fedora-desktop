# Plan 00209: the frame for the v2 design (N0)

This file is the shared brief for the four area designs of N0. It holds the owner's directions,
the rules every area obeys, and the seams between areas. Each area design is written in its own
file and is then merged into one `DESIGN-v2.md`. The earlier `DESIGN.md` (a staged in-place
replacement of v1) is superseded by the owner's directions below. It stays as a record, and its
requirement list (section 2) and its reasoning are still inputs.

## The owner's directions

1. "the python version is NOT a golden archetype. its a prototype and first draft. this go
   version DOES NOT have to stick to python approaches etc. FIX THINGS IMPROVE THINGS MAKE IT
   EXCELLENT"
2. "i dont even care about staying compatible - now is the time for breaking changes and really
   making this system PROPERLY DECENT"
3. "already running teams like yours msut keep workiing", "teams v2 can be a different thing"

What follows from them:

- **v2 is a separate system.** It is designed from the requirements, not from v1. Its wire
  protocol, on-disk formats, locks, CLI and process model are new, and none of them has to stay
  compatible with v1.
- **v2 installs beside v1 and touches nothing v1 uses.** It has its own binaries, plugin and skill
  names, paths, state directories, units, homeserver instances and ports, and sudoers files.
  Nothing v1 reads or writes is changed. A v2 install with no teams is inert.
- **Teams move one at a time.** Each team moves from v1 to v2 through a migration the owner
  chooses to run (from ccy-teams, through meta-deploy). There is no big-bang cut-over and no team
  mixed across v1 and v2.
- **Each migration** is fail-fast, has an acceptance that proves it, and has a rollback (the v1
  team stays intact until the migration commits). Nothing is lost silently: the design says
  exactly what happens to queued and in-flight messages, room history, accounts and seats.
- **v1 is retired** only once every team has moved, and that is raised with the owner then.
- **Decided:** no relay mode in v2 for now. It is deferred, not abandoned, and is specified on v2
  when the hooks-daemon plugin API (#119) ships. There is a keeper and a link in every member (the
  processes; "worker" is a team role only),
  with the resident cost measured and reported.

## What every area keeps

The requirements and the hard-won lessons, wherever they still apply to a system built
properly. Sources:

- `DESIGN.md` section 2 (R1-R50), section 3.3 (why no daemon), section 6 (privilege), section 7
  (concurrency) and section 12 (what v1 got wrong). Section 6's rule that nothing an agent in a
  `cc` session can run changes membership without the owner's password is narrowed in v2 (C's
  DC19): the password guards every change to accounts, roles, humans, security fields, backups and
  restores; which session holds which seat is guarded by the owner's uid and the container mounts
  only, so a host process running as the owner can re-seat sessions (C10, K12);
- the four `RESEARCH-*.md` digests (v1's commands, files, locks, Matrix calls, Tuwunel quirks,
  LXC fragility, stopped containers, incidents, ccy-teams' calls and their costs);
- the specs already written from v1:
  - `SPEC-proto.md` (P1-P84; the Unicode facts, envelope un-forgeability);
  - `SPEC-state.md` (S1-S45, L1-L12, findings);
  - `SPEC-matrix.md` (M1-M44);
  - `SPEC-pingbus-hooks.md` (Claude Code's hook contract, budgets);
  - `SPEC-view.md` (stage 1 reads);
- the lessons reports in `subagent-reports/261021-*`;
- Plan 00211's `SPEC.md`: the reports and questions rooms, threads, `ask-human`. This is
  behaviour v2 must have, not a format it must keep;
- Plan 00194's SPEC-inject expectations E1-E26 (delivery into a session) and Plan 00203 (hot
  control), as requirements;
- the security invariants (R37-R39) and the repository rules: CLAUDE.md, src-go/CLAUDE.md,
  CLAUDE/GoProjectRules.md, fail fast, IaC only, no install-specific names.

Each area states, for every requirement it carries, where it came from. For every v1 behaviour
it drops, it states why. For every new idea, it states what it fixes.

## The four areas and their seams

| Area                         | File                       | Owns                                                                                                                                                                                                                                                                                                                                                                                                          |
| ---------------------------- | -------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| A. Protocol and rooms        | `DESIGN-v2-A-protocol.md`  | The Matrix event schema of v2 (types, content, versions), the rooms of a team (main, reports, questions, any new ones) and their power levels, the trust model (what a member verifies before it believes anything), identities and handles, message kinds, threads and replies, references and the forge checks, limits and rate rules, and what a human sees in Element.                                    |
| B. Member client             | `DESIGN-v2-B-member.md`    | Everything in a member session: the one static binary and its CLI (every verb, output contract for agents, exit codes), the on-disk state of a member, the keeper and link process model, locks, sync, delivery into Claude Code (envelope, inbox socket, transcript proof, receipts), hooks, the status line, multi-team and hot control, the plugin and skill, LXC guest operations, and the resident cost. |
| C. Host side                 | `DESIGN-v2-C-host.md`      | The homeserver instance per team, the installer and its lock, the privilege model (what needs root, the reader, sudoers), admin operations, seats and bundles, the host_vars declarations, Element profiles, the health check and backups, LXC member apply, and what ccy-teams reads and how fast.                                                                                                           |
| D. Coexistence and migration | `DESIGN-v2-D-migration.md` | The v2 product name and every name, path, port, unit, plugin and skill name, chosen so nothing collides with v1. Proof that a v2 install does not touch v1. The per-team migration: what moves (accounts, rooms, history, seats, roles, humans' Element logins, queued and in-flight messages, outboxes), its steps, acceptance and rollback, how a running session moves, and how v1 is retired at the end.  |

**Seams.** Where two areas meet, the area named first below owns the decision. The other cites
it and does not restate it.

- A ↔ B: A owns the event content and the trust rules. B owns how the client applies them and
  how a delivery is rendered into a session.
- A ↔ C: A owns room layout and power levels. C owns how the admin creates and maintains them.
- B ↔ C: C owns the bundle a seat receives (what is in it, how it is placed). B owns how the
  member reads and uses it.
- D ↔ all: D owns every name and path. The others use placeholders, `<v2-bin>`, `<v2-state>`,
  `<v2-unit>`, until D's names are merged, and say what each needs to be.

Where an area needs a decision from another area that has not been made yet, it states its
assumption under a heading **Assumptions on other areas**. The integration pass settles these.

## Form of each area design

- Numbered requirements first, then the design, then decisions for the owner (each with options
  and a recommended default, per "DO NOT GATE ON ME"), then risks, then what it drops from v1
  and why.
- Concrete: schemas as JSON, paths, state machines, failure modes, how each piece is tested
  (fakes, fakehs, real Tuwunel behind a build tag, the VM lab).
- Fail fast everywhere; no fallbacks between systems.
- No install-specific names: placeholders only.
- No time estimates.
