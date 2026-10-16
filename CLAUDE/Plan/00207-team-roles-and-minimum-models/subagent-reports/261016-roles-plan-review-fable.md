# Plan 00207 pre-merge review (Fable)

Branch `plan/team-roles`, commits dda6780d and 59e16b2e. Plan only: `PLAN.md`, `SPEC-roles.md`,
the journal and the README row. No code.

## Verdict: FIX-FIRST

Three design points need a change in the spec before this is merged as the design the owner
decided. Everything else is confirmed or non-blocking.

1. The terminal note (§11.4) can be forged by the coordinator and is indistinguishable from a
   real one (hole H1 below).
2. §4.5 says "a report saying `ok` never grants anything", but §4.3 check 3 and gate check G6
   both grant on the report's `ok` (hole H5).
3. The deployer's judgement step (§6.4 step 2) is satisfiable with five one-word answers (hole
   H6). The owner asked for a mandatory sanity check; as written it is a mandatory form.

## 1. Fact-check of the claims about the current code

| Claim (SPEC)                                                                               | Verdict         | Evidence                                                                                                                                                                                                                                                                                                                   |
| ------------------------------------------------------------------------------------------ | --------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Two roles, `orchestrator` reads as `coordinator` (`protocol.py:48-53`)                     | CONFIRMED       | `helpers/pingbus/protocol.py:48-53`: `ROLES = (ROLE_COORDINATOR, ROLE_WORKER)`, `LEGACY_ROLES = {"orchestrator": ...}`                                                                                                                                                                                                     |
| `PING_EXTERNAL` authority text (`envelope.py:68-69`)                                       | CONFIRMED       | `helpers/pingbus/envelope.py:68-69`                                                                                                                                                                                                                                                                                        |
| Chooser offers "Join without adding (... as external)" (`widen.go:18-20,62`)               | CONFIRMED       | `src-go/internal/teams/chooser/widen.go:19,62`                                                                                                                                                                                                                                                                             |
| Role templates and `role_lines` (`hooks.py:124-132,238-250`)                               | CONFIRMED       | `helpers/pingbus/hooks.py:124-132` (four templates), `:238-251`                                                                                                                                                                                                                                                            |
| Hooks record the transcript path for the watcher (`hooks.py:770-781`)                      | CONFIRMED       | `helpers/pingbus/hooks.py:770-782` `_record_transcript`, called from `prompt()` at `:764`                                                                                                                                                                                                                                  |
| Syncer re-verifies the room on power-level and record changes (`syncer.py:9-16`)           | CONFIRMED       | `helpers/pingbus/syncer.py:9-17`                                                                                                                                                                                                                                                                                           |
| No launcher sets a model or effort                                                         | CONFIRMED       | `grep -- '--model\|--effort\|fallback-model\|ANTHROPIC_MODEL\|effortLevel'` over `claude-yolo`, `cc`, `cc-teams.bash`, `agent-bus-claude`, `members.py`: no hits. `claude-yolo:3927` passes only `--dangerously-skip-permissions "${CLAUDE_CMD_ARGS[@]}"`; `cc:382` `claude "$@"`; `agent-bus-claude:95`; `members.py:711` |
| Cited lines `cc:382`, `cc-teams.bash:143`, `agent-bus-claude:95`, `members.py:711`         | CONFIRMED       | All four line numbers land on the launch line. Note the paths: `files/var/local/claude-code/cc` and `cc-teams.bash` (the spec gives bare names, fine)                                                                                                                                                                      |
| Claude Code 2.1.301 has `--model`, `--effort`, `--fallback-model`                          | CONFIRMED       | `claude --version` = 2.1.301; `claude --help` lists all three                                                                                                                                                                                                                                                              |
| Status line JSON carries `model.id`, `model.display_name`, `effort.level`; daemon reads it | CONFIRMED       | daemon `handlers/status_line/model_context.py:24,175-177`, `context_sidecar.py:124`                                                                                                                                                                                                                                        |
| pingbus status line reads none of those fields                                             | CONFIRMED       | `grep -i 'model\|effort' helpers/pingbus/statusline.py`: nothing                                                                                                                                                                                                                                                           |
| Transcript fallback shapes `model_refusal_fallback`, `content[].type == "fallback"`        | CONFIRMED       | daemon `utils/model_fallback_records.py:49,53,62` (`KEY_MODEL = "model"`)                                                                                                                                                                                                                                                  |
| Supervisor types `/model <family>` only to undo a downgrade, never effort                  | CONFIRMED       | `.claude/ccy/claude-supervise.py:27-52,1382,5349-5354`                                                                                                                                                                                                                                                                     |
| Each seat has its own Matrix account, discarded password, minted token                     | CONFIRMED       | `helpers/agent_bus/admin.py:364-367` `create_account`, `:724-762` `add_member`                                                                                                                                                                                                                                             |
| Tokens are per checkout (`<checkout>/.claude/ccy/pingbus/seats/`), ccy and cc alike        | CONFIRMED       | `helpers/pingbus/seat.py:75-76`; `files/var/local/claude-code/cc-teams.bash:34,142` (`--root "$checkout/$CC_TEAMS_SEATS_DIR"`). So a worker container in the same checkout can read a deployer bundle there: true as stated                                                                                                |
| Invite-only (`preset: private_chat`), `visibility: private`, room version 12               | CONFIRMED       | `helpers/agent_bus/admin.py:571-573`; `protocol.py:29` `ROOM_VERSION = "12"`                                                                                                                                                                                                                                               |
| Federation, E2EE, room creation off; no admin to first user                                | CONFIRMED       | `helpers/agent_bus/render.py:87,90,94,95`                                                                                                                                                                                                                                                                                  |
| Agents cannot invite, kick or write power levels (power 0, needs 100)                      | CONFIRMED       | `protocol.py:999-1009`: `users_default: 0`, `invite/kick/ban: 100`, `state_default: 100`, `m.room.power_levels: 100`; pingbus refuses any deviation (`check_power_levels`, `:1016-1020`)                                                                                                                                   |
| History visibility defaults to `shared` (nothing sets it today)                            | CONFIRMED       | No `history_visibility` anywhere in `helpers/agent_bus/`; `private_chat` preset's spec default is `shared`                                                                                                                                                                                                                 |
| Tuwunel 1.9.4 pinned                                                                       | CONFIRMED       | `files/usr/local/share/agent-bus/tuwunel.pin`: `tuwunel_version: v1.9.4`                                                                                                                                                                                                                                                   |
| Open issues #653, #649 and "latest release 2026-10-10"                                     | NOT CHECKED     | Upstream facts, offline here; the spec attributes them to the bus-stack research report, which is fine                                                                                                                                                                                                                     |
| Admin credentials are in **root-owned** `/var/lib/agent-bus/<team>/secrets/`               | REFUTED (minor) | `helpers/agent_bus/admin.py:5-8`: the tree is `0700, the running user's`, and the running user is `agent-bus` (the wrapper drops to it), not root. The consequence claimed (a cc session on the owner's uid cannot read it without sudo) still holds. Fix the word                                                         |
| `CLAUDECODE` is set in tool shells                                                         | CONFIRMED       | `CLAUDECODE=1` in this review's own shell; M0 still right to confirm on cc                                                                                                                                                                                                                                                 |
| A `HUMAN` line needs `m.text` from a user ID in the record's `humans`                      | CONFIRMED       | `docs/agent-bus-protocol.md:281`; agent text is `m.notice` with the `agent_bus.text` key (`protocol.py:40,45,1044-1053`). An agent account cannot produce a `HUMAN` line: the sender ID is the server's, not the body's                                                                                                    |
| The research reports are under `untracked/agent-reports/`                                  | NOT CHECKED     | Untracked by design                                                                                                                                                                                                                                                                                                        |

## 2. Security holes in the design

**H1. The terminal note is forgeable by the coordinator, and the room cannot tell. (blocking)**
§11.4 posts `↩ <human> answered in the terminal: <excerpt>` from the coordinator's own account,
through `pingbus note-terminal`, and the spec itself tells the coordinator to run that command
by hand in the multi-team case. Nothing distinguishes a hook-queued note from one the agent
typed. A coordinator can post "↩ alice answered in the terminal: yes, go ahead" into the
management room, and every human reading it later takes it as alice's word. The whole point
of the room is "the full record"; a record an agent can write human-attributed lines into is
not one. The source classification is also too generous: "anything else is the human"
includes the hooks daemon's cron ticks (failsafe cron, `persistent_crons`), Claude Code's
own agent-team messages (`SendMessage`) and a `/loop` re-prompt, all of which reach
`UserPromptSubmit` and would be noted as human replies inside the 1800 s window. Fixes, in the
spec: (a) the note is never a `HUMAN` line, is shown as `PEER`-grade in `history` and in the
envelope, and the §11.3 text says a terminal note carries no authority and never answers a
question for the record; the human's actual answer, if it matters, is the human typing it in
the room; (b) drop the "run `note-terminal` yourself" path, and make the command refuse unless
it is fed a queue entry the hook wrote (a nonce in `terminal-notes/`), which does not stop a
raw post with the token but stops the easy path; (c) classify a prompt as "terminal input" only
when it carries none of the daemon's cron/loop markers, and say so. Name this limit honestly in
§6.5 as well: it is a control against confusion, not a wall.

**H2. The deployer gate through the coordinator's relay.** A worker cannot reach the deployer,
but the coordinator can paraphrase anything into a `say`. That is by design (the gate limits
who, not what), and authority still needs a `HUMAN` line the deployer verifies in its own
`history` of the deployment room, which a coordinator cannot produce (fact table, last rows).
The design holds; the spec should say in §3.2 that the lock narrows the deployer's inputs to
one agent voice and does not sanitise that voice, and that only G5 (the verified `HUMAN`) is
authority. Not blocking.

**H3. Room history as a bypass.** D21 sets `joined` on the deployment and liaison rooms, so a new
deployer does not read the past. The management room is `shared`, which is right (humans and
coordinator only). The `workers` room stays `shared` and a seat re-roled worker to deployer
keeps its synced cache (§12.2 "a kicked member"). Nothing here grants authority: a `HUMAN`
authority older than `authority_max_age_s` or older than the commit fails G5. Not a hole.

**H4. Reconciler crash halfway.** Removes-before-adds means a crash between the kick and the
invite leaves a seat in no room. The spec does not say the reconciler is idempotent and
re-runnable from the desired state, nor who re-runs it. It should: step 1 computes desired
versus actual, so a re-run converges; the health timer reports the gap as drift (missing from
a room) and names `ccy-teams apply`; the hot-control command that triggered it must exit
non-zero with the partial move named. Add those three sentences to §13.2. Not blocking.

**H5. "A report saying `ok` never grants anything" is false within the spec. (blocking)**
§4.3 check 3 admits a seat when its observed model (the keeper report, written by the session
into the container-writable checkout) meets the floor; G6 passes on `role check is ok`. For a
cc deployer the warden and the gate can and should read the host transcript directly, and
§4.5 says the warden does; G6 must too, and must not consult the report at all. For a ccy
coordinator with a floor the report is the only source and a forged `ok` passes assignment.
Either strike the sentence or make it true: assignment and G6 read transcripts where they
can, and the spec says the ccy case is covered by the stated non-goal (Plan 00203 D6).

**H6. The judgement step is satisfiable without doing the work. (blocking)** `deploy-gate judge`
refuses only an empty or missing answer. Five "no" answers pass. Tie each answer to the
check output: J1 must name every queued plan from G4 and every area from G8; J2 and J3 must
address each attention item G8 flagged, by id, with a word on it; J4 must name each
acceptance script; J5 must state the sessions `ccy-teams` lists as working. The command
refuses an answer that omits an item, and records the items beside the answers in
`verdict.json`. That turns the form back into a check.

**H7. "Disagreement means deny" coverage.** §13.4 covers the ping and agent-text paths at
delivery and the send path. Gaps: (a) the `HUMAN` path is ungated by role, correctly, but a
human posting in a room they are not listed in cannot happen (humans are in every room);
(b) `MEMBERSHIP` and local notices never meet a room; (c) the per-room record equality
check (exit 10) protects against one room carrying a stale record, good. The one path not
written down: a seat reading a room it is a member of but whose role `rooms_for` disallows
(drift against itself) is reported in `pingbus status`, but §13.4 does not say the member
stops *syncing* that room. Say that it does: a drifted room is not synced until repaired.

**H8. Forging a `HUMAN` envelope.** Not possible from an agent account: the sender ID is the
server's, agent text is `m.notice` with the `agent_bus.text` key, and the human path demands
`m.text` from a listed human ID (`docs/agent-bus-protocol.md:281`). Rooms do not weaken this.

## 3. Plan quality

- Every D-item has a recommendation: D1-D18, D20-D25 in the table; D19 is decided with the
  owner's words recorded. Good.
- Every milestone names its tests (M1-M6b "tests first"; M7 acceptance list; SPEC §3.7, §13.7).
- No time estimates in either file.
- Install-specific names: none. `fedora-desktop` (SPEC:469) is the permitted self-reference.
  `Owner: joseph` (PLAN:5) matches the convention of Plans 00203, 00205 and 00206.
- README row present (`CLAUDE/Plan/README.md:59`) and matches the status line.
- Journal has the scaffold, the design entry and the owner-decision entry.

## 4. Non-blocking points

- YAGNI: the **reviewer role** (D1) and **G9** are proposed without an owner ask; the argument
  "one fleet upgrade carries all" is sound, so keep D1 as a decision but cut G9 and
  `deploy.require_review` from §6.4 and §7.1 until D1 is yes. The **maximum** (§4.6) is an
  owner ask, keep it. The board letters and picker previews (§13.6) are fine as pane work.
- `root-owned` in §12.2 should read `owned by the agent-bus user, 0700` (fact table).
- §4.4 refuses `ANTHROPIC_MODEL` and `CLAUDE_CODE_EFFORT_LEVEL` in the environment, but the
  ccy launcher passes a fixed `-e` list (`claude-yolo:3901-3924`), so inside a container the
  environment is the launcher's; say the check is for cc and the LXC unit.
- §11.2's `pingbus ask` numbering `Q<n>` per team needs a home for the counter and the open
  set; say it is the management room's history (count `ask` posts without a reply), so the
  session holds no state.
- §13.5 step 1 says the reconciler invites only members "that report the new version". The
  only report channel is the keeper report, which is advisory (H5); an older member is never
  invited and so sees one room, which is safe. Fine, but say that an unreported member is
  treated as old.
- The spec repeats the owner's quotes in §3, §11.1 and §12.1 and PLAN "Decided by the owner";
  one copy in the plan and references from the spec would be enough.
