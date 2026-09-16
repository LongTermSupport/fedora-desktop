# Plan 00131: the `|| true` rule is blind to the enclosed form

**Status**: Not Started
**Created**: 2026-09-16
**Owner**: joseph
**Priority**: Medium

## Overview

`.semgrep/bash-conventions.yml` anchors its `|| true` pattern to end-of-line, so it
cannot see the enclosed form — `$( cmd || true )`, `cmd || true; other`,
`{ cmd || true; }`. Its own comment argued the anchoring was acceptable because the
write-time hook catches the rest. The hook does: it blocked an attempt to write a
fixture for exactly this case. What neither can catch is a file that reached disk any
other way, and that is how two instances shipped in Plan 00122's work before its review
found them by reading.

Widening the pattern to `(?m)\|\|[ \t]*(true|:)[ \t]*($|[);&|#])` makes the gate see
the enclosed form. Run against the repo that way, it finds **18 live sites in 8 files**.
`|| true` is banned outright by `CLAUDE.md`'s Fail Fast rule, so each of those is either
a real error-hiding defect or a case needing the explicit `grep_rc`-style treatment —
there is no third category.

Plan 00122 reverted the widening deliberately rather than land an 8-file change in the
same commit as an unrelated review. **Four of the sites are the git hooks that gate
secret scanning for this public repository**, and two of those are the `grep -nE` that
actually finds the secrets. A careless change there fails open, silently, on a public
repo. That is the whole reason this is its own plan: the risk, not the size.

## Goals

- The `|| true` / `|| :` rule sees the enclosed form as well as the line-anchored one.
- All 18 sites are resolved — each either fixed to consume the status explicitly, or
  annotated `# FAIL-FAST-OK: <reason>` where the suppression is genuinely correct.
- The secret-scanning hooks are changed **last**, separately, and proven still to catch
  a planted secret afterwards.
- The widened rule has a fixture that fails against the old pattern.

## Non-Goals

- Rewriting the rule file's other patterns. One rule, one gap.
- Plan 00129's subject — that is the *pass line* claiming a per-rule coverage nobody
  has. Related in spirit, not the same defect.

## Tasks

### Phase 1: The rule, with a fixture that proves the gap

- [ ] ⬜ **Task 1.1**: Add an annotated fixture for the enclosed form to
  `.semgrep/bash-conventions.bash` and confirm the CURRENT rule misses it. A gap
  asserted from a journal entry is not a gap measured today.
- [ ] ⬜ **Task 1.2**: Widen the pattern. Confirm the fixture now trips it, and that no
  *correct* line newly trips it — a rule that fires on `|| true` inside a comment or a
  string is worse than the narrow one, because it will be suppressed rather than fixed.

### Phase 2: The 16 non-hook sites

Recorded by Plan 00122 so nobody pays to rediscover them; re-measure rather than trust
the list, since the files have moved since.

- [ ] ⬜ **Task 2.1**: `files/home/.local/bin/clean-paste` (41),
  `files/usr/local/bin/watermark` (279, 296)
- [ ] ⬜ **Task 2.2**: `files/home/.local/bin/nord` (211, 382, 432, 462, 521)
- [ ] ⬜ **Task 2.3**: The rclone trio — `rclone-cache-status` (126, 138, 163),
  `rclone-cache-warm` (71, 99), `rclone-tail` (113). Mostly `grep … || true`, where
  no-match is an ANSWER and needs the `grep_rc` treatment, not suppression.
  These three also carry their own copies of the mount cmdline walk that Plan 00099's
  `rclone_rc_addr_for_mount` now owns; converting them is named there and is not this
  plan's job.
- [ ] ⬜ **Task 2.4**: Each fixed site gets its behaviour on the failing path checked,
  not just its lint status. `grep` exiting 1 on no-match and exiting 2 on a real error
  are different facts and the fix has to keep them apart.

### Phase 3: The secret-scanning hooks — separately, and last

- [ ] ⬜ **Task 3.1**: Plant a known-format test secret and confirm
  `scripts/git-hooks/pre-commit` and `commit-msg` CATCH it, before touching either.
  Establish the baseline first, or a green run afterwards proves nothing.
- [ ] ⬜ **Task 3.2**: Fix `commit-msg` (45, 93) and `pre-commit` (223, 241).
  `commit-msg:93` and `pre-commit:241` are the `grep -nE` that finds the secrets; a
  wrong status treatment there fails OPEN.
- [ ] ⬜ **Task 3.3**: Re-run Task 3.1's planted-secret test. It must still be caught,
  and a clean commit must still pass — both directions, or the test proves nothing.

## Success Criteria

- [ ] The widened rule trips on the enclosed-form fixture and the old rule does not
- [ ] `./scripts/qa-all.bash` is green with the widened rule in place
- [ ] Every one of the 18 sites is fixed or explicitly annotated `# FAIL-FAST-OK:`
- [ ] A planted secret is still caught by both git hooks after Phase 3, and a clean
  commit still passes
- [ ] `qa-reviewer` returns PASS

## Delivery & Milestones

- Carried from Plan 00122 Task 3.7, which found the gap, measured the 18 sites, reverted
  the widening deliberately, and recorded why. The evidence is in that plan's
  `JOURNAL/00122-Journal-26-09-15.md`.
