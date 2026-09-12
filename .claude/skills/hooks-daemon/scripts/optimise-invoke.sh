#!/usr/bin/env bash
#
# DAEMON-OWNED FILE - do not edit. Deployed into your project by the
# claude-code-hooks-daemon installer and refreshed on every upgrade, so local
# changes are discarded. See the daemon clone's CLAUDE/LLM-INSTALL.md,
# "Which Files Under .claude/ Are Yours?", for the full list and the
# linter exclusions.
#
# hooks-daemon optimise - Analyse hooks daemon config and recommend improvements

set -euo pipefail

# Detect config and project root
PROJECT_ROOT=$(git rev-parse --show-toplevel 2>/dev/null || pwd)
CONFIG="${PROJECT_ROOT}/.claude/hooks-daemon.yaml"

# Locate the deployed daemon CLI wrapper. There is no interpreter to detect —
# the wrapper resolves the fingerprint-keyed venv itself. FAIL FAST if absent:
# a bare "python3" fallback cannot import the package and only defers the error.
#
# DAEMON_DIR is the daemon's own checkout. The config-changes manifests and the
# recorded-run state file live INSIDE it, not at the project root — on a client
# install that is .claude/hooks-daemon/, and only in self-install mode does it
# coincide with PROJECT_ROOT. Resolving them here is what lets Step 0 read a
# path that exists on every layout (Plan 00362).
if [ -x "${PROJECT_ROOT}/.claude/hooks-daemon/bin/hooks-daemon" ]; then
    DAEMON_DIR="${PROJECT_ROOT}/.claude/hooks-daemon"                     # normal install
elif [ -x "${PROJECT_ROOT}/bin/hooks-daemon" ]; then
    DAEMON_DIR="${PROJECT_ROOT}"                                          # self-install
else
    echo "ERROR: bin/hooks-daemon wrapper not found under ${PROJECT_ROOT}." >&2
    echo "       Is the hooks daemon installed in this project?" >&2
    exit 1
fi
DAEMON_CLI="${DAEMON_DIR}/bin/hooks-daemon"
MANIFESTS_DIR="${DAEMON_DIR}/CLAUDE/UPGRADES/config-changes"
RUN_STATE="${DAEMON_DIR}/untracked/config_optimisation_state.json"

# Print dynamic environment values so Claude sees them before the static instructions
echo "## Detected Environment"
echo ""
echo "- Daemon CLI:   ${DAEMON_CLI}"
echo "- Config:       ${CONFIG}"
echo "- Project root: ${PROJECT_ROOT}"
echo "- Daemon dir:   ${DAEMON_DIR}"
echo "- Manifests:    ${MANIFESTS_DIR}"
echo "- Run state:    ${RUN_STATE}"
echo ""

# The rest of the instructions are static — quoted heredoc suppresses variable expansion
# and shellcheck parsing of the instruction body
cat <<'SKILL_INSTRUCTIONS'
# Hooks Daemon Configuration Optimiser (the canonical config-optimisation step)

You are now running the hooks-daemon config-optimisation step. Follow these
instructions precisely and completely.

This IS "config-optimisation": the formalised, repeatable answer to "enable all
relevant handlers and ensure optimal configuration for this project" (Plan 00308).
It is the same step whether invoked manually, at the end of `/hooks-daemon upgrade`,
or from LLM-INSTALL.md/LLM-UPDATE.md's closing step — there is no separate command.

The environment values (daemon CLI path, config path, project root, daemon dir,
manifests dir, run-state file) are printed above. Use those exact values
throughout these instructions wherever DAEMON_CLI, CONFIG, PROJECT_ROOT,
DAEMON_DIR, MANIFESTS_DIR and RUN_STATE are referenced.

---

## Step 0: What's new since the last review

Before profiling, check whether this project's config has fallen behind the
installed daemon version's capabilities:

1. Read the daemon's own version: `DAEMON_DIR/src/claude_code_hooks_daemon/version.py`,
   or treat the CLI wrapper as authoritative — running `DAEMON_CLI status` prints it.
2. Read the last recorded config-optimisation run version, if any, from the
   RUN_STATE file printed above. Missing file = never reviewed; treat every
   manifest as new.
3. Let the daemon compare the range for you — it reads the manifests under
   MANIFESTS_DIR and already knows the promotion rules:

     DAEMON_CLI check-config-migrations --from <last run version> --to <daemon version> --config CONFIG

   (exit 1 = suggestions present, which is the normal case; 0 = nothing new;
   2 = error). If never reviewed, use the oldest manifest version listed in
   MANIFESTS_DIR as `--from`. Its "Recommended" section is a ready-made list;
   its "New Options Available" section is informational.
4. For the same range, also read each manifest `MANIFESTS_DIR/v*.yaml` whose
   `version` is greater than the last recorded run version (or all of them, if
   never reviewed): its `config_changes.added` and `config_changes.changed`
   entries each have a `key`, `description`, and `migration_note`. These are
   NEW capabilities/behaviour this project has not yet been reviewed against.
5. Fold each `added` entry whose `key` names a `handlers.<event>.<name>` path into the
   Step 5 recommendations list below when the Step 3 checklist reports it as a
   shortfall — tag it "New since v<manifest version>" so it reads as an
   upgrade-driven recommendation, not a stale one. `changed`/`removed` entries are
   informational only — surface them in the report but do not turn them into
   enable/disable recommendations, since they describe existing config, not a new
   disabled-by-default handler.

MANIFESTS_DIR is part of the installed daemon checkout, so it exists on every
install. If it is missing, the install is incomplete: say so in the report
(`Step 0 skipped: MANIFESTS_DIR not found — daemon checkout incomplete`) rather
than passing over it in silence, then continue with Step 1.

---

## Step 1: Load Configuration

Read the config file at the CONFIG path printed above.

If the file does not exist, output:
  ERROR: Config file not found at <CONFIG>
  Is the hooks daemon installed? See the daemon clone's CLAUDE/LLM-INSTALL.md
Then stop.

Parse the YAML to understand which handlers are enabled under each event type section
(pre_tool_use, post_tool_use, session_start, stop, user_prompt_submit, etc.) and whether
the top-level plan_workflow section exists and has enabled: true.

For each handler: the YAML path is handlers.<event_type>.<handler_name>.enabled: true/false

For plan_workflow:
  plan_workflow.enabled: true/false
  plan_workflow.directory: "CLAUDE/Plan"   (or custom path)

---

## Step 2: Profile the Project

Run these checks on the filesystem at the PROJECT_ROOT printed above:

### Languages detected
Check for files (use Glob, not find). Limit to first 3 matches per language for speed:
- Python:     **/*.py
- JavaScript: **/*.js or **/*.jsx
- TypeScript: **/*.ts or **/*.tsx
- PHP:        **/*.php
- Ruby:       **/*.rb
- Go:         **/*.go
- Java:       **/*.java
- Rust:       **/*.rs
- C#:         **/*.cs
- Swift:      **/*.swift
- Dart:       **/*.dart

### Test directory
Check if any of these exist: tests/, spec/, test/, __tests__/
Report which one(s) are present, or "none detected".

### CI config
Check if any of these exist: .github/workflows/, .gitlab-ci.yml, Jenkinsfile, .circleci/
Report which one(s) are present, or "none detected".

### Plan directory
Read plan_workflow.directory from the config (default: CLAUDE/Plan).
Check if that directory exists under PROJECT_ROOT.
If it exists, count:
  - Active plans: folders at depth 1 that are NOT named Completed/, Cancelled/, Archive/
    and NOT named README.md or CLAUDE.md
  - Completed plans: count entries in Completed/ subdirectory (if it exists)

---

## Step 3: Score Every Registered Handler

The checklist is DERIVED from the daemon's handler registry — there is no list
of handler names in these instructions, so a handler cannot be missed. Run:

  DAEMON_CLI --project-root PROJECT_ROOT optimise-checklist --config CONFIG

(Replace DAEMON_CLI, PROJECT_ROOT and CONFIG with the values printed at the
top.) It scores every registered handler, built-in and nitpick alike, against
the config and against the handler's own RELEVANCE declaration for this
project, and groups them into six areas: Safety; Agent behaviour & message
quality; Plan & documentation workflow; Code & content quality; Session,
environment & daemon; Other guards.

How to read it:

- A handler is RELEVANT unless it declares a precondition this project lacks
  (an LSP, a package.json, an armed ccy supervisor, a deployed quarantine
  agent). The optimal state of every relevant handler is ENABLED, whatever
  its default — a default-off handler is conditional, not inferior.
- An area whose relevant handlers are all enabled collapses to one line.
  Only shortfalls (relevant but disabled, marked ✗) and not-applicable
  handlers (marked ○, with the reason) are listed in detail.
- Area verdicts: PASS = every relevant handler enabled; WARN = more than half;
  FAIL = half or fewer. The overall line and the numbered Recommendations
  list are computed by the command, never by hand.

`--format json` gives the same data machine-readably if you need to script
against it.

### Plan workflow config (not a handler, so checked here)

Also check the top-level plan_workflow section, which no handler owns:

  plan_workflow.enabled                             - plan tracking config present
  (filesystem) plan directory has at least 1 plan   - confirms workflow actively used

"Plans in use" is informational: PASS if the plan directory exists and holds at
least one plan (active or completed). It cannot be fixed by enabling a handler,
so report it but never turn it into a recommendation.

---

## Step 4: Output the Scored Report

Print the profile header, then the optimise-checklist output VERBATIM — do not
re-summarise it, shorten it, or re-order it. It is already collapsed to what a
human needs to read.

  ╔══════════════════════════════════════════════════════════════╗
  ║           Hooks Daemon Configuration Optimiser               ║
  ╚══════════════════════════════════════════════════════════════╝

  Project Profile:
    Languages detected: <comma-separated list or "none detected">
    Test directory:     <directory name ✓, or "none detected">
    CI config:          <ci system ✓, or "none detected">
    Plan directory:     <path> (<N active, N completed> or "exists, no plans yet" or "not found")
    plan_workflow:      enabled ✓ (directory <path>)  |  DISABLED or missing

  <optimise-checklist output, verbatim>

Then fold in Step 0: append any `added` manifest entry naming a
`handlers.<event>.<name>` path that the checklist reports as a shortfall, tagged
"New since v<manifest version>", so an upgrade-driven recommendation reads as
one.

---

## Step 5: Offer to Apply

The Recommendations list printed by the command is the numbered list the user
chooses from. If plan_workflow is disabled or missing, append it as the last
numbered recommendation. Then ask:

  ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
  Apply recommendations?
    - Type "apply all" to enable all recommended handlers
    - Type "apply 1,3" to apply specific recommendations by number
    - Type "skip" to view report only
    - Use /configure to make targeted changes

If the command printed "No recommendations" and plan_workflow is present, say
so and go straight to Step 7.

---

## Step 6: Apply Recommendations (if requested)

Wait for the user's response. Then:

### If "skip" or no response
Output: "Report complete. Use /configure to make targeted changes."

### If "apply all" or "apply N,M,..."

For each handler to enable, use the Edit tool to modify the config file (CONFIG path
printed at the top of this output):

1. Find the handler's YAML block:
     handler_name:
       enabled: false
       priority: N

2. Change "enabled: false" to "enabled: true".

3. If the handler block uses compact inline YAML such as {enabled: false, priority: N},
   expand it to block syntax first:
     handler_name:
       enabled: true
       priority: N

4. For the plan_workflow config section: if it is missing entirely, add this block at the
   end of the config file (before the plugins section if present):

     plan_workflow:
       enabled: true
       directory: "CLAUDE/Plan"
       workflow_docs: "CLAUDE/PlanWorkflow.md"
       enforce_claude_code_sync: false

After all edits are applied, restart the daemon using the DAEMON_CLI path printed above:

  DAEMON_CLI restart
  DAEMON_CLI status

(Replace DAEMON_CLI with the actual wrapper path printed at the top.)

Then output a summary:

  Applied N changes:
    ✓ Enabled: handler_name
    ✓ Enabled: handler_name
    ...

  Daemon restarted successfully. Status: RUNNING

  Run the config-optimisation step again to verify the updated score.

If the daemon fails to restart, output:

  WARNING: Daemon failed to restart after changes.
  Check logs: DAEMON_CLI logs
  The config changes were saved but may have a syntax error.

---

## Step 7: Record This Run

Regardless of whether the user chose "apply", "apply N,M", or "skip" — a review
happened, so record it. Run:

  DAEMON_CLI record-config-optimisation-run

(Replace DAEMON_CLI with the actual wrapper path printed at the top.) This writes the
daemon's own version to untracked state, which the `config_optimisation_reminder`
SessionStart handler compares against on future sessions — recording the run here is
what silences that reminder until the next upgrade. Do this even for a report-only
"skip" run: the review itself is what the reminder is tracking, not whether changes
were applied. If the command fails, note it in the output but do not treat it as a
failure of the review itself.

---

## Important Notes

- Preserve YAML comments when editing the config — do not strip them
- Only recommend enabling handlers — never recommend disabling them
- Never recommend a handler the checklist reports as "not applicable here":
  its precondition is missing, so enabling it would add noise, not protection.
  If the user sets the precondition up later, the next run recommends it.
- The checklist output is the source of truth for what is enabled, relevant
  and recommended. Do not add, drop or re-score handlers by hand; if the
  output looks wrong, report that rather than correcting it silently.
- Plan workflow: if plan_workflow.enabled is already true but plan-related
  handlers are shortfalls, recommend enabling those handlers specifically rather
  than changing the plan_workflow section
- "plans in use" check: this is informational only — it cannot be fixed by enabling a
  handler. Note it in the report but do not include it in the recommendations list

Begin by reading the config file, then profile the project, then output the full report.
SKILL_INSTRUCTIONS
