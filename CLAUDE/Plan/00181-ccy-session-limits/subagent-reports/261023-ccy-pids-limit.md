# ccy session container pids limit (Defence Before Fix)

Rescued 2026-11-06 from an agent worktree's untracked scratch (Plan 00250 Task 2.3); it was held by no ref.

Not plan work. Plan 00181 (ccy session limits: memory, admission) is related but Not Started,
so nothing is recorded there; the record is docs/ccy.md "Container limits", CLAUDE/QA.md
"ccy-session-container-default-pids-limit" and docs/ccy-changelog.md 3.100.17.

Branch: agent-a5ee80f767118a918-a448e021. Tip: 67ea4ed21 (red 21c2b65b2, fix 2eebc4b79,
re-scope and review round 1 67ea4ed21).

STATUS: HANDED OFF, NOT MERGE-READY. The sections below were written for the first commit
(16384, 1..4194304, 3.100.16). HEAD now has default 4096, range 256..32768, never off, CCY
3.100.17, and the value passed to the tmux pane. Review round 2 returned CHANGES (finding list in
261023-ccy-pids-limit-review-2.md). The current state, what is left, the health-finding and
snapshot assessments and the meta-deploy entry are in
untracked/scratch/261023-a5ee80f767118a918-handoff.md, which supersedes this file where they
differ. Red commit: 21c2b65b2.

## Specification followed

DBF method specification 1.1.0 (cached copy, `<dbf-spec-cache>/SPEC.md`),
project-prompt.md (cached), detector spec 1.0.0, toolchain spec 0.2.0.

## Toolchain

No DBF manifest declaration in this repository (no composer.json/package.json form; nothing
added). The detector is the project's own: Semgrep rules in `.semgrep/bash-conventions.yml`,
run by `scripts/qa-patterns.bash` (Go `src-go/qa/patterns`) over every shell script git would
publish, with `semgrep --test` against `.semgrep/bash-conventions.bash` as the single-rule
harness (the gate runs it every time). Identifier resolution: the rule id is a heading in
CLAUDE/QA.md. There is no defence listing command and no recorded-exceptions list; calibration
assumed: every finding blocks, no exceptions.

## Defect and class

Originating instance: `files/var/local/claude-yolo/claude-yolo`, the session container run
(`ccy_agent_container run $DOCKER_FLAGS --rm ... --label ccy=true ...`, near the end of the
launcher) passed no `--pids-limit`, so every ccy session ran at podman's built-in 2048.

Class: a container relies on the engine's default resource limit. Hazard, as drawn for the
rule: a ccy agent session's container gets the engine's pids default (podman 2048 threads,
docker none); a busy session's forks are refused (EAGAIN on posix_spawn) and claude aborts.

## Rule

`ccy-session-container-default-pids-limit` (Semgrep, bash, `pattern-regex` with lookaheads):
an engine `run`/`create` (podman, docker, an `*ENGINE*` variable, `container_cmd`,
`ccy_agent_container`), read with its backslash continuation lines, that labels the container
`ccy=true` and passes no `--pids-limit`. Scope: files/, scripts/, helpers/, fedora-install/,
CLAUDE/Plan/. Documented in CLAUDE/QA.md (what, why, what it does not see, how to fix).

Excluded as not carrying the hazard: one-shot probes (`run --rm IMAGE claude --version`,
`alpine wget`), whose thread counts are tiny and bounded.

Next wider rule considered: every long-running or interactive container run in the repo
(`-it`/`-d`/foreground services) must set `--pids-limit`. Not built: "long-running" cannot be
told from the text (ccdt has no `-it`/`-d`), and the long-running non-session containers found
(below) run workloads that do not approach podman's default, or are docker/LXC with a
different hazard (no limit at all), which is an owner decision (below).

Not caught (stated in QA.md): argv built in an array elsewhere; an engine named by a variable
without ENGINE in its name.

## Proof

Fixture: 5 `ruleid` cases (the launcher's own shape, a one-liner, `"$CONTAINER_ENGINE" run -d --label=ccy=true`, `${ENGINE} create --label 'ccy=true'`, `exec docker container run` over
two lines) and 5 `ok` cases (the fixed launcher shape, `--pids-limit=` on a continuation
line, a `--version` probe, `podman ps --filter label=ccy=true`, a label on the next,
non-continued line). `semgrep --test --config .semgrep/bash-conventions.yml .semgrep/bash-conventions.bash`: 7/7 rules passed.

Real tree red: the rule run over the launcher as committed at the red commit (copied from
`git show HEAD:...` into untracked/scratch/red/) reports
`ccy-session-container-default-pids-limit` on the session run. The red commit is
RED_COMMIT_PLACEHOLDER (rule and fixture only, no fix).

## Independent search

A `defence-before-fix:independent-searcher` was dispatched before the rule was written, with
the class and hazard only, kept out of `.semgrep/`. (It has no Write tool, so it returned its
findings inline; they are summarised here.)

- ccy session launches: only the originating one (`claude-yolo`, the `ccy_agent_container run`). Go and Python launch no containers; no quadlets or Ansible container modules.
- Test data: the difftest goldens (`src-go/internal/ccylaunch/difftest/testdata/golden/*.txt`)
  pin the launch line; re-recorded with `-update` (25 goldens, one line each).
- Other long-running launches, no pids limit: `files/home/.local/bin/ccdt` (podman, the
  devtools web viewer), `files/etc/profile.d/zz_lts-fedora-desktop.bash` `docker run $dp`
  (dnode/dnpm/dnpx/dyarn, the user's own commands on docker's unlimited default),
  `files/var/local/docker-in-lxc` (LXC Docker host, no `pids.max`), the unifi compose
  services in `play-unifi-controller.yml`, ccy's `compose up` of a project's own compose file.
- Plan harnesses: Plan 00161 `_acceptance-u23.inc.bash` (an LXC member and a `docker run -d ... sleep infinity` member), Plan 00055 acceptance CPU burners (time-boxed).
- Text search alone found the run lines and the absence of any pids setting; reading found the
  `$dp` indirection, the compose body inside an Ansible `copy`, the LXC config append, and the
  one-shot/open-ended judgement.

The search found no ccy session launch the rule misses, so the rule was not widened. Its other
findings are a different hazard (no limit, or a workload that cannot reach the default) and
are referred below.

## Sweep and count

Sweep scope (recorded as this remediation's decision; the project had none): every shell
script git would publish (qa-patterns' own scope), which covers bash in files/, scripts/,
helpers/, fedora-install/ and CLAUDE/Plan/; Go and Python checked by the independent search
(no container launches). Vendored trees excluded by qa-patterns. Count: 1 instance (the
originating launch).

## Fixes

1 of 1 fixed, examined individually: the session run now passes `--pids-limit "$CCY_PIDS_LIMIT"`. No suppression; the rule has no escape annotation.

## Decisions referred to the owner

The class as the coordinator named it ("a container relies on the engine's default resource
limit") also covers containers that are not ccy sessions. These were not fixed and are not in
the rule, because the hazard there is different and whether to bound them is the owner's call:

1. Docker and LXC containers with NO pids limit at all (docker's and LXC's default):
   `zz_lts-fedora-desktop.bash`'s `docker run $dp` functions, `docker-in-lxc`'s LXC host,
   the unifi compose services. A fork bomb there reaches the user's or host's task limit. To
   fix: `--pids-limit` on the dnode functions, `lxc.cgroup2.pids.max` in docker-in-lxc's
   container config, `pids_limit:` in the unifi compose file; then a second rule for "a
   docker/LXC/compose container with no pids limit".
2. `ccdt` (podman, at the 2048 default): a node web viewer, far below it. Leave, or add the
   flag for consistency.
3. Plan 00161 acceptance members (LXC and docker): plan-local scaffolding.

Count fixed 1, remaining in the referred class: 3 production sites, 1 plan harness file.

## Permanence

The rule is in `.semgrep/bash-conventions.yml`, run blocking by `scripts/qa-patterns.bash`
(in qa-all, qa-changed and CI). Green through the project's entry point:
`./scripts/qa-patterns.bash` -> `patterns: 495 files OK`; `./scripts/qa-changed.bash` green
at the fix commit.

## Toolchain and detector gaps

No DBF manifest declaration and no listing command that enumerates the project's defences with
its exceptions (toolchain spec clause 8.5); identifier resolution is by the QA.md heading, not
a resolver command. Reported, not worked around.

## The original defect

`files/var/local/claude-yolo/lib/container-limits.bash` (new): `CCY_PIDS_LIMIT_DEFAULT=16384`
and `ccy_container_limits`, which settles `CCY_PIDS_LIMIT` (host env override, else the
default) and refuses anything but a whole number 1..4194304 (0 and -1 mean unlimited to the
engine). The launcher calls it right after `ccy_lifecycle_validate_options` (before anything
is built) and passes `--pids-limit "$CCY_PIDS_LIMIT"` on the session run; `--debug`'s
simplified run line shows it. Test: `scripts/test-ccy-container-limits.bash` (new qa-gates row
`ccy-container-limits`), 23 checks: default, overrides, 12 refused values, the launcher wiring.

Why 16384: eight times podman's default, which busy sessions reached (other sessions peaked at
1160 and 1503). It stays a finite limit: the containers run in the owner's user slice, whose
TasksMax is shared with the desktop, so a runaway session must stop at its container.

Wiring: CCY_LIBS, the source line, play-claude-yolo.yml's lib list, QA.md rows, the
qa-all addedgates list. bash-limits: the launcher may not grow, so `--debug`'s seven echo
lines became one printf (net -3 code lines, -2 statements; allowlist tightened).

CCY 3.100.16; container unchanged (2.79.9). docs/ccy-changelog.md and docs/ccy.md
("Container limits", including a `pids.current/peak/max` check from the host).

## Health signal (task 2): proposed, not built

container-watch (`helpers/containerwatch`, Python, deployed by play-container-watch.yml, a
systemd user timer) is the fitting tool: it already attributes every process to its container
from `/proc/<pid>/cgroup`, so it knows each container's cgroup path, and `pids.current`,
`pids.peak`, `pids.max` and `pids.events` are world-readable beside it under /sys/fs/cgroup.
It was not extended here because it is a standalone Python CLI, and CLAUDE.md requires one to be
ported to Go when it is next reworked; a new finding kind is a rework (scan, report schema,
`list`/`status` rendering, the panel's containers section and its contract checker). Proposal,
for the Go port or an owner waiver:

- Finding `pids-pressure` (wrong now): a container cgroup with `pids.current >= 80%` of
  `pids.max`, or a rise in `pids.events`' `max` counter since the last tick (forks refused).
- Advisory (state): `pids.peak >= 80%` of `pids.max` since the container started.
- Rendered as its own table in `container-watch list` (not through the process columns).
  Until then docs/ccy.md gives the one-line check.

## Meta-deploy entry (not edited)

`playbooks/imports/play-claude-yolo.yml` — installs ccy 3.100.16: the session container is
started with `--pids-limit 16384` (CCY_PIDS_LIMIT on the host overrides), and the new
lib/container-limits.bash. No image rebuild. Running sessions keep podman's 2048 until each is
relaunched (exit and run ccy again, or a ccy restart).

## Review

REVIEW_PLACEHOLDER
