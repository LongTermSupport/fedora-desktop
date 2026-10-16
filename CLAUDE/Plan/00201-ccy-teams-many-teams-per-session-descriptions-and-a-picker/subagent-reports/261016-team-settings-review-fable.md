# Review of feat/team-settings (Task 2.15, team settings) — Fable

Branch `feat/team-settings` (93bd0f1b, d2239c55) from F44 `0c61144d`, reviewed against local
F44 `110ea6e4`. Nothing in the branch was changed by this review except this file. `qa-all`
was not run, as instructed.

## Verdict: FIX-FIRST

The code is sound and the screen meets the owner's UX requirements (visible menu line, context
heading, every field editable or fixed with a reason, one write, one apply path). Three things
must be put right before it merges: the version numbers, the deploy queue, and the one
validation gap the play has that `set` cannot see (a removed person's name can never return).

## Targeted tests (this review's run)

| Suite                                                                       | Result                       |
| --------------------------------------------------------------------------- | ---------------------------- |
| `go test ./internal/declare/... ./internal/teams/...`                       | ok, every package            |
| `gofmt -l`, `go vet` on settings, declare, picker                           | clean                        |
| `python3 -m pytest tests/helpers/agent_bus/test_declare.py`                 | 145 passed                   |
| `scripts/test-ccy-teams-command.bash` (PINGBUS\_\*, CCY_SESSION_NAME unset) | 369 PASS, exit 0             |
| `scripts/test-ccy-lifecycle.bash` (same environment)                        | passed 215, failed 0, exit 0 |

## Owner requirements, checked

- **Every field editable or fixed with a reason**: yes. `fields.go` lists the 12 keys
  `declare.py settings` prints; 7 editable, 5 fixed with a sentence each; coordinator "changed
  at once". `TestTheScreenListsEveryFieldThenSaveDiscardAndBack` pins the rows.
- **Visible controls, no key-only path**: "Edit settings" is a menu line with no shortcut
  (`menu.go:98`, `shown()` returns "" for it); Save, Discard and Back are rows; Esc/q are named
  in the header as alternatives to the Back row. F44's `CLAUDE/InteractiveScripts.md` has 13
  rules; there is no rule 14 on F44 yet, so this is judged against the intent in the brief and
  it passes.
- **Context heading**: `Team <team> › Settings`, `… › <field>`, `… › Save and apply`,
  `… › Back`, `… › Coordinator` (`settings.go:95-101`), tested by title assertions.
- **One code path**: `describe` and `drop-limit` now call `set_values`; the screen's save is one
  `declare set` then `ops.ApplyTeams`; the coordinator goes through the injected
  `seats.ChangeRole`. No fallback chains. The picker's older `inputDescription` offer (Describe
  on a team row) still exists beside the new Description field; two UI entry points, one write
  path. Acceptable, noted below.
- **Fail fast**: `set` judges the whole entry with `teamfile.parse_team_file` before any write,
  reads the result back, and refuses (78) a comment it would lose or a vaulted entry. The Go
  side returns `flow.Leave(ExitFailure)` on anything but a typed refusal.

## Attacks

### 1. Validation parity and atomic write

`set_values` (`helpers/agent_bus/declare.py:379`) runs `teamfile.parse_team_file` on the entry
as it would be, so every shape rule the play applies to a team file is applied before the
write, and `--check` runs the same code. The write (`declare.py:678`, `write`) is a backup
copy, then an `O_EXCL` temporary beside the file, `fsync`, `os.replace`: atomic on the same
filesystem, mode kept. Good.

**Gap (BLOCKING, small fix):** the play has one rule for an editable key that is decided
against the live homeserver, not the file. `admin.py:_ensure_humans` (lines 516-525) raises
`human X's account is deactivated (removed from the team earlier); a deactivated account cannot return: choose another name`. So: remove a person, Save and apply (they are deactivated for
good), later add the same name back; `set --check` accepts it, `set` writes it, the play
fails, and it keeps failing on every apply until the name is edited out again. The host_vars
is then "saved but not applied" and the team reads out of date. The screen's failure message
(`settings.go:351`) does say how to re-apply, but nothing tells the person the cause is
permanent or how to recover (remove the name again, or choose a new one). Fix: the People
field's `about` text (`fields.go:43`) and the summary line for removals (`settings.go:372`) must
say a removed name can never come back; `docs/agent-bus.md` table row for People likewise.
`set` itself cannot check this (no homeserver access as the user), which is fine as long as it
is said.

Also in `_ensure_humans`: accounts are created in list order and the error is raised when the
deactivated name is reached, so adding A and re-adding removed B in one save creates A then
fails. That partial apply is the play's, not this branch's, and a second apply after removing
B completes it. Noted under 4.

### 2. People removal is permanent: confirmed, default No, named

Yes. `save()` computes `removedPeople()`; `Confirm(..., defaultYes: len(removed) == 0)`
(`settings.go:316`), and the Picker's Confirm puts the cursor on No (`--bind=start:last`,
`ui/picker.go:216`). The summary names them: `Removed, their account deactivated for good: …`
(`settings.go:372`); the field header says it "cannot be undone" (`fields.go:45-46`).
`TestAnEditIsCheckedShownChangedThenSavedAndAppliedOnce` asserts `--bind=start:last` and
"deactivated"; `TestASaveThatRemovesNobodyDefaultsToYes` asserts the inverse. Good.

Nit: `removedPeople()` (`settings.go:378-397`) returns nil on a JSON unmarshal error, which
would silently turn a removal into a default-Yes, unnamed save. Both inputs are produced by
code that already validated them (declare's `Settings` checks `json.Valid`; `inputJSON`
marshals), so it is unreachable today, but it is an error-hiding shape in exactly the place
where it matters most. Return the error and fail the save instead.

### 3. Allowed networks restarts the homeserver: said before Save

Yes. `fields.go:72-73` applies text: "restarts the homeserver: the bus is down for a few
seconds". The summary prints each distinct `applies` sentence once (`settings.go:363-370`), so
it is on the Save and apply question. Also shown in the row header while browsing.

### 4. Partial application

- `set` is all-or-nothing (one write). Save then apply: if `Set` fails with 64/78 nothing is
  written and the screen says why (`settings.go:325-328`). If apply fails, the screen says
  "saved in the host_vars but not applied … ccy-teams apply <team>" (`settings.go:351`,
  tested by `TestAFailedApplySaysTheChangeIsSavedButNotApplied`). `ops.ApplyTeams` itself
  reports its own partial states (homeserver applied, Element not).
- Not reported: `Set` succeeding with `Changed == false` (someone else already wrote the same
  values) leaves the screen silently (`settings.go:334-336`). Say "Nothing to save: the
  host_vars already had these values." Non-blocking.
- The "old" value in the summary (`settings.go:362`) is the value read when the screen opened,
  not the value on disk at save time. Cosmetic; the write itself is against the current file.

### 5. Concurrency with other declare edits

No locking anywhere in `declare.py` or the Go client (none before this branch either). `set`
reads the file, edits the text, reads back its own result, writes. Two `declare` edits in the
same instant are last-writer-wins, with the loser's change in the other's backup file. The
settings screen does not widen this: pending changes live in memory and the file is only read
at save time, so another edit made while the screen is open is kept. A pre-existing property
of every `declare` edit; non-blocking, worth a plan note for a lock around `write`.

### 6. Install-specific names

The added lines were scanned for usernames, home paths, hostnames, non-example emails and
non-documentation IPs: none. Addresses are 192.0.2.x, names are `alpha`/`example-org`.

### 7. Versions (BLOCKING)

The branch claims CCY 3.99.10.2 / container 2.75.2. Local F44 already holds CCY 3.99.10.2
(the container-name race fix, container 2.75.1) and its changelog already has a
`3.99.10.3 — container 2.75.2` entry. Other branches hold 3.99.10.3 / 2.75.2
(fix/hermetic-helper-tests), 3.99.12.2 / 2.77.2 (fix/coordinator-two-paths), 3.99.12.3 /
2.77.2 (feat/hot-control-m6), and the brief names 3.99.12.4 / 2.77.3. `git merge-tree` against
F44 conflicts on `claude-yolo`, `Dockerfile` (via other branches) and `docs/ccy-changelog.md`
for exactly this reason.

If this merges after those: **CCY 3.99.12.5, container 2.77.4**, in all six places:
`files/var/local/claude-yolo/claude-yolo` (CCY_VERSION and REQUIRED_CONTAINER_VERSION),
`files/var/local/claude-yolo/Dockerfile` LABEL, the `docs/ccy-changelog.md` heading, the
`meta-deploy.list` comment, PLAN.md Task 2.15 line, and the opus report. If it merges before
them, take the next free number at that moment; never reuse 3.99.10.2.

### 8. meta-deploy.list (BLOCKING on resolution)

The branch's list was edited from the `0c61144d` state. F44 has since moved. Queues now:

| F44 (110ea6e4)                                        | branch                                      |
| ----------------------------------------------------- | ------------------------------------------- |
| playbooks/imports/optional/common/play-md-viewer.yml  |                                             |
| playbooks/imports/play-claude-yolo.yml                | playbooks/imports/play-claude-yolo.yml      |
|                                                       | playbooks/imports/play-agent-bus.yml        |
| 00203-teams-hot-control-and-robustness                | 00203-teams-hot-control-and-robustness      |
| 00203-teams-hot-control-and-robustness/triage-hs.bash |                                             |
| 00170-unifi-network-tooling/clear-ledger-broken.bash  |                                             |
|                                                       | 00161-agent-team-bus-matrix/acceptance.bash |

The report says commit 741191c3 "on dbf/hidden-bindings and fix/coordinator-two-paths, not yet
on F44" empties the queue. That is stale: 741191c3 is on F44 already (and on every listed
branch), and F44 has refilled the queue since. The merge must produce F44's five entries plus
`play-agent-bus.yml` with its comment, directly after `play-claude-yolo.yml`, with this
branch's extension of the play-claude-yolo comment; and must **not** resurrect
`00161-agent-team-bus-matrix/acceptance.bash`, which F44 removed in 741191c3. The
version numbers in the comment change with item 7.

### 9. Tests

- Red first: the journal entry (09:41) records the red counts (Python 10 failures and 2 errors,
  Go not compiling) before green. The branch is one squashed commit, so history cannot confirm
  it; the journal is the evidence and it is specific enough to accept.
- Real path: `TestRealSettingsAndSet` runs the real `declare.py` over a temp host_vars for
  Settings, CheckSet (refusal typed as ErrUsage) and Set. The Python `SetTest` covers in-place
  flow lists, block lists replaced whole, key insertion after `team:`, removal, branches kept,
  `--check` writing nothing (backup count 0), comment refusal, vaulted refusal, type and
  double-key usage errors. The screen tests drive scripted fzf and gum over a faked declare,
  and `TestATeamsMenuOpensItsSettings` goes through the picker's menu into the screen.
- Weak spot: the fake refusal text in `settings_test.go:163` says "1 to 20 entries"; the real
  rule is `HUMANS_MAX = 16`. Harmless (the screen only relays stderr), but a fake that misquotes
  the rule invites a wrong expectation later. Use the real wording.
- `scripts/test-ccy-teams-command.bash` has no settings case; the Go tests own this screen,
  which is consistent with the other Go-only flows.

## Non-blocking items

- `settings.go:378-397` `removedPeople` swallows unmarshal errors (see 2).
- `settings.go:334-336` silent exit when `Set` reports no change (see 4).
- `settings.go:362` stale "old" value in the summary (see 4).
- `fields.go:43` People `about` says "at least one" but not the maximum (16); `allow_from`
  `about` does not say the LXC bridge subnet is added by the play regardless.
- `settings.go:224-228` any unreadable value, including a fixed key such as `listen`, blocks
  editing of every field. Conservative and safe; say which key in the notice (it does: names).
  Fine as is.
- Two UI entry points for description (team row Describe and the settings field). The report
  defers it to the other branch; a follow-up should drop the older offer.
- No lock around `declare.write` (see 5), pre-existing.
- `fields.go:63-67` Path limit `about` does not say prefixes must be relative and end in `/`;
  the refusal says it, so acceptable.

## Merge notes (git merge-tree, this branch into each)

- **F44 (110ea6e4)**: conflicts in `CLAUDE/Plan/meta-deploy.list`, `docs/ccy-changelog.md`,
  `files/var/local/claude-yolo/claude-yolo`. All three are the version/queue items above.
- **fix/picker-visible-actions**: conflicts in `meta-deploy.list`,
  `src-go/internal/teams/picker/picker_test.go` (the footer string with "edit alpha's settings
  via Enter" against that branch's footer).
- **dbf/hidden-bindings**: conflicts in `meta-deploy.list`, `docs/ccy-changelog.md`,
  `claude-yolo`, `picker/menu.go` (the `keySettings` constant and `shown()` case against the
  `ui.Action` rewrite), `picker/picker_test.go`. After that merge, `keySettings`'s menu line
  becomes one `ui.Action` and the `shown()` case goes; the screen itself is unaffected.
- **fix/coordinator-two-paths**: conflicts in `meta-deploy.list`, `docs/ccy-changelog.md`,
  `Dockerfile`, `claude-yolo`, `picker/keys.go` (the `case keySettings` beside its new
  coordinator cases), `picker/menu.go`. `settings.New` takes `s.ChangeRole` by function value
  (`teams/app.go:109`), so if that branch renames or splits ChangeRole, the one line in
  `teams.Wire` is the only change.
- `declare.py`, `test_declare.py`, `help.txt`, `usage.txt`, `docs/agent-bus.md`,
  `test-ccy-lifecycle.bash` auto-merge cleanly against every branch.
- d2239c55 (`unset PINGBUS_MEMBERSHIP` in `wrapper_case`) is independent of the feature and
  auto-merges; it could be cherry-picked to F44 on its own.
