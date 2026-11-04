# Review: Plan 00213 Task 2.3 cut-over (tip 26ad4b79), Opus stand-in for Fable

Read-only adversarial review of `git diff F44...agent-a71c72da1d538764a-d92a1e80`, focused on
the owner constraint "already running teams must keep working".

VERDICT: MERGE, with the three conditions under "Before merge / before deploy".

## Verified (targeted runs in a detached worktree at 26ad4b79)

- `go test ./internal/pingbus/statusline/...` passes (the package, the golden parity suite, and
  the new S21 bounds-vs-Python test).
- `tests.helpers.pingbus.test_statusline`, `test_seat` and `test_plugin_contract` all pass.
- A `CGO_ENABLED=0 -trimpath -buildvcs=false` build is "not a dynamic executable", GOARCH=amd64
  (GOAMD64 v1 by default), and `go version -m` shows `build CGO_ENABLED=0`, which is what the
  acceptance greps for. The only external thing it runs is `/bin/sh` for the inner status line.
  So it runs on an EL9 LXC member, the Debian ccy image and the Fedora host alike: no libc and
  no Python. On an EL9 member it takes Python out of the status line altogether.

## (1) LXC members

- `helpers/agent_bus_lxc/members.py` does not change. `kit_tar` tars the whole host kit
  directory (no file list), keeps the mode (0755), and `KIT_SYNC_PY` writes every file in it,
  so the binary reaches each running member with no change to the member step. In
  play-agent-bus.yml the build task comes right after `software` and before the LXC
  absent/present phases, so a member never gets a kit whose settings name a missing binary.
- In a member, the unit runs `agent-bus-claude`, which now always puts `$KIT` first on PATH, so
  the bare `pingbus-status-line` in the settings resolves.
- A member that is stopped keeps its old, self-consistent kit: the old settings run
  `pingbus status-line` and the old launcher has no binary check. Its next sync brings all of it.
- The EL9 regression is NOT fixed here and is not made worse. The `/usr/bin/python3` 3.11 gate
  in `KIT_SYNC_PY` still refuses an EL9 member, and `run_present` fails the whole helper, so
  play-agent-bus.yml fails at "Join the LXC members", after the kit and teams are installed.
  The host stays consistent: kit binary, settings and launcher all new. But deploy.bash then
  stops before play-claude-yolo.yml and the acceptance. **Queue this entry after the EL9 fix
  is merged and deployed**, or it fails exactly as the last deploy did.

## (2) Sessions started before vs after the deploy

- Before the deploy, ccy: the old image's seat guard runs `exec pingbus status-line`, and the
  Python zipapp is still there. Unaffected.
- Before the deploy, cc: Claude Code reads `--settings` once, so the session keeps
  `pingbus status-line`. Unaffected.
- Before the deploy, LXC: same as cc, and the old launcher already put `$KIT` on PATH. Unaffected.
- After the deploy, ccy: REQUIRED_CONTAINER_VERSION 2.78.9 forces one rebuild, and the image
  links `/usr/local/bin/pingbus-status-line`. The new seat.py guard runs it by name. Works.
- After the deploy, cc: `cc` already runs with `PATH="$CC_TEAMS_KIT:$PATH"` (cc:385, on F44).
  Works.
- The Go binary and statusline.py both skip a settings command of either name, so the bus
  segment cannot loop or be drawn twice (new golden `settings-never-the-kit-command`).

## (3) "Refuse a kit without it" and partial deploys

- In playbook-main.yml, play-claude-yolo.yml (36) runs BEFORE play-agent-bus.yml (46). A main
  run that fails between them, or a lone play-claude-yolo.yml run, installs the new
  cc-teams.bash, which needs `kit/pingbus-status-line`, while the host kit still lacks it. Every
  `cc --teams` / `cc --bus` launch is then refused (78) until play-agent-bus.yml runs. The
  message names both plays, so it is actionable. ccy and LXC members are not affected (the image
  carries its own binary). deploy.bash runs the plays in the safe order. Low; worth noting in
  the meta-deploy text, or have cc-teams check the binary only when the kit settings name it.
- agent-bus-claude refuses with "the kit at … is incomplete: pingbus-status-line is missing".
  That message does NOT name play-agent-bus.yml, although the play comment says "naming this
  play" (play-agent-bus.yml:199-200). Under the member unit (restart on any exit) it would
  crash-loop. The binary is installed before any member sync, so I found no path to that state
  short of a sync interrupted between `kit/agent-bus-claude` and `kit/pingbus-status-line`
  (sorted write order). Low: add "run play-agent-bus.yml on the host" to the die message.
- `agent-bus-install software` installs the new settings.json and launcher one task before the
  Go build. If the build fails, the host kit is left naming a missing binary. Fail-fast stops
  the play and the next run repairs it. Low.
- The Dockerfile's `chmod`/`ln -s` of the binary is unconditional. A run with
  `--tags dockerfile` only (the Dockerfile copy carries `dockerfile`; the build task carries only
  `scripts,skills`) would break the image build for every ccy user. The pingbus zipapp has
  exactly the same exposure already. Low, existing pattern.

## (4) Versions

- CCY <ip> / container 2.78.9 are bumped consistently (script, Dockerfile, changelog).
  Branch agent-a1a822193f01dd6b3-255f4a9c also holds <ip>, so whichever merges second
  re-bumps. (Note only.)

## (5) meta-deploy.list, acceptance, PlanScriptStandards

- **Merge conflict in CLAUDE/Plan/meta-deploy.list** with F44 (`git merge-tree`). Since the
  base, F44 has dropped the 00211 spike and stage-1 entries (they already ran). The branch
  still carries them as context and replaces the separate 00213 1.3/1.4 plays with one
  `00213…/deploy.bash` entry. Resolve to F44's list minus the two standalone 00213 play
  entries, plus the branch's deploy.bash entry. Do NOT bring back the 00211 entries.
- Drift in the acceptance text: acceptance.bash's header (l.17), usage (l.52) and C5 leg name
  (l.101) say "under a tenth". The code (`_acceptance.inc.bash:192`, `go * 5 > python`),
  PLAN.md:170 and meta-deploy.list say "under a fifth". Fix the three strings.
- C3 rejects any declared present LXC container that is not RUNNING. After a reboot that is the
  normal state, and the play itself only defers such a member. The meta-deploy text says so;
  the owner must start each member first, or the entry is reported as failed. Acceptable and
  documented, but flag it to the owner.
- R1 bootstrap, `plan_init`, `plan_prime_sudo` before `plan_start_log`, `--check` handling, exit
  codes, the report in PLAN_RUN_DIR, and no install-specific names: all good.
- Small doc drift: the seat.py `session_settings` docstring (l.455) still says the guard runs
  "`pingbus status-line` with it".
- `build-go-command.yml`'s `environment:` is now one templated dict for every Go command
  (ccy-teams, ccy-sessions, cc-project-dir). That is the documented Ansible pattern, but no
  host run has exercised it yet, and a mis-render would fail the whole ccy play. The deploy is
  the first proof.

## Before merge / before deploy

1. Resolve the meta-deploy.list conflict as described above.
2. Queue/run this deploy only after the EL9 (Python 3.9) kit-step fix is deployed.
3. (Trivial) Change "tenth" to "fifth" in acceptance.bash; optionally name the play in
   agent-bus-claude's incomplete-kit message.
