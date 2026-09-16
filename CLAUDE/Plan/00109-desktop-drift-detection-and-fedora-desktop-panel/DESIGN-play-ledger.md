# The host play-run ledger — design

Phase 1's design output (Tasks 1.1, and the shape 1.2 and 1.3 must build to). Referenced from
[`DECISIONS.md`](DECISIONS.md) Decision 2; this file owns the detail.

The ledger answers one question the repo cannot answer about itself: **which plays have actually
been run on this host, and against which version of themselves.** Everything in Phase 2 is a
comparison against that answer, so a ledger that is silently wrong makes every check downstream
silently wrong. That constraint drives most of what follows.

## 1. The record

One record per **play**, not per playbook run. `run.bash` invokes `playbook-main.yml`, which
imports many plays; per-playbook granularity would record one line saying "everything ran" and
lose the axis Phase 2 needs.

| field                  | example                                             | why                                                         |
| ---------------------- | --------------------------------------------------- | ----------------------------------------------------------- |
| `play`                 | `playbooks/imports/play-gnome-shell-extensions.yml` | repo-relative; the key Phase 2 joins on                     |
| `name`                 | `Gnome Shell Extensions`                            | the play's `name:`, for human-readable reports              |
| `commit`               | `e5c9d82d…` (40 hex)                                | repo HEAD at run time                                       |
| `dirty`                | `true` \| `false`                                   | whether the tree had uncommitted changes                    |
| `play_sha256`          | `9f2c…`                                             | hash of the play file **as executed**                       |
| `outcome`              | `ok` \| `failed` \| `unreachable`                   | from the run's stats for that play's hosts                  |
| `changed`              | `3`                                                 | task-level changed count, for "did this run do anything"    |
| `started` / `finished` | `2026-09-14T09:31:07Z`                              | UTC, ISO 8601                                               |
| `schema`               | `1`                                                 | so a later reader can refuse a shape it does not understand |

### Why the hash, stated correctly

`PLAN.md` justifies `play_sha256` as *"commit alone cannot tell you whether this play changed"*.
That is not quite right, and the difference matters for what the hash is allowed to prove: given
two commits, `git log -- <play>` answers "did this play change" precisely. **The hash's real job
is the case where the commit is a lie** — a run from a dirty tree, or from a checkout that was
edited and never committed. `dirty` flags that a lie is possible; `play_sha256` says whether
*this particular play* was one of the edited files. Without it, a play hand-edited and run is
indistinguishable from the committed one, and Phase 2 would report it fresh forever.

### The limitation to state, not hide

`play_sha256` covers the play file only. A play that `import_tasks` a file under `tasks/`, or
reads `vars/`, can change materially with its own bytes untouched. Phase 2's freshness check
must therefore be driven by **git history over the play path**, with the hash as the dirty-tree
guard — not by hash comparison alone. Widening the hash to the transitive import closure is a
later refinement, and until it exists no check may claim "this play is unchanged", only "this
play file is unchanged".

## 2. Where it lives

`~/.local/state/fedora-desktop/play-ledger/runs.jsonl`, dir `0700`, file `0600`.

- **Host state, not repo state.** Under `$XDG_STATE_HOME` (falling back to `~/.local/state`), so
  it is never in the working tree, cannot be committed, and survives a re-clone — which is the
  point: a re-clone must not reset the host's memory of what has been run on it.
- **Per-user.** Plays run as the invoking user with `become`; the ledger belongs to whoever ran
  them. `0600`/`0700` because it is a record of what this machine has had done to it, which is
  nobody else's business on a shared box.
- **Matches the convention already in the repo** — Plan 00110's bridge keeps its audit log under
  `~/.local/state/`.

**Append-only JSONL, not one file per play.** History is what Task 1.3 needs and what makes
"what changed since you last ran this" answerable; a latest-only file throws it away. Volume is
a non-issue: 43 optional plays plus the main set, a few dozen bytes each, appended a handful of
times a week. "Latest run per play" is a fold over the file at read time.

Appends are `O_APPEND` writes of a single line under the platform's atomic-append guarantee, so
two concurrent runs interleave records rather than corrupting one.

## 3. The hook point, and its honest limits

`ansible.cfg` today declares **no** `callback_plugins` path and enables only
`ansible.builtin.default` (`callbacks_enabled`, `stdout_callback`). So a callback plugin is a new
mechanism here, not an extension of one.

- `v2_playbook_on_play_start` gives the play and its name, and its `_ds.ansible_pos` gives the
  **source file** — which is what makes per-play granularity possible at all, since a play
  imported by `playbook-main.yml` must be recorded against its own path, not the importer's.
- `v2_playbook_on_stats` is when the records are written.

> **CORRECTED.** This section previously said "`v2_playbook_on_stats` gives the outcome".
> It does not: stats are **per-host totals for the whole run**, so a failure in them cannot be
> attributed to any particular play — and with `playbook-main.yml` importing many plays, that is
> every run. The outcome is instead folded from the per-task `v2_runner_on_ok` /
> `on_failed` / `on_unreachable` events into whichever play is currently open, worst-wins, with
> `unreachable` outranking `failed` (a host that could not be reached did not run the play at
> all, and "failed" would claim it ran and did not work). An **ignored** failure
> (`ignore_errors`) folds as `ok`: the run continued by design, and recording it as failed would
> make every later report distrust a good run. Stats remains the write trigger, nothing more.

**A `--check` run is not recorded**, nor is `--syntax-check` or any `--list-*` run. None of them
applies anything, and a record from one would tell Phase 2 the play is fresh on a host that never
received it — the precise lie this plan exists to catch. The plugin is a pure adapter over
`helpers/play_ledger/`, because `ansible` is not importable by the interpreter that runs this
repo's tests: anything decided inside the plugin is decided untested.

**Why this is close to unbypassable, and where it is not.** Ansible loads `callbacks_enabled`
from `ansible.cfg`, and every playbook in this repo is executed through a shebang that `cd`s to
the repo root first, so `ansible-playbook <path>` run directly by a human picks the plugin up
too. That is the property Task 1.2 asks for. It is defeated by `ANSIBLE_CONFIG` pointing
elsewhere, or by an invocation from another directory with a different config on the path.
Neither is worth engineering against — but neither may be described as impossible, and Phase 2's
report must not imply the ledger is complete by construction.

### A callback cannot fail a run, so the failure has to be recorded instead

Task 1.2 requires "a ledger write failure must not silently produce a blank ledger". This is the
hard part: **Ansible catches exceptions raised inside a callback and continues**, so the obvious
implementation of fail-fast does not work — the run would go green with nothing written.

The design converts the unfailable hook into a failable check:

1. On any write error the plugin prints a loud `LEDGER-WRITE-FAILED` line to stderr, and
2. writes a sentinel at `~/.local/state/fedora-desktop/play-ledger/BROKEN` containing the error
   and the timestamp.
3. **Phase 2's checks read the sentinel first and report FAIL while it exists**, refusing to
   answer freshness questions from a ledger known to have holes. Clearing it is deliberate.

An absent record and an unwritable ledger are then distinguishable, which is the whole
requirement. A check that reported "nothing stale" from a ledger it could not write is exactly
the class of defect this plan exists to catch on the host.

## 4. Seeding, without lying or flooding (Task 1.3)

A fresh ledger knows nothing, and the naive reading of that is "all 43 optional plays are stale",
which is noise on day one and trains the reader to ignore the report.

**No backfill.** The rule is instead a reporting rule, which Task 2.1 already half-states:
**a play with no record has never been run here, and silence is the correct output for it.**
Staleness is only ever asserted about a play the ledger has actually seen. Nothing is inferred
about the past, so nothing is invented.

The one thing written at creation is a `genesis` record: schema version, creation timestamp, and
the commit at that moment. It lets a reader tell *"no record because it was never run"* from
*"no record because the ledger is younger than the run"* — without it, the ledger's own age is
unknowable and its silences are ambiguous. A report covering a period before `genesis` says so.

## 5. What Phase 2 may and may not conclude

Written here because these are properties of the ledger, and a check that overstates them is
worse than no check:

| may conclude                                                           | may not conclude                                                                         |
| ---------------------------------------------------------------------- | ---------------------------------------------------------------------------------------- |
| "this play ran here, at commit X, and the play file has since changed" | "this play is up to date" — imports and vars are outside the hash (§1)                   |
| "this play has never been run here"                                    | "this play has never been run" — another user or a bypassed invocation is invisible (§3) |
| "the ledger is untrustworthy" (sentinel present)                       | anything at all, while the sentinel is present                                           |

## 6. The freshness check (Task 2.1)

`helpers/play_ledger/freshness.py` decides, `git_history.py` supplies git's answers,
`check_freshness.py` wires them and prints. Run it with
`python3 -m helpers.play_ledger.check_freshness`.

### Four verdicts, because two would lie

| verdict       | when                                                            | why it is not folded into another                                                                                |
| ------------- | --------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------------------- |
| `FRESH`       | no commit touched it since the run, and the bytes still match   | not reported at all                                                                                              |
| `STALE`       | a commit touched it, **or** a dirty run's bytes no longer match | the actionable case                                                                                              |
| `GONE`        | ledgered, absent from HEAD                                      | outranks `STALE` however many commits touched it: "re-run this play" is wrong advice for a play that is gone     |
| `UNEXPLAINED` | clean run, no commit, different bytes                           | the ledger and the repo disagree and nothing available says which is right; guessing is how a check starts lying |

**Git history is the authority; `play_sha256` is only the dirty-tree guard** (§1). So a
commit that reverts a play to byte-identical content is still `STALE` — the play's history
moved since the run even though its content did not, and that is what a reader deciding
whether to re-run wants to know. Conversely a dirty run whose hash still matches is `FRESH`:
the tree was dirty but *this play* was not among the edited files, which is exactly the
discrimination the hash was added for.

### The two silences, both structural

Neither is a filter someone can later drop:

1. **A play the ledger has never seen is never queried.** `plays_to_query` reads the ledger,
   not the playbook tree, so the 43 never-run optional plays cannot appear — there is no code
   path that would mention them. §4's reporting rule is enforced by the shape of the data.
2. **A `BROKEN` sentinel withholds every verdict**, rather than printing them under a warning.
   A per-play answer folded from a history with an acknowledged hole is a *specific false
   statement*; the general warning it would sit beside does not undo it. `check_freshness`
   also skips the `git fetch` entirely in that state — answering nothing means doing nothing.

### Exit statuses

`0` clean, silent. `1` findings, on stdout. `2` **untrustworthy** — the sentinel, a corrupt
ledger line, a failed fetch, or a ledgered commit git cannot resolve.

`2` exists because "nothing is stale" and "I cannot tell you whether anything is stale" are
different answers, and a caller that cannot distinguish them will read the second as the
first. That is this plan's entire subject, so its own check must not commit it. In particular
an unresolvable ledgered commit — a dropped branch, a shallow clone — raises rather than
reading as "no commits touched it", which would report such a play fresh for ever.

`git_history` never merges, pulls, checks out, resets or rebases, and a test asserts those
verbs never reach the argv: this runs at the end of a login on a machine somebody is using,
and moving their working tree is a Non-Goal of this plan, not merely a rudeness.

## 7. Why neither drift check belongs in `qa-all.bash` (Task 2.3)

Both checks answer one question: **is this host what the repo says it should be?**
`qa-all.bash` runs before every commit, and at that moment nobody is asking it — the
answer cannot change what the commit should contain, and a developer cannot act on it
without stopping to run a playbook. The freshness check would also `git fetch` on every
commit, putting the network on the commit path.

Their home is Phase 3's login-time health surface, where the user is present, the answer
is actionable, and silence-when-clean is the designed behaviour.

**The "skip cleanly in CCY and CI" requirement is the argument, not a detail.** In a
container there is no ledger and none of the pinned software is installed, so both checks
would find nothing and exit 0 — two gates that **cannot fail wherever CI runs them**,
installed into the gate suite by the very plan written because a green gate suite hid a
broken host. A check that is structurally incapable of failing in the environment that
runs it is worse than no check, because it is counted.

`qa-deployed-drift.bash` looks like a counter-example and is not. Its subject is a
**deployed artefact the commit under review may have just invalidated** — edit a script in
`files/home/.local/bin/` and its deployed copy is stale *because of this commit*. That
makes pre-commit exactly its moment, and it is why it earns its host skip rather than
being defeated by it.

What does belong in `qa-all.bash` is the **tests**: `qa-helper-tests.bash` already runs
every test in `helpers/play_ledger/` and `helpers/version_pins/`, so a regression in the
logic fails QA on the machine that made it. The logic is repo state; the verdicts are host
state; only the first is a pre-commit concern.

## 8. An empty ledger is its own check (Task 4.2)

`check_freshness` asks "has any recorded play drifted since it ran". On a host whose ledger
is empty it answers "no" — there are no plays to have drifted — and publishes
`play-freshness: ok`. That verdict is byte-identical to the one a fully provisioned, fully
current host produces, and the two mean opposite things. **This plan exists because a green
tick meant nothing was wrong on an axis nothing was watching**, so the same shape reappearing
inside the mechanism built to prevent it is not a tolerable rough edge.

Reinterpreting freshness is the wrong fix, for three separate reasons: its `EXIT_OK` on an
empty ledger is correct for the question it asks, that behaviour is tested twice with its
reasoning recorded, and it has other callers who would inherit a changed contract they never
asked for. So `helpers/play_ledger/ledger_presence.py` is a check of its own, published as its
own `play-ledger` section — a new question, not a new answer to an old one.

**Emptiness is a fault, not an unknown.** `run.bash` ledgers every play, and a play is what
deploys the unit that runs the login report, so by the time anything reads the ledger at least
one record must exist. "Nothing recorded" is therefore a state that cannot be honestly arrived
at: it means the ledger was lost or was never being written, and either way every drift check
downstream is answering from an empty set while presenting as though it had looked.

Three answers, because two would collapse a distinction again:

1. **Populated** — silent.
2. **Empty** — `broken`. The finding says what the emptiness costs, not merely that it is
   empty.
3. **Unreadable** — `unchecked`. A ledger that could not be read has not been *shown* to be
   empty, and calling it empty would report a fault nobody established. That is the mirror of
   the defect this module exists for, so the distinction is carried rather than folded.

Emptiness is measured **line-wise**, not by file size: a ledger holding only newlines is as
empty as a ledger holding nothing, and a size check would call it populated.

The check is **silent while the `BROKEN` sentinel exists**. That marker already says the ledger
has a hole and must not be trusted, and §6's `check_freshness` refuses to answer while it is
there and prints the reason. Adding "and it is also empty" describes one absence twice, and two
voices on one fact read to a user as two problems.
