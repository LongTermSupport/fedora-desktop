# Plan 00225 review r2 (adversarial, Opus)

Scope: `git diff dcb76af56..HEAD` (PLAN.md, DESIGN.md, README row). Checked against
`helpers/pingbus/` (protocol.py, syncer.py, cli.py, config.py), the pingbus SKILL.md, the ccy
launcher and Dockerfile, and Plans 00207, 00211, 00222, 00224.

## Round 1 findings

All resolved in the text:

- B1: verbatim hand-off (LZ5a), `reworded` reason, LD11.
- B2: `not-your-handoff`, `earlier-than-question` and `wrong-room` reasons; the rule change is
  stated openly (section 3).
- M1: relays go to the coordinator only.
- M2: `human_text` is part of LZ1 and LZ10.
- M3: LZ16a and Task 7.4; an unreadable status counts as inactive.
- M4: LZ3 and section 7 cite 00207 SPEC-roles:97.
- M5: quiet items are never delivered while the liaison is active, with the 900 s fallback.
- M6: LZ4a and Task 1.1.
- m1 to m5: covered by the revision wake, LZ23, 4.1's note on terminal blocking, section 9,
  and LD11.

Facts I verified as true:

- `build_question` writes the body as `Q{n} {text}`.
- `parse_question` accepts a post from any agent.
- `_questions_outcome` behaves as LZ4a says.
- 00211 D5, D6, D9, D14 and Task 2.8 exist and say what the plan cites.
- 00222 R1, R2, R7 and its Non-Goals are quoted correctly.
- 00207's "liaison room" appears in both its PLAN and its SPEC-roles.

## Major

**M1. A verified relay can still hide what the human said.** LZ6 checks one human event on
its own. Three cases get past it:

- **Cherry-picking.** Two humans answer Q7: alice says "no", bob says "yes". The liaison relays
  only bob's "yes". The coordinator receives `HUMAN bob ... checked`.
- **Retraction.** The same human posts "wait, no" as a new message, not an edit. It stays a
  quiet item, so the coordinator does not see it until `liaison_relay_s` (900 s) has passed.
- **Reframing.** LZ4a makes the liaison's own questions-room posts valid. The liaison can post
  "to be clear, yes also covers X" in Q7's thread, and the human then answers "yes". The
  envelope shows `asked`, `note` and `answer`, but not that post.

Today the coordinator sees every one of these messages. This is a regression in the authority
model that the plan says is kept.

Fix: the coordinator's pingbus already reads Q7's thread to bind the relay. Have the relay
block (and `verify-human`) list every other human message attached to Q<n> after the post,
and every liaison post in that thread. Add a NOT VERIFIED reason, such as `superseded`, for
when the same human has posted later. Alternatively, forbid liaison posts in the thread of a
handed-off question. Add tests to Task 2.3.

**M2. Factual error in LZ12 and LZ14.** `pingbus history` of the main room shows only the
reader's own sends and items addressed to it. `Syncer.history` calls `_history_item`, which
calls `validate_event`, and that ignores anything not addressed to the reader (`not-addressed`
or `agent-text`). So the liaison never sees coordinator-to-worker traffic, or a `done` ping
between other members. Its progress updates rest on the reports room alone, and LZ14's "or a
`done` ping says so" cannot be met.

Fix: either design a read-only, team-wide view for the liaison (a protocol rule with tests,
plus the v2 requirement), or drop "bus history" and "done ping" as sources and say that
updates depend on agents posting reports.

**M3. Nothing produces LZ16's `LiaisonActive`.** The 00222 guard reads only each team's cached
role (`inbox.cached_role`, Go `statusline/roles.go`; 00222 R2). The LZ10 inputs are the
liaison's `agent_bus.status`, its questions-room join and the freshness of both. No cache of
these exists, and no task writes one. Task 4.1 tests only the pure `Decide`.

Fix: add a task in which the coordinator's syncer writes the activity verdict and its time to
a file the Go reader reads. Define how fresh the verdict must be, and test that a stale one
counts as inactive.

**M4. LZ17's backing rule breaks ccy.** ccy keeps all of Claude Code's state inside the
checkout, at `/workspace/.claude/ccy/`: credentials, history, and the supervisor wrapper
`claude-supervise.py` (claude-yolo:2350-2353 and 3634). If the writable paths are "backed by a
per-session directory, never the host checkout", the session loses its login, its resume
history and its wrapper on every launch.

Fix: these paths have to be rw binds of the real host subpaths. The design must then say
exactly which paths inside the checkout the liaison can write, and that `.claude/settings*`,
the hooks and the daemon config stay `ro`. Without that, a liaison could switch off its own
guard.

**M5. Phase 4 has no CCY version bump.** The guard is part of the member kit, and the kit is
baked into the ccy image (Dockerfile:428-440). Tasks 4.1 and 4.2 (the Go guard and the
`hooks.json` matcher) change the image, but neither is marked **CCY version bump**.

## Minor

- m1. Workers and the liaison can never start a conversation. LZ4 lets the liaison text only a
  worker that addressed it, and 4.1 lets a worker write to the liaison only to answer it. The
  owner asked for "maybe with other team members".
- m2. With an active liaison, `pingbus thread` with its human-mentioning ask still lets the
  coordinator ask the humans directly. Either gate it like `ask-human`, or name it as allowed.
- m3. `wrong-room` cannot be told apart from `not-found` with a single
  `GET /rooms/{questions}/event/{id}`: a main-room ID returns 404. Say how the check finds the
  event's room, or merge the two reasons.
- m4. LZ5a's `Liaison's note:` is described as a "section after it". `parse_question` runs
  `check_question_text` over the whole body after `Q<n> `, so the note becomes part of the
  question. Put the note under its own content key, or post it as a separate thread message.
- m5. LZ10's "`human_text` on" cannot be read by the coordinator, because the flag lives in
  the liaison's bundle. Use the questions-room join as the proxy and say so.
- m6. LZ10 says "no older than 600 s", but `agent_bus.status` carries `until`
  (`read_status`). State which value the check uses.
- m7. Task 1.0 omits golden tests for the guard, the board and `--list` bytes that LZ0 names.
- m8. LZ2's notice to the liaison ("no one to hand to") has no task.

VERDICT: FIX-FIRST
