# watchlist: specification

Plan 00200. The `watchlist` command keeps the state of the repository watch list
([CLAUDE/Watchlist.md](../../Watchlist.md)) and mirrors the watched repositories locally so an
agent can review what changed. It judges nothing: reviewing is the `watchlist-scout` agent's job.
Built to this spec, test first, in the manner of `SPEC-qa-ansible.md` (Plan 00192): each
expectation has a Go test named for its number, written and seen to fail before the code.
Process calls go through `execx` (git through `gitx`), faked in the tests.

## Command line and exit codes

- W1. `watchlist [--root DIR] <command> [args]`, commands `sync [repo]`, `mark <repo> <sha> --run ID`,
  `add <url>`. `--root` is the fedora-desktop checkout (default: the working directory); the list is
  `<root>/CLAUDE/Watchlist.tsv` and the mirrors live in `<root>/untracked/watchlist/`.
  `-h`/`--help` prints the usage to stdout, exit 0.
- W2. No command, an unknown command, an unknown flag, a missing argument or a missing
  `--run`: usage on stderr, exit 64. A value-less `--root` is exit 64.
- W3. Exit 0 success, 1 any failure. Every failure message is on stderr and starts `ERROR:`.
  stdout carries only the payload (sync's report, the one-line acknowledgement of add and mark).

## The list file

- W4. The file is strict. Blank lines and lines starting `#` are ignored; every other line has
  exactly four tab-separated fields: `url`, `branch`, `last_sha`, `run`. A line that does not is an
  error naming the file and line number. A missing file is an error (it is tracked; absence is a
  fault, never an empty list).
- W5. `url` is `https://github.com/<owner>/<repo>` (the host is pinned: the mirror directory and
  the names drop the host, so two hosts would collide) with no userinfo, port, query, fragment,
  `.git` suffix or trailing slash; `http://`, `ssh://`, `git@`, other hosts and local paths are
  errors.
- W6. `branch` is non-empty and holds no whitespace.
- W7. `last_sha` is 40 lowercase hex digits, or `-` for "never reviewed". `run` is a token with no
  whitespace naming the run that recorded the sha, or `-`; it is `-` exactly when `last_sha` is.
- W8. Two rows naming the same `owner/repo` (compared case-insensitively) are an error.
- W9. Writes replace the whole file atomically, keep the comment header and keep the rows in order.

## add

- W10. `add <url>` validates the URL (W5) and refuses a duplicate (W8) before it runs git.
- W11. It asks the remote for its default branch (`git ls-remote --symref <url> HEAD`). A
  failure, or an answer that names no branch, is an error and the file is untouched.
- W12. On success it appends `url<TAB>branch<TAB>-<TAB>-`, prints
  `added <owner>/<repo> (<branch>): initial full review pending`, exit 0. It does not clone.

## sync

- W13. `sync` handles every row in file order; `sync <repo>` only the named one, where `<repo>`
  is `owner/repo` or the full URL, case-insensitive. A name not on the list is an error. An empty
  list is an error (`the watch list is empty`).
- W14. The mirror of a row is `<root>/untracked/watchlist/<owner>__<repo>`. When the directory is
  absent it is cloned: `git clone --branch <branch> --single-branch <url> <dir>`. When present
  it is fetched: `git fetch origin +refs/heads/<branch>:refs/remotes/origin/<branch>`. Clone
  progress and git's own chatter are not passed to stdout. A directory that exists without a
  `.git` inside (a killed clone) is an error telling the user to delete it; a failed fetch adds
  that a renamed default branch is fixed in the branch column.
- W15. The new head is the commit `refs/remotes/origin/<branch>` names in the mirror.
- W16. A row with no `last_sha` prints, under its heading, `initial: full review`, `head: <sha>`
  and `mirror: <dir>`. It lists no commits or paths.
- W17. A row whose `last_sha` equals the head prints `last-checked: <sha> (run <run>)`,
  `head: <sha>`, `mirror: <dir>` and `unchanged`.
- W18. Otherwise it prints `last-checked`, `head`, `mirror`, then `commits (N):` with one `  <short sha> <subject>`
  line per commit of `last_sha..head` and `changed paths (M):` with one `  <path>` line per path
  of `git diff --name-only last_sha head`.
- W19. A `last_sha` that is not an ancestor of the head (`git merge-base --is-ancestor`) is an
  error naming the repository and the sha: the remote rewrote its history. This includes a
  `last_sha` the mirror no longer holds, and one it still holds, because git keeps commits a
  rewrite removed until a gc. No range is listed and the command never falls back to a full
  review on its own. *(First draft tested only whether the mirror held the object; a real
  force-push showed that is true for weeks after the rewrite.)*
- W20. Each repository's report starts with a heading line `== <owner>/<repo> (<branch>)`.
- W21. Any git failure (clone, fetch, rev-parse, log, diff) aborts the run: exit 1, `ERROR:` naming
  the repository and git's message, no later repository attempted. A failed fetch is never a skip.
  `sync` never writes the list file.

## mark

- W22. `mark <repo> <sha> --run ID` records a review: `<repo>` as in W13; `sha` is 40 lowercase hex
  (W7); `ID` a token (W7).
- W23. The mirror must exist and hold the commit; either missing is an error and the file is
  untouched. (Marking a commit nobody fetched would record a review of nothing.) The commit must
  also be an ancestor of (or equal to) `refs/remotes/origin/<branch>`, and, when the row has a
  `last_sha`, descend from it: a review never moves backwards and never lands off the branch.
  Each failure is an error naming the reason and leaves the file untouched.
- W24. On success the row's `last_sha` and `run` are replaced, the file is written atomically (W9),
  and `marked <owner>/<repo> at <sha> (run <ID>)` is printed.

## The shipped list

- W25. The tracked `CLAUDE/Watchlist.tsv` loads under W4 to W8 (a test reads the real file).
