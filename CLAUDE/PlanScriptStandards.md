# Plan-Script Standards — build orchestrators on `_planlib.inc.bash`

A plan that needs anything **run** ships a runnable `*.bash` in its own folder which the
operator runs with one command. This document is the single source of truth for the
**mechanics** of such a script; [PlanWorkflow.md](PlanWorkflow.md) owns where they live and
the transient-vs-persistent split.

- Implementation: [`Plan/_planlib.inc.bash`](Plan/_planlib.inc.bash)
- Tests: [`../scripts/test-planlib.bash`](../scripts/test-planlib.bash)
- Sibling implementation: lts-infra's `CLAUDE/Plan/_planlib.inc.bash`
- Donor of the concept: ballicom-infra's `CLAUDE/Plan/_planlib.bash`

Everything in [StderrHygiene.md](StderrHygiene.md), [QA.md](QA.md), and the repo-wide
fail-fast rule still applies in full. This document adds the orchestrator-specific rules.

## Why this exists

`triage.bash` for **Plan 00068** (scaffolded as 00066, renumbered) resolved its repo root with
`git rev-parse --show-toplevel`, because [PlanWorkflow.md](PlanWorkflow.md) said to. That
command answers about the **cwd**, not about the script. Run by path from another repo's
root it resolved to that repo, wrote its report there, and the probe meant to catch
deployed-vs-checkout launcher drift compared against a path that does not exist:

```
sha256sum: /home/<user>/Projects/LTS/lts-infra/files/var/local/claude-yolo/claude-yolo:
           No such file or directory
```

It reported `Could not checksum both files` — **the one check that mattered degraded into a
shrug rather than a failure.** That is the failure class this whole document exists to
prevent, and it is the same class as the `|| true` the repo already bans: a control that
silently becomes a no-op.

Fixing that one script would have fixed nothing, because the guidance was wrong. So the
dangerous primitives now live in one tested library where the correct behaviour is the only
behaviour available.

## The rules

A deliberate deviation carries `# STANDARD-EXCEPTION(Rn): reason` on the offending line.

### R1 — Bootstrap: script-relative, filesystem-only, bounded at the repo boundary

**Forbidden**: `git rev-parse` root resolution, a hardcoded `/workspace`, and fixed-depth
`../..` walks.

```bash
#!/usr/bin/env bash
set -euo pipefail
scriptDir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd -P)"
repoRoot="${scriptDir}"
while [[ "${repoRoot}" != "/" ]] && [[ ! -e "${repoRoot}/ansible.cfg" ]]; do
  if [[ -e "${repoRoot}/.git" ]]; then
    printf '[FATAL] no ansible.cfg between %s and the repo root %s\n' "${scriptDir}" "${repoRoot}" >&2
    exit 1
  fi
  repoRoot="$(dirname "${repoRoot}")"
done
[[ -e "${repoRoot}/ansible.cfg" ]] || { printf '[FATAL] no ansible.cfg above %s\n' "${scriptDir}" >&2; exit 1; }
# shellcheck source-path=SCRIPTDIR
# shellcheck source=../_planlib.inc.bash
source "${repoRoot}/CLAUDE/Plan/_planlib.inc.bash"
plan_init "${BASH_SOURCE[0]}"
```

**The `.git` bound is load-bearing.** This repo is routinely checked out inside lts-infra at
`untracked/repos/fedora-desktop`, and **both repos have an `ansible.cfg` at their root** — so
an unbounded walk from a plan script here finds the outer repo's marker and appears to work.
The bound uses a plain `-e` so a worktree's `.git` *file* stops the walk too, and involves no
`git` command.

Both `# shellcheck` lines are **source directives, not suppressions**. `source-path=SCRIPTDIR`
makes the relative `source=` path resolve from the script's directory rather than the linter's
cwd; without it `shellcheck -x` cannot follow the library and every check that depends on
following it silently lapses (`PLAN_USAGE` reads as unused, and so on).

### R2 — Declare where the script may run: `plan_require_host` / `plan_require_container`

**A script whose findings depend on host state calls `plan_require_host '<why>'` first.**

Claude's CCY container bind-mounts this repo, so any plan script is trivially runnable from
it — and for anything probing the container engine, host devices, systemd, or the deployed
`/var/local/…` tree, the answer obtained there is **not** an answer about the host. The
failure mode is not a missing result, it is a confident wrong one:

> An attempt to answer "is a missing `--device` fatal?" from inside the container failed with
> a userns/subuid error during an image pull, **before** it ever reached the device check — a
> non-zero exit that says nothing whatsoever about the question asked. Run on the host, the
> same probe answered it definitively.

`plan_require_host` names the container marker it found and tells the operator to re-run on
the host. This is the enforced version of [ContainerRules.md](ContainerRules.md)'s "EDIT
ONLY, DEPLOY ON HOST" — a guard, not a comment.

**`plan_require_container '<why>'` is the exact mirror, and some scripts need it.** A few
findings are facts about the CCY container itself — what is on its `PATH`, what its PID 1
holds, what the entrypoint installed into it. Run on the host, a script asking those
questions does not fail; it answers them about the wrong machine, and every probe goes
vacuously green or vacuously red. That is the same confident-wrong-answer failure as R2's
original incident, pointing the other way, so it gets the same enforced guard rather than a
comment. Pick exactly one of the two: a script that would accept either location is a script
whose findings do not depend on where it ran, which is rare and worth re-examining.

### R3 — Sudo priming: `plan_prime_sudo`, and **before** the run log

Plays here run against localhost with `become`, so a sudo password prompt can appear mid-run.
After the tee redirect that prompt is flooded and garbled. `plan_prime_sudo` primes the
timestamp on the terminal before the log opens, is idempotent (no second prompt when already
valid), and the ordering is **enforced** by the library rather than documented.

### R4 — Run log: `plan_start_log` only, and it is never committed

A raw `exec > >(tee …)` in a plan script is forbidden — that redirect belongs in the library
where the drain is deterministic. `plan_start_log` uses a **named pipe** whose writer PID the
handler `wait`s on, so the full log (including the final buffered chunk — the lines written as
a run was dying) is on disk before anything reports on it. A `>(…)` process substitution
cannot be waited on at all. The handler is armed for **EXIT and INT/TERM/HUP**, so a Ctrl-C'd
run still leaves a complete log.

**Teardown registers, it does not trap.** A script that creates something it must remove —
a throwaway container, a signal raised to a live session, a temp tree — calls
`plan_on_cleanup <function>` (library 1.3.0+) instead of installing its own
`trap … EXIT`. A hand-written EXIT trap *replaces* the library's handler and the log loses
its final buffered chunk, which is the lines written as the run was dying; chaining the
library's private `_plan_finalize_log` into a trap string avoids that and couples the
script to an internal name, so a rename stops teardown in a script nobody would re-test
and the only symptom is the resource left behind. Registered functions run on EXIT and on
INT/TERM/HUP alike, **before** the log drains so their output is in it, in registration
order, and a failing one does not stop the others — teardown is the one place where
continuing is right, because the alternative is leaking everything after the first
failure. Each failure is named. Registering a name that is not a function fails at
registration, where the script can still be fixed.

**Run logs are UNSCRUBBED and live under `untracked/plan-runs/<plan>/<script>/<timestamp>/`**,
and the library says so on every run. A play can stream vault-decrypted values, so the
*location* carries the rule: `untracked/` is excluded wholesale and is self-excluding, so a
log cannot reach an index by accident. Logs used to sit beside the plan's tracked files behind
a `*-runs/` glob, which held only while the directory kept a name that still matched.

This repo **does** have a secret scrubber — `scripts/lib/run-log-scrub.bash`, built by Plan
00121 with its own QA gate — but it is wired into the VM lab's run logs, **not** into these.
So the accurate statement is "plan run logs are unscrubbed", not "this repo has no scrubber";
the latter was true once, stopped being true, and would send a reader off to build a second
one. If committing plan run logs ever becomes worthwhile, wire that scrubber in **first**, in
the same change that moves them back under version control.

### R5 — Prompts: `plan_confirm` only

A bare `read` after `plan_start_log` is forbidden: ansible legs drain the inherited stdin so a
later plain `read` misfires, and a promptless partial line block-buffers in the tee pipeline so
the run wedges with nothing on screen.

Inside `plan_confirm` the prompt **text** goes to ordinary stdout terminated by a newline, and
only the **reply** is read from `/dev/tty`. Writing the prompt straight to `/dev/tty` is
unbuffered, bypasses the tee, and races *ahead* of buffered output so the prompt appears above
its own banner. Never hand-roll a `printf … >/dev/tty` prompt.

For genuinely interactive helpers the bounded-retry UX rules in
[InteractiveScripts.md](InteractiveScripts.md) apply; a plan orchestrator asks a single
specific question and takes a wrong answer as an abort, not as a retry.

`plan_confirm` is for a question a script genuinely needs answered — which of two hosts,
whether to destroy a named artefact it found. It is **not** for a blanket "may I proceed?";
see R8.

### R6 — Ansible: `plan_ansible_playbook` / `plan_ansible_adhoc`, from the repo root

Both wrappers `cd` to `PLAN_REPO_ROOT` in a subshell before invoking ansible. **That cd is
load-bearing**: `ansible.cfg`'s `inventory`, `roles_path`, `fact_caching_connection` and
`vault_password_file` are all **relative** paths, so running from anywhere else silently picks
up different — or missing — ones. Unlike lts-infra there is no wrapper script to delegate to;
`ansible.cfg` already supplies inventory, vault password file and `transport = local`.

Both close stdin so ansible cannot drain the script's own stdin out from under a later prompt.

A relative playbook path is resolved against `PLAN_REPO_ROOT`, never the cwd, so
`plan_ansible_playbook playbooks/imports/foo.yml` works whether the operator runs
`./deploy.bash` from the plan folder or by full path from anywhere else.

### R7 — Mode declaration and leg semantics

Declare `plan_mode deploy` or `plan_mode gather` before the first leg; the wrong leg-runner
for the declared mode is a hard error.

**Changes state?** → `plan_mode deploy` + `plan_deploy_leg`: fail-fast, abort on the first
failure, nothing downstream runs. This is the repo's #1 fail-fast rule applied to legs — there
is no "continue past a failed deploy leg to gather more". **Purely read-only?** →
`plan_mode gather` + `plan_gather_leg`: may continue past a failed leg to collect as much as
possible, but records every failure by name and exits non-zero.

`plan_deploy_leg` must be a **bare top-level statement**. Inside `$(…)`, a pipeline, or `( )`
its abort would terminate only the subshell and control would flow on to the next leg — the
run would look aborted while actually continuing. The library detects that misuse via
`BASH_SUBSHELL` and takes the whole run down rather than half-obeying.

### R8 — No blanket confirmation prompt; running the script IS the consent

A deploy script must **not** stop and ask the operator to confirm that it may change the
machine. Invoking a script whose name and header both say it deploys is already an
unambiguous statement of intent, and a second in-script prompt conveys nothing the
invocation did not carry. It also costs: in a batch run the prompt arrives minutes in, long
after anyone is watching, so it reads as a hang rather than a question. And a gate people
learn to type through stops being a gate — it trains the very reflex it exists to interrupt.

The library once had `plan_gate_change` for this. It has been removed, and
`scripts/test-planlib.bash` asserts it stays removed. Do not reintroduce it, in the library
or hand-rolled in a plan script.

What **does** protect a state-changing run is structural rather than conversational:

- **`plan_mode deploy` vs `plan_mode gather`** (R7) — the run declares whether it changes
  anything, and the wrong leg-runner for the declared mode is a hard error. A read-only
  script cannot mutate by accident because `plan_deploy_leg` refuses to run in `gather`.
- **`--check`** — the dry run is the rehearsal, and it is a flag rather than a prompt, so it
  is available to an unattended caller as well as to a human.
- **`plan_require_host`** (R2) — the run refuses to proceed anywhere but the HOST, which is
  the mistake that actually costs something; a container answer about the operator's
  workstation is a confident wrong one.

What the gate's description string used to say — *what this run changes* — is still owed to
the reader. Put it in the script's header comment, where it is legible before the run starts
rather than only once it is under way.

`plan_confirm` stays available for a **specific** question a script cannot answer itself
(R5). "May I proceed?" is not such a question.

### R9 — Triage gathers facts; acceptance renders the verdict

Keep the roles distinct, as [AgentNotes.md](AgentNotes.md) requires: `triage.bash` establishes
grounded facts and renders **no** verdict; `acceptance.bash` (or `verify.bash`) is the pass/fail
gate. A `gather` leg failing means "this probe did not run", not "the system is broken" — so a
triage script's non-zero exit says *the fact-finding was incomplete*, which is precisely why it
must still be non-zero rather than swallowed.

### R10 — Reports go in the run directory, and the script writes them

`plan_start_log auto` creates `untracked/plan-runs/<plan>/<script>/<timestamp>/`, exports it as `PLAN_RUN_DIR`, and
`plan_finish` lists every `*report*` file in it. Write reports **there**:

- it is inside the repo, so the agent reads it at the same path the operator sees (the CCY
  container bind-mounts the repo) — the property [AgentNotes.md](AgentNotes.md) wanted from
  `untracked/reports/`;
- it is gitignored, so an unscrubbed report is never committed;
- it is **per-run**, so re-running never clobbers the previous run's evidence — which
  `untracked/reports/<fixed-name>.md` does;
- `plan_finish` names it automatically, so the operator is never left hunting.

Never rely on the operator typing `> report.txt 2>&1`.

### R11 — No error hiding, no QA suppressions

The repo bans `|| true`, `2>/dev/null` to silence a failure, `set +e`, and QA-suppression
directives; `qa-patterns.bash` and the hooks daemon both enforce it. Capture into a variable and
report it — see the "Capturing a probe without an error-hiding redirect" pattern in
[InteractiveScripts.md](InteractiveScripts.md).

A missing tool is an **IaC gap**, not a runtime fallback to engineer around (see `CLAUDE.md`,
"Missing Dependencies"). So a check whose tool is absent **fails the leg**; it does not skip.
Absence of a check is not a passing check.

### R12 — Ship it executable, and run QA

The `Write` tool creates files `0644` and a plan script is invoked as `./script.bash`, so
`chmod +x` before `git add`. `scripts/qa-bash.bash` discovers every repo-owned `*.bash`
including plan scripts, so run `./scripts/qa-all.bash` before committing one.

### R13 — Stderr hygiene, correctly applied

Every library function that emits a **captured value** keeps stdout pure and puts diagnostics
on stderr (`_plan_find_repo_root`, `_plan_strip_cr`, `_plan_in_container`). The banners, leg
headers and prompts **do** use stdout, and that is correct under
[StderrHygiene.md](StderrHygiene.md)'s explicit carve-out: a plan orchestrator's entire job is
to print a run report for a human, and no caller captures it — "the human report is the
payload". Do not "fix" them to stderr.

### R14 — Deviation from "a library never exits", pinned by a test

Three library functions call `exit`: `plan_deploy_leg`, `plan_finish`, and
`plan_parse_common_flags --help`. For the legs, "abort the whole run" **is** the contract, and
pushing it out to `|| exit 1` at every call site reintroduces the R7 catastrophe — one forgotten
guard and a failed leg flows into the next. The library still sets **no** shell options; the
caller owns those. `scripts/test-planlib.bash` asserts that *exactly* those three functions
contain an `exit` statement, so a fourth cannot appear unnoticed. Do not extend the exception
without extending that test.

## Reference skeleton — a read-only triage

The bootstrap block is identical everywhere and elided as `<BOOTSTRAP>`; copy it verbatim from
R1. A working, lint-clean example lives at
[`Plan/Completed/00068-ccy-ci-runner-variant/triage.bash`](Plan/Completed/00068-ccy-ci-runner-variant/triage.bash).

```bash
#!/usr/bin/env bash
# triage.bash — gather grounded FACTS about <thing>. Fact-finding only: renders no verdict
# (R9) and changes nothing. Read-only and safe to re-run.
#
# WHERE TO RUN: on the HOST, in a terminal. Enforced by plan_require_host (R2) — a nested
# container result is not evidence about the host.
#
# Usage: ./CLAUDE/Plan/<plan>/triage.bash [-h|--help]
<BOOTSTRAP>

PLAN_USAGE="usage: triage.bash [-h|--help]"
plan_mode gather
plan_parse_common_flags "$@"

plan_require_host "it probes the host container engine and the deployed launcher tree"
plan_start_log auto

REPORT="${PLAN_RUN_DIR}/triage-report.md"      # listed by plan_finish (R10)
plan_gather_leg "engine identity" bash "${PLAN_SCRIPT_DIR}/probe-engine.bash" "${REPORT}"
plan_gather_leg "deployed vs checkout" bash "${PLAN_SCRIPT_DIR}/probe-drift.bash" "${REPORT}"
plan_finish
```

**Legs are commands, not local shell functions.** A leg command is passed to
`plan_gather_leg` by name, so a local function used that way is invoked indirectly and
`shellcheck -x` reports its whole body as unreachable (SC2317). Since suppressions are banned
(R11), non-trivial probe logic lives in its own script — which is independently runnable and
independently lint-clean.

## Verification before committing a plan script

```bash
bash -n CLAUDE/Plan/<plan>/<script>.bash
shellcheck -x CLAUDE/Plan/<plan>/<script>.bash
chmod +x CLAUDE/Plan/<plan>/<script>.bash        # R12 — Write creates 0644
./scripts/test-planlib.bash                       # if the library itself was touched
./scripts/qa-all.bash                             # always
```
