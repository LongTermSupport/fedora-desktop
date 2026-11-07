# Handoff: ccy pids limit (not plan work)

Rescued 2026-11-06 from an agent worktree's untracked scratch (Plan 00250 Task 2.3); it was held by no ref.

Branch `agent-a5ee80f767118a918-a448e021` (worktree
`<repo>/.claude/worktrees/agent-a5ee80f767118a918-a448e021`), based on F44 (not the 00209
stack). Tip 67ea4ed21. Everything is committed; nothing pushed, merged or tagged.

Commits: 21c2b65b2 DBF red (Semgrep rule + fixture only); 2eebc4b79 fix (3.100.16);
67ea4ed21 re-scope + review round 1 (3.100.17, default 4096, range 256..32768, never off).

## Done

- `files/var/local/claude-yolo/lib/container-limits.bash`: `CCY_PIDS_LIMIT_DEFAULT=4096`,
  MIN 256, MAX 32768, `ccy_container_limits` (host env override, refuses 0/-1/max/anything out
  of range). Launcher: CCY_LIBS, source line, call right after
  `ccy_lifecycle_validate_options` (~line 938), session run `--pids-limit "$CCY_PIDS_LIMIT"`,
  `--debug` echo block folded into one printf (bash-limits ratchet; allowlist tightened).
- `lib/tmux-session.bash:422`: settled value on the pane command
  (`${CCY_PIDS_LIMIT:+env "CCY_PIDS_LIMIT=$CCY_PIDS_LIMIT"}`), tested in
  `scripts/test-ccy-token-handoff.bash` (PANEPIDS/TMUXPIDS lines).
- Rule `ccy-session-container-default-pids-limit` in `.semgrep/bash-conventions.yml`, fixture
  in `.semgrep/bash-conventions.bash`; documented in CLAUDE/QA.md (section of that name, with
  "The wider class, referred to the owner").
- Suite `scripts/test-ccy-container-limits.bash` (qa-gates row `ccy-container-limits`,
  addedgates_test.go row, QA.md row). Play lib list. difftest goldens re-recorded
  (`cd src-go && go test ./internal/ccylaunch/difftest/ -run TestLauncherParity -update`).
- docs/ccy.md "Container limits" (setting, why 4096, restore takes default, container-watch
  proposal), docs/ccy-changelog.md 3.100.17.
- Reviews: untracked/agent-reports/261023-ccy-pids-limit-review-1.md (CHANGES, addressed in
  67ea4ed21) and -review-2.md (CHANGES, NOT yet addressed).
- Tool-shell soft rlimit: dropped by the coordinator; nothing of it was ever built.
- DBF conformance-reviewer could not be dispatched: its definition needs worktree isolation
  and the daemon's WorktreeCreate seeding fails in this nested worktree (vault-pass.secret,
  extensions/node_modules, localhost.yml resolve outside the repo root).

## Left (review 2 findings; the coordinator's latest rule: EVERY ccy podman run path sets the limit)

1. Every ccy run gets `--pids-limit "$CCY_PIDS_LIMIT"`, not just the session: claude-yolo
   ~2001 (update_claude_inplace npm install), lib/token-management.bash ~1117 and ~1257
   (setup-token, interactive), the help/version probes claude-yolo ~412, ~966, ~1921,
   lib/image-build.bash ~40, the network probe ~3362. Move `ccy_container_limits || exit 1`
   to just after the library source block (before ~412; also fixes finding 8, --teams seats
   before validation). Add the flag on existing lines only (bash-limits ratchet: claude-yolo
   and token-management are allowlisted; moving the call line is net zero). Then widen the
   rule to any engine run/create under files/var/local/claude-yolo/\*\* (keep the ccy=true
   form for other trees or drop it), update the fixture, QA.md and docs/ccy.md. Check also
   `podman build` (image-build) — `--pids-limit` exists for build too; decide and record.
2. Exported override leaks into the tmux server's env: in `ccy_tmux_start_detached`
   (tmux-session.bash ~463) start tmux with the variable removed (`-- env -u CCY_PIDS_LIMIT tmux ...`, on the existing line). Add a fake-tmux case in test-ccy-token-handoff.bash with
   the variable EXPORTED asserting `TMUXPIDS unset`. Docs: a restore and a ccy-teams restart
   take the default.
3. Meta-deploy: the coordinator said do NOT edit meta-deploy.list; reject this finding, state
   the entry in the report (below).
4. Regex: echo guard only at command position (start, or after `;` `&&` `||` `|`); stop both
   lookaheads at `;`/`&&`/`||`; refuse `0+` (`00`) as well as `-1`; accept `-l[ \t=]`;
   fixtures for each.
5. difftest scenarios in `src-go/internal/ccylaunch/difftest/testdata/scenarios.json`:
   `CCY_PIDS_LIMIT=8192` (run line shows 8192) and `CCY_PIDS_LIMIT=0` (exit 1, no build/run);
   re-record with -update. (src-go/CLAUDE.md says earlier-port replays "are not extended";
   difftest is described as a recorded oracle on purpose — check before adding, or say why not.)
6. Report: rewrite untracked/agent-reports/261023-ccy-pids-limit.md (stale: says 16384 and
   3.100.16, has placeholders) to HEAD; include the snapshot assessment (below).
7. docs/ccy.md: quote observed peaks (sessions seen at 1160 and 1503 of 2048) and how to
   compute sessions-at-cap under TasksMax.
   Then qa-changed, commit (needs a CCY bump each commit: 3.100.18; fold changelog into one
   entry), review round 3 (fresh Opus general-purpose, no isolation) until MERGE.

## Assessments to carry into the report

- Health finding (fast climb +500 in 5 s, near pids.max, refused forks in pids.events, the
  deepest same-command chain scrubbed of paths): proposed, not built. container-watch
  (helpers/containerwatch, standalone Python CLI, play-container-watch.yml) fits — it
  attributes processes to containers from /proc/<pid>/cgroup and pids.\* files are readable —
  but CLAUDE.md "Tooling Is Go" ports a standalone Python CLI to Go when it is next reworked.
  Also its 60 s timer cannot see a 10 s climb: a fast-climb finding needs a faster sampler
  (a Go watcher, or the snapshot below).
- Near-limit process-tree snapshot (`--debug-trace`): not cheap in this branch. The launcher's
  `podman run` is in the foreground, so a host-side poller is a new background process
  (bash ratchet: must be Go); inside the container entrypoint.sh is allowlisted (cannot
  grow). Cheapest fit: a small static Go command baked into the image (like
  pingbus-status-line), started by the entrypoint beside claude, polling
  /sys/fs/cgroup/pids.current every ~250 ms and, once past 90% of pids.max, writing one
  snapshot (`/proc/*/stat` + cmdline: pid, ppid, depth, command counts, the deepest
  same-command chain) under `<repo>/.claude/ccy/` beside the session records; needs a
  container version bump, a Go spec and tests, and the play's image build.
- Meta-deploy entry (not edited): `playbooks/imports/play-claude-yolo.yml` — installs ccy
  3.100.17 (renumbered at merge): every session container starts with `--pids-limit 4096`
  (CCY_PIDS_LIMIT on the host, 256..32768, overrides), new lib/container-limits.bash; no image
  rebuild; running sessions keep podman's 2048 until each is relaunched.
- Independent search (defence-before-fix:independent-searcher) found only the one ccy session
  launch; other long-running containers (ccdt, profile.d dnode docker, docker-in-lxc LXC,
  unifi compose, Plan 00161 harness members) are recorded in QA.md as referred to the owner.
- Plan 00181 (ccy session limits) is related but Not Started, so nothing recorded there.
