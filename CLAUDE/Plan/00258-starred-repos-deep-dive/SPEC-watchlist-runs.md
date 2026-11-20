# SPEC: the watchlist command, extended to the star set and study runs

Extends `src-go/cmd/watchlist` and `src-go/internal/watchlist` (Plan 00200's
`SPEC-watchlist.md`). It supersedes that spec's list format, `sync` and `mark`. The process
it serves is in `CLAUDE/Watchlist.md`; this spec covers only the command.

## Paths (relative to `--root`)

| What                                                         | Path                                                                              |
| ------------------------------------------------------------ | --------------------------------------------------------------------------------- |
| The list                                                     | `CLAUDE/Watchlist.tsv`                                                            |
| A run                                                        | `CLAUDE/WatchlistRuns/<run-id>/manifest.tsv`, with reports `<group>.md` beside it |
| Clone of a repository in state `study`, `new` or `unstarred` | `untracked/starred-repos/<licence>/<owner>/<repo>`                                |
| Clone of a repository in state `excluded`                    | `untracked/starred-repos-excluded/<licence>/<owner>/<repo>`                       |

The old mirrors in `untracked/watchlist/<owner>__<repo>` are no longer read or written.

## The list: `CLAUDE/Watchlist.tsv`

The command writes a fixed comment header that documents the columns, then one row per
repository, sorted by `repo`. It reads strictly: a wrong column count, an unknown enum value,
a duplicate repo or an inconsistent `reuse` is an error naming the line.

| #   | Column         | Values                                                                                                |
| --- | -------------- | ----------------------------------------------------------------------------------------------------- |
| E1  | `repo`         | `<owner>/<repo>` on github.com                                                                        |
| E2  | `source`       | `star` (from the owner's stars) or `added` (by `add`)                                                 |
| E3  | `licence`      | SPDX id, or `LicenseRef-<name>`, or `NONE`, or `NOASSERTION` (not yet verified)                       |
| E4  | `reuse`        | `lift`, `whole-file` or `clean-room`, derived from `licence` (E5)                                     |
| E5  |                | `lift`: MIT, Apache-2.0, ISC, 0BSD, any `BSD-*`. `whole-file`: MPL-2.0. Everything else: `clean-room` |
| E6  | `state`        | `new`, `study`, `excluded` or `unstarred`                                                             |
| E7  | `group`        | lower-case kebab name; `-` only when `state` is not `study`                                           |
| E8  | `licence_file` | name of the licence file at the clone's root, or `-`                                                  |
| E9  | `licence_sha`  | first 16 hex of the sha256 of that file, or `-`                                                       |

The licence file is the first root entry, sorted by name, matching `^(licen[cs]e|copying)`
case-insensitively.

## Verbs

### `sync [<repo>...]`

| #   | Behaviour                                                                                                                                                                                                                                                                                                                                    |
| --- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| S1  | With no repo named, it reads the stars: `gh api user/starred --paginate`, each star's `full_name` and `license.spdx_id`. A `gh` failure stops the command.                                                                                                                                                                                   |
| S2  | A star not in the list is added with `source=star`, `state=new`, `licence` set to GitHub's id (or `NONE`), and `group=-`, then cloned.                                                                                                                                                                                                       |
| S3  | A `source=star` row whose repository is no longer starred becomes `state=unstarred`. Its clone is kept. An `unstarred` repository that is starred again becomes `new`.                                                                                                                                                                       |
| S4  | A row whose clone is missing is cloned in full: `git clone`, never shallow.                                                                                                                                                                                                                                                                  |
| S5  | Every clone, or only the named ones, is fetched (`git fetch --tags --prune origin`) and fast-forwarded to `origin/HEAD`. A clone that cannot fast-forward (rewritten history or local changes) stops the command, naming the repository and the remedy (re-clone it).                                                                        |
| S6  | `licence_file` and `licence_sha` are recomputed. A change against the row is reported as `licence-changed`, and the row keeps its `licence` until `set` changes it.                                                                                                                                                                          |
| S7  | Output, on stdout, one line per event: `new <repo>`, `unstarred <repo>`, `restarred <repo>`, `cloned <repo>`, `licence-changed <repo> <file>`, `moved <repo> <last-studied-sha>..<head>` (for a `study` row whose head differs from its newest `done` study, R4), `unstudied <repo>` (a `study` row never studied). Progress goes to stderr. |
| S8  | The list is written once at the end, only if anything changed.                                                                                                                                                                                                                                                                               |

### `add <url>`

| #   | Behaviour                                                                                                                 |
| --- | ------------------------------------------------------------------------------------------------------------------------- |
| A1  | Only `https://github.com/<owner>/<repo>`. It refuses duplicates.                                                          |
| A2  | It adds the row with `source=added` and `state=new`, licence from `gh api repos/<owner>/<repo>`, then clones it as in S4. |

### `set <repo> [--licence <id>] [--state <state>] [--group <group>]`

| #   | Behaviour                                                                                                                                                          |
| --- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| T1  | Updates the named fields. `reuse` is re-derived. `--state study` requires a group, given now or already set. `--state excluded` sets `group=-`.                    |
| T2  | If the clone's directory changes (a new licence, or a move into or out of `excluded`), the clone is moved there. The command refuses if the target already exists. |
| T3  | `licence_file` and `licence_sha` are recomputed from the clone.                                                                                                    |

### `run new <run-id> --mode baseline|diff [--group <g>]... [--repo <r>]...`

| #   | Behaviour                                                                                                                                                                        |
| --- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| R1  | `<run-id>` must be `<mode>-<yyyymmdd>-<hhmm>`, and its mode must match `--mode`. Runs are ordered by that timestamp. An existing run directory is refused.                       |
| R2  | It refuses while any row is `new`: triage first.                                                                                                                                 |
| R3  | It takes the `study` rows, filtered by `--group` and `--repo` when given. For each row, `to_sha` is the clone's `HEAD`.                                                          |
| R4  | `from_sha` is the `to_sha` of the repository's newest `done` row in any earlier run, or `-`.                                                                                     |
| R5  | In `baseline` mode every row is `baseline`. In `diff` mode a row with no `from_sha` becomes `baseline`; a row whose `from_sha` equals `to_sha` is left out; the rest are `diff`. |
| R6  | It writes `manifest.tsv`: a fixed header, then `repo, group, mode, from_sha, to_sha, status=pending`. It prints the run's groups and their counts.                               |

### `run rows <run-id> [<group>]`

| #   | Behaviour                                                                                                                                                                                   |
| --- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| W1  | Prints the run's rows, joined with the list: `repo`, `licence`, `reuse`, `mode`, `from_sha`, `to_sha`, `status`, and the clone path. This is the `{{ROWS}}` the study brief is filled with. |

### `run mark <run-id> <group>`

| #   | Behaviour                                                                                                                                                                                                                                          |
| --- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| M1  | Reads `CLAUDE/WatchlistRuns/<run-id>/<group>.md`. For every `pending` row of the group, the report must contain a heading `## <repo>` and, after it and before the next `## `, a line beginning `Studied: ` that contains the row's full `to_sha`. |
| M2  | If every row passes, they become `done`. Otherwise nothing changes, and the command lists each failing repository and the reason.                                                                                                                  |

### `run status [<run-id>]`

| #   | Behaviour                                                                                                                                                           |
| --- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| U1  | Without a run id it uses the newest run. It prints each group with its `pending` and `done` counts, then the pending repositories. The exit status is 0 either way. |

## General

| #   | Behaviour                                                                                                   |
| --- | ----------------------------------------------------------------------------------------------------------- |
| G1  | Fail fast: the first failure stops the command, with a message naming the repository and the step.          |
| G2  | Every git and gh call goes through `execx`, and tests use the fake runner, as in the current `app_test.go`. |
| G3  | The `mark` verb is removed, and so is the old `sync` mirror layout.                                         |
