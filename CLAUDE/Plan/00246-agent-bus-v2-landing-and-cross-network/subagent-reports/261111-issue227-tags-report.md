# Issue 227 re-dispatch: play-teamchat.yml tagged

## Items 1, 2 and 3 were already fixed

Commit `bdaeb0f4e` on F44 fixed these three items. Its full DBF record is
`261110-issue227-report.md`, and they were not redone here:

- **Item 1:** the Tuwunel install stats the binary as well as the record. The rule is
  `install-guard-trusts-record`, with three instances fixed.
- **Item 2:** the controller's Go is probed (`go env GOVERSION`, `GOTOOLCHAIN=local`). A missing
  or older Go is refused before any change.
- **Item 3:** the assert "Refuse a Scope or an Approval Naming a Team This Run Does Not Apply"
  checks the team lists. The rule is `filter-list-unchecked`, with two instances fixed.

I re-read the play and all three are present.

## Tags (issue 244's direction, this play only)

| Tag       | Runs                                                                                                                                                |
| --------- | --------------------------------------------------------------------------------------------------------------------------------------------------- |
| `always`  | the input checks, the team-list check, the firewalld check and refusal, the owner group, the control account, the directories, the owner-group file |
| `admin`   | the controller Go check, the shared build steps, the admin copy and its hash check, the admin link                                                  |
| `member`  | the controller Go check, the shared build steps, the kit directory, the member copy and its hash check, the kit's current link, the member link     |
| `tuwunel` | zstd, the record and binary stats, the install block, the server pin                                                                                |
| `units`   | units list, print and write, the stub removal, tmpfiles, daemon-reload, and re-applying every team when a base unit changed                         |
| `teams`   | approve, remove, apply, and the timers                                                                                                              |

A single team is run with `--tags teams` plus `teamchat_only_teams: [<team>]`. The member program
alone is run with `--tags member`.

Structural changes this needed:

- The build block has no tag of its own, because a block's tag reaches every child. Each child
  is tagged `admin`, `member` or both, and so is the `always:` cleanup.
- The one check over both copies was split into a check per program. A member-only run therefore
  checks only what it copied.
- `tasks/teamchat-build-go-commands.yml` is now `import_tasks`, not `include_tasks`. With an
  include, the tag reaches the include task but not the tasks it reads.
- The re-apply for a base unit change is tagged `units`, because only a run that writes units
  can change one.

## Test

`src-go/internal/teamchat/installplay/tags_test.go` checks three things:

- every task has a tag, either its own or one inherited from a block; a block is judged by its
  children;
- each named guard carries `always`;
- there are no dynamic includes.

It has a fixture that discriminates. It was red on the untagged play before the edit and is
green after it.

## QA

`qa-changed` passed every gate except one step of go-qa: the coverage step.

- The other go-qa steps passed: tests, lint, build and the project rules.
- The coverage step failed with "statement coverage 0.0%". Diff mode scoped the run to
  `installplay`, which holds tests only, so there were no statements to count.
- This is a fault in the gate, not in the change. It is logged as issue 246 (dogfood rule).
- `qa-all` measures coverage over the whole module, so it does not hit this fault.

## Not done / left

- **Item 4** (pruning) and **item 5** (nothing runs the play) remain open, as dispatched.
- **Not host-verified.** No tagged run has happened, because the play has never run (item 5).
- **Issue 244's repo-wide detector, sweep and gate were not built.** That is the owner's
  separate work. The test here covers this play only.
