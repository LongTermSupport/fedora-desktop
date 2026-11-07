# Review 2: ccy 3.100.17, bounded session --pids-limit (F44...HEAD: 21c2b65b2, 2eebc4b79, 67ea4ed21)

Rescued 2026-11-06 from an agent worktree's untracked scratch (Plan 00250 Task 2.3); it was held by no ref.

Verdict: **CHANGES**

## What I checked and confirmed

- `scripts/test-ccy-container-limits.bash`: 26 passed, 0 failed.
- `scripts/test-ccy-token-handoff.bash`: 128 passed, 0 failed. This includes the new pane cases.
- `./scripts/qa-patterns.bash`: 495 files OK.
- `./scripts/qa-bash-limits.bash`: OK. The launcher shrank and its allowlist row was tightened.
  `tmux-session.bash` gained comment lines only.
- DBF red, then green. I ran the rule as it now stands, with `paths:` removed, on two files:
  - `git show F44:.../claude-yolo` reports line 3881, the session run. Red.
  - HEAD's launcher and `lib/` report nothing. Green.
- Validation: `^[1-9][0-9]{0,4}$` plus the 256..32768 range check. It refuses 0, -1, `max`,
  padded values, leading zeros, suffixes and shell text. It reports through `print_error`,
  which writes to stderr. It runs at claude-yolo:938, before the tmux re-exec (:1199) and
  before any build.
- The difftest `baseEnv` scrubs the environment, so a developer's own export cannot change
  the goldens. The goldens show `--pids-limit 4096`.
- The Go front runs no container itself.
- The tool-shell soft rlimit is absent: there is no ulimit, rlimit or nproc anywhere in the diff.
- Version: F44 is at 3.100.15 and HEAD at 3.100.17. The changelog entry says 3.100.16 never
  shipped. The container version is unchanged, which is correct.

## Review 1 findings

| #   | Status                                                                                                                                                                                                                         |
| --- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| 1   | Resolved for the plain-variable case: the value goes on the pane's command, and there is a fake-tmux test. Not resolved when the value is exported, which is how a one-shot `CCY_PIDS_LIMIT=8192 ccy` works (finding 2 below). |
| 2   | Resolved: the library cites the real rule ID.                                                                                                                                                                                  |
| 3   | Resolved acceptably. QA.md names the class honestly, and its section "The wider class, referred to the owner" lists the other containers. No issue was filed. The QA.md section is durable enough.                             |
| 4   | Resolved: the proposal is in docs/ccy.md, "Container limits".                                                                                                                                                                  |
| 5   | Resolved: there are fixtures for `-l`, a trailing comment, `-1`, `=0` and `echo`, and my probes confirm them. New misses are in finding 4 below.                                                                               |
| 6   | Partly resolved. The default is now 4096, justified as "three quarters of 2048". The only evidence (sessions peaking at 1160 and 1503) is in the untracked agent report.                                                       |
| 7   | Resolved: the lib, yml and test comments are now pointers.                                                                                                                                                                     |
| 8   | Not addressed, and no reason given: there is no difftest scenario with an override or an invalid value (finding 5 below).                                                                                                      |

## Findings

1. **Requirement (1) is not met: several ccy launch paths still get the engine's default.**
   That default is 2048 under podman and no limit at all under docker. The paths are below,
   followed by why QA.md's justification does not hold, then the fix.

   The paths that run no `--pids-limit`:

   - `claude-yolo:2001`, `update_claude_inplace`: `container_cmd run --name … sh -c 'npm install -g …'`.
     A full npm install, with lifecycle scripts, from the network.
   - `lib/token-management.bash:1257`: `container_cmd run -it --rm --entrypoint claude … setup-token`.
     An interactive claude, open for as long as the user takes.
   - `lib/token-management.bash:1117`.
   - The help and version probes at `claude-yolo:412`, `:966` and `:1921`, and
     `lib/image-build.bash:40`.
   - The network probe at `claude-yolo:3362`.

   QA.md:941 says "One-shot probes do not carry the hazard". That covers the true probes, but
   the npm install and the setup-token run are not probes. The requirement says *every* ccy
   podman/docker launch path.

   Fix:

   - Give every ccy `run` the same flag from the same setting. One way is a single helper,
     for example `ccy_container_run`, that adds `--pids-limit "$CCY_PIDS_LIMIT"`.
   - Settle the limit before the earliest of these runs. The `--help` probe at :412 runs
     before :938, so either move `ccy_container_limits` earlier or settle the default where
     the probe runs.
   - Widen the rule from "labelled `ccy=true`" to "any engine `run`/`create` in
     `files/var/local/claude-yolo/**`".
   - Update QA.md and docs/ccy.md to say every ccy container run is bounded.

2. **An exported override leaks into the tmux server's global environment, so the docs claim
   "no later one" is false.**

   - Where: `lib/tmux-session.bash:462-466`; `docs/ccy.md:998`; `docs/ccy-changelog.md:34`.
   - How it happens:
     - `CCY_PIDS_LIMIT=8192 ccy` exports the variable to the launcher.
     - `ccy_container_limits` keeps the export attribute.
     - When this launch starts the `-L ccy` server, `systemd-run --scope -- tmux … new-session`
       runs in the launcher's environment, so the server's global environment holds 8192
       until the server dies.
     - Every pane on that server inherits it, through restores and team restarts as well
       as pane shells.
   - Effect:
     - A team restart (`src-go/internal/teams/restart/jobs.go:288`) or a restore
       (`src-go/internal/sessions/restore.go:236`) of any session on that server runs at
       8192, not the default.
     - A team restart of a session launched with an export, on another server, silently
       drops back to the default.
     - The docs mention only "restored after a reboot", not `ccy-teams` restart.
   - The new token-handoff test cannot see this: it sets the variable as a plain shell
     variable, never exported.
   - Fix:
     - Start the server and session with the variable removed, for example
       `env -u CCY_PIDS_LIMIT systemd-run …` or `-u` on the tmux call. The pane's command
       still carries the settled value.
     - Add a fake-tmux case with the variable exported, asserting the
       `TMUXPIDS unset` line.
     - Say in docs/ccy.md that a restore *and a team restart* take the default.

3. **The host run is not queued (CLAUDE.md "Host runs go through meta-deploy.bash").**

   - `CLAUDE/Plan/meta-deploy.list` is empty on HEAD, and the branch does not touch it.
   - The change only takes effect after `playbooks/imports/play-claude-yolo.yml` runs. That
     play installs `lib/container-limits.bash` and the new launcher, with no image rebuild.
   - Fix: add a `playbooks/imports/play-claude-yolo.yml` entry (or `changed-plays`) with a
     description. The description should say: ccy 3.100.17, sessions start with
     `--pids-limit 4096`, and running sessions keep 2048 until they are relaunched.
   - If the coordinator queues it at merge, say so in the merge commit.

4. **The rule regex has realistic false negatives that QA.md does not list.** I probed with
   the rule, `paths:` removed, on `untracked/scratch/pidsprobe2/probe.bash`.

   - Missed:
     - `echo hi && podman run -i --label ccy=true "$IMAGE"`. The `(?!echo|printf)` guard
       rejects the whole prefix, so a status echo chained before a real run hides that run.
     - `podman run -it --label ccy=true "$IMAGE"; podman run --pids-limit 10 x`. The
       lookahead runs on past `;` into a different command.
     - `--pids-limit 00`, which the engine reads as 0, meaning unlimited.
     - `-l=ccy=true`.
   - Correctly flagged: `--pids-limit "-1"`, `--pids-limit=-1`, a limit only in a trailing
     comment, `--label="ccy=true"`, and an `env FOO=1` prefix.
   - Fix:
     - Apply the echo guard only to the command word: anchor at the start of the line or
       after `;`, `&&`, `||` or `|`.
     - Stop both lookaheads at `;`, `&&` and `||`.
     - Refuse `0+` as well as `-1`.
     - Accept `-l[ \t=]`.
     - Add a fixture for each case, or list the leftovers in QA.md, "What it does not see".

5. **No end-to-end proof of the override (review 1 finding 8, not addressed).**

   - The launcher checks in `test-ccy-container-limits.bash` are greps and an awk ordering
     check.
   - Fix: add difftest scenarios to `src-go/internal/ccylaunch/difftest/testdata/scenarios.json`:
     - `CCY_PIDS_LIMIT=8192`, whose run line shows `--pids-limit 8192`;
     - `CCY_PIDS_LIMIT=0`, which exits 1 with the error and starts no build or run.

6. **Requirement (3) is missing, and the agent report is stale.**

   - No near-limit process-tree snapshot report is in the branch.
   - It is also not in `untracked/agent-reports/261023-ccy-pids-limit.md`. That file still
     describes the first commit: 16384, the range 1..4194304, 3.100.16, and placeholders.
   - Fix:
     - Write the snapshot assessment: build it if cheap, or say what it would take, for
       example a supervisor-side `pids.current` poll with a `ps -eo pid,ppid,args` tree
       dump at about 80%.
     - Bring the report up to date with HEAD.

7. **Minor: the justification of 4096 rests on untracked evidence.**

   - Where: `docs/ccy.md:990`, "seen peaking at three quarters of 2048".
   - The only evidence is in the untracked report: peaks of 1160 and 1503.
   - The headroom for several concurrent sessions under the user slice's `TasksMax` is not
     quantified.
   - Fix: quote the observed peaks, and give the session count that the default allows under
     a typical `TasksMax`, or a command that computes it.

8. **Minor: with `--teams`, the seat is handled before the limit is validated.**

   - Where: `claude-yolo:894` (`ccy_launch_teams`, `ccy_seats_check`, the team chooser) runs
     before `ccy_container_limits` at :938.
   - Effect: an invalid `CCY_PIDS_LIMIT` with `--teams` first prompts or checks seats, then
     fails. The other option checks share this order, so it is not new to this branch.
   - Fix: consider settling the limit right after argument parsing, ahead of the teams block.
