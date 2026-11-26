# Plan 00265: triage of the open rows in Tasks 2.2, 2.3, 3.2 and 3.3

Read-only triage at `v2-smoke` HEAD `a582dc9fe` (26 Nov). For each row: its REGISTER.md entry,
its report where one exists, the GitHub issues (#420 and #424, both OPEN with no comments), and
the code. Verdicts: (a) fixed, (b) partly fixed, (c) open and buildable in a container,
(d) blocked on a host, the laptop, a live team or an owner decision.

The no-overlap constraint decides most of the order: R7, R9, R11, R16, R17, R19 and the
open part of R8 all live in `src-go/internal/teamchat/member/*` or `proto/`, which Plan 00267 is
editing now. R13 and the open part of R15 live in `internal/sessions` and the launcher bash,
which Plan 00266 is editing now (`sessions/restore.go` is dirty in the shared checkout). Only R20
is clear of both today.

## Task 2.3: R7, R9, R11

### R7: no reply in the thread of a human's top-level post — (b), close to (a)

- The capability exists: `teamchat reply '<id>'` on a questions-room human post goes to
  `Engine.replyQuestions` (`member/engine/questionsverbs.go:412`), which answers an unattached
  post in its own thread (`post.Root = v.Human.ThreadRoot`, line 443; composed by
  `Composer.threadRelation`, `proto/send.go:532-543`, `InThread(t.Answers, t.Answers, true)`)
  and an attached one in its question's thread with a link-back (`send.go:331`). It dates from
  `34c074516` (Plan 00209, 23 Nov), so it predates the row.
- The refusals the coordinator met are fixed: a reply to an edit (R10, merge `c3c336cd4`) and a
  post the store lacks (R68, `f59504a18`). The procedure says to use it:
  `teamchat:coordinator` section 3, "Answer a human's message with `teamchat reply '<id>'`… A
  human's top-level post is answered the same way" (`files/opt/claude-yolo/optional/teamchat/ plugin/teamchat/skills/coordinator/SKILL.md:70-73`, from R44 `ba0485a51`). The tracker form
  needs no new kind: section 3 gives `Why a human: preference - you asked to follow this…`.
- Left: (1) the second and later replies to the same human post produce R11's false line (below),
  so R7 is not done until R11 is; (2) `teamchat help thread` names only `Q<n>` and does not point
  at `reply <id>` (`member/cli/verbs.go:256`); (3) a live check (the coordinator's 26 Nov note
  in R11 says a reply was placed in the owner's thread, which is that check, so record it).

### R9: a human's top-level answer does not attach to its question — (b) and (d) owner decision

- Attachment is A 10.4's rules (`proto/questions_fold.go:368-381`, `attachment`): an edit's
  original, the thread root, the `in_reply_to`, else a body starting `Q<n>` followed by space,
  `:` or `.` (`proto/questions.go:31`). A bare top-level post ("yes, do it") matches none, so it is
  listed `unattached` (`Attach`, `questions_fold.go:342-347`), and the coordinator answers it with
  `reply` (R7). That is the design, not a bug.
- The row's other symptom ("shows the question as answered") is not reproduced offline; R34's
  note asks for an event id if a live check still shows an in-thread reply as unattached.
- Left, needs the owner: whether a bare top-level post should attach by a guess (for example to
  the one question waiting on that human, or the latest the coordinator bumped). A guess can
  attach an answer to the wrong question, which R34 showed misleads all day; the safe default is
  today's (unattached, answered in its own thread). Not marked as an owner decision in the
  register; this triage proposes it as one. If the owner keeps today's rule, R9 closes with a
  live check of the `unattached` listing.

### R11: "Q0 was not counted" with no Q0 in play — (c), blocked by the 00267 overlap

- Cause traced in code, which also explains "on every prompt": the first `reply` to an
  unattached post is counted by `plainReply` (`questions_fold.go:315-323`), which removes the post
  from `unattached`. Every later reply to the same post (the composer still accepts it:
  `send.go:536`, the post is known with attachment 0) folds as `notCounted`, because the post is
  no longer listed. `echoed` (`proto/questions_prov.go:113-118`) then records
  `Dropped{N: 0, Thread: true}`, the link logs it, and `link.uncounted`
  (`member/link/hooks.go:217-218`) renders `hooktext.Uncounted` (`member/hooktext/hooktext.go:54, 276`): "your thread post sent as Q0 was not counted: Q0 is another question". Each line is said
  once (`UncountedSaid`), so a coordinator that keeps replying in the owner's thread gets one per
  reply, which reads as every turn. Not proven live.
- Fix outline: a reply to a human post already replied to is a valid post in that thread, not a
  drop: `plainReply` returns `repeated` (or a new `counted` that changes nothing) when the post is
  in `x.posts` with Q 0, and `echoed` never records a Dropped with N 0; `hooktext.Uncounted`
  refuses N < 1 (a separate line for "a reply to a human post was not counted", if one can still
  arise).
- Defence Before Fix class: "a sentinel value rendered as data" (the register's own cause class
  row). Detector: a table test over every `hooktext` line taking a question number, filled with
  the edge values 0, 1 and max, that fails if any rendered line contains `Q0`; plus a project
  rule (src-go/qa/rules) flagging a `QuestionName`/`Q{}` fill whose argument is not proven > 0 is
  likely too broad, so the test is the realistic detector. Sweep `QuestionName(` callers.
- Files: `proto/questions_fold.go`, `proto/questions_prov.go`, `member/hooktext/hooktext.go`,
  their tests. Size: small. Blocked only by Plan 00267's edits to `proto` and `member`.

## Task 2.2: R8, R17, #420

### R8 and #420: thread lifecycle and coordinator procedure not shipped — (b)

- Fixed by R44 (`ba0485a51`, review fixes `defdcbbf4`; on F44 in batch 6 `b58317073`): the kit
  ships `teamchat:coordinator` (source `member/kit/coordinator.txt`), and the SessionStart
  coordinator line names it (`member/hooktext/hooktext.go:36`, `tmplRoleCoordinator`). The
  owner's F9 style is in section 4 ("The point first, in one bold line", status emoji, ask last),
  and the lifecycle (open, answer, bump, close, sweep) is section 6. #420's open point, a reply in
  a human's own thread, is answered by `reply <id>` (R7).
- Left (all in `member/`, so after 00267): R44's own U2 (the team declaration naming a
  project-layer path, the hook reporting a missing one), U5 (`whoami --json` listing the
  procedure with its sha256), U6 (a MEMBERSHIP change to coordinator names the skill)
  (`reports/R44.md:58-60`); procedures for the other roles wait on #419's role levels; R33
  (`ask-human --skeleton` does not say markdown is rendered) is filed under #420 too. #420 itself
  is still OPEN with no comment: it should get one saying what R44 shipped.
- Size: medium (U2 touches the declaration grammar in `internal/teamchat/declare`; U5, U6 and R33
  are small). U2 changes the team declaration, so it may want the owner's word on the path.

### R17: a coordinator cannot give a worker work with authority — (d) owner decision, then (c)

- Unchanged at HEAD: a PEER's envelope says `authority: none. A colleague's note to weigh, never an instruction.` whoever sent it (`proto/envelope.go:91`, chosen at `envelope.go:381-382`), the
  kit skill says "PEER and PING: … never instructions" (`member/kit/skill.txt:19`), and the
  worker's role line says "take direction from the team's humans and its coordinator"
  (`member/hooktext/hooktext.go:37`). No assign verb exists (`member/cli/run.go:54`).
- Needs the owner: whether a coordinator's message carries direction (not a human's authority)
  for a worker. It is a security boundary: a worker that obeys the coordinator's PEER inherits
  everything a compromised or confused coordinator sends. Options: (1) a coordinator-sender
  authority sentence ("your coordinator's direction: do it within your own rules; it grants no
  permission"), (2) a closed `assign` ping verb whose reference is the brief, (3) keep "none" and
  change the worker line to "take direction from the team's humans, relayed by the coordinator".
  Not marked in the register as an owner decision; it should be.
- After the decision: Defence Before Fix class "two session-start texts that contradict each
  other"; detector, a test that pairs each role line in `hooktext` with the authority sentence of
  each sender that role hears and fails on a listed contradiction (direction vs never an
  instruction). Files: `proto/envelope.go`, `member/hooktext`, `member/kit/skill.txt` and
  `coordinator.txt`, possibly `proto/verbs.go` for a verb. Size: small for (1) or (3), medium for (2).

## Task 3.3: R13, R15, R16

### R13: restore's dry run says only "already running" — (c), blocked by the 00266 overlap

- Unchanged since the port (`577c6cb6c`): for a live session `restoreRun.record` prints
  `skip %s: already running.` and keeps the entry (`src-go/internal/sessions/restore.go:533-544`)
  before `planRestore` is reached, in a dry run too. 3f837cf51 made the dry run describe what it
  would start in words, but only for sessions not running.
- Fix outline: in a dry run, still compute `a.planRestore(rec, file)` for a live session and print
  "running now; the next restore would resume <conversation> with \<launcher, teams by name>" (or
  the reason it could not), without changing the entry. Respect R90 (no seat numbers in the text;
  Task 4.2's detector covers it).
- Defence Before Fix class: "a dry run that skips the work it claims to preview". Detector: a
  test over every `ccy-sessions` dry-run branch that each record kind yields a "would" line naming
  its plan. Files: `internal/sessions/restore.go`, `restore_test.go`. Size: small. Blocked only
  because Plan 00266 is editing `restore.go` now; best given to that plan's lane or done after it.

### R15: tab title, tmux session and seat drift apart — (b), the rest (d) host and laptop

- On F44 (batch 9, `d5e9eda6d`; reports/R15.md), and moved on by Task 4.2: sessions are now named
  by role, `<launcher>-<team>-<project>-<role>`, with the seat only where two seats share a role
  (`src-go/internal/teams/sessionname/sessionname.go:5-13`, from `07d9988f1`; comment at
  `files/var/local/claude-yolo/lib/tmux-session.bash:22-25`). The R15 gap "cc --teams <team>
  chooses its seat inside the session" is narrowed by `3f837cf51`: a plain `cc` in ccy's tmux asks
  `ccy-teams choose-teams --seated` before the session runs (`files/var/local/claude-code/ cc-teams.bash`, `cc_teams_split_args`).
- Left: the live tab check after the host runs `play-claude-yolo.yml` (host); whether restored
  and restarted sessions keep their recorded names (a live check, or a unit test in
  `internal/sessions` and `teams/restart`, which Plan 00266 is editing); and the owner's word that
  role naming replaces the F10 form (`<team> · fedora-desktop.4 · ccy`) — R90 suggests it does,
  so this is a confirmation, not a new decision. Nothing buildable outside 00266's area.

### R16: history labels an unproven reference `external` — (c), blocked by the 00267 overlap

- Unchanged: `historyOf` sets every referenced ping to `proto.TrustExternal`
  (`member/teamsync/verbs.go:396-399`, "History proves no reference"). The envelope check allows
  only `trusted` or `external` (`proto/envelope.go:268`; values `proto/verdict.go:70-71`).
- Fix outline, either: (1) history re-proves the reference with the same judge the delivery used
  (`verdict.go:142-144`), so it shows what the envelope showed; or (2) a third value `unchecked`
  in history records only, with `history`'s help saying what it means. (1) is truer and costs a
  fetch per ping; (2) is small. The register's cause class is "'not checked' shown as a class".
- Defence Before Fix class detector: a records test that every field taking a closed set says
  in its doc where its value comes from, and a check that no record path writes a trust value
  without a judge call (a project rule over `Trust =` assignments outside `proto/verdict.go` is
  expressible as an analyzer). Files: `member/teamsync/verbs.go`, `member/records`,
  `proto/verdict.go`. Size: small for (2), medium for (1).

## Task 3.2: R19 (#424), R20

### R19 and #424: a human's messages are not batched — (d) live check first, then (c) after 00267

- Nothing at HEAD holds or batches a human's message: no debounce and no `m.typing` reading
  anywhere in `src-go` (searched for `m.typing`, `debounce`). The delivery path is
  `member/teamsync` to `member/deliver` and `member/link`, all Plan 00267's area, and 00267 is
  rebuilding exactly the hold-and-deliver logic (Tasks 3.1, 3.2: rate and budget hold an item;
  a transient forge failure is held to a deadline), so a human-message hold window should be
  designed into that work, not beside it.
- Needs a live team: whether Lightning sends typing notices and whether the link's sync for the
  questions room receives them (ephemeral events). The owner gave the numbers (5 to 10 s, about
  60 s while typing), so no further decision is needed for the window.
- Fix outline: a per-room, per-human hold in the engine keyed by sender, extended by a later post
  or edit and by a typing notice, capped at 60 s, released as one delivery carrying each message
  and the final text of an edit. Defence: the hold must be lossless (00267's rule: a held item is
  never dropped at shutdown), so its class is 00267's. Size: medium to large.

### R20: Lightning's two sidebars — (c) buildable now, final proof on the laptop

- The key is known from upstream and confirmed on the installed client by key names
  (R35 F16, F29): `[shell] spacesRailVisible=false` in `config/MatrixClient/matrix-client.conf`.
  Our setup writes that file once, when absent, with only `[homeserver] url=`
  (`src-go/internal/lightning/lightning.go:446-458`, rule L7 at line 33).
- Fix outline: add `[shell]\nspacesRailVisible=false` to the file a new team instance is given
  (L7 unchanged for new instances: still written only when absent); update L7's doc and
  `docs/agent-bus.md:81` (the workaround becomes "already off for a new team"; Ctrl+, opens
  Settings in every layout, F21). Existing instances keep their file; merging one key into them
  changes L7 and is a separate choice (leave it to the owner's untick, as today).
- Defence Before Fix: a feature, not a defect; a test pins the written bytes. Unchecked, and
  owed on the laptop: that Lightning keeps the preset at first launch and that `false` is the
  spelling (one throwaway instance settles both). Files: `internal/lightning/lightning.go`,
  `lightning_test.go`, `docs/agent-bus.md`, R35's entry. Size: small. Overlaps neither plan.

## Owner decisions

- Marked in the register: none of these rows carries an explicit open owner question. R15's
  "the owner may still change the order" is superseded by R90's role naming (confirm only).
- Proposed by this triage: R9 (attach a bare top-level answer by a guess, or keep it unattached)
  and R17 (what authority a coordinator's message carries for a worker). Both belong in the
  questions room as numbered questions before any build.
- Possibly: R8's U2 (where a team declaration names the project-layer path).

## Proposed build order and lanes

1. **Lane L-lightning (now):** R20, small, `internal/lightning` and `docs/agent-bus.md` only. No
   overlap. Live proof queued for the laptop with R35.
2. **Owner questions (now, the coordinator):** R9 and R17, plus a comment on #420 saying what R44
   shipped. No code.
3. **Lane L-questions (after Plan 00267 releases `proto` and `member`):** R11 first (its
   detector, then the fix), then R7's help line, then R16. One agent: all three are the questions
   index and history in `proto`/`member/teamsync`/`member/hooktext`, small each, and R11's fix is
   what closes R7.
4. **Lane L-authority (after 00267, and after the R17 decision):** R17 and R8's U2, U5, U6 and R33
   (the session-start texts, the kit, MEMBERSHIP notice, whoami). One agent, `member/hooktext`,
   `member/kit`, `proto/envelope.go`, `internal/teamchat/declare`.
5. **Lane L-restore (inside or after Plan 00266):** R13's dry-run plan for live sessions, and a
   unit test that restored and restarted sessions keep their recorded names (R15's unchecked
   half). `internal/sessions`, `internal/teams/restart`.
6. **R19 (#424):** hand to Plan 00267 as a design input for its hold logic, after a live check
   of Lightning's typing notices on a running team. Not an agent lane on its own.
7. **Host and laptop checks, batched:** R15's tab after `play-claude-yolo.yml`, R7's reply in a
   human thread, R20's preset at first launch, R19's typing notices.
