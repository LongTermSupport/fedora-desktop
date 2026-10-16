# Review: `fix/lxc-seat-clash` 7b8c4261 (cut from F44 f01d993a) — Fable

**Verdict: MERGE.** No blocker.

## 1. Is the handle `<repo>.<seat>+<host>.lxc`, and does anything downstream key on (team, host, seat) without the repo?

Checked; nothing does.

- `helpers/agent_bus_lxc/derive.py:384-385` — `Identity.handle` is `registry.build_handle(repo, seat, host, "lxc")`; `derive_seat` (404-432) numbers within `identity` (repo + host): `roster.get(identity.handle(number))`.
- `helpers/agent_bus/registry.py:118-123` — the registry counter key is `prefix_of` = `<repo>+<host>.<type>`; members are keyed by the full handle. Two repos at seat 1 are two counters and two member records.
- Matrix account is `@<handle>:<server>` (`members.py:550-556`, `bundle_handle`), so two repos never mint one account or token.
- Guest bundle: `~<user>/.config/pingbus/<team>/member.json` (`members.py:72`, `_join` 881) — per container filesystem, per user, per team. Two containers are two filesystems; the same user name does not collide.
- Env file: keyed `(container, user)` and carries only the host (`_check_between` 452-454, `merge_env`); it does not hold a seat, so it cannot clash on one.
- ccy-side seats: `helpers/pingbus/seat.py:216-221` — `seats/<team>/<seat>/` under a *checkout's* `.claude/ccy/pingbus/`, so per checkout, i.e. per repo. `ccy-teams` board keys rows by `"<dir>|<seat>@<team>"` (`src-go/internal/teams/board/collect.go:429`, `snapshot.go:40-41`) and by handle for state; `seats.go:nextSeat` counts within `top` (a checkout). None of these apply to LXC members anyway (they live in the container), but even where seats are shared across checkouts the key carries the directory.
- `/var/lib/agent-bus/<team>/` holds the team file and registry (`members.py:77,1193`); no per-seat directory on the host.

`resolve_members` is consistent with the new check: `taken` (519-520) is filtered `repo in (None, identity.repo)`, the same rule the fix applies in `_check_between`.

## 2. "Unknown repo, so refuse"

Repo is `None` after resolution only for an entry that declares `host` and `seat` explicitly and omits `repo:` (`resolve_members` 512-516 leaves it untouched; every derived entry comes back with `repo=identity.repo`, line 524). The present phase therefore always knows the repo for the entries that matter here (seatless ones). Refusing when one side is unknown is exactly the pre-fix behaviour for that shape, so it is conservative with no regression. At parse time (`parse_members` -> `_check_between`) seatless entries have `host=None` and are not judged; unchanged.

## 3. Idempotency, no renumbering

`derive_seat` returns the bundle's seat first (414-416) and `resolve_members` adds every bundle-held seat to `claimed` before deriving any new one (503-510). `SecondContainerTest` asserts the second run emits `[]` and `minted == 2`, and the renumbering test puts the new entry *before* the joined one in the file and checks the joined one keeps seat 1.

## 4. Tests: real path, red first

Ran `python3 -m unittest tests.helpers.agent_bus_lxc.test_members tests.helpers.agent_bus_lxc.test_derive tests.helpers.agent_bus.test_registry`: 232 tests, OK.

Red check: swapped in `members.py` from f01d993a and re-ran `test_members`: 3 errors, each `MemberError: agent_bus_lxc_members gives seat 1 of team team-a to two containers; set seat on one` (the owner's message, placeholder names), from `ParseMembersTest`, `ResolveMembersTest` and `SecondContainerTest.test_two_repos_join...`. The last goes through `run_phase("present")`, i.e. `run_present` -> `_derive_hosts_and_seats` -> `resolve_members` -> `_join`, with `read_checkout` and `suggest-handle` faked per container: the real resolve-and-join path. Worktree restored; `git status` clean.

## 5. meta-deploy.list

Only additions (`+6`); every prior F44 entry is kept. The entry names `playbooks/imports/play-agent-bus.yml` with a comment saying what the run does and that joined members keep handles. Correct.

## 6. Install-specific names

Scanned the added lines for usernames, home paths, hostnames, IPs: none. Tests use `container-a/b`, `proj-a/b`, `example-org`, `/srv/agent/repo-a`.

## Nits (not blocking)

- `_check_between` docstring now carries the handle grammar a second time (derive.py has it); acceptable as the reason for the key.
