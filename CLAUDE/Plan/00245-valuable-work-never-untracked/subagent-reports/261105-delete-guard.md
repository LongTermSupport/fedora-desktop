# The bulk-delete guard (issue #150)

Branch `feat/branch-delete-guard`, not merged into F44.

## What it is

`scripts/branch-delete-list.bash [--root DIR] [--scripts DIR] [FILE]` builds and runs
`src-go/qa/cmd/branch-delete-list` over `src-go/qa/branchdelete`.

- Input: branch names, one per line, from FILE (relative to the working directory), or from
  stdin when FILE is absent or `-`. Surrounding space is trimmed and blank lines are skipped.
  Nothing else is dropped. A `#` line is not a comment: it is refused as an unsafe name.
- Origin is asked once: `git ls-remote --symref origin`. The HEAD symref line gives the default
  branch, `refs/heads/` the branches and `refs/archive/` the archives.
- The policy is read by `branchhygiene.ReadPolicy`, newly exported from the hygiene package.
  `qa-branch-hygiene` now reads it through the same function, so the two cannot disagree.
- A candidate is refused, with every reason that applies, when it:
  - is exempt in the policy;
  - is parked in the policy (the reason and date are shown);
  - matches `^F[0-9]` (F42, F44-arm, F45 even when unlisted; `Fix-x` is not a release line);
  - is origin's default branch;
  - is listed twice;
  - has an `origin/` or `refs/` prefix, or is not a plain name (`[A-Za-z0-9][A-Za-z0-9._/-]*`,
    so nothing printed can be read by a shell as more than a name);
  - is not a branch on origin;
  - has no `refs/archive/<name>`, or one at a different commit from the tip. The archive push
    command is given with the exact sha.
- A protected name (exempt, parked, release line, default) is not given the archive hint,
  because archiving would not let it through.
- One refusal refuses the list. The offenders go to stderr as `REFUSED <name>: <reasons>`, with
  a count line. Stdout stays empty, so no partial list exists. When every name passes, stdout
  holds one `git push origin --delete <name>` per name, in input order. Nothing is deleted.
- Exit codes:
  - 0: every name passes.
  - 1: refused.
  - 2: could not judge. That is bad usage (an unknown flag, two FILEs, `--root` with no value),
    an empty list, an unreadable list, a missing or invalid policy, an unresolvable root, git
    failing or failing to start or timing out (2 min), ls-remote output that cannot be read or
    has no HEAD symref, or a stdout that cannot be written.

## Test evidence

- Tests came first and were run red: before the source existed they failed to compile
  (undefined symbols). Two later expectations were also seen to fail, then pass: no archive
  hint for a protected branch, and "1 candidate passes".
- `go test -race -cover`:
  - `branchdelete`: 99.3%.
  - `branchhygiene`: 97.5%, including the new `policy_test.go`.
  - `cmd/branch-delete-list`: 55.6%, the same shape and figure as `cmd/qa-branch-hygiene`. Its
    tests cover the closed stdout, dispatch, and a real git repository with no origin (exit 2).
- `golangci-lint run` on the touched packages: 0 issues.
- Git is faked in the unit tests (`proc.Runner`), with no network.
- Dogfood against the real origin (read-only `ls-remote`):
  - The incident list `F42, F43, worker/433-unowned-decisions, feat/release-version, origin/skip-docker-env, F44-arm` exited 1. It refused F42, F43 and F44-arm as exempt and
    release lines, `feat/release-version` as unarchived (archive command given), and the
    `origin/`-prefixed name. It printed no list.
  - `worker/433-unowned-decisions` alone exited 0. It printed one delete line, because it is
    archived at its tip.
  - The first dogfood run found two faults, both fixed by tests first. It told F44-arm to
    archive its tip, and it printed "1 candidates".

## Docs

- `CLAUDE/QA.md`, "Branch hygiene": the manual three-step procedure is replaced. This command is
  now the only way to build a bulk delete list.
- `src-go/CLAUDE.md`: the package and command are added beside `branchhygiene`.

## Left open

- The command is not wired into anything: it is the owner's tool, like `qa-branch-hygiene`, and
  not a gate.
- An owner `!` command can still delete branches without using it. The forge's ruleset bypass
  is unchanged.
- The full Go gate (`qa-go-qa`) runs only in `qa-changed`. See the reply for its result.
