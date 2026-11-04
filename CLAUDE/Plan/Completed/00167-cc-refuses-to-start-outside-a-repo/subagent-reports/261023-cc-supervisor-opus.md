# cc sessions and the ccy supervisor: why cc context grows "unbounded"

Not plan work. Read-only investigation; no tracked file changed.

## Verdict

**A design gap, not a regression and not a hooks-daemon defect.** `cc` has never run the
supervisor, and it also lacks ccy's second compaction guard, the 600,000-token
`CLAUDE_CODE_AUTO_COMPACT_WINDOW` cap. On a 1M-context model a cc session therefore compacts
only at Claude Code's own default, near the full window. That is the growth the owner is
seeing. The hooks-daemon side has no container or CCY env gate that would block a host
session.

## 1. Where the supervisor lives and how ccy starts it

- The supervisor is **not tmux-driven**. It is a PTY wrapper,
  `.claude/ccy/claude-supervise.py`, plus a `#!/bin/sh` launcher, `.claude/ccy/claude-supervise`.
  Both are git-tracked, and the daemon deploys and refreshes them because
  `.claude/hooks-daemon.yaml:37-38` sets `ccy: deploy_supervisor: true`. Its comment at
  :33-36 says "Deploying it does NOT arm it — arming is the launcher's job". It injects
  `/compact` into claude's stdin through the pty it owns. It does not type into a tmux pane.
- Arming: `.claude/ccy/ccy.env` (last line of its code) contains
  `export CCY_CLAUDE_WRAPPER="${CCY_CLAUDE_WRAPPER:-<repo>/.claude/ccy/claude-supervise --arm --}"`.
  The `<repo>` path is hard-coded there, so the file only resolves inside the container.
- `files/var/local/claude-yolo/entrypoint.sh:445-449` sources `ccy.env` (and `ccy.env.local`)
  inside the container. `:604-661` then defaults the wrapper to the supervisor (unarmed)
  when nothing set one, and `:740-756` runs `exec "${_ccy_seat_exec[@]}" "${_ccy_wrapper[@]}" "$@"`.
- tmux is only terminal-death insulation (`lib/tmux-session.bash`, `claude-yolo:77-78`), and
  the supervisor does not depend on it.
- The second guard is `files/var/local/claude-yolo/claude-yolo:3844-3863` (rationale) and
  `:3914`, which passes `-e "CLAUDE_CODE_AUTO_COMPACT_WINDOW=${CLAUDE_CODE_AUTO_COMPACT_WINDOW:-600000}"`
  to podman. It was added in `389d41045` (Plan 00133).
- The Go code under `src-go/` only mentions the supervisor in teams, restore and set-going
  logic and help text. It has no supervisor of its own and no auto-compact env.

## 2. What the supervisor needs

- python3 >= 3.11 (`claude-supervise:31-33, 95-109`). Without it, claude runs unsupervised
  with a warning.
- To run as claude's parent, with `--arm` so it really compacts (`ccy.env` header).
- The trigger is the daemon's context sidecar. The `status_line.context_sidecar` handler
  (`.claude/hooks-daemon.yaml:297`) writes it, and its source is
  `.claude/hooks-daemon/src/claude_code_hooks_daemon/handlers/status_line/context_sidecar.py:86-125`.
  `matches()` returns True and there is no environment gate. The file lands under the daemon
  untracked dir (`sidecar_path`, :80-83). The red threshold is shared with `model_context`.
- Supporting handlers:
  - `pre_compact.compaction_signal` (yaml:288)
  - `goal_injection` (yaml:207)
  - `session_actions_directive` (yaml:258)
  - `ccy_supervisor_integrity`
- The relevance of those handlers comes from `utils/ccy_supervisor.py:90-121` (`is_armed`
  and `supervisor_relevance`). They read the **project file** `.claude/ccy/ccy.env` for the
  `CCY_CLAUDE_WRAPPER` export, not a container check. So on the host they are equally
  "relevant", and they write signals that nobody consumes.
- No tmux pane id or `CCY_*` env is needed by the supervisor itself.

## 3. cc

- The host script is `files/var/local/claude-code/cc`. It is deployed by
  `playbooks/imports/play-claude-yolo.yml:736-737` and aliased at :902-904. The Go piece
  from Plan 00167 is only `cc-project-dir` (play :725).
- `cc:66-68` re-execs into tmux on ccy's server, with sessions named `cc-<project>`.
- `cc:387` runs bare `claude "$@"`. With `--teams`, `cc:385` and
  `cc-teams.bash:139-145` run `pingbus seat exec ... claude ...`.
- No supervisor wrapper, no `ccy.env`, and no `CLAUDE_CODE_AUTO_COMPACT_WINDOW`.
- The status line is the same: the project's `.claude/settings.json`, so the sidecar is written.
- `git log -S supervis -- files/var/local/claude-code/cc` and `-S claude-supervise` /
  `-S CLAUDE_WRAPPER` on `files/var/local/claude-code/` return nothing. It was never there.
- The repo documents the gap as intended:
  - `docs/ccy.md:373` ("For a session with no supervisor (`cc`, ...)")
  - `docs/agent-bus.md:161`
  - `CLAUDE/Plan/00203-.../SPEC-team-add-and-restart.md:27` ("F3. cc has no supervisor")
  - `CLAUDE/Plan/00194-.../PLAN.md:46`
  - Plan 00135 journal 26-10-08:134

## 4. Hooks-daemon gate

There is no container or CCY env gate on the sidecar or the supervisor relevance. The only
environment split in `.claude/hooks-daemon.yaml` is `autonomy:` (:28-30, `hosts: []`), which
turns off the work-driving nudges on the host. It does not touch compaction. Daemon: no defect.

## 5. Fix

Smallest correct fix, in `files/var/local/claude-code/cc`:

- **(a)** Export `CLAUDE_CODE_AUTO_COMPACT_WINDOW="${CLAUDE_CODE_AUTO_COMPACT_WINDOW:-600000}"`
  before the launch at :383-388. One line, and parity with `claude-yolo:3914`. This alone
  bounds the growth.
- **(b)** Optionally, run claude through the project's supervisor when
  `$CC_PROJECT_DIR/.claude/ccy/claude-supervise` exists, i.e.
  `"$CC_PROJECT_DIR/.claude/ccy/claude-supervise" --arm -- claude "$@"`. This needs an owner
  decision:
  - the docs and Plan 00203 treat "cc has no supervisor" as a design fact (set-going, the
    relaunch on team join);
  - `ccy.env` cannot be sourced on the host because of its `<repo>` path;
  - `--teams` would need the wrapper in front of `pingbus seat exec`;
  - the docs (ccy.md:373, agent-bus.md:161) would need updating.

Do (a) now. Do (b) only as a planned change. Deploy is play-claude-yolo, through
meta-deploy. Option (a) is a `cc` change, so no `CCY_VERSION` bump is needed (per Plan 00167
Task 2.3); add a `docs/ccy-changelog.md` cc entry.

## Read-only checks on a live cc session (host)

```bash
tmux -L ccy list-sessions | cat                       # cc-<project> present
tmux -L ccy list-panes -a -F '#{session_name} #{pane_id} #{pane_pid}' | cat
pstree -alp <pane_pid> | cat                           # expect bash cc -> claude, NO claude-supervise.py
pgrep -af '[c]laude-supervise' | cat                   # expect nothing for cc
tr '\0' '\n' < /proc/$(pgrep -n -x claude)/environ | grep -E 'AUTO_COMPACT|CCY_' | cat   # expect no CLAUDE_CODE_AUTO_COMPACT_WINDOW
ls -la <checkout>/untracked/hooks-daemon/*sidecar*/ 2>&1 | cat   # sidecar is written, unconsumed (exact subdir: context_sidecar.py:80)
```

In the session itself, `/autocompact` with no argument prints the resolved window and where
it came from. Expect the model default, about 1M, not 600000.
