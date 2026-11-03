## 11:23 · action · —   — feat/bus-v2-n2 merged into the PR branch

Merged `feat/bus-v2-n2` (the reviewed N2 host side: backup, restore, the restore engine and
its verbs, n2g) into `spike/v2-merge-up`, on top of F44, n3e and n3f. The owner asked for it
on pull request 124. Nine files conflicted (17 hunks) and were resolved by meaning, not by
side: the admin verb table and exit codes took both sides' verbs; `health/files` kept the
restore records beside the v2 status files; the path tables took both sides' names; the
project-rule table (`qa/rules/table.go`) kept both sides' allowances and the one row n2 moved
(tuwunelhs `Start` to `StartWith`).

The merged tree needed adapting where one side's change met the other's, none of it a
conflict: `teamlock.Take` now takes the guard (every taker names its own), so the tests pass
`teamlock.Unguarded` and the production takers `restore.Guard`; `Status.Refresh` takes the
events a restore adds; `apply.MemberActive` and `HumanActive` are `MemberEnrolled` and
`HumanEnrolled`; the restore engine's unit state uses `systemd.ActiveStateActive`; the
restore control sleep is `clock.System().Sleep` (the one home of real timers); and
`lxc.Deps` gained the guard.

Gates on the merged tree: build, vet in three builds (default, `busfailpoints`, `tuwunel`),
golangci-lint 0 issues in the same three, the project rules driver clean in the same three.
