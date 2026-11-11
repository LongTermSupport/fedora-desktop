# Phase 13 Task 13.5: `ccy-sessions declare`

Phase 13 added the `fresh-if-no-conversation=yes` mark. Nothing could write a record for a
session that had never run, because only a running launch writes one. This task adds the
command that provisioning runs to write it.

## The command

```
ccy-sessions declare [--name ccy-NAME] [--fresh-if-no-conversation] [--replace] DIR [-- CCY-ARGS...]
```

Example (as the login user, never root):

```
~/.local/bin/ccy-sessions declare --fresh-if-no-conversation /home/<user>/Projects/app -- --token work --ssh-key /home/<user>/.ssh/id_github --compose start
```

- stdout is one line: `<name> DECLARED`, `<name> UNCHANGED` (the same record is already there;
  nothing is written) or `<name> REPLACED`. Exit codes: 0 on success, 1 for a refusal, 64 for a
  usage mistake.
- An Ansible task: `command` with `argv`, `become_user` the login user, and
  `changed_when: "' UNCHANGED' not in result.stdout"`, or
  `creates: ~/.local/state/ccy/sessions/<name>`. docs/ccy.md carries the full task.

## What the record is

It goes through `registry.Store.Write`, the registry's one Go writer. It is the record that
`ccy_tmux_insulate` writes for a ccy launch of the same argv in DIR:

- name: `ccy-` plus `projectName(DIR)`, the Go twin of `get_project_name`. `--name` overrides
  it and must match `^ccy-[A-Za-z0-9_-]+$`.
- dir: DIR as given. It must be absolute, clean and an existing directory, because the launcher
  records `$PWD`.
- launcher `/var/local/claude-yolo/claude-yolo`, prefix `ccy`, `restore=yes`.
- args: `registry.ReplayArgs`, new, the Go twin of `ccy_registry_launch_args` followed by
  `ccy_registry_replay_args ccy`.
- compose: `--compose start` gives `started`, `skip` gives `declined`, and `ask` or no flag
  gives none. The launcher records the same outcome after its compose step, and the restore
  replays it, so an unattended start does not stop at the compose question.
- `fresh-if-no-conversation=yes` only with `--fresh-if-no-conversation`.

Proof: `TestDeclareWritesTheLaunchersRecordAndCheckJudgesItRestorable` compares the declared
file byte for byte with what the real bash library writes (`get_project_name`, `launch_args`,
`replay_args`, `ccy_registry_write --compose started --fresh yes`) for the same argv in the same
folder. Then `restore --check` prints `ccy-proj RESTORABLE`, and `restore --dry-run` says it
would start fresh with `--compose start` and `--supervise`.

## Refusals (nothing is written)

- Whatever `planRestore` refuses, so `restore --check` can never call a declared record
  UNRESTORABLE. That covers no mark with no conversation, `--no-bus`, and a directory that is
  gone.
- `--no-restore` and `--opencode`/`-oc`, which the restore skips. `--compose` with a value
  other than start, skip or ask.
- A live tmux session with the name, whatever the record says. The one exception is a record
  identical to the declared one, which is UNCHANGED, so a re-run while the session is up does
  not fail.
- A different record under the name, unless `--replace` is given. An unreadable record there
  is refused too.
- Running as root, whose registry no boot restores.
- A note on stderr (not a refusal) when this user's restore unit is not enabled.

## A drift fixed along the way

The Go `dropFlags` was missing eight flags that the bash list has: `--list-containers`,
`--stop-containers`, `--read-launch-config`, `--check-token`, `--check-network`,
`--claude-versions`, `--remove-network` and `--stop`. Go classed them `unknown` and so would
have replayed them. The new test `TestEveryFlagTheLauncherListsHasTheSameClassInGo` reads
bash's four arrays themselves and was red on all eight; the old parity test used a hand-picked
word list. These flags exit before a session exists and never reach a real record, so no live
behaviour changed. That is also why there is no separate DBF rule: the new test is the
detector for this class (the Go and bash flag lists drifting apart).

## Tests (red, then green)

The sessions tests failed first: they did not compile, because `projectName` and the
subcommand did not exist. The registry tests failed on `ReplayArgs` being undefined, and then
on the eight flags above. All of them now pass:

- registry: `TestReplayArgsAgreesWithTheLaunchersOwnFilter`,
  `TestEveryFlagTheLauncherListsHasTheSameClassInGo`
- sessions: `TestDeclareWritesTheLaunchersRecordAndCheckJudgesItRestorable`,
  `TestDeclaringTheSameRecordAgainChangesNothingEvenWhileItRuns`,
  `TestADifferentRecordUnderTheNameIsReplacedOnlyWhenAskedAndNeverWhileLive`,
  `TestALiveSessionWithNoRecordIsNotDeclaredOver`,
  `TestTheNameCanBeGivenAndIsAccountedForInTheCheck`,
  `TestDeclareRefusesARecordTheRestoreCouldNotStart` (5 cases),
  `TestDeclareRefusesADirectoryTheRestoreCouldNotStartIn`, `TestDeclareUsageMistakes`,
  `TestDeclareRefusesRootAndSaysWhenRestoreIsOff`, `TestTheDefaultNameIsTheLaunchersProjectName`
  (checked against the real `get_project_name`)

## Scope left out

- cc sessions: a cc record's launcher path depends on how `cc` was invoked. The headless case
  this serves is ccy, the only kind the restore's SSH key askpass applies to.
- No bash was touched. `lib/session-registry.bash` stays at its ratchet.
- CCY 3.119.0. The container version stays 2.84.4, because no image binary imports `registry`
  or `sessions` (checked with `go list -deps`). The play still re-runs the builder stage at the
  new commit.

## Phase 13 review fixes, in the same commit

This is the QA review of 6405e2c7c. Its should-fix 1 is the `declare` command above.

- Doc drift: the `GoingNone` comment (`internal/restore/manifest.go`) now covers a fresh
  restore; docs/ccy.md's "not restored" rule now says "unless provisioning marked its record";
  Plan 00203 Decision 10 notes that Phase 13 revised it; Plan 00227 Task 3.3 says a rewritten
  record keeps `FreshIfNoConversation`.
- `ccy_registry_write`: the option loop takes an option only when a word follows it, so it no
  longer needs `${2?}`, which ended the shell. A missing value now reaches the record's own
  checks and the function returns 1. The function's required positionals still use `:?`, as
  every function in the file does; that class is left as it is.
- The restore/fresh refusal prints both values in the reader's form ("restore='x' (yes or no)
  or --fresh='y' (yes or empty)") and no longer claims the mark is wrong. A message for each
  value costs three statements, and the file is held at its bash-limits record (849/870, still
  within it), so it was not done. Fully fixing it means porting the library, or dropping
  `ccy_registry_set_restore`, which only tests call now (the Go `Store.SetRestore` replaced it).
- meta-deploy.list: the Phase 13 note is accurate about the builder stage. The stray backslash
  in Task 13.2 is gone; the line was rewrapped so mdformat does not escape it again.
- A journal entry for each: `JOURNAL/2026-11-11/221221Z-action-4e2928.md` (13.5) and
  `221752Z-action-feaf28.md` (the review fixes).
- Task 13.4 (HOST, the owner) now names `declare` as the way to write the record.
