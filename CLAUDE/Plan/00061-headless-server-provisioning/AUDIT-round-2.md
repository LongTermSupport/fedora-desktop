# Audit — Round 2 (Fable, adversarial auditor)

**Scope**: only the deltas listed in `PROPOSAL.md`'s "Revision log (round 2)".
Everything else was verified in round 1 and is not re-litigated here.

**Method**: same as round 1 — verify against real files, execute where
possible rather than reason abstractly. For the two highest-risk deltas
(the fixed Check 4 loop, the container-watch diff) I ran the actual code
against the actual repo files, not just fixtures.

---

## 1. BLOCKER 1.1 fix — **verified fixed, empirically**

Built the complete, verbatim Check 4 loop body from round-2 §4 (multi-play
guard, both `|| true` guards, the comment/CRLF-stripping awk, the full
if/elif chain) into a standalone harness and ran it under `set -euo pipefail` against six constructed fixtures covering every branch, **plus two
real files from this repo** (`playbooks/imports/optional/experimental/play-virtualbox-windows.yml`
and `playbooks/imports/play-podman.yml`, both currently untagged/unmodified):

```
  ERROR (scope): .../play-untagged.yml - no play-level scope tag
  OK: .../play-tagged-general.yml
  ERROR (scope): .../play-multi-scope.yml - multiple play-level scope tags declared
  ERROR (scope): .../play-invalid-scope.yml - invalid scope tag(s): scope-desktop
  OK: .../play-commented-tag.yml
  OK: .../play-mixed-tags.yml
  ERROR (scope): playbooks/imports/optional/experimental/play-virtualbox-windows.yml - multi-play file: 2 plays
  ERROR (scope): playbooks/imports/play-podman.yml - no play-level scope tag
---
TOTAL ERRORS: 5
SCRIPT COMPLETED WITHOUT ABORT
$ echo $?
0
```

This confirms, on the exact code (not a paraphrase):

- **No crash on zero valid tags** (`play-untagged.yml`, and the real,
  currently-untagged `play-podman.yml`) — the case that killed the script in
  round 1 now reports cleanly and the loop continues to the next file. This
  is the blocker; it's fixed.
- **Correct branch for every case**: exactly-one → OK, zero+zero → missing,
  zero+some-invalid → invalid (with the right offending value named),
  two-or-more → multiple. All four branches exercised, all four correct.
- **The `bad=$(... | grep -vxE ... | paste ...)` line is genuinely safe on
  its branch, confirmed empirically, not just by re-reading the guard.**
  `play-invalid-scope.yml` hit that exact branch and printed `invalid scope tag(s): scope-desktop` with no abort. Sonnet's reasoning (the branch only
  runs when `scope_like_count -gt 0` is already established, so `grep -vxE`
  is guaranteed at least one line to invert-match) checks out in practice,
  not just on paper.

No finding. This is the one item that had to work, and it does.

---

## 2. SHOULD-FIX 5.1 fix (multi-play guard) — **verified fixed, fires on the real file, doesn't false-trigger**

Same harness run (above) is also the evidence here:

- **Fires correctly on the real `play-virtualbox-windows.yml`** — genuinely
  2 `- hosts:` blocks in this file (re-confirmed: line 3 "Install
  Virtualbox", line 44 "Setup Windows VMs"), and the guard reports "multi-play
  file: 2 plays" and `continue`s without ever reaching (or being confused by)
  the awk tag-extraction step.
- **Does not false-trigger on a normal file** — `play-podman.yml` (1 play)
  and all six single-play fixtures fall through the guard (`hosts_block_count -eq 1`) exactly as before, reaching the normal tag-parsing logic unchanged.

**Any way a 2-play file could still slip through (the audit brief's
question)?** I don't find one. The guard's `grep -cE` uses the *identical*
regex to the `-q` check that gates loop entry, so undercounting relative to
that check isn't possible for a file that got this far. The only way to
evade it would be a second `- hosts:` line that doesn't match
`^[[:space:]]*-[[:space:]]+hosts:` — i.e., non-standard formatting this
repo's own style guide (`CLAUDE/AnsibleStyle.md`) doesn't permit and no real
playbook in the repo uses (I re-confirmed in round 1 that
`play-virtualbox-windows.yml` is the *only* multi-play file in the whole
`playbooks/` tree). The theoretical opposite risk — the regex over-counting
due to a `content: |`/`block: |` scalar that happens to contain literal text
matching the pattern — is a **false positive**, not a slip-through, and it's
inherited from Check 2's identical, already-accepted regex, not a new
exposure this proposal introduces.

Also verified the checklist's specific split instructions (§7 step 4) against
the real file: it claims the first play ("Install Virtualbox") spans
"lines 1–43... plus its `handlers:` block." Confirmed: the file has a
`handlers:` block at line 40 (inside the first play, which the second play's
`- hosts:` interrupts at line 44) — so "plus its handlers: block" is a
slightly redundant phrase (the handlers block is already inside "lines
1–43," not something additional), but not wrong. Not filing as a finding —
cosmetic wording only, the line boundaries themselves are accurate.

No finding.

---

## 3. SHOULD-FIX 1.2 fix (comment/CRLF strip) — **verified fixed, normal case unaffected**

Same harness's `play-commented-tag.yml` fixture used the exact real-world
shape flagged in round 1:

```yaml
  tags:
    - scope-gnome  # needs a live GNOME session for gsettings/dconf
```

Result: `OK` — `valid_count=1`. The two added `sub()` calls
(`sub(/[[:space:]]*#.*$/, "")` then `sub(/\r$/, "")`) correctly strip the
trailing comment before the exact-match grep runs. I also exercised the
**normal, uncommented case** in the same run (`play-tagged-general.yml`,
plain `- scope-general`) and it still resolves `OK` — the new `sub()` calls
are no-ops on a line with nothing to strip, so the fix doesn't regress the
common path. `play-mixed-tags.yml` additionally exercises "a legitimately
unusual-but-valid tag" (a valid scope tag coexisting with an unrelated
non-scope tag, `packages`) and that also resolves `OK`, confirming the strip
logic doesn't interact badly with multi-item tag blocks either.

No finding.

---

## 4. SHOULD-FIX 6.1 fix (container-watch 8-task diff) — **verified fixed; the 8th-task catch is real; all 8 correct; no 9th**

Read the real `playbooks/imports/optional/common/play-container-watch.yml`
in full (184 lines) and traced every task's `register`/`when` chain by hand
against §3.4's diff:

- **Genuinely 8, not 7 or 9.** The file has 16 tasks total. Lines 88, 96,
  142, 151, 161, 166, **175**, 180 are the GNOME-shaped ones — every task
  whose module is `gnome-extensions`-flavoured, plus the two `debug:` tasks
  at the end of that chain. The other 8 (helper-library deploy, CLI wrapper,
  systemd `--user` timer deploy/probe/enable) are all general — none of them
  touch `gnome-extensions`, D-Bus, or GNOME Shell in any way; the systemd
  `--user`/`XDG_RUNTIME_DIR` pattern at lines 110–137 is the same
  headless-safe user-session pattern already verified general in round 1's
  `play-podman.yml` review (works via `loginctl` linger, not GNOME-specific).
  I checked every task's body for a missed GNOME dependency and found none —
  the 8 in the diff are all 8, and only 8, real ones.
- **The register/`when` hazard is real, confirmed by direct trace, not just
  plausible.** Line 166 (`Enable container-watch extension`) is the *only*
  task that `register: enable_result`. Both line 175 (`when: enable_result.rc == 0`) and line 180 (`when: enable_result.rc != 0`) consume it. Round 1's
  audit caught 180 but genuinely missed 175 — confirmed by re-checking my own
  round-1 enumeration against the real file line numbers. Had 175 shipped
  untagged while 166 is skipped on a server run (`--skip-tags scope-gnome`),
  `enable_result` would never be registered on that host and `when: enable_result.rc == 0` would error on an undefined variable — this repo's
  `ansible.cfg` comment confirms undefined-variable references are always
  fatal (`# Undefined vars now always cause errors by default`), so this
  isn't a benign skip, it's a real would-have-shipped bug. The general
  principle §3.4 draws from it (check for register/when chains before
  applying a task-tag override, or prefer a file-split for chains like this)
  is sound engineering advice, not overstated.
- **Field-for-field diff check**: compared each of the 8 quoted task bodies
  in §3.4 against the real file — `become`/`become_user`, `register`,
  `changed_when` (including the exact `"'is now enabled' in enable_result.stderr or enable_result.rc == 0"` expression), `when`, and
  `failed_when` with its `FAIL-FAST-OK` annotations all match verbatim, with
  only the appended `tags:\n        - scope-gnome` block (8-space indent,
  consistent with this repo's existing task-level tag convention, e.g.
  `play-python.yml`'s `pyenv` tags) added. No accidental alteration to task
  logic anywhere in the diff.

No finding — this is a correct, complete, well-verified fix, and it's a
genuinely better catch than round 1's own enumeration.

---

## 5. NITPICK fixes 6.2 / 6.3 / 6.4 — **all confirmed present and correct**

- **6.2**: §5 now reads "insert...immediately before line 17, the existing
  `## Core Playbooks (Automatically Run)` heading (i.e. directly after the
  `## Quick Navigation` section that precedes it)" — both halves of the
  sentence now point the same direction (before line 17 = after Quick
  Navigation). Self-contradiction resolved.
- **6.3**: §5 gained a new bullet ("`docs/playbooks.md`'s existing per-play
  sections") instructing updates to the three affected per-play write-ups,
  including specifically calling out the `play-prevent-ssh-suspend.yml` bug
  fix. §7 step 7 was also updated to reference it. Confirmed present in both
  places.
- **6.4**: not code-changed (as the revision log says), but §4 now carries an
  explicit paragraph explaining *why* `TOTAL` isn't extended, citing Check
  3's identical precedent. I independently re-confirmed against the original
  `qa-ansible.bash` that Check 3 (self-ref vars) indeed doesn't extend
  `TOTAL` either — the precedent claim is accurate, not invented after the
  fact to excuse an oversight.

No findings.

---

## 6. Regression check — **no new problems found**

Specifically looked for: a new `set -e` hazard, a broken awk branch, an
internal contradiction with an untouched section, or a checklist step now in
conflict with another.

- The new `hosts_block_count=$(grep -cE ... || true)` line is itself
  guarded the same way as the fixed `valid_count`/`scope_like_count` lines —
  consistent, no new hazard. (It's arguably unreachable in practice — a file
  only reaches this line after already passing the `-q` check on the same
  pattern, so `grep -cE` here is guaranteed ≥1 match and would never
  exit 1 — but the defensive guard is harmless and consistent with the
  file's now-established idiom.)
- Re-read §1.1's core-31 table and §3.1–§3.3 in this pass — content is
  byte-identical to round 1 where the revision log claims no change,
  corroborating that claim rather than taking it on faith.
- §7's checklist step 4 and step 6 were both updated together (the
  `play-virtualbox-windows.yml` split requirement appears in step 4's body
  *and* step 6 now explicitly says "once `play-virtualbox-windows.yml` has
  been split per step 4") — no stale cross-reference, the two steps agree.
- §3.4's "file-split is out of scope for Phase 3" framing and §7 step 4's
  "apply the exact 8-task interim diff... do not perform the file-split in
  this pass" are consistent with each other, same as round 1 — this was
  already checked, not re-flagged.

No regressions introduced by the round-2 edits.

---

## Summary

**Zero remaining findings.** Every item raised in `AUDIT-round-1.md` — the
one BLOCKER and all four SHOULD-FIX items — is fixed, and I verified each
fix by executing the actual revised code (not just re-reading it) against
either constructed fixtures covering every branch or, where possible, the
real files in this repo (`play-virtualbox-windows.yml`, `play-podman.yml`,
the full `play-container-watch.yml`). The one substantive new thing this
round surfaced — the 8th container-watch task and the register/`when` hazard
behind it — is real, correctly diagnosed, and correctly fixed; I traced it
independently against the real file rather than taking the revision log's
word for it. The three NITPICKs are also resolved. I did not manufacture
additional findings to pad this round — I looked for a regression
specifically and didn't find one.

**Verdict for the team lead**: no remaining blockers. This is now
**implementable as-is** — Phase 3 can proceed directly from this
`PROPOSAL.md` without a round 3.
