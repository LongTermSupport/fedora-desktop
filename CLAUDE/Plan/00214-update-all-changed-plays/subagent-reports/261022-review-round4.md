# Plan 00214 adversarial review, round 4

Scope: the whole `F44...HEAD` diff of `feat/update-all-changed-plays` (d28b68ce8), reviewed
fresh. I searched by meaning for live text outside `CLAUDE/Plan/` history and `untracked/`
that says how plays are judged stale, what the report counts, or what the panel counts. The
search covered docs/, CLAUDE/\*.md, helpers/, extensions/, files/, playbooks/, scripts/,
tests/, src-go/ and run.bash. I then re-checked the code.

Checks run: the affected Python unit modules (321 tests, OK),
`node --test tests/extensions/test-panel-sections.mjs` (rc 0), and
`./scripts/qa-changed.bash --base F44`, which passed. It reported one file not covered,
`tests/helpers/pingbus/test_identifier_grammar.py`. That file is on F44 and not on this
branch. It shows up because the base is the F44 tip and the branch is behind it.

## FIX-FIRST

### F1. The panel test's header comment still states the old count and the old producer

`tests/extensions/test-panel-sections.mjs:609-613`:

> the label carries the count of plays the document marks as not fresh (carried from
> `check_freshness`, never recomputed here)

This no longer matches the code. `plays.js:40` counts every state except `fresh` **and
`unresolved`**. The test eight lines below (`the label counts the plays run.bash --changed runs`) puts in an `unresolved` row and expects it not to be counted. A play that is not
fresh is therefore not always counted. The judgement is now `changed_plays.judge`, which is
what `plays.js:15-17` says. This is the same kind of text that rounds 1 and 2 flagged
FIX-FIRST: a live comment in a test that states the old count.

Fix: "the label carries the count of plays `run.bash --changed` would run (every state but
`fresh` and `unresolved`), as the document carries them from `changed_plays.judge`; never
recomputed here".

### F2. `fedora-desktop-health --help` describes the old report

`files/home/.local/bin/fedora-desktop-health.j2:42-44`:

> Prints this machine's fedora-desktop health report: what is broken now, what could not
> be checked, plays that have changed since they last ran here, and installed versions …

Before this branch, "changed since they last ran" was the whole rule. The report now also
lists a play whose last run failed (`check_freshness._STATE_LABEL[FAILED]`) and a play that
last ran from a dirty checkout. It also prints the `<checkout>/run.bash --changed` line
(`login_message.render`). The help text describes the command this branch changes, and it
leaves out a whole class of finding.

Fix: "…plays that have to run again here (changed, failed, or last run from a dirty
checkout) with the command that runs them all, and installed versions…". The play that
deploys the template picks the change up through `--changed` in the normal way.

### F3. PLAN.md claims a refusal the code does not make; play_runner's docstrings name the old row set

`CLAUDE/Plan/00214-update-all-changed-plays/PLAN.md:66-68` (Task 1.3, ticked):

> A ledgered play outside `playbooks/imports/` (a `playbooks/dev/` play) is … no longer a
> panel row, so `fedora-desktop-health --run-play` refuses it.

`--run-play` does not refuse it:

- `play_runner.main` builds `ledgered` straight from the ledger
  (`helpers/host_health/play_runner.py:118`, `set(ledger.fold_latest(...))`), not from the
  document's rows.
- `validate` requires only a canonical path under `playbooks/` that is in that set, present
  and executable.

So a ledgered `playbooks/dev/` play still passes the gate and runs.
`docs/playbooks.md:1022-1026` describes this correctly ("which this host's ledger lists").
The plan contradicts both the docs and the code, and the plan is live text with its task
ticked.

The same change left two play_runner docstrings stale:

- `play_runner.py:4-5`: "The list is the plays this host's ledger has seen"
- `play_runner.py:53`: "per ledgered play under `playbooks/`"

The rows are now only the plays `changed_plays.judge` judged, which are those under
`playbooks/imports/`. Ledgered `playbooks/dev/` and `untracked/` plays are no longer rows.

Fix:

1. Correct PLAN.md to say the panel offers no row for such a play, and that `--run-play`
   still runs any ledgered play under `playbooks/`. If a refusal is actually wanted, it is
   new code with a test, and it should not be claimed until it exists.
2. Change the two docstrings to "the host plays (under `playbooks/imports/`) the judgement
   judged".

## NOTE

- **N1. "Changed" covers the dirty case loosely in short summaries.**

  - `CLAUDE/PlanWorkflow.md:149` says "`changed-plays` for every play changed since it ran
    here", and `scripts/test-meta-deploy.bash:55` reads the same way.
  - `login_message._RUNS_CHANGED` ("every play that changed or failed since it ran here")
    does not mention dirty runs.

  `meta-deploy.list`'s header uses the accurate phrase ("has to run again"). These are
  one-line pointers, not definitions, so this is not blocking. Using "has to run again" in
  them as well would close the gap.

- **N2. `collector.py:145` is a 130-column docstring line**, left by round 3's rewrap. Ruff
  passes it, so it is cosmetic.

- **N3. Pre-existing, not introduced here.** `judge` finds a GONE play from the working tree
  (`exists_now`), but `retired.validate` checks the map against HEAD. Suppose a play is
  deleted in the working tree but the deletion is not committed, and the play has a
  `retired-plays.json` entry. Then both the report and `--changed` answer NoAnswer ("names
  a play still at HEAD"). F44 had the same behaviour (`changed_plays.py:162,237`). It is
  now shared by the report, and it fails loudly, not silently. Not blocking.

- **Code re-check, no defect found:**

  - Panel count against what `--changed` runs: GONE rows are dropped by
    `play_runner.runnable`, so `plays.js`'s NOT_RUN set cannot count a gone play.
  - `freshness_section` states coverage only for a run that judged.
  - `run_changed_command` takes the checkout from `__file__`, quotes it, and writes no path
    into the repo.
  - The digest in `once_a_day` covers the command line, so a changed path re-shows the
    report.
  - meta-deploy `changed-plays`: `planDir=run.bash` keeps the unit name `changed-plays`
    (the `PLAN_ROOT/*.bash` rename does not apply), arguments are passed through, and
    words after it are refused.
  - The acceptance sits last with no `changed-plays` entry above it.

- Earlier rounds' notes are not re-raised. None of them is blocking.

VERDICT: FIX-FIRST
