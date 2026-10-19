# Task 3.8 member roles: re-merge onto F44 5ce37f5e

Branch `fix/teams-member-roles-2`, made from `fix/teams-member-roles` 60584607, with F44
5ce37f5e merged in `--no-ff`. Not pushed, not merged into F44.

## Versions

CCY 3.99.12.15, container 2.77.11 (the branch had 3.99.12.17 / 2.77.14). Set in
`claude-yolo` (`CCY_VERSION` and its comment, `REQUIRED_CONTAINER_VERSION`) and in the
Dockerfile label. The changelog section is on top, "Follows 3.99.12.14 (container 2.77.10)",
and the old line naming the unmerged 3.99.12.15 and .16 states is gone. F44's 3.99.12.11 to .14
sections are kept below it.

## Conflicts, and how each was resolved

- `meta-deploy.list`: lines 1-21 are F44's, byte for byte (diffed). F44 had emptied the queue
  (the stt-watch and play-lock runs were deployed), so those two entries are dropped. There is
  one `play-agent-bus.yml` entry first, then one `play-claude-yolo.yml`. Each comment is F44's
  text with this branch's text after it. The agent-bus comment now says "the image below",
  since that entry now comes first. The claude-yolo comment names 3.99.12.15 / 2.77.11 and says
  it carries F44's 3.99.12.14 work.
- Journals 26-10-16 and 26-10-19: merged as the union of both sides with `journal_union.py`.
  Every `## ` entry heading from both sides is present. Trailing blank line trimmed.
- PLAN.md: F44's two Task 3.8 entries and Tasks 3.9-3.11 are kept. This branch's Task 3.8 entry
  goes after F44's 3.8 entries and before 3.9, with the version added. ACCEPTANCE.md keeps the
  role in the status line, `👥 acc-m3a (<role>), acc-m3b (<role>)`.
- `hot/hot.go`: F44's `x.head = r.heading()` is set first, then the branch's
  `memberRole` dispatch for a handle, so a member-role question has a heading too. In
  `heading()`, the context says "member <handle>" for a handle and "session <name>"
  otherwise.
- `hot/role.go`: F44's `who` wording, with the branch's default of No on the
  coordinator-leaves question. F44's default was yes, as it had been since the question
  arrived in 7b5ce2e2 (not a later change on F44), which breaks the no-destructive-first rule.
- `ops/takedown.go`: F44's heading argument, with the branch's order and default ("Leave it
  running" first).
- `settings/settings.go`: the branch's "No coordinator" row, last and only when there is a
  holder, with F44's heading argument.
- `lxcadd/adder.go`: the branch's `Roles: roles.New(...)` and shared `apply`, with F44's
  `Paths: &pathinput.Asker{...}`. F44 had replaced `Picker`.
- `picker/menu.go`: the branch's `memberActions`/`seatOwnActions`/`roleAction` and
  `lastDestructive`, with F44's `addActions` (the add lines are inserted after Start, and the
  destructive ones are still moved last). Remove seat uses F44's `row.Label()`.
- Tests: `menu_test.go` keeps the branch's assertion, using F44's full seat label.
  `picker_test.go` keeps the branch's header order (help before the destructive actions) with
  F44's "add a running session" part. `order_test.go` (not in conflict, but failing after the
  merge) needed two changes in `sharedShape`. It leaves out "Add this session to another team",
  which F44 gives only to a running ccy seat. It also normalises `MEMBER (coordinator)`, because
  F44's `Label()` adds the role to the Remove seat line.

`scripts/bash-limits.allowlist` is the same as F44's, so no row grew.

## Targeted QA, all green

- `go build ./...`, `go vet`, `gofmt -l` (empty), the project rules driver over `./...`.
- `go test -race ./internal/teams/... ./internal/agentbus/... ./internal/ui/... ./cmd/...`
- `golangci-lint run ./...`: 0 issues.
- `bash scripts/test-ccy-teams.bash` 214/0, `test-ccy-teams-command.bash` 383/0,
  `test-ccy-lifecycle.bash` 223/0.
- Python: every `tests/helpers/{agent_bus,agent_bus_lxc,pingbus}/test_*.py` with `PINGBUS_*`
  unset: 2403 tests, OK.
- `scripts/qa-bash-limits.bash` BASH-LIMITS-OK; `scripts/qa-hidden-bindings.bash`
  HIDDEN-BINDINGS-OK.
- `ansible-playbook --syntax-check playbooks/imports/play-agent-bus.yml`: OK.

`qa-all` was not run, as instructed.

## Where this collides with fix/lxc-stopped-member-2

This branch does not edit `helpers/agent_bus_lxc/members.py`. It writes the declared entry
(`declare lxc-role`) instead of changing `plan_member`, so the two branches do not collide there.
They do collide in these places:

- `picker/menu.go` `kindActions` (semantic): lxc-stopped adds a guard so that a member row with
  no handle (a deferred LXC member) gets `{open, start, help}` with no Park. On this branch, the
  non-team, non-human path goes to `memberActions`, so the guard has to move into
  `memberActions`. It should drop both Park and the role action for a row with no handle,
  because `roleAction` reads the role by handle. Expect a textual conflict, or a guard that is
  silently lost. `TestEveryMemberKindIsOfferedTheSameRoleActionInTheSameWords` covers only rows
  that have a handle.
- `picker/menu_test.go`: both branches edit it.
- `lxcadd/adder.go`: `New()` changes `Roster/SetRole` here and `HostVars/AppliedDir` there, two
  lines apart, so a conflict is likely. lxc-stopped makes `coordinatorOf` a pure function that
  still sets `held.declared`, and this branch's `demote` (via `roles.Step{Entry: held.declared}`)
  still works with it. Check that lxc-stopped's tests do not fake `SetRole`, which this branch
  removed from `Adder`.
- `meta-deploy.list`: lxc-stopped was written against the older queue (claude-yolo first,
  agent-bus after). Its agent-bus and claude-yolo text should go into this branch's two entries,
  with agent-bus first.
- PLAN.md and the 26-10-19 journal: plan text, plus the journal union.
- Versions: lxc-stopped does not bump ccy, but if it changes anything in the image it needs a
  number after 3.99.12.15 / 2.77.11.
