# Launch summary (issue #125), branch feat/ccy-launch-summary, CCY 3.105.0

## What was built

- `ccy-teams launch-summary --type=podman --token= --ssh= --network= [--teams=] [--roles=] [--skip-teams]`
  (Go, `src-go/internal/teams/chooser/summary.go`). One gum screen: "Launch with these
  choices" (Enter), "Change token / SSH key / network", and "Change team T (seat S, role R)" per
  team. Payload on stdout: `TEAMS`, `ROLES`, `ASK` lines. `ASK` is `none` or the one question
  (token, ssh, network) the launcher asks next.
- A team is marked `NEEDS AN ANSWER` when its seat is held, its scope does not cover the
  checkout (widen), its role differs from the remembered one, its seat cannot be checked, or it
  cannot be taken (excludes the repo, homeserver down, gone: left out when its line is picked).
  Enter lands on the first marked line; Launch is refused while any line is marked. Only the
  marked line is asked: scope (existing `settle`), then seat and role (existing `PickSeat`).
  A scope answer is not asked again in the same launch.
- Bus independence: the summary reads the bus only through `Chooser.Probe`, implemented by
  `choose.Probe` (v1 today). v1 files, `helpers/agent_bus`, pingbus: untouched. Roles are
  remembered in a new `LAST_TEAM_ROLES` line of `.last-launch.conf` (reader key added in
  `ccylaunch/launchconfig.go`; format version unchanged, a file without it still reads).
- Bash: new `lib/launch-summary.bash` (3 small functions); `claude-yolo` calls it in place of
  the Y/n read when a person is at a terminal with gum and ccy-teams (no terminal: old prompt,
  so the bash suites and difftest goldens are unchanged). Token and SSH changes are asked in a
  loop that returns to the summary.

## Red, then green

- `choose/probe_test.go`: red = `h.c.Probe undefined`; green after `choose/probe.go`.
- `chooser/summary_test.go` (13 tests: accept-all asks nothing more, taken seat, refused launch
  while marked, scope widen/join/leave, role changed, excluded team, unreadable seat, change
  token/ssh/network, one of several teams, Esc, no teams, usage): red = `s.c.Probe undefined`;
  green after `summary.go` (after fixing three test mistakes: the help text contains the mark
  words, gone teams show "role unknown", the probe-error test cleared its error).
- `ccylaunch/launchconfig_test.go`: red = "LAST_TEAM_ROLES is not a key ccy writes"; green after
  the key was added.
- `teams/app_test.go`: dispatch of `launch-summary` and the diverted stdout.

## Not done / for the owner

- The network line is not asked inside the summary: its question lives inline late in the
  launcher (it depends on engine state), so choosing it clears the saved network and the usual
  network step asks it. Token and SSH return to the summary.
- Adding a team from the summary is not offered (the team list is still `choose-teams`).
- The real gum screen and the bash loop were not run here (no podman, no terminal); a host look
  after `play-claude-yolo.yml` is the check.
- `qa-all` was not run (sub-agent rule).
