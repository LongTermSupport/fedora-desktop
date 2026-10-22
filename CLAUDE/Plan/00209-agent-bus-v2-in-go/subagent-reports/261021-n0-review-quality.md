# Plan 00209 N0 integrated review: quality and owner experience

Reviewer concern: is the design properly decent, and the simplest that meets the requirements.
Read: DESIGN-v2-FRAME.md, DESIGN-v2.md (all), B 1-3 and 5 (all of the member chapter except 2.15
and 6), A 1-13 and 17-19, C 3.1-3.5.0, 3.11, 3.13, 3.15, 4-5, D 1-3.3 and 5-9, PLAN.md lines 1-135,
the integration handoff. Worktree tip read at 0b498397.

## Verdict: FIX-FIRST

The design is careful, well reasoned and mostly decent. Its main quality problems are (a) the
agent-facing text disagrees between the chapter that owns its grammar (A) and the chapter that
owns the verbs (B), so an agent is told to run commands that do not exist and to do the weaker of
two checks; (b) the member's process model carries more hand-over machinery than the frame's own
decision ("a keeper and a worker in every member") needs; (c) a name collision at the centre of
the member; (d) the milestones the review is asked to judge are not written yet, and PLAN.md still
states the contract-era plan.

Counts: 0 blocker, 7 major, 9 minor.

## Findings, ranked by the value of fixing them

### 1. major: the envelope, frame text and skill tell the agent different things

Files: A 13.1, 13.2, 13.5, 13.7; B 2.2.1, 2.6 step 03, 2.7.2, 2.12.

A owns the envelope grammar and the corpus pins it exactly (A 13.3), but A's templates were not
moved to B's CLI:

- A 13.5 item 4 (the frame text, at every SessionStart) tells the agent to confirm a consequential
  HUMAN request with `teamchat --team <team> history` and compare the decoded text by hand. B7 and
  B 2.12 item 3 replace exactly that with `teamchat verify '<id>'`. The agent gets both, from the
  frame and from the skill, and the frame's is the weaker, error-prone one B7 exists to remove.
  A never mentions `verify`.
- A 13.2 `truncated:` points at `teamchat --team <team> history (id ...)`; B 2.7.2 says
  `truncated: ...; the whole text: teamchat verify '$<43>'`.
- A 13.1/13.2 `held back:` says "read them with `teamchat --team <team> history`"; B 2.7.3 says
  "`teamchat recv` shows them now". B is right: held-back items stay pending (BD7), and `history`
  is not how a pending item is taken.
- A uses a global `teamchat --team <team> <verb>` form (`history`, `status`, `thread`) in five
  templates; B's verbs take `--team` after the verb (`history [--team T]`, 2.2.1), and B 2.6 step 03
  writes `teamchat history --team <team>`. One of the two is wrong, and the envelope is the text an
  agent copies verbatim.
- A 13.2's reply command in the questions room is `teamchat --team <team> thread <q>`; B 2.2.1 says
  `reply '<id>'` decides team, room and thread and replaces `thread` for replies. Two ways to do one
  thing, and the envelope teaches the one B demoted.
- A 13.7 orders a delivery by `origin_server_ts` across teams; B24 and B 2.7.2 put HUMAN first.
- A 13.5 says the frame is told "once per session"; BD12 says at every SessionStart.

Failure: an agent with a truncated HUMAN request runs the `truncated:` command and gets exit 64; an
agent about to push on a forged envelope follows the frame's history comparison instead of
`verify`. Both are what the redesign set out to stop.

Fix: B's verbs win. Rewrite A 13.1, 13.2 (`truncated:`, `held back:`, `answered:`, the questions
reply command as `teamchat reply '<id>'`), 13.5 item 4 (`teamchat verify '<id>'`) and 13.7 (humans
first, as B24). Extend B 2.12's skill-versus-code test to every command in A's templates and the
frame text, so no envelope can name a verb or flag form the binary refuses.

### 2. major: run the worker for the whole session; delete the socket hand-over machinery

File: B 2.1, 2.4.1, 2.4.2, 2.8, 2.10 (leave-only worker), 2.3.1/2.3.3 (two writers).

The frame decided "a keeper and a worker in every member" (D15). B instead runs the keeper alone
when no team is held, which forces: a second, "reduced" server inside the keeper for the four hook
operations; a listening-descriptor hand-over in both directions over `SCM_RIGHTS` with drain and
stop-accepting phases; the session facts and the unusable count handed back and forth; two
alternating writers of `conversation.json` and `status.json`; `"worker": "none"`; and a third
process shape, the leave-only worker (2.10). Each is careful, and each is code, tests and failure
cases that exist only to save one idle process in a teamless session.

Simpler, and closer to the frame: the keeper always starts the worker. The worker binds
`worker.sock` itself (under `worker.lock`, unlink then bind) and is its only server. When a worker
dies its listener dies with it, so a client's connect fails at once with `ECONNREFUSED` and the CLI
exits 69: B45 ("no request waits on a socket nobody serves") is met by the kernel, with no keeper
server. Hooks already have their own "nothing answers" template (2.8). The keeper keeps only what
it keeps now in memory (session facts, inbox token) and hands them to each new worker, as it
already does for the token. One writer for every file.

Cost: a worker with no team is resident in a teamless session (estimated 12-20 MiB, B 2.14) with
no long-poll and one heartbeat a minute, which can be dropped while no team is held. B40's
no-team bar becomes "keeper and worker" instead of "keeper alone".

### 3. major: cut `repair` and the keeper's adopt mode

File: B 2.4.5, 2.4.4 (socket facts on Stop for an adopted session), 2.11 (`keeper-lost` resume).

A keeper death is a bug on a small event loop that makes no network call. For it the design adds
`repair --list`, `repair --runtime`, `keeper --adopt` with five refusals, the stale-socket unlink
exception to the never-unlink rule, `run.ended.reason = keeper-lost` with run-id matching so the
adopted run posts nothing, and the rule that `hook.stop` may set socket facts so an adopted idle
session is reachable. That is a second start path into the most delicate state in the member.

Fix: on a keeper death the worker exits as designed and the session runs bus-less, saying so once
(as now). The recovery is the one the owner already has: `ccy-teams restart`, which resumes the
same conversation (B43). The LXC unit restarts its own session. Delete adopt mode, `repair`,
`keeper-lost` resume, and the socket-facts-on-Stop rule. If keeper deaths prove common in the
pilot, that is a defect to fix, not a path to support.

### 4. major: "worker" names both the process and the team role

File: B throughout (2.1, 2.2.2, 2.2.4, 2.9), A 6.7, C 3.6.4.

The member process is "the worker"; the roster role is `worker` (beside `coordinator`). An agent
whose role is worker reads `status` records with `"role":"worker"` next to
`"kind":"runtime","worker":{...}`, and meets exit 69 with code `no-worker` ("tell a human"), which
reads as "there is no worker in this team". The owner reads "the worker crashed" in a log and
cannot tell a process from a member. This breaks the project's no-jargon rule at the exact point a
newcomer starts.

Fix: rename the process (for example `teamchat link`, "the link": it is the session's link to the
team's homeserver), with `link.lock`, `no-link`, `link-failed`, `"link": {...}` in the `runtime`
record. Also drop "bus" from user-facing strings (status line `bus runtime not running`, hook text
"the bus runtime is restarting", `bus membership refused`): the product is `teamchat`, and "bus" is
v1's word.

### 5. major: milestones N1-N6 are not written, and PLAN.md states the superseded plan

File: PLAN.md (Status, "Owner direction", Goals, Non-Goals, Owner decisions D5, Milestones M0-M6);
DESIGN-v2.md section 3 ("Later (N2)" and so on).

The review was asked whether N1-N6 deliver owner-visible value early and are each safely
deployable alone. They exist only as tags in DESIGN-v2 section 3 and in the integration handoff's
"left" list. PLAN.md still says "Only the compatibility contract binds", lists "a wire protocol 2"
as a non-goal, keeps D5 "wire protocol 1 kept through and after the migration", and stages the
work as M0-M6 on v1's data model. A build session that opens PLAN.md first is told the opposite of
the owner's direction 2.

Fix (task 2 of the handoff, before N0 closes): rewrite PLAN.md around DESIGN-v2. For the milestone
shape, from the quality side:

- each milestone ends with D 4.1's static gate and the footprint check passing, so every one is
  deployable alone and inert until a team is declared;
- put owner-visible value before LXC and migration: the first milestone that installs a member
  should finish with P3 (a throwaway v2-native team) usable from Element and the board. The owner's
  daily path is ccy, so a ccy member plus the ccy-teams rows is the vertical slice that shows value.
  The coordinator ordered LXC first (B 2.13); if that stands, say what the owner can use at the end
  of that milestone (an LXC-only pilot team in Element), so the value is still stated;
- migration (D 5) and the retirement come last, as now.

### 6. major: nothing posted in a room can ever be removed, even a human's leaked secret

File: A 4.2, 12.2 ("Delete a message"); C 3.3 (no redact verb).

Redaction is closed for everyone, including the admin's verbs. Agents run a secret check before
posting (A 7.3), humans do not: a human who pastes a token or password into main from Element has
no way to remove it, and nor has the owner, because `teamchat-admin` has no redact verb. The
history is `shared`, backed up daily and exported at migration, so the leak spreads.

The reason given ("the record an agent acted on must stay") is weaker than it looks: edits are
already allowed and delivered as revisions, and the original stays in history either way.

Fix: keep members' and humans' redaction closed (I4 unchanged), and add one admin verb,
`teamchat-admin redact --team T --event ID --reason secret`, reached through ccy-teams with the
owner's password. Members already drop an event carrying `redacted_because` (A 7.6 rule 2), so
nothing else changes. Add it as an owner decision with this default.

### 7. major: one delivery should never span several teams' stores

File: B 2.3.2 (`awaiting.teams`), 2.7.3 ("Several teams in one delivery").

A delivery that mixes teams writes an awaiting entry into each team's store in sorted order, then
the ledger into each, and on restart reasons about which stores hold the nonce, with a skip rule
for a team no longer held. It is a small two-phase commit across files, written to batch entries
from different teams into one model turn.

Fix: a delivery holds one team's items only. The delivery goroutine alternates teams. Every store
transition is then one atomic replace of one file, the `teams` list and its crash cases go, and
B20/B21 are easier to prove. Cost: in a session on several busy teams, a burst becomes two
deliveries 2 s apart instead of one; multi-team sessions are the uncommon case.

### 8. minor: exit 12 `no-room` contradicts A's fixed layout

File: B 2.2.2 (error example), 2.2.4 row 12; A 4.1, 19.

A creates all three rooms with the team and lists "the `no-room` refusal" among what v2 drops; B
keeps exit 12, "the side room does not exist yet ... created by the host when the team is
applied". Fix: delete exit 12 and code `no-room`; a missing or unverified side room is exit 22
`untrusted`, as A 6.5 says. Replace the error example.

### 9. minor: `ping` reads as a liveness check

File: B 2.2.1, BD4; D 6.1 A7.

To a newcomer and to a model, `teamchat ping alice` checks that alice is alive; here it sends a
closed-verb request (`review`, `fetch`, `halt`, or an answer such as `done`). D's acceptance A7
"one acknowledged ping" reinforces the liveness reading. Fix: name the verb `request` (`teamchat request review <ref> --to N`); the envelope kind can stay PING or become REQUEST to match.

### 10. minor: the first version of a new product prints `"v": 2`

File: B 2.2.2, BD3. Every other v2 record (store, session, socket request, roster) starts at 1;
only the agent-facing records start at 2, which tells a reader there was a v1 of `teamchat`. Fix:
`"v": 1`.

### 11. minor: `recv` and `inbox` exit 3 when nothing is pending

File: B 2.2.4 row 3. Kept from v1. Claude Code shows any non-zero exit as a failed tool call, so an
empty inbox looks like an error to the agent, and with JSON Lines an empty answer is already
unambiguous. Fix: exit 0 with no records; keep 4 for `wait` timing out.

### 12. minor: the skill carries the whole exit-code table into every member's context

File: B 2.12 item 5. Eighteen rows with their codes, loaded into every session. The "what to do"
column has three classes: fix the message (10, 13, 15, 64), wait (14, 20), tell a human (1, 21, 22,
69, 78). Fix: the skill teaches the three classes and that the error's `message` names the next
step; the full table stays in `teamchat help exit-codes`.

### 13. minor: ping conversations make main a wall of grey notices

File: A 7.2, 8.1, 12.1, OD-A3. With a few agents, every ping, `done` and `blocked` is a top-level
line in main, where the humans' own messages live. Fix: post answers as `m.thread` replies under
the ping (Element already shows a reply relation; a thread collapses it to one line with a count),
and render `pr:` and `commit:` references in the Element `body` as `https://github.com/...` URLs,
which Element links without rich text (OD-A9 stays plain).

### 14. minor: `teamchat.carried` is a permanent wire kind for a one-time act

File: A 7.7, 13.2 `carried:`, 13.3; B 2.7.2; D 5.2.1. A 7.7 puts the carried kind in wire major 1
with no feature name, so a v2 member will understand v1 migration items for as long as major 1
lives. Fix: put it under a feature name (for example `v1-carry`), so the retirement plan (D 7.2)
can drop it and its renderer without a new major. A larger simplification is worth one line in the
owner digest: drain before the fence (a bounded wait until running sessions' inboxes and outboxes
are empty), then report what remains, which D9 allows; it would remove most of the carry plan.

### 15. minor: the versioning machinery is built before its first use

File: A 3.1-3.2 (key registry with a corpus test, `features`, `proto.send`). The raise verb is
already deferred (DESIGN-v2 section 3 row 23). The registry test and the `features` decoding are
still N1 work for a protocol with one major and no features. Fix: keep the rule ("unknown keys
ignored, a trust-narrowing key needs a feature") as text and a refusal of any `features` list;
build the registry when the first feature is proposed.

### 16. minor: the "nothing answers" hook file is the one file a hook writes

File: B 2.8 (`<v2-run>/no-worker.json`). With finding 2 the keeper is always present and can count
these itself; until then this is the one hook-side write, and its name embeds the term of finding
4\. Fold it into the keeper's memory, or rename with finding 4.

## What I checked and found sound

- **DC4 (the board reads status records).** A good trade for the owner. The open costs about 0.1 s
  with no sudo and no process per team (C 3.13); liveness of ccy and host seats is live from the
  seat lock; only membership, devices, room drift and backups can be up to about 70 s old, and the
  one moment that needs fresh device facts (a sign-in) re-reads with a bounded, `sudo -k`-ended
  poll; an expired record is shown as a problem. No NOPASSWD rule. Keep it.
- `reply '<id>'` taking team, room, thread and addressee from the item (B4), `verify`, JSON Lines
  with a closed `code` set and the error object last on stderr, `team-failed` plus exit 5 for
  partial multi-team answers.
- Humans first in a delivery (B24), `ack` folded into an `answered:` line (OD-A2), flood-held items
  kept pending rather than consumed (BD7), missing deliveries returned to `recv` (BD8).
- Element: `m.text` with a mention only when an agent writes to a human, `m.notice` otherwise;
  questions as numbered threads that notify; reports quiet and read-only; reactions, polls and
  stickers closed by the allow-list; human edits delivered as revisions; replies and thread replies
  to an agent's own event address it (A 7.5 rules 3-4). Read receipts as the visible proof of
  delivery.
- The team id and display name split (TID): a rename is a display change only.
- The questions flow (A 10.6) and per-question reminders (OD-A6).
- One homeserver per team (DC1); its cost (one Element window per team) is the same as v1's.
- The migration as the owner experiences it: one command, one confirmation after M0's plan,
  rollback until commit, a report naming every item not carried (D 5.10). INT3 is now settled by a
  short id (0b498397).
- The status line: one exec, one file read, one lock probe, pushed on change (B 2.9).
- The defaulted decisions: none of the flagged ones looks wrong from the quality side; the one that
  is missing is the admin redaction (finding 6).

## Outside my concern, briefly

- DC16 (a released session keeps a valid token until `reissue`) is a security trade I did not
  weigh.
- The cost table's per-member bars (B 2.14) will change with finding 2.
