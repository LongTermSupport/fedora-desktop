# Review: ccy 3.100.16, session container --pids-limit (F44...HEAD, 21c2b65b2 + 2eebc4b79)

Rescued 2026-11-06 from an agent worktree's untracked scratch (Plan 00250 Task 2.3); it was held by no ref.

Verdict: **CHANGES**

## What I checked and confirmed

- `scripts/test-ccy-container-limits.bash`: 23 passed, 0 failed.
- `./scripts/qa-patterns.bash`: green (495 files OK).
- The red commit is really red: the rule, with its `paths:` filter taken out, run on
  `git show 21c2b65b2:files/var/local/claude-yolo/claude-yolo` reports line 3881 (the
  `ccy_agent_container run`). On HEAD it reports 0.
- Launch paths. There is one session `run` (claude-yolo:3879). The Go front
  (`src-go/internal/ccylaunch/front.go:477`) only execs the bash launcher, so it inherits
  the fix. The in-session restart (claude-yolo ~3814) execs the launcher, which keeps the
  host's exported value or the default. The other `container_cmd run`s are short-lived
  helpers: `update_claude_inplace` :2001, the network probe :3362, and the token setup in
  token-management.bash :1117/:1257. I found no session launch that lacks the flag.
- Validation: the regex plus the `-gt 4194304` check rejects 0, -1, empty-after-default,
  padded, leading-zero and suffixed values. It runs at claude-yolo:938, before the tmux
  re-exec (:1199), before any build, and before the Quick Launch prompt.
- The docs' `podman exec ... /sys/fs/cgroup/pids.*` check is accurate. Inside this CCY
  container `/sys/fs/cgroup/pids.max` reads 2048 (the defect itself) and `pids.peak` exists.
- Choosing to propose rather than build the container-watch signal is justified.
  container-watch is a standalone Python CLI (`helpers/containerwatch/`, wrapper
  `files/home/.local/bin/container-watch`). CLAUDE.md says such a CLI "is ported to Go when
  it is next reworked", so adding a signal now would force the whole port.

## Findings

1. **The documented override does not reach the session on the normal path.** It is lost
   when the ccy tmux server is already running, and it sticks for every later session when
   the server is not yet running.

   - Where: `files/var/local/claude-yolo/lib/tmux-session.bash:422` and `:461-465`;
     `docs/ccy.md:988-992`.
   - Why it happens: every interactive launch re-execs itself in a pane on the shared
     `-L ccy` tmux server (claude-yolo:1198-1199). A pane gets the server's global
     environment, not the client's. The launcher in the pane re-runs `ccy_container_limits`
     and reads whatever environment the server was started with.
   - Reproduced with real tmux (`untracked/scratch/tmuxenv-probe.bash`):
     - Server started with the variable unset, client exports `CCY_PIDS_LIMIT=32768`: the
       pane sees `unset`, so the session gets 16384.
     - Server started with `CCY_PIDS_LIMIT=999`, later client has it unset: the pane sees
       `999`.
   - Effect: an owner who raises the limit after hitting it gets the default without being
     told. One test launch with a small value silently caps every later session until the
     tmux server dies. Boot restore (`ccy-sessions restore`, through the same
     `ccy_tmux_start_detached`) never sees a shell export either.
   - So "Export it on the host before launching to override it for that launch" is false
     for every non-headless launch.
   - Fix: carry the settled value on the pane's command, as the token hand-off does:
     `${CCY_PIDS_LIMIT:+env "CCY_PIDS_LIMIT=$CCY_PIDS_LIMIT"}` beside
     `CCY_GH_TOKEN_HANDOFF` at :422. Decide what a restore uses: record it in the session
     registry, or say in the docs that a restore takes the default.
   - Add a case under the fake-tmux harness of `test-ccy-token-handoff.bash`, or a new
     one, proving the inner launcher gets the outer launcher's value.
   - `CCY_EXTRA_MOUNTS` (docs/ccy.md:1701) has the same pre-existing flaw. Note it, or
     file it as a follow-up.

2. **The library names a rule that does not exist.**

   - Where: `files/var/local/claude-yolo/lib/container-limits.bash:3` says "Defence Before
     Fix rule container-engine-default-limit".
   - Why: the rule is `ccy-session-container-default-pids-limit`. A reader who greps the
     cited ID finds nothing.
   - Fix: cite the real ID.

3. **The rule is drawn narrower than the class, and the sweep's leftovers are recorded
   nowhere durable.**

   - The class asked for is "a container relies on the engine's default resource limit".
     The rule only covers pids, and only containers labelled `ccy=true`.
   - `CLAUDE/QA.md`'s "What it does not see" names `ccdt` (`files/home/.local/bin/ccdt:173`,
     a long-running `exec podman run`), the docker `dnode` functions
     (`files/etc/profile.d/zz_lts-fedora-desktop.bash:121`) and docker-in-lxc as "left to
     the owner". No issue or plan records that hand-off, so it will be forgotten.
   - Fix (either one):
     - widen the rule to every long-running engine `run` (not `--rm ... <one-shot>`) and
       fix or annotate each finding; or
     - keep the narrow rule, rename the class honestly in QA.md ("a ccy session container
       relies on the engine's default pids limit"), and file a GitHub issue for the
       remaining containers that QA.md links to.

4. **The container-watch proposal is not in the branch.**

   - No commit, plan, issue or doc on the branch mentions container-watch or a proposal.
     The proposal exists only in an agent's reply, which is not durable.
   - Fix: file it as a GitHub issue (pids.current/pids.max per container, warn above about
     80%), or put it in a plan, and reference it from docs/ccy.md "Container limits"
     beside the manual `podman exec` check.

5. **The rule regex lets realistic spellings through and flags one harmless line.** Probed
   with the rule (paths removed) on `untracked/scratch/pids-probe.bash`.

   - Missed:
     - the short label flag `-l ccy=true` (podman and docker both accept `-l`);
     - `--pids-limit` mentioned only in a trailing `# TODO add --pids-limit` comment, because
       the negative lookahead reads comments;
     - `--pids-limit -1`, which the engine treats as unlimited and which QA.md says must
       never satisfy the rule.
   - Flagged by mistake: `echo "podman run --label ccy=true ..."`, an echo of a command,
     not a launch.
   - Fix:
     - accept `(?:--label|-l)` in the label lookahead;
     - stop the pids lookahead at `#`, using `[^\n#]*` before `--pids-limit`;
     - require the value to be neither `0` nor `-1`, for example
       `--pids-limit[ =]+(?!["']?-?[01]?["']?(?:\s|$))`, or simply refuse `--pids-limit[ =]+["']?(?:-1|0)\b`;
     - anchor the command so a quoted string does not count, as the
       `ok` fixture with `echo` already tries to.
   - Add a fixture line for each case.

6. **The figure 16384 is justified by a claim with no evidence in the repo.**

   - Where: `lib/container-limits.bash:12`, "eight times podman's, which busy sessions were
     measured reaching".
   - Why: the sentence is ambiguous (did sessions reach 2048, or 16384?), and no
     measurement is recorded anywhere. For comparison, this container shows `pids.peak`
     1160\.
   - Fix: say plainly that sessions reached 2048 and that 16384 is the headroom chosen. Give
     the user-slice ceiling it must stay under (logind `UserTasksMax`, by default a share of
     the kernel's thread limit) and how many concurrent sessions that allows.

7. **The narrative is written out six times (single source of truth).**

   - Where: the same "a pid is a thread / Go builds, linters, Ansible forks / EAGAIN /
     claude aborted" paragraph appears in `lib/container-limits.bash`,
     `.semgrep/bash-conventions.yml` (a 20-line comment block),
     `CLAUDE/QA.md`, `docs/ccy.md`, `docs/ccy-changelog.md` and the test header.
   - Fix: keep the explanation in QA.md (the rule) and docs/ccy.md (the setting). Make the
     lib, yml and test comments one-line pointers. This also brings the yml comment back
     within the comment-size budget.

8. **Minor: the test only matches strings.**

   - The launcher checks in `test-ccy-container-limits.bash` are greps and an awk on line
     numbers: the validate call must sit at exactly `v + 1`. They prove the text is
     present, not that the value reaches the run. This is acceptable because the difftest
     goldens show `--pids-limit 16384`.
   - Fix: add a difftest case with `CCY_PIDS_LIMIT=32768` and one with an invalid value
     (exit 1, nothing built), so the override is shown working end to end.
