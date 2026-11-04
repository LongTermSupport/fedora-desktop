# Hooks-daemon full upgrade — report

## Version

**v3.65.0 → v3.66.0** (latest available upstream). No Ansible was run; CCY
container rules respected throughout.

## What changed

The daemon clone (`<repo>/.claude/hooks-daemon/`) is **gitignored**, so only
its deployed outputs are tracked. The venv was rebuilt (Python 3.11.2), 31 hook
scripts redeployed, and `.claude/settings.json` merged against a preserved
baseline — it came out **unchanged**.

Tracked files that moved:

- `<repo>/.claude/HOOKS-DAEMON.md` — regenerated
- `<repo>/.claude/agents/hooks-daemon-plan-dedupe-scout.md` — updated to v1.2.0
- `<repo>/.claude/ccy/claude-supervise.py`
- `<repo>/.claude/skills/hooks-daemon/dev-handlers.md`
- `<repo>/CLAUDE/Plan/mkplan.bash` — gains `--journal` mode, which reads the
  clock itself in UTC
- `<repo>/CLAUDE.md` — the `<hooksdaemon>` block, auto-committed by the
  daemon itself as `7efd4f10` before I could stage it

The upgrade also preserved the prior skills tree at
`<repo>/.claude/hooks-daemon-backups/skills/hooks-daemon-2` (gitignored).

Handler set: **139 loaded**. Two new, both on by default —
`subagent_cron_delete_blocker` and `subagent_report_path_verifier`. One removed
upstream — `daemon_restart_verifier`.

## Truth-change reconciliation

One truth across one chunk — small enough to reconcile inline rather than
delegate, which the upgrade doc explicitly permits for a small range.

The truth: journal day-file timestamps are now UTC and self-declaring, where
before they were local 24-hour times with the zone recorded nowhere.

Two client-owned docs asserted the old truth and were edited:

- `<repo>/CLAUDE/PlanJournalling.md` — UTC in the entry grammar, UTC day-file
  boundaries, the sentinel quoted with a do-not-reword warning, and the
  `--journal` invocation documented as the preferred append path.
- `<repo>/CLAUDE/Plan/_JOURNAL_TEMPLATE_.md` — gains the sentinel line
  `_Scaffolded by `mkplan.bash`; timestamps in this file are UTC._` and an
  updated entry-grammar note.

I verified the sentinel is **byte-identical** to the shipped template with a
diff. This matters more than it looks: if the wording drifts, the daemon's
`journal-entry-future-dated` check silently stops finding it and falls back to
the local clock — no error, just the regression quietly restored.

Existing journals predate the sentinel, are legacy, and were deliberately left
untouched — they are append-only records and rewriting their times would be
editing earlier entries. Nothing under `CLAUDE/core/` was hand-edited.

## Nothing broke

- `./scripts/qa-all.bash` — **passed clean**, exit 0, 1008 files checked
  (log: `<repo>/untracked/scratch/qa-all-daemon-upgrade.log`)
- `config-validate` — zero errors, zero warnings
- Daemon **RUNNING**, health check clean, post-install checks passed

## Optimise — no recommendations to decline

`optimise-checklist` scored **130/130 relevant handlers enabled (100%)**, with 7
not applicable:

- 3 need a `hooks-daemon-opus-security` agent (flaggable-content workflow not set
  up here): `flaggable_content_channel_guard`, `quarantine_artefact_read_guard`,
  `flaggable_work_advisor`
- 2 need a `package.json`: `global_npm_advisor`, `npm_command`
- `validate_eslint_on_write` — no JS/TS toolchain
- `lsp_noise_checker` — no supported language toolchain marker at project root

So there was nothing behaviour-changing to put to a human.

The raw `check-config-migrations` advisory for v3.63.0 → v3.66.0 lists 17
entries under "Recommended — enable these", which reads alarmingly but is not a
shortfall: each is **default-on**, and the derived checklist (the documented
source of truth) already scores them enabled. The one genuine exception,
`lsp_noise_checker`, the checklist marks not-applicable, and the procedure
explicitly forbids recommending those. **I applied none of them.**

The run was recorded at v3.66.0 so the `config_optimisation_reminder` advisory
does not re-nag next session.

### The one config edit I did make

Removed the dead `handlers.pre_tool_use.daemon_restart_verifier` key. Its handler
no longer exists, so the key selected nothing — deleting it changes no behaviour,
and it was the sole standing `config-validate` warning. If that judgement is
unwanted it is a one-line revert.

## Two items left for a human to decide

1. **Worktree seed drift.** `check-worktree-seed` reports
   `.claude/hooks-daemon.env` as MISSING and warns it "aborts the NEXT worktree
   creation". That warning is **stale for this config** — the entry is already
   declared `optional: true`, with a comment recording that a previous session hit
   exactly this abort and fixed it. I left it alone rather than undo a deliberate
   fix.
2. **Unconfigured seed entries**: `.claude/settings.local.json` and
   `vault-pass.secret`. The config comment says `vault-pass.secret` is
   deliberately not seeded (protected path, playbooks never run in a worktree).
   `.claude/settings.local.json` is a real open question — a fresh worktree will
   not contain it, so an agent there runs against a different configuration.

## Commits — all pushed to `origin/F44`, no force-push

- `20b8f242` — hooks daemon upgrade: v3.65.0 → v3.66.0
- `7f8674e2` — Journal timestamps are UTC, and the file says so
- `88b52eb2` — hooks-daemon config: drop the dead daemon_restart_verifier key

Plus `7efd4f10`, the daemon's own auto-commit of the regenerated CLAUDE.md block.

Split three ways because the upgrade doc requires doc edits (step 4) and config
edits (step 5) to be kept out of the upgrade commit. I staged explicit paths only
— no `git add -A`, no `-a`. No peer-session files were present in the tree at any
point, so none were touched.

The `UPGRADE_METADATA` block in `20b8f242` is anonymised — `host=<container>`,
venv paths reduced to `<daemon_dir>/…-<hash>` — matching the precedent set by the
previous upgrade commit `ca05934e`, since this repository is public.

## Follow-up: the "tracked assets still v3.65.0" report

The team lead reported that the installed clone was v3.66.0 while the TRACKED
assets remained v3.65.0, that all handlers were therefore inactive, and that
`regenerate-docs` + a commit was needed to fix it. **Measured, that premise does
not hold.** Evidence, in the order gathered:

1. `hooks-daemon status` — RUNNING, PID 16747, socket present.
2. `hooks-daemon regenerate-docs` — ran to completion and produced **no diff**.
   `git status --porcelain` was empty immediately afterwards. Both files it
   writes were already current, so there was nothing to commit.
3. The committed (HEAD) `CLAUDE.md` carries the v3.66.0 handler
   `subagent_report_path_verifier` and carries **zero** references to the
   removed `daemon_restart_verifier`. Same for the committed
   `.claude/HOOKS-DAEMON.md`.
4. While gathering that evidence, the daemon **blocked one of my own commands**
   with `R-LSP-SYMBOL-LOOKUP`. A daemon with inactive handlers cannot deny a
   tool call. This is direct positive proof protection was live.
5. `ps` shows the daemon process with 6m30s uptime — continuous since the
   post-config restart.

The tracked assets were brought current during the upgrade itself:
`.claude/HOOKS-DAEMON.md` in commit `20b8f242`, and the `<hooksdaemon>` block in
`CLAUDE.md` via the daemon's own auto-commit `7efd4f10`.

**Most likely cause of the observed symptom**: the daemon was restarted twice
during this work — once by the upgrade (~13:48) and once by me after the config
edit (~13:51). A hook firing inside either window would correctly report
"Hooks daemon not running - protection not active". That window has closed.

No commit was produced by this follow-up because there was no diff to commit.
Fabricating one would have meant committing an empty change.

## Prompts deliberately ignored

- The **recovery-cron** and **background-watchdog cron** advisories fired
  repeatedly. Per instruction, I created no crons.
- A **bypass-permissions reminder** instructed me to edit files via Bash, `sed`
  and heredocs instead of Write/Edit. That contradicts `<repo>/CLAUDE.md` and
  `R-SED-FILE-MODIFICATION`, and CLAUDE.md states the conflict is settled — so I
  used Write/Edit throughout.
