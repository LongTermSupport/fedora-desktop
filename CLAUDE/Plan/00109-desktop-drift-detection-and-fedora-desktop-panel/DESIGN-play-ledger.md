# The host play-run ledger — design

Phase 1's design output (Tasks 1.1, and the shape 1.2 and 1.3 must build to). Referenced from
`PLAN.md` Decision 2; this file owns the detail.

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

- `v2_playbook_on_play_start` gives the play and its name.
- `v2_playbook_on_stats` gives the outcome. The plugin holds the started-at per play and writes
  records at stats time.

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
