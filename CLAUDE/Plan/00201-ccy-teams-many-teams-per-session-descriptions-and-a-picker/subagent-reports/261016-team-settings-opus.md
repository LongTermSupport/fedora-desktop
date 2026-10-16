# Task 2.15: a team's settings from ccy-teams (opus)

Branch `feat/team-settings`, from F44 `0c61144d`. Spec: SPEC-many-teams.md section F (S-F1 to
S-F13). This change touches only ccy-teams and declare.py. The pre-commit gate required a
container version bump, because the image carries `helpers/agent_bus/`. The versions are now
CCY 3.99.10.2 and container 2.75.2, with an entry in docs/ccy-changelog.md. No session
behaves differently.

## The owner's request

"In the CCY Teams menu, when looking at a specific team, I should be able to update config for
the team as well. a single update config thing would be sensible that then opens a pane that
then has all of the different things editable. description, name. Are there any other fields?"

Yes, there are other fields. The table below lists every one: those in the team's
`agent_bus_teams` entry and those kept elsewhere.

## Every field of a team's config

| Field                        | Editable here                    | What applying a change involves                                                                                                                                                | Validated by                                                                                        |
| ---------------------------- | -------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ | --------------------------------------------------------------------------------------------------- |
| `team` (name)                | no, fixed with reason; Task 2.16 | a new homeserver unit, data folder, server name, accounts, room, tokens, bundles and every session's seats; the installer refuses it (`render.check_unchanged`)                | `protocol.is_team_name`; `check_unchanged` refuses any change                                       |
| `description`                | yes                              | host_vars and team.json; bootstrap sets the room topic; no restart (S-A4)                                                                                                      | `protocol.is_description`                                                                           |
| `humans` (People)            | yes                              | bootstrap creates and invites each added person (who then sets a password); a removed person is kicked and deactivated for good; power levels and record rewritten; no restart | 1 to `HUMANS_MAX` distinct `is_human_localpart`                                                     |
| `org`, `repos`, `exclude`    | yes                              | the team record in the room, which running sessions' syncers read live; no restart                                                                                             | `is_owner`, `is_owner_repo`, `is_repo_name`; org or repos required; exclude needs org, not in repos |
| `path_prefixes` (Path limit) | yes                              | the team record; no restart                                                                                                                                                    | `is_path_prefix`; empty removes the key                                                             |
| `allow_from`                 | yes                              | firewall rules and the drop-in's `IPAddressAllow` change, so the homeserver restarts                                                                                           | canonical CIDRs, no default route or multicast, at most 32                                          |
| `port`, `listen`             | no, fixed with reason            | `base_url` is in every member's bundle, so every member would need re-issuing, which nothing does                                                                              | port range; canonical non-wildcard addresses                                                        |
| `server_name`                | no, fixed with reason            | Tuwunel cannot change it without wiping the database; the installer refuses it                                                                                                 | `check_server_name`                                                                                 |
| `forge_api`                  | no, fixed with reason            | it has one allowed value (D66)                                                                                                                                                 | must be GitHub's API                                                                                |
| `state`, `purge`             | no: the menu's Take down         | `remove`, with its questions and backup                                                                                                                                        | `STATES`                                                                                            |
| coordinator (registry role)  | yes, changed at once             | `agent-bus set-role` as root, through `seats.ChangeRole`; no host_vars change                                                                                                  | roster; at most one                                                                                 |
| members, seats, LXC members  | no: their own rows and flows     | seats follow sessions; "Add an LXC container's agent" is its own menu line                                                                                                     | `members.parse_members`                                                                             |
| Element profile              | no                               | derived from listen and port (`declare element`)                                                                                                                               | -                                                                                                   |
| backups                      | no                               | the timer and `BACKUPS_TO_KEEP = 7` are the same for every team; `ccy-teams backup` and `restore` exist                                                                        | -                                                                                                   |

## What was built

- **declare.py `settings`** prints one `SETTING<TAB>key<TAB>JSON|-|!vaulted|!invalid` line for
  each key.

- **declare.py `set --value KEY JSON... [--check]`** changes the editable keys in one write. The
  whole entry is first judged by `teamfile.parse_team_file`, the play's own rules (exit 64 names
  the rule). The edit is read back before it is kept. A comment the edit would lose, or a
  vaulted entry, is exit 78 and nothing is written. `describe` and `drop-limit` now call the
  same `set_values`, so there is one line-editing path instead of three. One side effect:
  `describe` now also refuses an entry that is otherwise invalid, which the play would refuse
  anyway.

- **Go**: `declare.Client.Settings`, `Set` and `CheckSet`, and a new package
  `src-go/internal/teams/settings`. The team row menu has one visible **Edit settings** choice
  with no key. It opens the screen `Team <team> › Settings`, which lists:

  - the editable fields with their values, a pending change marked `(changed)`;
  - `Coordinator: … (changed at once)`;
  - the fixed fields, marked `(fixed)`;
  - **Save and apply (n changes)**, **Discard changes** and **Back**.

  Each row's header says what the field is and what applying it does, or why it is fixed.

- **Editing a field** opens `gum input`, titled `… › <field>`. Each answer is judged by
  `set --check` together with the other pending changes, so the check is the same one the CLI
  and the play use. A refused answer shows the reason and asks again, three times at most.
  Esc leaves the field as it was.

- **Save and apply** first lists each change as `old → new`, then what applying does, each
  effect once. No is the default when a person is removed. It then makes one `declare set`
  call and runs `ops.ApplyTeams`, the code path `ccy-teams apply` takes. If the apply fails,
  the screen says the change is saved but not applied and names `ccy-teams apply <team>`.

- **Back** asks before dropping unsaved changes, with No as the default.

## Rename: recommendation

I recommend keeping the name fixed, with the reason shown in a sentence, and recording a proper
rename as Task 2.16 (done). Reasons:

- The apply path itself refuses a changed name (`render.check_unchanged`).
- The name is in the homeserver unit, the data and install-state folders, the server name, and
  so in every account and room ID, every token and bundle, the registry, every seat file and
  session record, and every running session.
- A partial rename would leave sessions on a team that no longer exists.
- Doing it properly needs its own research: carrying the room history over, re-issuing every
  credential, moving seats while sessions run. That is well beyond a settings screen.

## Coordination

- **dbf/hidden-bindings** (`ui.Action`): my picker changes are kept to one action line in
  `menu.go`, the `keySettings` constant, one `case` in `keys.go`, one field in `picker.go`,
  one paragraph in `help.txt` and `usage.txt`, and one updated footer string in
  `picker_test.go`. The settings screen uses `flow.Kit.Choose`, `Confirm` and `Gum.Input`.
  Moving its rows to `ui.Action` later is mechanical. Expect small conflicts in `help.txt`,
  `keys.go` and `picker_test.go`.
- **fix/coordinator-two-paths**: when I built this it held no coordinator commits beyond F44's
  deploy-queue commits. The coordinator row calls `seats.ChangeRole`, the one role path, through
  an injected function, so whatever that branch changes in `ChangeRole` reaches the screen with
  no change here. If the two-path choice becomes a new function, rewire the one line in
  `teams.Wire`.
- **Description editing**: on the Python side `describe` now goes through `set_values`, so
  that logic is shared. The picker's describe offer (`inputDescription`) was left alone because
  the other branch is changing it. It could later reuse the settings field editor.
- **meta-deploy.list**: `play-claude-yolo.yml` was already queued (Plan 00203 M5). I extended
  its comment and added `play-agent-bus.yml`, because declare.py is part of the admin zipapp:
  every team reads "out of date (software)" until that play runs, and it restarts no homeserver.
  Commit `741191c3` (on dbf/hidden-bindings and fix/coordinator-two-paths, not yet on F44)
  empties the queue. When those merge, keep both entries for this task.

## Tests

- Python: `tests/helpers/agent_bus/test_declare.py` adds SettingsTest and SetTest; all tests
  in the module pass.
- Go: the declare unit tests, a real-declare.py integration test, the settings screen tests
  (scripted fzf and gum) and the picker's menu test all pass.
- `qa-changed`: passed, including the go gate, docs, test-ccy-teams-command.bash (369) and
  test-qa-changed.bash.
- `qa-all` was not run, as instructed.
- The launcher bump brought `test-ccy-lifecycle.bash` into qa-changed, and three of its cases
  failed. The cause is not this change: the suite inherited `PINGBUS_MEMBERSHIP` from the team
  session it ran in, and the entrypoint adds the supervisor plugin whenever that variable is
  set. Its `wrapper_case` now unsets the variable, and the suite passes, 215 cases. That fix is
  committed separately.

The owner's check is ACCEPTANCE-owner.md step 13.

## After Fable's FIX-FIRST (261016-team-settings-review-fable.md)

This section supersedes the version numbers and the queue notes above.

- **A removed person can never return (S-F14).** `set` cannot read the homeserver, so the removal
  is now recorded in the declaration. `set` adds every name it takes off `humans` to a new
  optional team-file key, `removed_humans`. `teamfile.parse_team_file` refuses a `humans` list
  that names any of them. That one rule is run by `set --check`, `set`, the play's
  `render check` and `declared_config`, so the screen refuses the name as it is typed, and no
  apply can be broken by it. The play's own account check stays for removals made by hand
  before this key existed. The People text, the removal line of the summary, docs, help and
  usage all say a removed name can never be added back. Removed people is shown as a fixed
  field. Tests first: test_teamfile `RemovedHumansTest`, test_declare
  `test_a_removed_person_is_recorded_and_can_never_return`, and the real-declare.py Go test.
- **Versions.** F44 `0976d613` is merged in. The versions are now CCY 3.99.12.6 and container
  2.77.5, with the changelog section on top.
- **meta-deploy.list.** It holds F44's five entries, plus `play-agent-bus.yml` with its comment,
  directly after `play-claude-yolo.yml`. The 00161 acceptance is not restored.
- **Non-blocking fixes.**
  - `removedPeople` returns its errors.
  - Save and apply reads the settings again before its summary, so the old values shown are
    those on disk at save time, and a value that became unreadable stops the save.
  - A `set` that changes nothing now says "Nothing to save".
  - The fake refusal in the test quotes the real limit, 16.
