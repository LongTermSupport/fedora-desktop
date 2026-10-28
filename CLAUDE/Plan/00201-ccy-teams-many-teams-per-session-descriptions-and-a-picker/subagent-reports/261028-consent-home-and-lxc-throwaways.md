# Consent home and LXC throwaways

Branch `plan/00201-consent-home-and-lxc-throwaways`. Two commits, not pushed or merged:
`3c658f867` (part 1) and `529bc35da` (part 2). `./scripts/qa-changed.bash` passed before each
one. qa-all was not run.

## Part 1: the premise was wrong; the fix is the passwd home

pingbus does **not** look for `element-accepted` under PINGBUS_HOME or XDG_CONFIG_HOME:

- `helpers/pingbus/config.py`: `load_bundle` (l.276-277) calls `element_accepted(_home_of(owner), owner)`.
  `_home_of` (l.424) is `pwd.getpwuid(uid).pw_dir`, and `element_accepted` (l.289) joins
  `ELEMENT_ACCEPTED_DIR` to that. `resolve_home` (PINGBUS_HOME > XDG > HOME) is used only for
  bundles.
- `src-go/internal/pingbus/statusline/roles.go` `checkBesideElement` uses `w.userHome(uid)`, which
  is a passwd lookup (`statusline.go` `passwdHome`).
- `helpers/agent_bus/element.py` `check_user` calls `config.element_accepted(home, uid)` with the
  user's home.

So a chooser that honoured PINGBUS_HOME would have *created* a disagreement. The real outlier
was the chooser's use of the `HOME` variable (`consent.go`). The fix:

- `host.Host.UserHome func(uid int) (string, error)`. `Wire` defaults it to a
  `user.LookupId` passwd lookup. `teamtest.New` answers it with the world's scratch HOME, so no
  test can reach a real home.
- `chooser.consent` takes `c.Host.UserHome(c.UID())`. A uid with no passwd entry exits 78 and
  names it.
- Tests: three in `chooser/consent_test.go` (the record goes under the passwd home whatever
  HOME, PINGBUS_HOME or XDG_CONFIG_HOME say; a record there answers; no passwd entry is exit 78),
  plus `host_test` (`TestTheUserHomeIsThePasswdDatabases`, and Wire fills the field). They were
  red first, then green, and both are journalled (`JOURNAL/2026-10-28/102913Z-action-*`).
  `golangci-lint run ./internal/teams/...` reports 0 issues, `go test -race ./internal/teams/...`
  passes, and coverage is chooser 91.4% and host 91.8%.
- No shared Go home exists for the record path. `statusline` keeps its own constants and
  unexported `passwdHome` as part of its parity port with the Python, and I left it unchanged.
  So there are now two five-line passwd lookups, one in `statusline` and one in `teams/host`.

**Step 08:** pointing an environment variable at a scratch directory cannot work: in cc and
in pingbus alike, no variable moves the record, and pingbus checks it when the host seats
start. `ACCEPTANCE-owner.md` step 08 and the PLAN.md item now say this. The step keeps the
rename-and-restore of the real record, which the owner authorised. Because the host agent's
hooks refuse that write, the owner types the two `! mv` lines in the host agent's session.
This is still a handover to the owner, and the lead may want to decide it differently.

## Part 2: Task 3.23's throwaways

In `CLAUDE/Plan/00203-teams-hot-control-and-robustness/`:

- `lxc-throwaways-up.bash` builds two containers, `t00203-coord` and `t00203-worker`.
  Each comes from the download template (Fedora at this host's release), is started and waits
  for an IPv4 address. Then dnf installs python3, git-core, util-linux-core, shadow-utils and
  gh. Each gets user `agent`, a checkout `~/t00203-work` (git init with no remote, so the seats
  are `t00203-work.1+local.lxc` and `.2`), and gh logged in with the desktop user's token,
  passed on stdin only. That login is required: the join play's `_check_gh` refuses a member
  without it. A failed up destroys what it made.
- `lxc-throwaways-down.bash` runs `lxc-destroy -f` on both, then checks that neither remains.
- `_lxc-throwaways.inc.bash` holds the shared legs. Every create, attach and destroy refuses
  a name that does not start with `t00203-`. The lxc-attach stdio is piped as in U23.
- Both scripts follow the same plan-script pattern: R1 bootstrap, `plan_require_host`, one
  sudo prompt and then `sudo -n`, a run log, deploy legs, and a report in the run directory.
  shellcheck is clean.
- PLAN.md Task 3.23 now lists the route in 8 steps. Nothing was queued in meta-deploy.

Deviations from the brief:

- **There is no `ccy-teams roles` subcommand.** The route reads each role from the board instead.
- **ccy-teams has no LXC member remove.** The removal step sets `state: absent` on both
  `agent_bus_lxc_members` entries in the run's host_vars, then runs `ccy-teams apply chooser201a`.

None of this has been run: the scripts need lxc and sudo on the host.
