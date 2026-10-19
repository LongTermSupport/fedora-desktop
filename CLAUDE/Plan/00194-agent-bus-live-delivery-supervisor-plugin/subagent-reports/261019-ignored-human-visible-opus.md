# Plan 00194 Task 2.0f: a human message that reaches no agent is visible

Branch `feat/ignored-human-visible`, from F44 50782762 (contains 442143f5). Not pushed, not
merged.

## The incident

The owner typed `@room ...` in Element as plain text, with no pill. The event therefore had no
`m.mentions.room`. All five live members' watchers ignored it as `not-addressed`
(`protocol._human_outcome`, step 08h). An IGNORE writes nothing, while a DROP appends to
`dropped.log`, so the owner saw nothing happen and could not find out why.

## What changed

- **`helpers/pingbus/protocol.py`**: new `reached_no_agent(event, ctx, outcome)`. It is true
  only for a message from a team human that was ignored as `not-addressed` and whose
  `m.mentions` names no agent holding a role: no `user_ids`, or none of them an agent in
  `roles`. A missing or malformed `m.mentions` counts as naming nobody. New constants
  `IGNORE_NOT_ADDRESSED` (both existing `"not-addressed"` literals now use it) and
  `UNHEARD_REASONS`.
- **`helpers/pingbus/inbox.py`**: `log_ignored` / `ignored_count` write and count
  `state/ignored.log`. It shares one append path and one line-count path with `dropped.log`,
  so it has the same five fields (team, event ID, sender, reason, ms), the same grammar
  scrubbing, 0600, and the same refusal of symlinks and non-regular files. The body is never
  written.
- **`helpers/pingbus/syncer.py`** `_receive`: logs such a message. This is the only place a
  fresh not-addressed event is seen: the inbox holds only accepted items, and the re-fetch
  paths work on those.
- **`helpers/pingbus/cli.py`** `status`: the `TEAM` line gains `ignored=N`, after `dropped=`.
  The fields are key=value, and the only other parser (Plan 00161's `u20_check.py`
  `status_fields`) reads them by key, so an added field does not break it.
- **Delivery is unchanged.** A typed `@room` still reaches no one. It is not a drop: there is
  no `DROPPED` count, no change to the exit code, and no model turn.

### One decision beyond the brief

The brief says to log a human message that is "ignored as not-addressed". Taken literally,
every member would also log every human message meant for a *different* agent. `ignored=`
would then count ordinary traffic, not "reached no one". So the log is limited to messages
that name **no agent of the team**. A message for the coordinator is the coordinator's, and
the other members log nothing for it (tested). A message that names only a human is logged,
because no agent receives it. If the owner wants the literal version (every not-addressed
human message, per member), the change is a single condition in `reached_no_agent`.

### Things left undone, and why

- **Status line: not added.** `ignored.log` keeps a running total with no "seen" marker. A
  status-line mark would stay on for good after the first plain-text `@room`, which is not
  clear. Making it clear needs some "acknowledge" state, which is beyond this task. The
  status-line renderer also deliberately imports no other pingbus module and runs on every
  refresh.
- **Rotation and size cap: there are none to copy.** `dropped.log` has no rotation and no
  size cap: it is append-only and grows without limit (`inbox.log_drop`). `ignored.log`
  matches it, and the spec now says so in §9 "Unheard". The growth is one short line per
  unaddressed human message, per member. If a cap is wanted, it should be added to both
  logs together.

## Docs

- `docs/agent-bus-protocol.md`:
  - The notation now excepts unheard messages from "Ignore".
  - §7 says that a `@room` typed as plain text addresses no one and is logged.
  - §9 08h points to a new **Unheard** paragraph. That paragraph gives the rule, the fields,
    the single reason code, "not a drop", and "not rotated".
  - §12 lists `ignored.log` in the state layout.
  - §13 `status` lists the `ignored` field.
- `docs/agent-bus.md`, section "Talking to agents from Element": `@room` must also be picked
  from the list as a pill, because typed out it is plain text and reaches no one. A new bullet
  says where messages that reached no agent are logged (`ignored.log`, `ignored=` in
  `pingbus status`).
- `docs/ccy-changelog.md`: a 3.99.12.17 / 2.77.13 entry.

## Versions and deploy

- CCY `3.99.12.17` (`CCY_VERSION` and its comment), container `2.77.13`
  (`REQUIRED_CONTAINER_VERSION` and the Dockerfile `LABEL`). These are placeholders for the
  coordinator to renumber at merge.
- `CLAUDE/Plan/meta-deploy.list`: `playbooks/imports/play-agent-bus.yml`, then
  `playbooks/imports/play-claude-yolo.yml`, each with its description. They come after the
  existing `play-cli-tools.yml` entry. Header lines 1-21 are untouched.
- Owner check after deploy: post a plain-text `@room` from Element, then run `pingbus status`
  in a member. It shows `ignored=1` on that team's line, and the member's `state/ignored.log`
  has one line with no text.

## Verification

- Test first: the new tests failed before the code existed (44 failures and errors across
  `test_protocol`, `test_inbox`, `test_syncer`, `test_cli_status`), and pass after it.
- Every pingbus Python test module (30 modules, `PINGBUS_*` unset): 1597 tests, OK.
- `bash scripts/test-ccy-lifecycle.bash`: passed 223, failed 0.
- `scripts/qa-changed.bash`: passed, 17 changed files. That covers the ccy suites the
  launcher and Dockerfile name, including `test-ccy-container-version-hook.bash` (94 passed).
  `qa-all` was not run, as instructed.
- Plan: PLAN.md Task 2.0f (🔄, waiting on review and deploy) and a journal `action` entry.
