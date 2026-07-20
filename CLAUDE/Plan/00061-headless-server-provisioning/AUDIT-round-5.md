# Audit — Round 5 (Fable, adversarial auditor)

**Scope**: only the round-5 self-guard-mechanism deltas (gate moves from
`playbook-main.yml`'s import-site `when:` into a 2-task guard inside each
`gnome`/`server` play, making every play standalone-runnable). §1
classification and §2 `group_vars` detection are reused verbatim from round
3/4 (already converged in `AUDIT-round-3.md`) and are only spot-checked here
for reuse integrity, not re-litigated.

**Method**: same discipline as every prior round — verify against real
files, execute rather than reason. This round I built an independent
extraction harness (not a copy of Sonnet's testing) reproducing the **exact**
awk/bash from §5.2 byte-for-byte, plus a standalone-run fixture set using
`ansible-core 2.19.11` to re-derive all 6 scope×profile cases and the typo
case myself rather than trusting the revision log's table.

**Headline result**: this round is clean. **Zero blockers.** One real,
verified SHOULD-FIX (a narrower version of the same comment-stripping
asymmetry class I've now found in rounds 1 and 3, this time in the guard
extractor rather than the scope extractor). Everything else — the canonical
guard's own round-trip, guard-placement edge cases, all 6 runtime cases plus
the typo fix, the combined Check 4's `set -e` safety across eleven fixture
branches, and reuse integrity — checked out under direct execution.

---

## 1. The uniform Check 4 (§5.2) round-trip against the real canonical guard — **canonical guard passes its own gate; 1 SHOULD-FIX found in a related edge case**

Built the exact §5.2 awk/bash extractor (not simplified) and ran it against
the exact canonical 2-task guard from §3.1, dropped into a realistic
gnome-play fixture:

```
scope_val=[gnome] scope_count=[1]
t1_name=[Scope guard — assert provisioning_profile is recognised]
t2_name=[Scope guard — end play if provisioning_profile does not match declared scope]
t2_meta=[end_play]
t2_when=[(scope == 'gnome' and provisioning_profile == 'server') or (scope == 'server' and provisioning_profile != 'server')]
GUARD_OK=1
```

**The canonical guard passes its own gate, byte-for-byte, on the first try.**
This was the single most important thing to verify this round and it holds.

I then pressure-tested the specific asymmetry the brief flagged — "does the
awk strip a leading `when: ` but NOT a trailing comment/space":

### SHOULD-FIX — `TASK2_META`/`TASK2_WHEN` extraction doesn't strip trailing comments (or CRLF), unlike `SCOPE` extraction

The `SCOPE` extraction rule explicitly does both:

```awk
sub(/^    scope:[[:space:]]*/, "", val)
sub(/[[:space:]]*#.*$/, "", val)
sub(/\r$/, "", val)
```

The `TASK2_META`/`TASK2_WHEN` rules only strip the leading key prefix — no
comment-strip, no CRLF-strip. I confirmed this is a live gap, not
theoretical: took the exact canonical guard and added a trailing explanatory
comment to its `when:` line (a natural thing to do, since this repo's own
style explicitly encourages "comments explain WHY not what" — someone might
reasonably annotate the boolean's meaning):

```yaml
      when: (scope == 'gnome' and provisioning_profile == 'server') or (scope == 'server' and provisioning_profile != 'server')  # explains the boolean
```

Result: `t2_when` now includes the trailing comment text verbatim, the
exact-string-equality check against `GUARD_END_WHEN` fails, and an
**otherwise byte-correct guard is rejected** as "missing or incorrect."

**Severity context**: lower-stakes than the equivalent round-1 finding,
because the design's own stated intent is that this guard text is copied
**verbatim, never customized** (§3.1: "byte-identical in every
`gnome`/`server` play") — there's less organic pressure to touch this line
compared to a `scope:` value line, which invites exactly this kind of
comment by convention. Still real, still worth fixing, because the failure
mode is confusing: a contributor who adds one comment to an otherwise-perfect
copy-paste gets a generic "missing or incorrect" error with no hint that a
comment is the problem.

**Verified fix** — mirror `SCOPE`'s two `sub()` calls onto both `TASK2_META`
and `TASK2_WHEN`:

```awk
in_tasks && task_count == 2 && /^      ansible\.builtin\.meta:[[:space:]]*/ {
    val = $0
    sub(/^      ansible\.builtin\.meta:[[:space:]]*/, "", val)
    sub(/[[:space:]]*#.*$/, "", val)
    print "TASK2_META|" val
    next
}
in_tasks && task_count == 2 && /^      when:[[:space:]]*/ {
    val = $0
    sub(/^      when:[[:space:]]*/, "", val)
    sub(/[[:space:]]*#.*$/, "", val)
    print "TASK2_WHEN|" val
    next
}
```

Re-ran both the trailing-comment fixture and the plain canonical fixture
through the fixed extractor: the commented guard now matches
`GUARD_END_WHEN` correctly, and the unmodified canonical guard still matches
— no regression from adding the strip.

---

## 2. Guard placement + extraction edge cases (§5.2) — **robust; only the position requirement itself is (correctly) strict**

Tested every case the brief asked for, using the real extractor:

- **Guard at tasks 3–4 instead of 1–2**: correctly fails. `task_count > 2`
  stops the extractor from ever looking past task 2, so a guard placed later
  is invisible to the check and the play is reported as "missing or
  incorrect guard" — exactly the required behaviour, confirmed by execution.
- **`when:` before `meta:` in task 2 (reordered)**: still extracts correctly
  and still matches. The two extraction rules are independent line-pattern
  matches within the `task_count == 2` window, not a sequential state
  machine — order between sibling task properties genuinely doesn't matter
  to the parser. This is a permissive-but-correct property (Ansible itself
  doesn't care about key order either), not a bug.
- **Blank line or comment between the two guard tasks**: still extracts
  correctly. Confirmed with a `# comment` line inserted between task 1 and
  task 2 — no state corruption, both tasks still found.
- **Blank line or comment between `- name:` and its own `meta:`/`when:`
  inside task 2**: still extracts correctly, same reason — a line that
  doesn't match any of the specific patterns is simply skipped by awk's
  default no-match behaviour; `in_tasks`/`task_count` only reset on a
  genuine top-level dedent (`/^  [^ ]/`), never on arbitrary interstitial
  content. **This is a materially more robust design than round 3/4's Check
  4a**, which had a catch-all flush rule that *did* fire on any
  non-matching line and silently dropped state (the round-3 blocker I
  found). Round 5's extractor has no equivalent catch-all-that-mutates-state
  — worth calling out as a genuine improvement, not just parity.
- **`scope:` as the last of several `vars:` keys** (mirroring
  `play-container-watch.yml`'s real shape): correctly found regardless of
  position among sibling vars.
- **No `vars:` block at all**: `scope_count` stays `0`, correctly reported
  as "missing vars.scope," confirmed **no crash** (`exit 0` on the
  extraction step, and the full combined script — see §4 below — correctly
  emits the error and continues).

No finding beyond the trailing-comment issue in §1.

---

## 3. Two-task guard runtime behaviour — **all 6 cases + typo case re-derived independently, all correct**

Built my own standalone fixture set (three separate play files, one per
scope, not copying Sonnet's) and ran each one **standalone**
(`ansible-playbook playbooks/play-X.yml`, never through a `main.yml`) against
both profiles via `-e`:

| scope     | profile   | Guard fires? | Real task ran? | Matches table? |
| --------- | --------- | ------------ | -------------- | -------------- |
| `general` | `desktop` | No           | **Yes**        | ✓              |
| `general` | `server`  | No           | **Yes**        | ✓              |
| `gnome`   | `desktop` | No           | **Yes**        | ✓              |
| `gnome`   | `server`  | **Yes**      | No             | ✓              |
| `server`  | `desktop` | **Yes**      | No             | ✓              |
| `server`  | `server`  | No           | **Yes**        | ✓              |

All 6 rows matched §3.1's table exactly, confirmed via `PLAY RECAP` output
and a distinctive marker string in each real task, standalone, with no batch
context at all.

**Typo case**: `ansible-playbook playbooks/play-gnome.yml -e provisioning_profile=srever`, standalone — hard failure, `exit 2`,
`[ERROR]: Task failed: Action failed: provisioning_profile=srever is not recognised.`, real task never reached. Confirms §3.4's central claim: the
two-task guard's own `assert` closes the standalone-typo gap that a
single-task guard (and `play-AA`'s batch-only assert) cannot.

**`--syntax-check`**: ran against the guard-carrying play directly — `exit 0`, clean parse, no 2.19 splitter complaint on the parenthesised
`and`/`or` boolean.

No finding. This section is exactly as strong as claimed.

---

## 4. `set -e` safety of the combined Check 4 — **zero aborts across eleven fixture branches, every verdict correct**

Built the **exact** §5.2 script (not a stand-in) and ran it against a
fixture tree covering every branch requested: clean general (no guard),
clean gnome (correct guard), clean server (correct guard), missing scope,
invalid scope, multiple scope, gnome missing its guard entirely, general
**with** an unnecessary guard, a multi-play file, an archived play, a
no-`vars:`-at-all play, and `playbook-main.yml` itself (no `- hosts:` line).

```
  ERROR (scope): .../play-multi-play-file.yml — multi-play file (2 plays)
  OK (general, no guard): .../play-clean-general.yml
  OK (gnome, guard present): .../play-clean-gnome.yml
  OK (server, guard present): .../play-clean-server.yml
  ERROR (scope): .../play-missing-scope.yml — missing vars.scope
  ERROR (scope): .../play-invalid-scope.yml — invalid vars.scope value: desktop-thingy
  ERROR (scope): .../play-multi-scope.yml — multiple vars.scope entries declared
  ERROR (scope): .../play-gnome-no-guard.yml — scope=gnome requires the 2-task canonical guard; missing or incorrect
  ERROR (scope): .../play-general-with-guard.yml — unnecessary scope guard on a general-scope play
  ERROR (scope): .../play-no-vars.yml — missing vars.scope
---
TOTAL ERRORS: 7
SCRIPT COMPLETED WITHOUT ABORT
```

Every one of the 11 fixtures landed in exactly the right bucket: 3 clean
plays passed, 7 genuinely-broken plays flagged with the correct specific
error, the archived play and `playbook-main.yml` were both silently and
correctly exempted (neither appears anywhere in the output), and the script
completed with **zero aborts** in every case, including the two hazards that
specifically broke earlier rounds (a play with no valid scope at all, and a
file with no `vars:` block whatsoever).

**Confirmed the "zero `grep -c` in the main loop" claim precisely**: read
the per-play extraction loop end-to-end — it is 100% `awk` state machine +
`while IFS='|' read` counting, no `grep -c` anywhere in it. The *only*
`grep -c` in the entire check is `hosts_block_count`, and it carries the
`|| true` guard forward from round 1's fix, confirmed present and confirmed
exercised without incident by the multi-play-file fixture above.

This also directly confirms checklist step 9's claim ("pass once every
playbook... carries a valid `vars.scope` and, where required, the exact
2-task guard") is **true for this round's actual script** — unlike round
3/4's equivalent claim, which I found to be false due to the core-play
exclusion bug. Round 5's unification of core and optional plays under one
check structurally removes the class of bug that caused that failure — this
isn't a patch on top of the old design, it's a redesign that eliminates the
bug's precondition.

No finding beyond §1.

---

## 5. `playbook-main.yml` handling (§4) — **confirmed correct against the real, currently-unmodified file**

Checked the real `playbooks/playbook-main.yml` on disk: it is still in its
original, pre-Plan-00061 state (no round 3/4 `when:` lines were ever
actually applied to the real repo — all prior rounds were proposal/audit
work only). Diffed §4's "reverted" target state against the real file:
**identical**, line for line, including both ordering-rationale comment
blocks (LXC/Docker, claude-yolo/claude-code) at their original positions.
This confirms §4's diff target is correct regardless of whether it's reached
by literally reverting round 3/4's changes or, as is actually the case here,
by never having applied them.

Also confirmed directly: `grep -cE '^[[:space:]]*-[[:space:]]+hosts:' playbooks/playbook-main.yml` returns 0 matches — the file genuinely has no
`- hosts:` line (it's import-only), so Check 4's playbook-discovery guard
(`grep -qE ... || continue`) naturally and correctly skips it with no
special-case code needed, exactly as §4/§5.2's comments claim.

No finding.

---

## 6. Regression / reuse integrity — **confirmed, no contradictions found**

- §1.1's core-31 table (21 general / 10 gnome / 0 server) and §1.2/§1.3
  read as substantively unchanged from what `AUDIT-round-3.md` verified —
  same tallies, same three core mixed-play discoveries, same
  `play-container-watch.yml` 8-task register/`when:` hazard, same
  `play-virtualbox-windows.yml` two-play finding.
- §2.1's `group_vars/desktop.yml` file content is byte-identical to round
  3/4's (diffed directly against my round-3 audit's quoted content). §2.3's
  `play-AA` assert content is also byte-identical; the section gained a new
  explanatory paragraph on *why* it's still needed alongside §3's guard
  (batch-run earliness vs. standalone-run coverage are different
  properties) — this is a legitimate clarification, not a contradiction,
  and I checked it doesn't conflict with §3.4's own reasoning.
- §6.1–§6.4 (the three core mixed-play task-level `when:` edits plus
  container-watch's 8-task diff) are byte-identical to what I verified
  against the real files in `AUDIT-round-3.md` — same task bodies, same
  `when: provisioning_profile != 'server'` gates, same list-form ordering
  with the profile gate first for the four `register`-dependent tasks.
- Searched the whole document for stale `4a`/`4b` or import-site-`when:`
  references that might contradict the new design: every hit is either (a)
  inside the round-3/4 historical revision-log section (correctly preserved
  as a record, not live spec) or (b) an explicit, correctly-framed
  backward-reference explaining what round 5 replaces ("replace round 3/4's
  split 4a/4b Check 4 with this round's single uniform per-play loop"). No
  place in the document still treats the import-site `when:` or the 4a/4b
  split as the current design.
- Checklist step 3 ("revert `playbook-main.yml`... or confirm the file was
  never touched if starting fresh from round 5") correctly anticipates both
  possible starting states — and, per §5 above, the real repo is in the
  "never touched" state, so this step is currently a no-op confirmation,
  accurately anticipated by the checklist's own wording.

No finding.

---

## Summary

- **0 BLOCKERs.**
- **1 SHOULD-FIX**: `TASK2_META`/`TASK2_WHEN` extraction doesn't strip a
  trailing comment or CRLF, unlike `SCOPE` extraction, which does both —
  confirmed via execution that a canonical, otherwise byte-correct guard
  with an added trailing comment on its `when:` line is falsely rejected.
  Lower stakes than the equivalent round-1 finding (this guard text is
  meant to be copied verbatim, not customized, so there's less organic
  pressure to trigger it) but real and worth the two-line fix (mirror
  `SCOPE`'s two `sub()` calls onto both `TASK2_META` and `TASK2_WHEN`),
  verified sufficient with no regression on the canonical case.
- Everything else checked out under direct execution: the canonical guard
  passes its own gate byte-for-byte on the first try; every placement/
  extraction edge case the brief named (wrong task position, reordered
  `meta:`/`when:`, interstitial blank lines and comments, `scope:` not
  first among vars, no `vars:` block) behaves correctly, and the design is
  demonstrably **more robust** than round 3/4's parser (no
  catch-all-that-silently-mutates-state, which was the exact shape of the
  round-3 blocker); all 6 scope×profile cases plus the typo fix were
  re-derived independently, standalone, and matched the table exactly;
  `--syntax-check` is clean; the combined Check 4 survived eleven fixture
  branches under `set -euo pipefail` with zero aborts and every verdict
  correct, including both hazards that broke earlier rounds; the
  `playbook-main.yml` revert target matches the real, currently-unmodified
  file exactly; and reuse integrity against round 3/4 holds everywhere I
  checked, with no stale reference to the superseded design anywhere live.

**Verdict for the team lead**: no blockers this round. The canonical guard
**does** pass its own gate — I verified the byte-for-byte round-trip
directly, which was the single highest-risk claim in this revision. The one
SHOULD-FIX I found (trailing-comment stripping asymmetry in the guard
extractor) is small, well-understood, and doesn't touch the happy path any
implementer will actually hit copying the guard verbatim as instructed. This
round is **implementable as-is**; I'd fold the one-line-class fix into the
same commit that implements Phase 3 rather than spinning up a round 6 just
for it, but that's a judgment call for whoever's driving implementation, not
a gate I'd hold the process open for.
