# Review: Plan 00211 Phase 1 (reports and questions rooms, member side)

Branch `agent-a8f11e118d842a7bf-eaf9300d` (tip fe18dbb4), reviewed as `git diff F44...branch`.
Read-only adversarial review standing in for Fable, covering the qa-reviewer checklist.

## VERDICT: FIX-FIRST

The main room behaves as before for old readers, and the on-disk formats are compatible.
Three things should still change before the merge, and one is a process gate:

- the D9 sign-off is outstanding;
- the agent guidance ships unconditionally, before the rooms exist;
- side-room failures are not isolated from the main room in code that will run at deploy 2.

## Tests run (targeted, in the branch worktree)

- `tests.helpers.pingbus.{test_protocol_rooms,test_inbox_rooms,test_syncer_rooms,test_cli_rooms,test_envelope,test_limits,test_cli_status,test_hooks}`: 396 OK.
- `tests.helpers.pingbus.{test_syncer,test_cli_offline,test_protocol_doc,test_plugin_contract}`
  and `tests.helpers.agent_bus_lxc.test_members`: 333 OK.
- `test_admin` and the Go test were not run (host load).
- `git merge-tree F44 branch`: clean.

## What holds (verified in code)

- **Inbox, main room.** `inbox._stored` writes only `STORED_KEYS` when there is no `room` key, so a
  main-room item is byte-identical. `_revalidate` passes `room` through. An old reader that meets a
  questions item marks it `rejected` and skips it. It does not delete it, so nothing is lost.
- **Envelope, main room.** `_entry_lines` adds `room:`, `question:` and `revises:` only when
  `entry.room` is set, so a main-room envelope is unchanged. The HUMAN line gains a trailing `-`,
  which is append-only (§15). Every parser of it is in-process (`cli.py:1940,1947`). No Go or
  shell consumer parses `recv` HUMAN lines.
- **Rate limits.** The outbox gate keeps exactly `{bucket, recent}`. The report times are in their
  own `reports.json`, under the outbox lock. The old `DuplicateWindow.from_dict` accepts any
  string key, so the new verbs (`report`, `question`, `thread`) cannot upset an old reader.
- **File writes.** `rooms.json` and `reports.json` go through `_write_atomic`: an O_EXCL temporary
  file at 0600, fsync, then rename. Concurrent writers (the watcher and a short-lived command)
  converge, because both derive the content from homeserver state.
- **`report_per_hour`.** It is read only from an owner-set member `limits` object. The admin never
  writes it into a bundle.
- **The coordinator `human_text: false` rule.** It is refused only at role assignment:
  `members.py:459` (after `normalise_role`, present entries only) and `admin.add_member`. A sync
  only marks the room `refused`, which `status` and the hook context name.
- **The Go pin.** `TestParseRosterSkipsRoomLines` pins that ROOM lines are skipped.
  `agent-bus list` itself does not change in Phase 1.
- **The version chain.** It is consistent on the branch: each image-touching commit bumps,
  3.99.13.0/2.78.0 through 3.99.13.5/2.78.5, and the Dockerfile LABEL equals
  `REQUIRED_CONTAINER_VERSION`. The changelog has one entry for 3.99.13.5.
- **Plan scripts.** They follow R1 bootstrap, `plan_mode deploy`, `plan_require_host` and
  `plan_deploy_leg`. Acceptance is the last leg (R9), and `--fake` carries a STANDARD-EXCEPTION(R2).
  The `deploy-stage-1.bash` wrapper is a correct answer to meta-deploy passing no arguments. The
  meta-deploy order is right: spike, then deploy 1, with agent-bus before claude-yolo, which needs
  the built pingbus.
- **SKILL.md.** It says that workers never ask a human ("Questions go to the coordinator, never to
  a human") and that the coordinator bubbles up only what it cannot answer.
- **No install-specific names.** The only `**Owner**: joseph` is the repo-wide plan convention.

## Findings

### F1 (process, blocker): D9 is not signed off

PLAN.md D9 ("Extend v1 Python now, or wait for Plan 00209's Go") still reads "**Needs the
owner's sign-off**". It departs from the CLAUDE.md HARD RULE: a standalone Python CLI is ported to
Go when it is next reworked. This change adds four subcommands, a multi-room sync and an inbox
format, which is a rework. Merging to F44 queues the host deploy, so the merge is the commitment.

**Fix:** get the owner's D9 answer, recorded in PLAN.md, before merging. The alternative is to
hold the branch.

### F2 (high): agents are taught the rooms before deploy 2 creates them

The changes:

- `hooks.py` `role_coordinator` now always says "Run `pingbus questions` now…".
- Every `role_worker*` line says "Report … with `pingbus report`".
- SKILL.md teaches `ask-human`.
- SKILL.md tells workers "Write to a human only to answer a human who wrote to you".

None of this is gated on the rooms existing. Between deploy 1 and deploy 2, an open-ended window
(Phase 2 is unwritten and D9 is open):

- every coordinator session start runs `pingbus questions`, which exits 4 `no-room`;
- every report attempt exits 4 `no-room`;
- SKILL's exit-code row tells the agent to "tell a human". That is new main-room noise, the
  opposite of the goal.

The coordinator also loses its documented route to the human: `ask-human` fails, and nothing
says "until then, ask in the main room".

**Fix:**

- Emit the room sentences only when `state.side_rooms()` names the room. `_team_view` already
  reads it.
- In SKILL.md, make `no-room` mean "the rooms are not deployed: skip the report / ask the human
  in the main room as before", not "tell a human".

### F3 (high): a side-room failure can break the main room, in code that runs at deploy 2

`Syncer._reports_room` and `_questions_room` catch only `matrix.Forbidden` and `NotFound` around
`client.join`, and `verify_side` does the same around `get_state`.

`matrix.Client.call` maps any other status to a plain `MatrixError`. A restricted-join refusal
such as M_UNABLE_TO_AUTHORISE_JOIN, which Synapse returns as a 400, would raise one. So would a
500\. A `RateLimited` after its retries would also escape.

These calls run inside `verify_room` (`_sync_side_rooms`). The exception escapes and every
main-room command fails: say, recv and watch. That contradicts SPEC §4.3: "the main room stays
trusted". Phase 1 is the member code that will meet the rooms at deploy 2, and fixing it later
costs another member-side deploy plus a restart of every session.

**Fix:**

- Catch `MatrixError` (not the transient `Unreachable`) per side room.
- Record the room as `refused` or `untrusted` with the status.
- Add a test with a fake that answers 400 or 500 to the join.

### F4 (medium): the spike does not test what `verify_side` depends on

`verify_side` requires the power levels and join rules to match exactly (`_canonical` equality),
as the server returns them. The spike checks only the refusals (R1-R4). It never reads the
reports or questions `m.room.power_levels` and `m.room.join_rules` back to run
`protocol.check_side_power_levels` and `check_join_rules` on them.

A server that normalises either one (restricted `allow` ordering, or defaulted keys) would leave
every side room `untrusted`.

**Fix:** add R5, a read-back that runs both checks against the created rooms, including the
closed questions form.

Also: `spike.bash`'s header says "SPEC … amended before Phase 1 is deployed", but meta-deploy
runs deploy 1 even when the spike fails, as the completion report admits. Reword the header to
"before Phase 2".

### F5 (medium): merge conflict with Plan 00213's branch

Branch `agent-ac068362476b47038-2c58bb45` is at CCY 3.99.12.21. A
`git merge-tree branch 00213-branch` conflicts in four files:

- `CLAUDE/Plan/meta-deploy.list`
- `docs/ccy-changelog.md`
- `files/var/local/claude-yolo/Dockerfile`
- `files/var/local/claude-yolo/claude-yolo`

There is no conflict in `seat.py`: 00211 does not touch it.

**Fix:** whichever branch merges second takes the next version above both (for example
3.99.13.6 and container 2.78.6 if 00213 lands first). It must also fix the changelog's
"Follows 3.99.12.19" line and keep both meta-deploy entries, in dependency order.

### F6 (low): `history --room questions` gives different exit codes from `questions`

`history --room questions` with no room, or run by a worker, raises `RoomUntrusted` (exit 10,
which SKILL.md glosses as "not trusted … tell a human"). `questions`, `report` and `ask-human`
give exit 4 `no-room` or `role` for the same situations.

**Fix:** route it through `_side_room` and `_coordinator`.

### F7 (low): with several coordinators, each one's posts are logged as drops by the others

`_questions_outcome` drops every non-human sender as `sender`. The record may name several
coordinators, so one coordinator's `ask-human` and thread posts are drop-logged and counted by
the others on every sync. The spec calls this "impossible without drift", which is not true.

**Fix:** IGNORE a post whose sender holds the coordinator role.

### F8 (low): the first sync after a questions join skips the gap fill

On the fresh-join path, `_questions_events` returns the timeline after the join and ignores
`limited`. If more than 50 events arrive between the join and that sync, the gap is not filled.

### F9 (low): plan and doc drift

- `CLAUDE/Plan/README.md` lists 00211 as "Not Started", but PLAN.md says In Progress.
- SPEC.md line 3 still says "Design only. Nothing here is built."

### F10 (low): nits

- `inbox.admit_report` writes `reports.json` with no directory fsync, unlike `write_json_file`.
- `envelope.HEADLINE_MAX` and `QUESTIONS_ROOM` duplicate `protocol.HEADLINE_MAX` and
  `PURPOSE_QUESTIONS` (DRY).
- `spike_cleanup` sends `kill -TERM` and then runs `rm -rf` at once, without waiting. It is
  harmless because the probe already stopped the server.

### F11 (owner heads-up)

- The new `members.py` refusal fails the whole deploy-1 agent-bus play if any present LXC entry
  is declared `role: coordinator` with `human_text: false`. That failure is loud and correct, but
  the meta-deploy description should name it.
- S1 checks the staged build-context pingbus, not the rebuilt image. It is labelled honestly; that
  is acceptable.
