# Plan 00225: team liaison role

**Status**: Not Started (plan only; retargeted to agent bus v2, `teamchat`, since v1 is feature-frozen; the round-5 review's three blockers and the owner's Element input are folded in, review in [subagent-reports/](subagent-reports/); decisions LD1-LD13 carry coordinator defaults the owner may change; no real team can use it before Plan 00209 Phase 6 migrates it to v2)
**Created**: 2026-10-23
**Owner**: joseph
**Priority**: Medium

## Overview

The owner, dictated: "a coordinator is very very busy on a reasonably sized team ... it's too
busy to handle my human questions. because between one message from me and the next message
from me, it might get multiple [messages] from different agents. ... we need another role within
teams ... whose focus is ... human interface ... it's the one that would give me progress updates
and would ask me questions. and the coordinator would delegate to it when it needs to talk to me.
and it would mean that the coordinator thread then doesn't have to be checked for questions that
are actually for me. ... [it] would not do any work, would not do any development, would not do
any coordination, would not do any triage or anything, it would purely discuss things from my
point of view. Ideally it has read-only access to all of the relevant code ... primarily its goal
is just to ask questions and then to liaise with the coordinator and maybe with other team
members." The owner named the role **liaison**.

This plan adds `liaison` as a team role in agent bus v2 (`teamchat`), at most one per team. v1
(Python `pingbus`) is feature-frozen, so nothing here is built in v1 (DESIGN section 0). The
coordinator keeps running `teamchat ask`; with an active liaison, `teamchat` hands the question to
the liaison instead of posting it. The liaison asks the human, in the questions room or its own
terminal (one conversation in both, mirrored), and is the one woken for human messages there. A
question posted for authority is the coordinator's own text, verbatim and never edited. The
liaison passes each answer back with `teamchat relay`, which names the human's event. The
coordinator's own `teamchat` fetches that event, the question it answers, the question's edit
state and the hand-off behind it from the homeserver. It shows the human's verbatim words only
when all bind to a hand-off the coordinator sent. So the liaison's words never count as
authority, and the HUMAN and PEER rules hold for all of them (DESIGN section 3 states the one
addition). The liaison builds progress updates from the reports room, the questions room and
what members write to it, citing its sources. It cannot edit, commit or push: the guard denies
it, in ccy and LXC its checkout and launch configuration are read-only, and it is launched with no
SSH keys and no write-capable GitHub token. A team with no liaison behaves exactly as it does
today.

The design, with numbered requirements LZ0-LZ24, is [DESIGN.md](DESIGN.md).

## Goals

- `liaison` is a v2 roster role, one per team, needing a coordinator, held by a session with no
  other seat (DESIGN LZ1-LZ4a).
- Human questions flow coordinator -> liaison -> human -> liaison -> coordinator, and the
  coordinator is woken for the questions room only as a fallback (LZ8-LZ11).
- A relay is a `PEER` text to the coordinator. The human's answer is checked by the receiver on
  the homeserver against the coordinator's own verbatim, unedited hand-off, shown with the relay
  and every liaison post around it, and confirmed by `teamchat verify-human` (LZ5-LZ7).
- The owner talks to the liaison in Claude Code or Element interchangeably; only a human-sent
  Matrix event counts as authority (LZ7a, LZ7b).
- Progress updates come from the reports room, the questions room, the hand-offs and members'
  texts to the liaison (main-room traffic between others is not read), and each one says when it
  is from and what it is based on (LZ12-LZ14).
- The liaison cannot edit, commit or push: a guard in every runtime, a read-only checkout and
  launch configuration in ccy and LXC that the session cannot undo, and no write credentials
  (LZ15-LZ20).
- With no liaison, or an inactive one, behaviour is byte for byte today's (LZ0, LZ10).

## Non-Goals

- Any v1 change: v1 is feature-frozen (Plan 00209 direction 7).
- A filesystem or credential boundary for a host `cc` liaison (same uid as the owner; LZ19).
- A liaison shared across teams. Plan 00207 records the owner's ruling against a liaison that
  relays between teams, so one session holds one team's liaison seat (DESIGN section 7, LD1).
- Timed progress digests (LD8), labels for the liaison (LZ22), relays to workers (LZ5).
- Editing Plan 00209's, 00222's or 00224's files: their owners get the requirements (Tasks
  7.1, 7.2, 7.4).

## Dependencies

- **Plan 00209 Phase 6 (migration).** v2 is on F44 (Plan 00246), but teams run v1 until they are
  migrated; a liaison can serve only a migrated team. Phases 1-6 can be built and tested on the
  VM lab and a throwaway v2 team before that; Task 8.2's acceptance needs a v2 team.
- **Plan 00222** (the guard and `Decide`): not built, and written against v1's role source. Phase
  4 extends it if it has landed, or builds `Decide` with LZ16a's inputs itself (LZ16a).
- Plan 00221 (the tree) for LZ22's board line; until it lands, the flat board gets the role word.
- Plan 00224 (`notify-owner`) takes LZ21 as a requirement (Task 7.1).
- Plan 00194 (live delivery), which the hand-off latency rests on; in v2 the delivery is the
  member's keeper.

## Tasks

Every build task is test first: the failing test is committed or recorded before the code.

### Phase 0: Facts

- [ ] ⬜ **Task 0.1**: `triage.bash` in this folder (host; throwaway container, v2 team and unit,
  removed in an `EXIT` trap). It records:
  - whether a ccy container started with `CAP_SYS_ADMIN` can make `ro` bind mounts after the
    entrypoint's writes, a real Claude Code session run against them (hooks fire, the session
    starts and writes its state), and, after the kit binary drops the capability from every set
    and sets `no_new_privs`, that the session's own `umount` and `mount -o remount,rw` on a bound
    path fail with `EPERM` (LZ17); if any of it fails, LD12 is the design;
  - which paths inside the checkout a ccy session (Claude Code with the hooks daemon) writes,
    found by launching one with the checkout `ro` and logging each `EROFS`, each classified
    as state (bound `rw` at its real path) or configuration (stays `ro`; LZ17), with every
    entry of `.claude/ccy/` classified and the files the entrypoint writes at launch listed;
  - every file the host launcher reads at the checkout's `.claude/ccy/` (LD12's list, LZ17);
  - whether `git status` and `git log` work on a `ro` checkout, and that a host commit shows
    through the `ro` bind;
  - the same for an LXC unit with `BindReadOnlyPaths=`;
  - that a coordinator's token can `GET /rooms/{room}/event/{id}` and
    `GET /rooms/{room}/relations/{id}` in the questions room, and sees an edit and a redaction of
    another member's post (LZ6);
  - which v2 signal tells a coordinator's member that the liaison's session is live (LZ10);
  - how a GitHub token's write capability is read at launch (LZ17a).
- [ ] ⬜ **Task 0.2**: Findings in `RESEARCH-facts.md`, with the exact files behind DESIGN
  section 8's packages; LZ17 and LZ20's writable list is the measured one; a refuted fact changes
  DESIGN.md first.
- [ ] ⬜ **Task 0.3**: The owner answers LD1-LD13, or the defaults stand. Each is recorded in the
  journal.

### Phase 1: The role in the v2 protocol (upgrade-first)

- [ ] ⬜ **Task 1.0**: Golden tests for a team with no liaison: envelopes, wakes, `ask` posts,
  the questions-room outcomes. Phases 4 and 6 open the same way, with goldens for the guard's
  answers and for the board and `--list` bytes before their code (LZ0). All of them stay green
  through every later phase.
- [ ] ⬜ **Task 1.1**: `proto`: the `liaison` roster role, at most one, needing a coordinator; the
  sender-class rules of LZ4 (refused on send, dropped `role` on receipt); the `relays`, `handoff`,
  `handed-off`, `liaison_note` and `mirror` content keys and their grammar; LZ4a's questions-room
  outcomes (the liaison receives; its post is ignored `liaison-post` at a coordinator, never
  dropped); `questionsSpec` gains the liaison. Tests over every role and verb pair, refusal tests
  first.
- [ ] ⬜ **Task 1.2**: `docs/agent-bus-protocol.md`'s v2 part: the role, the keys, and the
  receiver's check (LZ5, LZ6). A note to Plan 00209's owner for its A design (Task 7.2).

### Phase 2: The member side

- [ ] ⬜ **Task 2.1**: Delivery and state:

  - the liaison's rooms: main, questions woken, and the reports timeline read (LZ9, LZ12);
  - the coordinator's quiet questions items: only those on a handed-off `Q<n>` are owed; never
    delivered while the liaison is active, marked `relayed`, delivered on the keeper's
    `liaison_relay_s` wake, on an edit after relay, and when the liaison goes inactive with the
    messages since it was last seen active (LZ9). A test first: a human's unattached message, a
    mirrored terminal exchange, or one on a question the liaison asked for itself, never reaches
    the coordinator while the liaison is active;
  - LZ10's producer: the coordinator's `liaison.json` per team (`joined`, `live_since`,
    `read_at`), written atomically each sync from Task 0.1's live signal; the active test over
    it, where missing, stale or unreadable means inactive;
  - after a relay of `Q<n>`, later human messages on it, and any edit or redaction of the `Q<n>`
    post, delivered directly (LZ6).

  Tests use a fake clock and the fake homeserver (`matrix/fakehs`).

- [ ] ⬜ **Task 2.2**: `teamchat` verbs:

  - `say --to-liaison`;
  - `ask` hand-off routing with `--update` and `--direct`, and the liaison's `ask --handoff`,
    which posts the hand-off's text verbatim with any `liaison_note` in its own key, and refuses
    later liaison posts in that thread (LZ5a);
  - a coordinator's `thread --ask` handed off like `ask` while a liaison is active;
  - `relay` to the coordinator only (with `--terminal`);
  - the liaison's terminal mirror (LZ7b): a `mirror: terminal` post, refused inside a `Q<n>`
    thread;
  - `verify-human`, `handoffs` and `history --room reports` (liaison only);
  - `thread`, `questions` and `history --room questions` opened to the liaison (LZ4a);
  - the hand-off timeout notice (LZ5, LZ7, LZ8, LZ11).

  The `relays` refusal tests come first.

- [ ] ⬜ **Task 2.3**: The envelope's `relays:` block, checked on the homeserver, with a test
  for each `NOT VERIFIED` reason of LZ6 (`not-human`, `redacted`, `not-found`, `unattached`,
  `not-your-handoff`, `reworded`, `edited`, `earlier-than-question`). Also tested: every other
  human answer and edit on `Q<n>` is listed, every liaison post in the questions room between
  the `Q<n>` post and the answer is listed (in the thread or not), and two answers that differ
  show `answers differ`. The `edited` case covers an `m.replace` of the `Q<n>` post by the liaison
  and a redaction of it, each made after the human answered; `verify-human` returns non-zero for
  both.

- [ ] ⬜ **Task 2.4**: `hooktext` role lines (LZ23). The member skill gets a Liaison section:
  LZ7's rule, LZ7b's mirror and confirmation, LZ14's update shape, and the hand-off flow. Also
  the liaison's notice when its team has no coordinator (LZ2), with a test.

- [ ] ⬜ **Task 2.5**: The container version bump the member kit's `teamchat` change needs, and
  `teamchat version`'s marker raised. **CCY version bump.**

### Phase 3: The admin side

- [ ] ⬜ **Task 3.1**: `teamchat-admin role apply` with a `liaison` role:
  - LZ1, LZ2 and LZ3's refusals, naming the holder or the other team, and a member declared with
    `human-text` false refused;
  - the refusal while a running member reports an older `teamchat` (DESIGN section 9).
- [ ] ⬜ **Task 3.2**: The rooms apply invites the liaison to the questions room and kicks a
  former one (`MembershipDiff` over `questionsSpec`); `health` reports a liaison that has not
  joined (offline means `OFFLINE`, never a failure). An LXC member may be declared `liaison`.

### Phase 4: The guard

- [ ] ⬜ **Task 4.1**: `Decide` extended as LZ16a says (`Call` and `LiaisonActive` inputs), or
  built with them if Plan 00222 has not landed, 00222's own cases kept as its first tests; table
  tests, then the code:
  - LZ15's liaison set: the git and gh allowlists, with cases for each allowed form, unlisted
    verbs (`pull`, `add`, `apply`, `restore`, `clean`, `worktree`, `update-ref`), `-C dir`,
    `env` prefixes, `git -c`, `--output`, `--ext-diff`, `--textconv`, `--exec-path`, `gh api`
    (with and without `-X`), and a quoted commit message;
  - LZ16's coordinator `AskUserQuestion` rule, for an active and an inactive liaison;
  - LZ3's single-role session.
- [ ] ⬜ **Task 4.2**: The kit's `hooks.json` gains a PreToolUse entry whose matcher is
  `Edit|Write|NotebookEdit|Bash|AskUserQuestion` (plus 00222's tools), and a test pins it. The
  deny texts are compared byte for byte. The guard reads `liaison.json` (LZ16), tested on a
  fixture home: missing, stale and active.
- [ ] ⬜ **Task 4.3**: The guard and `hooks.json` are in the member kit baked into the ccy image:
  **container version bump and CCY version bump**.

### Phase 5: Read-only code and no write credentials, per runtime

- [ ] ⬜ **Task 5.1**: `ccy-teams internal launch-mounts` (Go): the `ro` workspace and an `rw`
  bind of each state path measured in Task 0.1 at its real host path, entry by entry, never
  `.claude/ccy/` whole. Then, as Task 0.1 decided, either the kit binary the entrypoint calls once
  after its own writes (binds each configuration path `ro`, stops the launch naming any that
  fails, then drops `CAP_SYS_ADMIN` from every set and sets `no_new_privs` before `exec`), or
  LD12's per-seat directory, re-rendered at every liaison launch, with the launcher's reads
  (restart request, `config.json`, `sessions/`, `.last-launch.conf`) pointed at it (LZ17). Tests
  first, on a fixture, for a liaison launch: no configuration path is writable once the session
  starts (the project's `.claude/settings*`, hooks and daemon config, and in `.claude/ccy/` the
  user `settings.json`, `plugins/`, `skills/`, `claude-supervise.py`, `mounts`, `ccy.env`,
  `Dockerfile` and `allowed-hostnames`); an unclassified path is `ro`; a `disableAllHooks` written
  in one liaison session is gone at the next launch; on the in-container path, `umount` and
  `mount -o remount,rw` from the session fail; `/root/.claude.json` is untouched; nothing changes
  for any other role. **Container version bump.** The launcher appends Go's output in place of
  the workspace `-v`, adding no line (`qa-bash-limits` proves it), and the ccy launch tests pin the
  arguments first, red. **CCY version bump.**
- [ ] ⬜ **Task 5.2**: LZ17a: a liaison launch with SSH keys or a write-capable GitHub token is
  refused by the board, `ccy --teams`, Quick Launch and the LXC apply, naming which; tests first.
- [ ] ⬜ **Task 5.3**: LZ18: the board's `relaunch for read-only` state and the role-notice text,
  in Go with tests.
- [ ] ⬜ **Task 5.4**: v2's LXC apply: tests on the rendered unit come first. The unit gets
  `BindReadOnlyPaths=` and the `BindPaths=` lines for a liaison and none otherwise; a role change
  restarts the unit and the apply says so (LZ20).
- [ ] ⬜ **Task 5.5**: For `cc --teams` launched as the liaison, Go computes
  `--disallowedTools Edit Write NotebookEdit` (LZ19). The launchers do not grow.

### Phase 6: Host UX (Go)

- [ ] ⬜ **Task 6.1**: `ccy-teams`:

  - the roster reader takes `liaison`;
  - the board's liaison line, on the tree when Plan 00221 has landed (LZ22);
  - "Make liaison" and "Stop being liaison", with the refusals;
  - the role offered in bare `ccy --teams` and `cc --teams`, and in Quick Launch's summary.

  Picker tests come first. **CCY version bump** (ccy-teams ships with ccy).

### Phase 7: Handoffs to other plans

- [ ] ⬜ **Task 7.1**: A task line in Plan 00224's PLAN.md: its gate accepts `liaison` (LZ21). Its
  owner adds it, through the coordinator.
- [ ] ⬜ **Task 7.2**: A note to Plan 00209's owner seat, through the coordinator: the liaison
  role, its keys and the receiver's check, for v2's A and B design documents. This plan edits
  none of 00209's files.
- [ ] ⬜ **Task 7.3**: Plan 00207's SPEC-roles and PLAN: "liaison room" renamed "collaborator
  room", in one docs-only commit, so the word has one meaning. A note there that 00207 M2 and
  this plan's Phase 1 share one fleet upgrade if both are pending (DESIGN section 9).
- [ ] ⬜ **Task 7.4**: A requirement note for Plan 00222's owner, through the coordinator:
  LZ16a's `Decide` inputs, the two Non-Goals narrowed to "for workers", and that under the v1
  freeze its role source is v2's.

### Phase 8: Docs, deploy, acceptance

- [ ] ⬜ **Task 8.1**: `docs/agent-bus.md`'s v2 part: the liaison, how to seat one, what a relay
  proves, the terminal and Element switch, and what the read-only mount and the missing
  credentials cover per runtime.
- [ ] ⬜ **Task 8.2**: `deploy.bash` (`play-agent-bus.yml`, `play-claude-yolo.yml`, as v2's plays
  are then named) and `acceptance.bash` with a coverage line, both queued in `meta-deploy.list`.
  On a throwaway v2 team:
  - the stage-1 upgrade check;
  - `role apply` refused while a member is old;
  - a hand-off and its relay, with the envelope showing `checked on the homeserver`;
  - a relay naming an agent's event, refused on send;
  - a forged raw relay naming an old answer to another question, shown `NOT VERIFIED`;
  - the `Q<n>` post edited by a raw `m.replace` after the human answered: the relay shows
    `NOT VERIFIED: edited` and `verify-human` exits non-zero;
  - a terminal exchange mirrored into the questions room, and the owner's one-word confirmation
    there relayed as verified;
  - the liaison's `Edit`, `git commit` and `gh api -X POST` denied;
  - a write to the `ro` ccy checkout failing, and a push by `ssh` and by `curl` to the API failing
    for want of credentials;
  - a human's post in the reports room refused (LZ24);
  - a team with no liaison identical to before;
  - the liaison stopped: `ask` posting directly;
  - hand-off latency recorded.
- [ ] ⬜ **Task 8.3**: Adversarial review of each phase's diff until MERGE, then `qa-reviewer`.

## Decisions (coordinator defaults; the owner may change any)

| ID   | Decision                                                                           | Default                                                                                                                                                                                                                                      |
| ---- | ---------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| LD1  | One liaison per team, or one per human across teams                                | **One per team, one session per liaison seat** (LZ3, DESIGN section 7). A shared liaison would first need the owner to reverse Plan 00207's ruling against a liaison relaying between teams                                                  |
| LD2  | A liaison without a coordinator                                                    | Refused at assignment (LZ2)                                                                                                                                                                                                                  |
| LD3  | The coordinator loses `AskUserQuestion`                                            | Yes, only while a liaison is active in its team (LZ16)                                                                                                                                                                                       |
| LD4  | The coordinator's `ask` with an active liaison                                     | Handed to the liaison; `--direct` only after the hand-off timeout or with an inactive liaison (LZ8, LZ11)                                                                                                                                    |
| LD5  | An answer typed in the liaison's terminal                                          | Information only, `relays: terminal`; a decision that needs authority is confirmed in the questions room (LZ7a, LZ7b)                                                                                                                        |
| LD6  | Who owns `notify-owner`                                                            | Both may; the liaison is told to use it and the coordinator to hand it over (LZ21)                                                                                                                                                           |
| LD7  | Read access beyond the liaison's own checkout                                      | None at first; other repositories by a later plan if needed                                                                                                                                                                                  |
| LD8  | Progress updates                                                                   | When the human asks, or when the coordinator hands one over; no timed digests (LZ13)                                                                                                                                                         |
| LD9  | Timers                                                                             | `liaison_offline_s` 600, `liaison_relay_s` 900, `liaison_handoff_s` 1800, per team in the team's declaration                                                                                                                                 |
| LD10 | Where it is built                                                                  | **v2 only, all Go** (DESIGN section 0). Was: v1's Python member side as 00211 D9's interim; the owner's v1 freeze of 2026-11-02 settles it                                                                                                   |
| LD11 | May the liaison reword or edit a hand-off it posts                                 | **No**: posted verbatim, a `liaison_note` may go with it in its own key; a reworded post fails the check (`reworded`), and an edited or redacted one fails it too (`edited`, LZ5a, LZ6)                                                      |
| LD12 | How the ccy configuration is kept read-only                                        | In-container `ro` binds only if Task 0.1 shows the session cannot undo them once the kit drops `CAP_SYS_ADMIN`; otherwise, or if the owner prefers no `CAP_SYS_ADMIN` at launch, a per-seat `.claude/ccy/` re-rendered at each launch (LZ17) |
| LD13 | How a terminal answer gains authority (the owner's Claude Code and Element switch) | The liaison mirrors the terminal exchange into the questions room beside the `Q<n>` post and asks the owner to confirm there; the owner's reply in Element is the event a relay names (LZ7b)                                                 |

## Risks & Mitigations

| Risk                                                      | Impact | Probability | Mitigation                                                                                                                      |
| --------------------------------------------------------- | ------ | ----------- | ------------------------------------------------------------------------------------------------------------------------------- |
| The extra hop's latency slows an urgent question          | M      | M           | Live delivery wakes the idle liaison at once; a hand-off timeout and the inactive fallback (LZ10, LZ11); latency measured (8.2) |
| A stale or wrong summary misleads the owner               | M      | M           | `as of` time, source IDs and "no news since" in every update; it asks rather than guesses (LZ14)                                |
| A relay is taken as the human's authority                 | H      | L           | Relays are `PEER`; the human's verbatim event and the unedited question are checked by the receiver, not the liaison (LZ5-LZ7)  |
| The liaison edits the question after the human answered   | H      | L           | Any edit or redaction of the `Q<n>` post fails the check (`edited`) and is delivered to the coordinator directly (LZ6)          |
| The liaison pushes code by a route the mount does not see | H      | L           | No SSH keys and no write-capable token at launch; refused by every launcher (LZ17a)                                             |
| The liaison undoes its own read-only binds                | H      | M           | Binds trusted only with `CAP_SYS_ADMIN` dropped and `no_new_privs` set, probed; otherwise LD12 (LZ17)                           |
| The liaison is offline, and questions go unanswered       | M      | M           | Inactive after `liaison_offline_s`: the old route comes back, the coordinator is woken, nothing blocks (LZ10)                   |
| The `ro` mount breaks a session's own writes              | M      | M           | Task 0.1 measures and classifies them; state bound `rw` at its real path, configuration `ro` (LZ17)                             |
| A host `cc` liaison writes through Bash                   | L      | L           | Stated as guard-only (LZ19); ccy or LXC for a liaison with a boundary                                                           |
| Old members misread a roster naming `liaison`             | H      | M           | Upgrade-first; `role apply` refuses while a running member is old (DESIGN section 9)                                            |
| Teams stay on v1 for long, so the liaison is unused       | M      | M           | Stated as a dependency (Plan 00209 Phase 6); built and accepted on a throwaway v2 team                                          |

## Success Criteria

- [ ] A coordinator's `ask` reaches the human through the liaison. The answer comes back to the
  coordinator as a relay, with the human's verbatim words marked `checked on the homeserver`.
- [ ] A relay naming a non-human event is refused on send; a forged one, and one whose question
  was edited, are shown `NOT VERIFIED`.
- [ ] The owner can answer in the liaison's terminal and confirm in Element, and only the Element
  confirmation counts.
- [ ] The liaison cannot `Edit`, `Write`, commit or push; in ccy and LXC a Bash write to the
  checkout or the launch configuration fails, and the session cannot remount it.
- [ ] The coordinator is not woken for questions-room messages while the liaison is active, and
  is woken when it is not.
- [ ] A team with no liaison behaves as before; the golden tests pass.
- [ ] `qa-changed.bash` passes on each commit, every review ends at MERGE, and `qa-reviewer`
  passes.

## Delivery & Milestones

<!-- Curated milestones + delivery commit hashes only (git is the SSoT for
     "when" — do not add dates). The blow-by-blow activity log lives in
     JOURNAL/00225-Journal-YY-MM-DD.md — see CLAUDE/PlanJournalling.md. -->

- Plan and design filed (plan only); reviews in `subagent-reports/`.
- Adversarial review: five Opus rounds, each fixed in its own commit. Round 5 (CHANGES) found
  three blockers: in-container `ro` binds the session could undo, question posts whose edits went
  unchecked, and push credentials the mount does not cover. All three are fixed (LZ17, LZ6,
  LZ17a), with the round's should-fix items.
- Retargeted to v2 on F44 (DESIGN section 0), with the owner's Element input folded in (LZ7b,
  LZ24).
