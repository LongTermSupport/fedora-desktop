# Plan 00245 Phase 3: the private-name guard (report)

Branch `feat/private-name-guard`. No term appears in this report; findings are counts and
list-line numbers (`#N`: the line of `<git dir>/private-terms`).

## The defect and the class

A sweep agent pushed about 200 files to a public branch that named a client team, its seats and
its containers. The pre-commit denylist is built only from the gitignored `localhost.yml`, absent
in the container where agents commit (the hook printed "skipping"), and that file never held team,
seat or container names. Class: install-specific identifiers reaching a commit because no detector
knows them.

## What was built

- `src-go/qa/privatenames` (+ `cmd/qa-private-names`, wrapper `scripts/qa-private-names.bash`):
  learns terms from `pingbus status` (TEAM, MEMBER and SEAT lines; each handle gives the whole
  handle, `project.seat` and the project name with its first word) and the machine's host names
  (own and `CCY_HOST_HOSTNAME`). Commands: `refresh`, `staged`, `push`, `range`, `tree`.
- The list: `<git dir>/private-terms`, mode 0600. The git directory is never tracked, so no
  `git add` (forced or not) can commit it; all work trees of the checkout share it; a clean clone
  and CI have none. Chosen over `untracked/` because that tree is a path `git add -f` can
  reach and the audit gate polices it. Each run re-learns and unions with the stored list, so a
  team that left the bus stays guarded. Terms are never printed: findings are
  `PRIVATE-NAME place:line (source #N)`, a path holding a term is printed masked, and counts per
  list line are shown. `PRIVATE_NAMES_CONTEXT=1` adds the line, masked, for triage.
- Hooks: `pre-commit` runs `staged` (added lines and added paths); the new `pre-push` runs `push`
  over `remote..local` (or `local --not --remotes` for a new branch) for every ref: messages and
  added lines of every commit. The play verifies the new hook is present and executable. The
  `pre-commit` bash allowlist record shrank (it did not grow).
- Container rule: detection is the checkout under `/workspace` (CLAUDE/ContainerRules.md). There a
  missing `pingbus`, a failing `pingbus status`, no learned names, or an unreadable or damaged list
  is exit 2. Elsewhere with nothing known it prints `SKIPPED` and passes.
- Short and generic terms (the design): a public token (the allowlist plus the repo's own name) is
  never a term; under six characters is dropped; a term whose every word is generic (`dev`, `infra`,
  `desktop`, `test` and so on) is dropped; a name's first word is indexed alone only when long and
  not generic. Matching is case-insensitive on word boundaries (a hyphen, dot or slash is a
  boundary; letters, digits and underscore are not).
- Tests through fakes (`privatenames/main_test.go`, `cmd/qa-private-names/main_test.go`), invented
  names only; coverage 90%; golangci-lint clean. The guard caught a real handle that I had put in
  a test fixture on the first commit attempt; it is replaced by an invented one.

## DBF proof

- Red: `qa-private-names.bash range <leak>^..<leak>` on the leaking commit: exit 1, 60 findings in
  24 files.
- Sweep: the tree of the default branch: 329 findings in 98 files, from 7 of the 38 terms.
- Fixed: 133 occurrences in 53 current files (plan documents, plan scripts, test fixtures, the plan
  library header, docs) replaced by `example-team`, `example-repo.N` (seat handles, shape kept so
  fixtures still parse), `example-d`, `example-c`, `example-host`. qa-changed on the fix: green
  apart from ansible-syntax (below). Plan script and library suites pass.
- After the fix: 202 findings remain.

## Left for the owner

1. **Append-only plan journals: 83 findings** (`CLAUDE/Plan/*/JOURNAL/`). The journal hook refuses
   edits, and history is the owner's. Decide: a one-off scripted rewrite, or accept.
2. **List line #34 (a product name): 132 hits, 65 of them in file names.** It is the name of a
   public MIT product this repo installs, and a play is named after it. It is public by design in
   my reading, but narrowing the rule is the owner's call: add it to
   `.claude/public-token-allowlist.yml` (`public_tokens`) or rename. Until then the whole-tree
   gate cannot be green.
3. **`helpers/pingbus/statusline.py`: 2 findings** (a fixture seat handle). The file is baked
   into the CCY image, so editing it needs a container version bump; not done for a fixture.
4. **Whole-tree gate not yet in `scripts/qa-gates.tsv`** (it would fail every qa-all); recorded
   under "Gates waiting on a sweep" in CLAUDE/QA.md, Plan Task 3.5. The hooks are blocking now.
   Adding the row is one line once 1 to 3 are decided.
5. **ansible-syntax in qa-changed fails in a work tree** (`vault-pass.secret` absent there); the
   play edit is one `stat` task and the YAML parses. Not mine to create; the full qa-all on a
   checkout with the vault will judge it.
6. History: the leaking commit and its branch still hold the names; removal is a force-push, the
   owner's.
7. The list holds other projects' names from the bus; hosts that commit outside a container get
   the list only where `pingbus` or a stored list exists (otherwise `SKIPPED`).
