# QA Review — commit `9fce0498` (branch banners on retired Fedora branches)

**Verdict**: BLOCK

Reviewed: `scripts/mark-branches-superseded.bash` (new, 250 lines) and
`docs/development.md` (+36/-1), on branch F44 in `<repo>`.

---

## Blocking

### 1. F43's CI is red because of this change, and every future release will do the same to another branch

`<repo>/scripts/mark-branches-superseded.bash:193`

`.github/workflows/qa.yml` on F43 triggers on `push: branches: ["**"]`. The
stamping push woke it up:

```
gh run list --branch F43
{"conclusion":"failure","createdAt":"2026-09-11T18:03:36Z","headSha":"4ce03072bb86338ea5de9f42f088bfd1c0c78122",...}
{"conclusion":"success","createdAt":"2026-06-15T20:36:32Z","headSha":"327d3d5e...",...}
```

`4ce0307` is `origin/F43`'s tip — the stamping commit. Failure cause
(`gh run view 34631150482 --log-failed`):

```
X python: ruff: 17 issues
X QA FAILED: 0 errors in 287 files
```

Not the banner — F43's June-vintage Python under a newer unpinned ruff. **That is
the point: a retired branch's CI is not expected to still pass, and this script
is a machine for re-triggering it.** The script printed `F43 stamped` and exited 0
while turning a green branch red, permanently: there is no path back to green that
does not mean maintaining a branch the banner declares unmaintained. F42 escaped
only because it has no `.github/` at all (`gh run list --branch F42` returns `[]`)
— that is luck, not design, and F44 will not be so lucky when F45 lands.

**Fix**: append `[skip ci]` to the commit subject built at line 189, or scope the
workflow trigger away from retired branches. One line either way.

### 2. A README with an unterminated `SUPERSEDED-BANNER:START` is silently deleted in full, committed and pushed

`<repo>/scripts/mark-branches-superseded.bash:141-147`

The awk sets `skipping = 1` on START and only clears it on END. With no END line,
nothing else is ever printed. Demonstrated:

```
### CASE 2: START present, END MISSING (interrupted/corrupt)
<<<END OF CASE 2 OUTPUT>>>          <- awk produced ZERO lines
```

`stamp_readme` then writes banner-plus-nothing, `git diff --quiet` sees a change,
and lines 187-194 commit and push it to a public branch with no further check. A
half-banner is reachable from a hand edit, a bad conflict resolution, or any
future change to the marker pair.

`CLAUDE/AgentNotes.md` -> "Bash conventions the gates only partly enforce" already
names the sibling of this: *"an `awk` that matches nothing exits 0 and emits a
perfect copy"* — **verify the rewrite took**. The inverse guard is what is
missing. Refuse to write when the stripped body is empty, or when the marker
occurrences are unbalanced, and `die`.

Same code, second case — a README that legitimately contains the markers loses the
content between them. Probed with a README documenting the markers inside a fenced
block: the fence survived, everything between the two markers vanished, and the
surrounding prose was left looking intact. Lower likelihood, same one-line guard
fixes both.

---

## Should fix

### 3. "No retired branches found" is indistinguishable from a broken selector

`scripts/mark-branches-superseded.bash:225`

```bash
mapfile -t targets < <(retired_branches "$active")
```

`retired_branches` is a `git for-each-ref | grep | grep | sort` pipeline under
`pipefail`. Process substitution throws its status away. Probed:

```
D: after mapfile, $?=0 and count=0  (pipeline actually failed)
D: direct call rc=1 -- pipeline DOES fail
```

So if `for-each-ref` errors, or the `refname:strip=3` shape ever changes, the
script prints *"No retired F<VERSION> branches found — nothing to stamp."* and
exits 0. `docs/development.md:83` says "do not skip" about a step whose no-op is
dressed as success. This is `AgentNotes.md` -> "A discarded failure signal becomes
data", face 3. Run the selector into a variable in a condition, and state coverage
as a number (`N of M origin branches are retired`).

### 4. `cleanup` warns and continues on the success path

`scripts/mark-branches-superseded.bash:100-109`, called at `:172`, `:180`, `:196`

The comment justifies only the trap context ("the original error is already on its
way out and must not be masked"), which is correct — but the same function is also
called three times mid-loop in the *happy* path. There, a failed
`git worktree remove` is a plain failure that gets a `log "WARNING:"` and the run
continues to exit 0 with a leaked worktree registration. That is the "skip and
warn" pattern CLAUDE.md prohibits outright, and no gate sees it. Split it: a
`remove_worktree` that `die`s for the loop, and a trap handler that warns.

### 5. `--current` is never validated against origin, and a typo defaces the live default branch

`scripts/mark-branches-superseded.bash:203-207`

`--current F54` (finger-slip for `F45`) makes F44 "retired". `retired_branches`
returns it, and a `NO LONGER ACTIVE BRANCH — CURRENT BRANCH IS F54` banner telling
readers to check out `F54` is committed and pushed to the repository's default
branch. Nothing checks that `origin/$active` exists. Assert
`git rev-parse --verify "refs/remotes/origin/$active"` after
`resolve_active_branch`.

### 6. `--dry-run` contradicts the script's own documented exit contract, and nothing can detect a stale banner

`scripts/mark-branches-superseded.bash:25-26` vs `:175-181`;
`docs/development.md:151-152`

Header: *"Exit status: 0 = every retired branch is correctly stamped"*. A
`--dry-run` that reports `F42 would-stamp` exits 0 anyway. And
`docs/development.md:151-152` asserts an invariant — *"Everything other than
Active carries the superseded banner… A retired branch without one is a bug"* —
that has no detector: no `--check` mode, no gate, no CI. This is the review
dimension the repo keeps losing to (`AgentNotes.md` -> "A gate whose only visible
output is a failure"). A `--check` that exits 1 on any `would-stamp` would make the
invariant real and could run in CI on F44.

### 7. A failed `worktree add` leaks the mktemp dir and prints advice that also fails

`scripts/mark-branches-superseded.bash:163-165`, `:100-109`

`mktemp -d` creates the dir; if `worktree add` fails, `cleanup` runs
`git worktree remove` (with the force flag) on a directory git does not know:

```
fatal: '/tmp/superseded-0ZRwj4' is not a working tree
E: exit=128
E: dir still present? YES-LEAKED
```

The warning then tells the user to run the same failing command by hand. Track
whether the worktree was actually registered, and remove the bare temp dir
otherwise.

### 8. `docs/development.md:116-118` is inaccurate about local refs

> "Branches are read from and written straight back to `origin`, and your local
> refs are left alone — `git fetch` afterwards to pick the commits up."

The script itself runs `git fetch --quiet origin` at `:222`, and a successful push
updates `refs/remotes/origin/<branch>`. What is untouched is `refs/heads/*`. Say
that instead; the "`git fetch` afterwards" advice is not needed.

---

## Nits

### 9. Correctness depends on `fetch.prune`, which is a user-global setting

`scripts/mark-branches-superseded.bash:222`

Without pruning, a branch a maintainer deliberately deleted on GitHub survives as
a stale `refs/remotes/origin/F41`, and the push at `:193` **re-creates it on
origin**. Probed: a plain fetch keeps the stale ref when the deletion was made by a
third party. This repo's own IaC saves it —
`playbooks/imports/play-git-configure-and-tools.yml:25` sets
`git config --global fetch.prune true`, and `<home>/.gitconfig` confirms it live —
but a fresh clone elsewhere has no such guarantee. Use `git fetch --prune`.

### 10. Nothing documents that this requires push-bypass on the PR-protected `F*` branches

For a non-owner the push at `:193` is rejected and the script `die`s — correct
fail-fast, but the doc reads as though any contributor can run step 5.

### 11. `retired_branches` regex: clean

`feature/f44-migration` cannot match (the `F[0-9]` glob is case-sensitive and
requires a digit second), `F44-hotfix` and `F44/sub` are excluded by the anchored
`^F[0-9]+$`, `origin/HEAD` never matches the glob. Live output is exactly
`F42 F43 F44`, minus the active one. No finding.

---

## Your design question — hardcoding `F44` vs linking generically

Re-stamping every branch every release is a defensible answer, but it is solving a
problem the banner mostly does not have. Look at what you actually published:

```
> ### [Go to F44, the current branch](https://github.com/LongTermSupport/fedora-desktop)
```

The **URL is already generic** — the repo root always resolves to whatever
GitHub's default branch is, so the link never goes stale and never needs
re-stamping. Only two strings do: the heading text `CURRENT BRANCH IS F44`, and
the checkout line naming `F44`.

That matters because the failure mode of a skipped re-stamp is worse than a
generic banner would ever be: a banner that *names* F44 as current when F45 is
current is confidently wrong, and per `AgentNotes.md` a confident wrong answer is
strictly worse than no answer. You have made the correctness of every historical
branch depend on a manual step being performed forever, with finding 6 meaning
nothing detects the omission.

The materially better option is to make the banner say what is permanently true
about **this** branch and defer the rest to the link:

> This branch targets **Fedora 42** and is no longer maintained. The current
> branch is always the repository default — go there
> (https://github.com/LongTermSupport/fedora-desktop), then check out the branch
> it shows you.

Stamp-once-at-retirement, never stale, and finding 1 stops recurring because you
push to each retired branch exactly once in its life. Keep the re-stamp loop if
you want — it becomes a harmless idempotent refresh rather than a correctness
dependency. If you prefer naming the branch (and the louder banner is a real
benefit), then finding 6's `--check` mode is not optional: something has to be able
to tell you the step was missed.

---

## Checked and clean

- **Worktree/push flow**: non-fast-forward is correctly rejected, nothing is ever
  forced. Probed against a scratch origin —
  `! [rejected] HEAD -> F42 (non-fast-forward)`, `push exit status = 1` -> `die`.
  A race between `:222`'s fetch and `:193`'s push cannot silently clobber.
- **`worktree add` into an existing `mktemp -d`**: accepted by git 2.39.5 (probed).
- **EXIT trap across the loop**: `WORKTREE_DIR` is cleared inside `cleanup` before
  the removal, so a re-entry is a no-op; the in-loop calls leave nothing for the
  trap. The trap **does** fire on SIGINT in non-interactive bash (probed:
  `EXIT TRAP RAN` after `kill -INT`), so Ctrl-C does not strand a worktree.
- **Stderr hygiene**: held. `log`/`die` -> stderr; the only stdout is
  `<branch> <status>` and `usage()`, which `CLAUDE/StderrHygiene.md:114` names as
  the legitimate exception ("help/status/report commands whose entire job is to
  print for a human"). The dry-run banner preview correctly goes to stderr via
  `log`.
- **Idempotency**: stable. The leading-whitespace strip at `:148` is correct bash
  (`${body%%[![:space:]]*}` yields the leading run; `${body#…}` removes it) and
  converges after one run — confirmed by the single blank line between banner and
  `# Fedora Desktop` in both published READMEs. Trailing-newline normalisation
  converges too. No loop, no double-stamp, no thrash.
- **Published result**: `origin/F42:README.md` and `origin/F43:README.md` both
  carry a correct, correctly-versioned banner at lines 1-16 with the body intact
  from line 18 (208 and 248 lines respectively).
- **Script conventions**: `#!/usr/bin/env bash` (18 of 25 `scripts/*.bash` use it),
  `set -euo pipefail`, tracked mode `100755`, `local` everywhere, all expansions
  quoted, no in-place stream editing, no `|| true`, no `2>/dev/null` on an error
  path. Escaped backticks in the `banner_for` heredoc did **not** break semgrep's
  bash parser — the file is absent from `qa-patterns.bash`'s partial-parse list, so
  the `AgentNotes.md` backtick trap was dodged.
- **Naming**: `mark-branches-superseded.bash` says what it does. Banner marker
  names are explicit.
- **Public-repo safety**: no personal identifiers; `REPO_URL` is the public
  org/repo, which `AgentNotes.md` names as intentionally public.
- **Placement**: `scripts/` is right — this is a maintainer tool, not a deployed
  artifact and not plan-local. Not a playbook concern, so no IaC-graph question
  arises. No CCY version bump applies (nothing under
  `files/var/local/claude-yolo/`).
- **Plan Commit Rule**: N/A — not plan work; `git status --short` is empty, no
  untracked `CLAUDE/Plan/` dirs.
- **Doc index**: `docs/README.md` indexes documents, not scripts, and
  `CLAUDE/QA.md` lists QA gates only — no index row is owed.
  `docs/development.md` step renumbering (5 to 6) is consistent, and the later
  "Step 4 above" reference at line 122 is still correct.

---

## Mechanical gates

- **`qa-all.bash`: FAILS — but not on this diff.**
  `X python: ruff version mismatch — expected 0.16.4 (.ruff-version), found 0.16.0`,
  then `ERROR: Missing required tools.` The suite aborts before finishing. Per
  CLAUDE.md this is an **IaC gap to report, not to work around**:
  `playbooks/imports/play-python.yml:20` installs `ruff` unpinned, so the CCY image
  carries 0.16.0 against a 0.16.4 pin last moved in `d45da38`. Consequence for this
  review: **the mandatory pre-commit gate cannot have passed green for
  `9fce0498`.** Fix in IaC — pin ruff to `.ruff-version` in `play-python.yml` and
  rebuild the ccy image.
- **Gates that do cover this diff, run individually**: `qa-bash.bash` ->
  `bash: 204 files OK` (`shellcheck --severity=style` on the new script alone:
  clean; the 147 repo-wide issues are a pre-existing warning, none in this file).
  `qa-patterns.bash` -> `patterns: 204 files OK`. `qa-docs.bash` ->
  `docs: 64 files OK (links, anchors, playbook catalogue, topic index)`.
- **`plan-qa --sweep`**: `2 findings (0 block, 2 advise)` — staleness and
  journal-freshness on unrelated plans, pre-existing.
- **`ansible-playbook --syntax-check`**: **not triggered** — the diff contains no
  playbook, task, vars or `*.yml` file.
- **`qa-helper-tests.bash`**: **not triggered** — no `helpers/` or
  `tests/helpers/` change.
- **`check_extension_compat` / ESLint**: **not triggered** — no `extensions/`
  change.

---

## Key paths

- `<repo>/scripts/mark-branches-superseded.bash`
- `<repo>/docs/development.md`
- `<repo>/playbooks/imports/play-python.yml`
- `<repo>/playbooks/imports/play-git-configure-and-tools.yml`
