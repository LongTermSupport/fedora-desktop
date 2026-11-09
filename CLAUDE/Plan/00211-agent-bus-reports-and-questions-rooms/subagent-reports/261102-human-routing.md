# Task 2.14: where an agent's words to a human go

Branch `fix/pingbus-human-routing`, from F44 `b83158ca8`. v1 bug fix, no new command.

## The defect class (Defence Before Fix)

**Class:** an agent's message to a human sent where the human cannot answer it. In the main
room Element's "Reply in thread" carries no mention, so a human's answer under an agent's
message reached no agent, and reports cannot be answered at all.

**Detector:** pingbus's own send check (`cli._no_human_here`, inside `_explained_targets`, run
by `say` and `send`), plus the receive rule in `protocol._human_outcome`.

**Red proof:** the new tests were run before any code change: 18 of the 20 new tests failed
(9 failures, 9 errors). The other two were the before-the-rooms case and agent-to-agent
traffic, which pass by design.
Tests: `tests/helpers/pingbus/test_cli_human_routing.py` (end to end, fake homeserver) and
`TestHumanAnswerUnderAnAgentsMessage` in `tests/helpers/pingbus/test_protocol.py`. Green
after: all 41 pingbus test modules, 1853 tests, OK. They run in the helper suite, which
`qa-changed`/`qa-all` run, so the rule blocks.

**Sweep** (skill, docs, plugin, hook texts) for any instruction to write to a human in the main
room or to ask in a report, and what was done with each:

- `SKILL.md`, which said several times that a worker writes to a human and the coordinator is
  copied, that ack/nack/done/blocked "may also go to a human", gave a `say --to alice` example,
  and said "Not routine progress" about reports. All rewritten: a new "Never write to a human
  in the main room" rule, the questions room as the two-way channel, reports at every
  checkpoint with a checkpoint example, and "Needs you" in questions only.
- `hooks.py` session-start role texts, which said "copy you on what they tell humans" and "it
  is copied on what you tell humans". Removed. `role_reports` now asks for a checkpoint report,
  and `role_questions` names the two-way channel.
- `STYLE.md`, which put "Needs you" in reports and showed it in the example. Now version 2:
  a report never has a "Needs you" part, and there are two examples (a checkpoint report, and
  a question carrying the "Needs you" part).
- `docs/agent-bus.md`, which said "Answers are replies" (in the main room), "a reply without a
  mention reaches no agent" and that the coordinator is copied on what a worker tells a human.
  Updated.
- `docs/agent-bus-protocol.md`: §7 (addressing, and the agent-text rule), §9 Unheard, §12
  inbox `answers`, the REPLY section, and the `send`/`say` table rows.
- Left as is: the "before the side rooms exist" fallback in the skill (still true, since the
  rule applies only once the rooms exist); plan SPEC/design records (history, not read by
  agents); hook texts that say "tell the human" (they mean the session's own terminal user);
  `src-go/internal/teams/seats/role.go`'s owner-facing note.

The `defence-before-fix` skill's independent searcher and conformance reviewer were not
dispatched. The sweep was done with git grep.

## The rules and how each is enforced

1. **No agent addresses a human in the main room** (enforced). Once the side room of the
   sender's role exists (the questions room for a coordinator, the reports room for a worker),
   `say` and `send VERB` refuse any human among the addressees with `refused: target` (exit 4).
   The refusal names `pingbus ask-human` and `pingbus thread Q<n>` for a coordinator, and
   `--to-coordinator` and `pingbus report` for a worker. `say --all` names the agents only.
   The `REPLY` command of a main-room `HUMAN` line becomes `ask-human --re` (coordinator) or
   `say --to-coordinator --re` (worker), in `recv` and in the delivery envelope. Teams without
   the side rooms work as before, which matches the skill's existing `no-room` fallback.
   The check is send-side only: a receiver still accepts an older pingbus's text that names a
   human, so a mixed fleet does not drop traffic.
2. **The questions room is the two-way channel** (already true, now checked). Every human
   message there already reaches the coordinator. A threaded reply is covered by the existing
   `test_an_answer_in_a_thread_names_its_question_and_replies_by_thread`. Workers never post
   there: the skill says so, and the server's power levels already keep them out.
3. **Reports are read-only status** (guidance only, by the owner's decision). `pingbus report`
   does not inspect or refuse its body. The skill, STYLE.md, the role text and the docs ask for
   a short FYI report at each checkpoint (a milestone, a merge, a deploy queued, a phase done,
   something blocked), never a question, and move "Needs you" to the questions room.
4. **A human's thread reply in the main room under an agent's message reaches that agent**
   (fixed, cleanly). `protocol.reply_root` gives the thread root (or, outside a thread, the
   replied-to event). The syncer asks the homeserver who sent it (`Syncer.sender_of`, one GET
   per root, cached), and accepts the message when the sender is this member. It works for
   messages sent before the fix, because the lookup is by the server's sender and not by a
   local record. The answer is stored in the inbox item as `answers`, so the offline
   re-validation (`inbox.pending`, `_refetch`) gives the same outcome without a second lookup;
   `history` uses the lookup too. A reply under another agent's message is no longer logged as
   unheard (`reached_no_agent` checks the root's sender); one under a human's message still is.
   A human's main-room message that mentions no one and answers no agent still reaches nobody.
   It is logged in `ignored.log` as before.

## Versions and deploy

- CCY 3.104.17, container 2.79.21 (the image's pingbus and skill changed), with an entry in
  `docs/ccy-changelog.md`.
- `CLAUDE/Plan/meta-deploy.list`: the `changed-plays` description now names
  `play-agent-bus.yml` (the member kit's pingbus) and `play-claude-yolo.yml` (the ccy image
  rebuild).
- After deploy, sessions pick up the new pingbus and skill only when they restart.

## Files

`helpers/pingbus/{protocol,syncer,inbox,cli,hooks}.py`,
`files/opt/claude-yolo/optional/agent-bus/plugin/pingbus/skills/pingbus/SKILL.md`,
`CLAUDE/Plan/00241-agent-bus-message-style/STYLE.md`, `docs/agent-bus.md`,
`docs/agent-bus-protocol.md`, `docs/ccy-changelog.md`,
`files/var/local/claude-yolo/{claude-yolo,Dockerfile}`, `CLAUDE/Plan/meta-deploy.list`,
Plan 00211 `PLAN.md` (Task 2.14), and the two test files.
