# B.13 review fixes: report (partial, handed off)

The review is `team-winddown-review.md`. This pass fixed finding 1 and the TC15 nit, then handed
the rest off because its context was running out. B.13 stays unticked.

| Item                                    | State | Commit    | Tests (each seen failing before the code)                                                                                                                                                                                 |
| --------------------------------------- | ----- | --------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| 1. v2 Start never restarts              | fixed | 1a16e4f78 | `host/lxc/start_test.go` (3 tests); `admin` `TestLXCRestart/start` and the verb table; `lxcsession` `TestAV2SeatsStartRunsTeamchatAdminLxcStart`, `TestAV2SeatsStartSaysTeamchatAdminsRefusal`; `apply_test` offline line |
| Nit: TC15 wording                       | fixed | 1a16e4f78 | (spec text only)                                                                                                                                                                                                          |
| 2. v2 host seats closed                 | open  |           | drafted                                                                                                                                                                                                                   |
| 3. lxc member with no LXC record entry  | open  |           | drafted                                                                                                                                                                                                                   |
| 4. tmuxccy reads attach failures        | open  |           | drafted                                                                                                                                                                                                                   |
| Nit: picker sudo calls on every refresh | open  |           | design noted                                                                                                                                                                                                              |

## Finding 1

`teamchat-admin lxc start --member <container>/<seat key>` is a new verb (SPEC-n3 SU4). It uses
`lxc restart`'s seat lookup, refusals and offline causes, in its own words, then calls
`ResetFailedUnit` and `StartUnit` over the guest's systemd. A start leaves a running unit as it is,
so the session cannot be ended whatever changed after the row's check. ccy-teams' v2 Start now
runs this verb, and its check beforehand only produces a message.

Every place that told someone to start a seat with `lxc restart` now names `lxc start`:

- apply's offline line and the unit docs
- SPEC-n3 SU1, SU4 and the offline table
- SPEC-team-close TC16
- DESIGN-v2-C-host
- `docs/agent-bus.md` and `docs/agent-bus-lxc-members.md` (whose claim that ccy-teams has no v2 start line was stale)
- `localhost.yml.dist`

CCY version 3.122.37. No container bump: the hook found no image source changed.

DBF: the class is a check-then-act on a unit's state across two calls. No detector can tell a
guarding read from an informative one, so the fix makes the act itself safe.

QA: the first `qa-changed` run failed only in `ccylaunch/difftest`
TestFrontParity/launch-selinux-enforcing. That is the GPU device listing failing under load,
already logged as issue #327, and the scenario passes when run alone. The rerun was green.

## Left

The design for items 2 to 4 and the picker nit, with drafts (in `untracked/scratch/`, not
tracked), is in the handoff `untracked/scratch/261120-aaaa5159f2f4c41e6-handoff.md`.

For item 2, the session name is verified: it is the ccy tmux session name.
`ccy_membership_args` passes `ccy_tmux_current_session`, or the container name outside tmux, to
`ccy-teams internal launch-args`, which writes `Store.DesiredFile(session)`.
