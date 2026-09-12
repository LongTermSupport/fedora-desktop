# Housekeeping Pass

One invocation that runs everything a project's daemon can audit or tidy, in
an order that is safe to repeat (Plan 00330 Phase 3).

## Usage

```claude-code
/hooks-daemon housekeeping                        # report everything; only the formatters act
/hooks-daemon housekeeping --apply prune-venvs    # release one held step to act (repeatable)
/hooks-daemon housekeeping --list                 # the steps, in order, with their disposition
```

The subcommand forwards to `bin/hooks-daemon housekeeping`, which prints the
procedure. **Nothing runs in that process** — the procedure is followed by
Claude, one sub-agent per step, so that the coordinator's context holds
verdicts rather than every step's output.

## The steps, in order

The order is the contract: report-only steps first, so every audit sees the
tree and config the pass started with; mutating steps after; `optimise` last,
because it restarts the daemon.

| #   | Step                    | Command                       | Disposition                     |
| --- | ----------------------- | ----------------------------- | ------------------------------- |
| 1   | `plan-qa-sweep`         | `plan-qa --sweep`             | report only                     |
| 2   | `docs-qa-sweep`         | `docs-qa --sweep`             | report only                     |
| 3   | `check-worktree-seed`   | `check-worktree-seed`         | report only                     |
| 4   | `audit-handler-keys`    | `audit-handler-keys`          | report only                     |
| 5   | `check-permissions`     | `check-permissions`           | report only                     |
| 6   | `disk-usage`            | `disk-usage`                  | report only                     |
| 7   | `remote-docs-check`     | `remote-docs check`           | report only                     |
| 8   | `verdicts`              | `verdicts`                    | report only                     |
| 9   | `block-report`          | `block-report`                | report only                     |
| 10  | `harvest-background`    | `harvest-background`          | report only                     |
| 11  | `worktree-reap`         | `worktree-reap` (no `--reap`) | report only                     |
| 12  | `skill-scan`            | `skill-scan`                  | report only                     |
| 13  | `format-markdown`       | `format-markdown .`           | acts without confirmation       |
| 14  | `regenerate-docs`       | `regenerate-docs`             | acts without confirmation       |
| 15  | `reconcile-settings`    | `reconcile-settings`          | HELD until `--apply`            |
| 16  | `remote-docs-refresh`   | `remote-docs refresh`         | HELD until `--apply`            |
| 17  | `prune-venvs`           | `prune-venvs`                 | HELD until `--apply`            |
| 18  | `check-permissions-fix` | `check-permissions --fix`     | HELD until `--apply`            |
| 19  | `worktree-reap-reap`    | `worktree-reap --reap`        | HELD until `--apply`            |
| 20  | `optimise`              | the `optimise` subcommand     | HELD until `--apply`; runs LAST |

Every command is the daemon CLI verb of the same name; the printed procedure
carries the resolved wrapper path for this install. `optimise` has no CLI
verb — it is a confirmation-driven review — so the pass invokes it through
its own skill entry point, exactly as an upgrade does.

## Why only two steps act on their own

`format-markdown` and `regenerate-docs` are idempotent and reversible: running
them twice produces the same tree, and `git diff` shows exactly what they did.
Everything else in the mutating half changes config, settings, or deletes
files — a single command that silently does all of that is worse than several
explicit ones, so each of those steps reports what it would do and acts only
when you name it on `--apply`. Consent is never inferred from the findings.

## The sub-agent contract

Each step is delegated to a sub-agent whose final message is at most five
lines: the step name, its verdict (clean / findings / error), a count of
findings, the path of its full report under `untracked/reports/`, and what it
CHANGED — for a report-only step always `nothing`. The sub-agent must not
paste the command's output into its reply; the detail lives in the report
file, which has no size limit.

The report-only steps are independent and are dispatched in parallel. The
mutating steps run one at a time, in table order, after every report-only
sub-agent has returned.

## Relationship to the idle advisory

`idle_housekeeping_advisory` (opt-in, beta) is the idle TRIGGER for this same
pass: after repeated no-op failsafe-recovery ticks it points the session at
`bin/hooks-daemon housekeeping` and names the report-only steps. Its
guidance derives from the same step list, so the two cannot disagree. The
advisory never passes `--apply`.
